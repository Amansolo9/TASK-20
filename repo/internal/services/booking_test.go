package services

import (
	"testing"
	"time"

	"campus-portal/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransitionBooking_ValidTransitions(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	audit := NewAuditService(db)
	svc := NewBookingService(db, audit, nil)

	// Ensure venue exists
	db.FirstOrCreate(&models.Venue{Name: "Test Room", RoomType: "onsite", Capacity: 10}, "name = ?", "Test Room")
	var venue models.Venue
	db.First(&venue, "name = ?", "Test Room")

	slotStart := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	booking, err := svc.CreateBooking(1, nil, venue.ID, slotStart, 1)
	require.NoError(t, err)
	assert.Equal(t, models.BookingInitiated, booking.Status)

	// Initiated -> Confirmed
	err = svc.TransitionBooking(booking.ID, models.BookingConfirmed, 1, "confirming")
	assert.NoError(t, err)

	var updated models.Booking
	db.First(&updated, booking.ID)
	assert.Equal(t, models.BookingConfirmed, updated.Status)

	// Confirmed -> Canceled
	err = svc.TransitionBooking(booking.ID, models.BookingCanceled, 1, "canceling")
	assert.NoError(t, err)

	// Canceled -> Refunded
	err = svc.TransitionBooking(booking.ID, models.BookingRefunded, 1, "refunding")
	assert.NoError(t, err)
}

func TestTransitionBooking_InvalidTransitions(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	audit := NewAuditService(db)
	svc := NewBookingService(db, audit, nil)

	var venue models.Venue
	db.FirstOrCreate(&venue, models.Venue{Name: "Test Room", RoomType: "onsite", Capacity: 10})

	slotStart := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	booking, _ := svc.CreateBooking(1, nil, venue.ID, slotStart, 1)

	// Initiated -> Refunded (invalid)
	err := svc.TransitionBooking(booking.ID, models.BookingRefunded, 1, "skip")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid state transition")
}

func TestTransitionBooking_TwoHourCancellationRule(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	audit := NewAuditService(db)
	svc := NewBookingService(db, audit, nil)

	var venue models.Venue
	db.FirstOrCreate(&venue, models.Venue{Name: "Test Room", RoomType: "onsite", Capacity: 10})

	// Booking starts in 1 hour (< 2 hours)
	slotStart := time.Now().Add(1 * time.Hour).Truncate(time.Second)
	booking := &models.Booking{
		RequesterID: 1, VenueID: venue.ID,
		SlotStart: slotStart, SlotEnd: slotStart.Add(30 * time.Minute),
		Status: models.BookingConfirmed,
	}
	db.Create(booking)

	err := svc.TransitionBooking(booking.ID, models.BookingCanceled, 1, "too late")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot cancel less than 2 hours")

	// Booking 3 hours out — should succeed
	slotStart2 := time.Now().Add(3 * time.Hour).Truncate(time.Second)
	booking2 := &models.Booking{
		RequesterID: 1, VenueID: venue.ID,
		SlotStart: slotStart2, SlotEnd: slotStart2.Add(30 * time.Minute),
		Status: models.BookingConfirmed,
	}
	db.Create(booking2)

	err = svc.TransitionBooking(booking2.ID, models.BookingCanceled, 1, "ok")
	assert.NoError(t, err)
}

func TestCheckConflicts_RequesterConflict(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	audit := NewAuditService(db)
	svc := NewBookingService(db, audit, nil)

	var venue models.Venue
	db.FirstOrCreate(&venue, models.Venue{Name: "Test Room", RoomType: "onsite", Capacity: 10})

	slotStart := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	svc.CreateBooking(1, nil, venue.ID, slotStart, 1)

	conflicts, err := svc.CheckConflicts(1, nil, venue.ID, slotStart)
	assert.NoError(t, err)
	assert.NotEmpty(t, conflicts)
}

func TestCheckConflicts_VirtualVenueNoOverlap(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	audit := NewAuditService(db)
	svc := NewBookingService(db, audit, nil)

	var venue models.Venue
	db.FirstOrCreate(&venue, models.Venue{Name: "Virtual Test", RoomType: "virtual", Capacity: 100})

	slotStart := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	svc.CreateBooking(100, nil, venue.ID, slotStart, 100)

	conflicts, err := svc.CheckConflicts(101, nil, venue.ID, slotStart)
	assert.NoError(t, err)
	for _, c := range conflicts {
		assert.NotContains(t, c, "Venue is already booked")
	}
}

func TestGetAvailableSlots(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	audit := NewAuditService(db)
	svc := NewBookingService(db, audit, nil)

	var venue models.Venue
	db.FirstOrCreate(&venue, models.Venue{Name: "Test Room", RoomType: "onsite", Capacity: 10})

	date := time.Now().Add(72 * time.Hour)
	slots, err := svc.GetAvailableSlots(venue.ID, date)
	assert.NoError(t, err)
	assert.Equal(t, 24, len(slots)) // 8AM-8PM = 24 half-hour slots
}

func TestBookingAuditTrail(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	audit := NewAuditService(db)
	svc := NewBookingService(db, audit, nil)

	var venue models.Venue
	db.FirstOrCreate(&venue, models.Venue{Name: "Test Room", RoomType: "onsite", Capacity: 10})

	slotStart := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	booking, _ := svc.CreateBooking(1, nil, venue.ID, slotStart, 1)
	svc.TransitionBooking(booking.ID, models.BookingConfirmed, 1, "confirmed by user")

	audits, err := svc.GetBookingAudit(booking.ID)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(audits))
	assert.Equal(t, "initiated", audits[0].NewStatus)
	assert.Equal(t, "confirmed", audits[1].NewStatus)
}

func TestMatchPartners(t *testing.T) {
	db := getTestDB(t)
	defer cleanupTestData(db)
	audit := NewAuditService(db)
	svc := NewBookingService(db, audit, nil)

	db.Create(&models.TrainerProfile{UserID: 200, SkillLevel: 5, WeightClass: 160, PrimaryStyle: "boxing"})
	db.Create(&models.TrainerProfile{UserID: 201, SkillLevel: 4, WeightClass: 155, PrimaryStyle: "boxing"})
	db.Create(&models.TrainerProfile{UserID: 202, SkillLevel: 9, WeightClass: 220, PrimaryStyle: "wrestling"})

	matches, err := svc.MatchPartners(200, 2, 20, "boxing")
	assert.NoError(t, err)
	assert.Equal(t, 1, len(matches))
	assert.Equal(t, uint(201), matches[0].UserID)
}
