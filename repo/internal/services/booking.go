package services

import (
	"errors"
	"time"

	"campus-portal/internal/models"

	"gorm.io/gorm"
)

type BookingService struct {
	DB      *gorm.DB
	Audit   *AuditService
	Webhook *WebhookService
}

func NewBookingService(db *gorm.DB, audit *AuditService, webhook *WebhookService) *BookingService {
	return &BookingService{DB: db, Audit: audit, Webhook: webhook}
}

// MatchPartners finds compatible training partners based on criteria
func (s *BookingService) MatchPartners(userID uint, skillRange int, weightRange float64, style string) ([]models.TrainerProfile, error) {
	var requester models.TrainerProfile
	if err := s.DB.Where("user_id = ?", userID).First(&requester).Error; err != nil {
		return nil, errors.New("trainer profile not found")
	}

	var matches []models.TrainerProfile
	query := s.DB.Where("user_id != ?", userID)

	if skillRange > 0 {
		query = query.Where("skill_level BETWEEN ? AND ?",
			requester.SkillLevel-skillRange, requester.SkillLevel+skillRange)
	}
	if weightRange > 0 {
		query = query.Where("weight_class BETWEEN ? AND ?",
			requester.WeightClass-weightRange, requester.WeightClass+weightRange)
	}
	if style != "" {
		query = query.Where("primary_style = ?", style)
	}

	err := query.Find(&matches).Error
	return matches, err
}

// SlotInfo represents a time slot with its availability status.
type SlotInfo struct {
	Time      time.Time `json:"time"`
	Available bool      `json:"available"`
}

// GetAvailableSlots returns available 30-min slots for a venue on a given date
func (s *BookingService) GetAvailableSlots(venueID uint, date time.Time) ([]time.Time, error) {
	allSlots := s.GetAllSlots(venueID, date)
	var available []time.Time
	for _, slot := range allSlots {
		if slot.Available {
			available = append(available, slot.Time)
		}
	}
	return available, nil
}

// GetAllSlots returns all 30-min slots for a venue on a date, each marked available or booked.
// This is the single source of truth for slot generation — the frontend renders directly from this.
func (s *BookingService) GetAllSlots(venueID uint, date time.Time) []SlotInfo {
	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 8, 0, 0, 0, date.Location())
	endOfDay := time.Date(date.Year(), date.Month(), date.Day(), 20, 0, 0, 0, date.Location())

	var bookings []models.Booking
	s.DB.Where("venue_id = ? AND slot_start >= ? AND slot_start < ? AND status != ?",
		venueID, startOfDay, endOfDay, models.BookingCanceled).Find(&bookings)

	bookedTimes := make(map[int64]bool)
	for _, b := range bookings {
		bookedTimes[b.SlotStart.Unix()] = true
	}

	var slots []SlotInfo
	for t := startOfDay; t.Before(endOfDay); t = t.Add(30 * time.Minute) {
		slots = append(slots, SlotInfo{
			Time:      t,
			Available: !bookedTimes[t.Unix()],
		})
	}
	return slots
}

// CheckConflicts detects double-bookings or venue overlaps
func (s *BookingService) CheckConflicts(requesterID uint, partnerID *uint, venueID uint, slotStart time.Time) ([]string, error) {
	slotEnd := slotStart.Add(30 * time.Minute)
	var conflicts []string

	// Check requester conflicts
	var count int64
	s.DB.Model(&models.Booking{}).Where(
		"requester_id = ? AND slot_start < ? AND slot_end > ? AND status NOT IN ?",
		requesterID, slotEnd, slotStart, []string{string(models.BookingCanceled), string(models.BookingRefunded)},
	).Count(&count)
	if count > 0 {
		conflicts = append(conflicts, "You already have a booking during this time slot")
	}

	// Check partner conflicts
	if partnerID != nil {
		s.DB.Model(&models.Booking{}).Where(
			"(requester_id = ? OR partner_id = ?) AND slot_start < ? AND slot_end > ? AND status NOT IN ?",
			*partnerID, *partnerID, slotEnd, slotStart, []string{string(models.BookingCanceled), string(models.BookingRefunded)},
		).Count(&count)
		if count > 0 {
			conflicts = append(conflicts, "Selected partner has a conflicting booking")
		}
	}

	// Check venue overlap
	s.DB.Model(&models.Booking{}).Where(
		"venue_id = ? AND slot_start < ? AND slot_end > ? AND status NOT IN ?",
		venueID, slotEnd, slotStart, []string{string(models.BookingCanceled), string(models.BookingRefunded)},
	).Count(&count)

	var venue models.Venue
	s.DB.First(&venue, venueID)
	if venue.RoomType != "virtual" && count > 0 {
		conflicts = append(conflicts, "Venue is already booked for this time slot")
	}

	return conflicts, nil
}

// CreateBooking creates a new booking after validation
func (s *BookingService) CreateBooking(requesterID uint, partnerID *uint, venueID uint, slotStart time.Time, changedBy uint) (*models.Booking, error) {
	conflicts, err := s.CheckConflicts(requesterID, partnerID, venueID, slotStart)
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		return nil, errors.New("booking conflicts: " + conflicts[0])
	}

	booking := &models.Booking{
		RequesterID: requesterID,
		PartnerID:   partnerID,
		VenueID:     venueID,
		SlotStart:   slotStart,
		SlotEnd:     slotStart.Add(30 * time.Minute),
		Status:      models.BookingInitiated,
	}

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(booking).Error; err != nil {
			return err
		}
		return tx.Create(&models.BookingAudit{
			BookingID: booking.ID,
			ChangedBy: changedBy,
			OldStatus: "",
			NewStatus: string(models.BookingInitiated),
			Note:      "Booking initiated",
			Timestamp: time.Now(),
		}).Error
	})

	if err == nil && s.Webhook != nil {
		s.Webhook.Dispatch("booking.created", map[string]interface{}{
			"booking_id": booking.ID, "requester_id": requesterID, "venue_id": venueID,
			"slot_start": slotStart, "status": "initiated",
		})
	}

	return booking, err
}

// TransitionBooking moves a booking to a new state
func (s *BookingService) TransitionBooking(bookingID uint, newStatus models.BookingStatus, changedBy uint, note string) error {
	var booking models.Booking
	if err := s.DB.First(&booking, bookingID).Error; err != nil {
		return err
	}

	// Validate state transition
	valid := map[models.BookingStatus][]models.BookingStatus{
		models.BookingInitiated: {models.BookingConfirmed, models.BookingCanceled},
		models.BookingConfirmed: {models.BookingCanceled},
		models.BookingCanceled:  {models.BookingRefunded},
	}

	allowed, ok := valid[booking.Status]
	if !ok {
		return errors.New("booking is in a terminal state")
	}

	isAllowed := false
	for _, s := range allowed {
		if s == newStatus {
			isAllowed = true
			break
		}
	}
	if !isAllowed {
		return errors.New("invalid state transition")
	}

	// Enforce 2-hour cancellation rule
	if newStatus == models.BookingCanceled {
		if time.Until(booking.SlotStart) < 2*time.Hour {
			return errors.New("cannot cancel less than 2 hours before the session")
		}
	}

	oldStatus := booking.Status
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		booking.Status = newStatus
		if err := tx.Save(&booking).Error; err != nil {
			return err
		}
		return tx.Create(&models.BookingAudit{
			BookingID: bookingID,
			ChangedBy: changedBy,
			OldStatus: string(oldStatus),
			NewStatus: string(newStatus),
			Note:      note,
			Timestamp: time.Now(),
		}).Error
	})

	if err == nil && s.Webhook != nil {
		eventType := "booking." + string(newStatus)
		s.Webhook.Dispatch(eventType, map[string]interface{}{
			"booking_id": bookingID, "old_status": string(oldStatus),
			"new_status": string(newStatus), "changed_by": changedBy, "note": note,
		})
	}

	return err
}

// GetBookings returns bookings for a user
func (s *BookingService) GetBookings(userID uint) ([]models.Booking, error) {
	var bookings []models.Booking
	err := s.DB.Where("requester_id = ? OR partner_id = ?", userID, userID).
		Order("slot_start DESC").Find(&bookings).Error
	return bookings, err
}

func (s *BookingService) GetBookingAudit(bookingID uint) ([]models.BookingAudit, error) {
	var audits []models.BookingAudit
	err := s.DB.Where("booking_id = ?", bookingID).Order("timestamp ASC").Find(&audits).Error
	return audits, err
}

func (s *BookingService) GetVenues() ([]models.Venue, error) {
	var venues []models.Venue
	err := s.DB.Find(&venues).Error
	return venues, err
}

func (s *BookingService) GetAllBookings() ([]models.Booking, error) {
	var bookings []models.Booking
	err := s.DB.Order("slot_start DESC").Find(&bookings).Error
	return bookings, err
}
