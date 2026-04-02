package handlers

import (
	"net/http"
	neturl "net/url"

	"campus-portal/internal/services"

	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	ReportingSvc *services.ReportingService
	WebhookSvc   *services.WebhookService
}

func NewAdminHandler(reportingSvc *services.ReportingService, webhookSvc *services.WebhookService) *AdminHandler {
	return &AdminHandler{ReportingSvc: reportingSvc, WebhookSvc: webhookSvc}
}

func (h *AdminHandler) PerformancePage(c *gin.Context) {
	user := GetCurrentUser(c)

	clinicUtil, _ := h.ReportingSvc.GetClinicUtilization()
	bookingRates, _ := h.ReportingSvc.GetBookingFillRates()
	menuSell, _ := h.ReportingSvc.GetMenuSellThrough()
	slowQueries, _ := h.ReportingSvc.GetSlowQueries(50)

	c.HTML(http.StatusOK, "admin_performance.html", gin.H{
		"title":        "Performance Dashboard",
		"user":         user,
		"clinicUtil":   clinicUtil,
		"bookingRates": bookingRates,
		"menuSell":     menuSell,
		"slowQueries":  slowQueries,
	})
}

func (h *AdminHandler) WebhooksPage(c *gin.Context) {
	user := GetCurrentUser(c)
	endpoints, _ := h.WebhookSvc.GetEndpoints()

	c.HTML(http.StatusOK, "admin_webhooks.html", gin.H{
		"title":     "Webhook Management",
		"user":      user,
		"endpoints": endpoints,
	})
}

func (h *AdminHandler) RegisterWebhook(c *gin.Context) {
	rawURL := c.PostForm("url")
	eventType := c.PostForm("event_type")
	secret := c.PostForm("secret")

	// Validate URL format
	if rawURL == "" || eventType == "" || secret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url, event_type, and secret are all required"})
		return
	}
	parsed, err := neturl.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid URL: must be http:// or https:// with a host"})
		return
	}

	h.WebhookSvc.RegisterEndpoint(rawURL, eventType, secret)
	c.Redirect(http.StatusFound, "/admin/webhooks")
}

func (h *AdminHandler) RefreshViews(c *gin.Context) {
	h.ReportingSvc.RefreshMaterializedViews()
	c.JSON(http.StatusOK, gin.H{"message": "Materialized views refreshed"})
}

// API endpoints for internal systems
func (h *AdminHandler) APIClinicUtilization(c *gin.Context) {
	data, err := h.ReportingSvc.GetClinicUtilization()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (h *AdminHandler) APIBookingFillRates(c *gin.Context) {
	data, err := h.ReportingSvc.GetBookingFillRates()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (h *AdminHandler) APIMenuSellThrough(c *gin.Context) {
	data, err := h.ReportingSvc.GetMenuSellThrough()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}
