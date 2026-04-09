package handlers

import (
	"net/http"
	neturl "net/url"
	"strconv"

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

func getCallerOrgID(c *gin.Context) uint {
	orgID, _ := c.Get("orgID")
	return orgID.(uint)
}

func (h *AdminHandler) PerformancePage(c *gin.Context) {
	user := GetCurrentUser(c)
	orgID := getCallerOrgID(c)

	clinicUtil, _ := h.ReportingSvc.GetClinicUtilization(orgID)
	bookingRates, _ := h.ReportingSvc.GetBookingFillRates(orgID)
	menuSell, _ := h.ReportingSvc.GetMenuSellThrough(orgID)
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
	orgID := getCallerOrgID(c)
	endpoints, _ := h.WebhookSvc.GetEndpoints(orgID)

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

	if rawURL == "" || eventType == "" || secret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url, event_type, and secret are all required"})
		return
	}
	parsed, err := neturl.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid URL: must be http:// or https:// with a host"})
		return
	}

	orgID := getCallerOrgID(c)
	if err := h.WebhookSvc.RegisterEndpointForOrg(rawURL, eventType, secret, orgID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Redirect(http.StatusFound, "/admin/webhooks")
}

func (h *AdminHandler) RefreshViews(c *gin.Context) {
	h.ReportingSvc.RefreshMaterializedViews()
	c.JSON(http.StatusOK, gin.H{"message": "Materialized views refreshed"})
}

// Internal API endpoints — org-scoped
func (h *AdminHandler) APIClinicUtilization(c *gin.Context) {
	orgID := getCallerOrgID(c)
	data, err := h.ReportingSvc.GetClinicUtilization(orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

func (h *AdminHandler) APIBookingFillRates(c *gin.Context) {
	orgID := getCallerOrgID(c)
	data, err := h.ReportingSvc.GetBookingFillRates(orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

// Async report submission
func (h *AdminHandler) SubmitReport(c *gin.Context) {
	orgID := getCallerOrgID(c)
	user := GetCurrentUser(c)
	reportType := c.PostForm("report_type")
	if reportType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "report_type is required"})
		return
	}
	job, err := h.ReportingSvc.SubmitReportJob(orgID, reportType, user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID, "status": job.Status})
}

func (h *AdminHandler) GetReportStatus(c *gin.Context) {
	orgID := getCallerOrgID(c)
	jobID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job ID"})
		return
	}
	job, err := h.ReportingSvc.GetReportJob(uint(jobID), orgID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "report not found"})
		return
	}
	c.JSON(http.StatusOK, job)
}

func (h *AdminHandler) APIMenuSellThrough(c *gin.Context) {
	orgID := getCallerOrgID(c)
	data, err := h.ReportingSvc.GetMenuSellThrough(orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}
