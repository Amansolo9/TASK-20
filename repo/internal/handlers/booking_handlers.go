package handlers

import (
	"net/http"
	"strconv"
	"time"

	"campus-portal/internal/models"
	"campus-portal/internal/services"

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

	c.HTML(http.StatusOK, "bookings.html", gin.H{
		"title":    "Training Sessions",
		"user":     user,
		"bookings": bookings,
		"venues":   venues,
		"userMap":  userMap,
		"styles":   styles,
	})
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
	allSlots := h.BookingSvc.GetAllSlots(uint(venueID), date)
	c.JSON(http.StatusOK, gin.H{"slots": allSlots})
}

func (h *BookingHandler) MatchPartners(c *gin.Context) {
	user := GetCurrentUser(c)
	skillRange, _ := strconv.Atoi(c.DefaultQuery("skill_range", "2"))
	weightRange, _ := strconv.ParseFloat(c.DefaultQuery("weight_range", "20"), 64)
	style := c.Query("style")

	matches, err := h.BookingSvc.MatchPartners(user.ID, skillRange, weightRange, style)
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

	conflicts, err := h.BookingSvc.CheckConflicts(user.ID, partnerID, uint(venueID), slotStart)
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

	_, err = h.BookingSvc.CreateBooking(user.ID, partnerID, uint(venueID), slotStart, user.ID)
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
	note := c.PostForm("note")

	// Authorization: user must own the booking or be staff/admin
	var booking models.Booking
	if err := h.DB.First(&booking, uint(bookingID)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "booking not found"})
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

	// Verify the user owns this booking or is staff/admin
	var booking models.Booking
	if err := h.DB.First(&booking, uint(bookingID)).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "booking not found"})
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
	bookings, _ := h.BookingSvc.GetAllBookings()
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

	c.HTML(http.StatusOK, "bookings_admin.html", gin.H{
		"title":    "All Training Sessions",
		"user":     user,
		"bookings": bookings,
		"venues":   venues,
		"userMap":  userMap,
	})
}
