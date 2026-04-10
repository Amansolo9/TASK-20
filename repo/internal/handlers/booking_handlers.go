package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"campus-portal/internal/models"
	"campus-portal/internal/services"
	"campus-portal/internal/views"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type BookingHandler struct {
	BookingSvc *services.BookingService
	DB         *gorm.DB
}

func NewBookingHandler(bookingSvc *services.BookingService, db *gorm.DB) *BookingHandler {
	return &BookingHandler{BookingSvc: bookingSvc, DB: db}
}

func (h *BookingHandler) BookingPage(c *gin.Context) {
	user := GetCurrentUser(c)
	bookings, _ := h.BookingSvc.GetBookings(user.ID)
	venues, _ := h.BookingSvc.GetVenues()

	// Get only referenced user names (not the entire user table)
	userIDs := make(map[uint]bool)
	for _, b := range bookings {
		userIDs[b.RequesterID] = true
		if b.PartnerID != nil {
			userIDs[*b.PartnerID] = true
		}
	}
	userMap := make(map[uint]string)
	if len(userIDs) > 0 {
		ids := make([]uint, 0, len(userIDs))
		for id := range userIDs {
			ids = append(ids, id)
		}
		var users []models.User
		h.DB.Where("id IN ?", ids).Find(&users)
		for _, u := range users {
			userMap[u.ID] = u.FullName
		}
	}

	// Fetch distinct training styles from DB for the filter dropdown
	var styles []string
	h.DB.Model(&models.TrainerProfile{}).Distinct("primary_style").Where("primary_style != ''").Pluck("primary_style", &styles)

	bd := views.BookingsData{
		User:    &views.UserInfo{FullName: user.FullName, Role: string(user.Role)},
		UserMap: userMap,
		Styles:  styles,
		IsAdmin: false,
	}
	for _, v := range venues {
		bd.Venues = append(bd.Venues, views.VenueOption{ID: v.ID, Name: v.Name, RoomType: v.RoomType, Capacity: v.Capacity})
	}
	for _, b := range bookings {
		row := views.BookingRow{
			ID:          b.ID,
			SlotStart:   b.SlotStart.Format("01/02/2006 03:04 PM"),
			SlotEnd:     b.SlotEnd.Format("03:04 PM"),
			VenueID:     b.VenueID,
			RequesterID: b.RequesterID,
			PartnerID:   b.PartnerID,
			Status:      string(b.Status),
		}
		if b.PartnerID != nil {
			row.PartnerName = userMap[*b.PartnerID]
		}
		bd.Bookings = append(bd.Bookings, row)
	}
	views.Render(c, http.StatusOK, views.BookingsPage(bd))
}

func (h *BookingHandler) GetSlots(c *gin.Context) {
	venueID, _ := strconv.ParseUint(c.Query("venue_id"), 10, 64)
	dateStr := c.Query("date")

	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format"})
		return
	}

	// Return all slots with availability status — frontend renders entirely from this
	orgID, _ := c.Get("orgID")
	allSlots := h.BookingSvc.GetAllSlots(uint(venueID), date, orgID.(uint))
	c.JSON(http.StatusOK, gin.H{"slots": allSlots})
}

func (h *BookingHandler) MatchPartners(c *gin.Context) {
	user := GetCurrentUser(c)
	skillRange, _ := strconv.Atoi(c.DefaultQuery("skill_range", "2"))
	weightRange, _ := strconv.ParseFloat(c.DefaultQuery("weight_range", "20"), 64)
	style := c.Query("style")

	orgID, _ := c.Get("orgID")
	matches, err := h.BookingSvc.MatchPartners(user.ID, skillRange, weightRange, style, orgID.(uint))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Enrich with user names
	type PartnerMatch struct {
		models.TrainerProfile
		FullName string `json:"full_name"`
	}
	var enriched []PartnerMatch
	for _, m := range matches {
		var u models.User
		h.DB.First(&u, m.UserID)
		enriched = append(enriched, PartnerMatch{TrainerProfile: m, FullName: u.FullName})
	}

	c.JSON(http.StatusOK, gin.H{"matches": enriched})
}

func (h *BookingHandler) CheckConflicts(c *gin.Context) {
	user := GetCurrentUser(c)
	venueID, _ := strconv.ParseUint(c.Query("venue_id"), 10, 64)
	slotStr := c.Query("slot_start")

	slotStart, err := time.Parse(time.RFC3339, slotStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid slot time"})
		return
	}

	var partnerID *uint
	if pid := c.Query("partner_id"); pid != "" {
		id, _ := strconv.ParseUint(pid, 10, 64)
		uid := uint(id)
		partnerID = &uid
	}

	orgID, _ := c.Get("orgID")
	conflicts, err := h.BookingSvc.CheckConflicts(user.ID, partnerID, uint(venueID), slotStart, orgID.(uint))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"conflicts": conflicts})
}

func (h *BookingHandler) CreateBooking(c *gin.Context) {
	user := GetCurrentUser(c)
	venueID, err := strconv.ParseUint(c.PostForm("venue_id"), 10, 64)
	if err != nil || venueID == 0 {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": "invalid venue ID"})
		return
	}
	slotStr := c.PostForm("slot_start")

	slotStart, err := time.Parse(time.RFC3339, slotStr)
	if err != nil {
		slotStart, err = time.Parse("2006-01-02T15:04", slotStr)
		if err != nil {
			c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": "invalid slot time"})
			return
		}
	}

	var partnerID *uint
	if pid := c.PostForm("partner_id"); pid != "" {
		id, _ := strconv.ParseUint(pid, 10, 64)
		uid := uint(id)
		partnerID = &uid
	}

	orgID, _ := c.Get("orgID")
	_, err = h.BookingSvc.CreateBooking(user.ID, partnerID, uint(venueID), slotStart, user.ID, orgID.(uint))
	if err != nil {
		c.HTML(http.StatusBadRequest, "error.html", gin.H{"error": err.Error()})
		return
	}

	c.Redirect(http.StatusFound, "/bookings")
}

func (h *BookingHandler) TransitionBooking(c *gin.Context) {
	user := GetCurrentUser(c)
	bookingID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid booking ID"})
		return
	}
	newStatus := models.BookingStatus(c.PostForm("status"))
	note := strings.TrimSpace(c.PostForm("note"))
	if note == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a reason/note is required for booking status changes"})
		return
	}

	// Authorization: booking must belong to user's org, and user must own it or be staff/admin
	var booking models.Booking
	if err := h.DB.First(&booking, uint(bookingID)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "booking not found"})
		return
	}
	orgID, _ := c.Get("orgID")
	if booking.OrganizationID != orgID.(uint) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}
	if user.Role != models.RoleAdmin && user.Role != models.RoleStaff {
		if booking.RequesterID != user.ID && (booking.PartnerID == nil || *booking.PartnerID != user.ID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "you can only modify your own bookings"})
			return
		}
	}

	if err := h.BookingSvc.TransitionBooking(uint(bookingID), newStatus, user.ID, note); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.Redirect(http.StatusFound, "/bookings")
}

func (h *BookingHandler) BookingAudit(c *gin.Context) {
	user := GetCurrentUser(c)
	bookingID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid booking ID"})
		return
	}

	// Verify the booking belongs to the user's org, and user owns it or is staff/admin
	var booking models.Booking
	if err := h.DB.First(&booking, uint(bookingID)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "booking not found"})
		return
	}
	orgID, _ := c.Get("orgID")
	if booking.OrganizationID != orgID.(uint) {
		c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
		return
	}
	if user.Role != models.RoleAdmin && user.Role != models.RoleStaff {
		if booking.RequesterID != user.ID && (booking.PartnerID == nil || *booking.PartnerID != user.ID) {
			c.JSON(http.StatusForbidden, gin.H{"error": "access denied"})
			return
		}
	}

	audits, err := h.BookingSvc.GetBookingAudit(uint(bookingID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"audits": audits})
}

// All bookings for staff/admin
func (h *BookingHandler) AllBookingsPage(c *gin.Context) {
	user := GetCurrentUser(c)
	orgID, _ := c.Get("orgID")
	bookings, _ := h.BookingSvc.GetAllBookingsByOrg(orgID.(uint))
	venues, _ := h.BookingSvc.GetVenues()

	userIDs := make(map[uint]bool)
	for _, b := range bookings {
		userIDs[b.RequesterID] = true
		if b.PartnerID != nil {
			userIDs[*b.PartnerID] = true
		}
	}
	userMap := make(map[uint]string)
	if len(userIDs) > 0 {
		ids := make([]uint, 0, len(userIDs))
		for id := range userIDs {
			ids = append(ids, id)
		}
		var users []models.User
		h.DB.Where("id IN ?", ids).Find(&users)
		for _, u := range users {
			userMap[u.ID] = u.FullName
		}
	}

	bd := views.BookingsData{
		User:    &views.UserInfo{FullName: user.FullName, Role: string(user.Role)},
		UserMap: userMap,
		IsAdmin: true,
	}
	for _, v := range venues {
		bd.Venues = append(bd.Venues, views.VenueOption{ID: v.ID, Name: v.Name, RoomType: v.RoomType, Capacity: v.Capacity})
	}
	for _, b := range bookings {
		row := views.BookingRow{
			ID:          b.ID,
			SlotStart:   b.SlotStart.Format("01/02/2006 03:04 PM"),
			SlotEnd:     b.SlotEnd.Format("03:04 PM"),
			VenueID:     b.VenueID,
			RequesterID: b.RequesterID,
			PartnerID:   b.PartnerID,
			Status:      string(b.Status),
		}
		if b.PartnerID != nil {
			row.PartnerName = userMap[*b.PartnerID]
		}
		bd.Bookings = append(bd.Bookings, row)
	}
	views.Render(c, http.StatusOK, views.BookingsPage(bd))
}
