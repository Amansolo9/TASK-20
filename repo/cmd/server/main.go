package main

import (
	"html/template"
	"log"
	"path/filepath"
	"time"

	"campus-portal/internal/auth"
	"campus-portal/internal/config"
	"campus-portal/internal/handlers"
	"campus-portal/internal/middleware"
	"campus-portal/internal/models"
	"campus-portal/internal/services"
	tmpl "campus-portal/internal/templates"

	"github.com/gin-gonic/gin"
)

// NOTE: Most page rendering has been migrated to Templ components (internal/views/*.templ).
// The html/template setup below is retained only for the menu_manage page which still
// uses legacy templates. Once menu_manage is also migrated, this can be removed entirely.

func main() {
	cfg := config.Load()

	// Initialize field encryption
	if err := services.InitEncryption(); err != nil {
		log.Fatalf("Failed to initialize encryption: %v", err)
	}

	// Database
	db := models.InitDB(cfg)
	models.AutoMigrate(db)
	models.SeedDefaults(db)

	// Services
	authSvc := auth.NewAuthService(db)
	auditSvc := services.NewAuditService(db)
	webhookSvc := services.NewWebhookService(db)
	healthSvc := services.NewHealthService(db, auditSvc, cfg.UploadDir)
	bookingSvc := services.NewBookingService(db, auditSvc, webhookSvc)
	menuSvc := services.NewMenuService(db, auditSvc)
	reportingSvc := services.NewReportingService(db, cfg.CacheTTL)

	// Handlers
	authHandler := handlers.NewAuthHandler(authSvc, auditSvc, db, cfg)
	healthHandler := handlers.NewHealthHandler(healthSvc, auditSvc)
	bookingHandler := handlers.NewBookingHandler(bookingSvc, db)
	menuHandler := handlers.NewMenuHandler(menuSvc)
	adminHandler := handlers.NewAdminHandler(reportingSvc, webhookSvc)

	// Background workers
	csvWatcher := services.NewCSVWatcher(db, cfg.WatchedDir)
	csvWatcher.Start()
	defer csvWatcher.Stop()

	// SSO sync (separate from CSV enrollment)
	if cfg.SSOSyncEnabled && cfg.SSOSourcePath != "" {
		ssoSource := &services.FileSSOSource{FilePath: cfg.SSOSourcePath}
		ssoSync := services.NewSSOSyncService(db, ssoSource, auditSvc)
		ssoSync.Start(cfg.SSOSyncInterval)
		defer ssoSync.Stop()
	}

	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			authSvc.RevertExpiredAccess()
		}
	}()

	go func() {
		ticker := time.NewTicker(cfg.MVRefreshInterval)
		defer ticker.Stop()
		for range ticker.C {
			reportingSvc.RefreshMaterializedViews()
			reportingSvc.CleanupSlowQueryLogs(30)
		}
	}()

	// Router
	r := gin.Default()
	htmlTemplates := template.Must(
		template.New("").Funcs(tmpl.FuncMap()).ParseGlob(
			filepath.Join("internal", "templates", "*.html"),
		),
	)
	r.SetHTMLTemplate(htmlTemplates)
	r.Static("/static", "./static")
	r.MaxMultipartMemory = cfg.MaxUploadMB << 20

	// Global middleware
	r.Use(middleware.RateLimit(cfg.RateLimitPerMin))
	r.Use(middleware.SlowQueryLogger(db, cfg.SlowQueryMs))

	// Public routes
	r.GET("/login", authHandler.LoginPage)
	r.POST("/login", authHandler.Login)

	// ===================== Browser (session-authenticated) routes =====================
	authed := r.Group("/")
	authed.Use(middleware.AuthRequired(authSvc))
	authed.Use(middleware.DataScope())
	authed.Use(middleware.CSRFProtect())
	{
		authed.GET("/", func(c *gin.Context) { c.Redirect(302, "/dashboard") })
		authed.GET("/logout", authHandler.Logout)

		// Token issuance (session-auth users can get an API token)
		authed.POST("/api/tokens", authHandler.IssueToken)

		// Dashboard + document access (all roles)
		authed.GET("/dashboard", healthHandler.DashboardPage)
		authed.GET("/health/download/:id", healthHandler.DownloadAttachment)
		authed.GET("/health/history", healthHandler.RecordHistory)

		// Self-upload: students/faculty can upload documents to their OWN record only.
		// The handler enforces self-only for student/faculty, scoped for clinician/admin.
		authed.POST("/health/upload", healthHandler.UploadAttachment)

		// Health record mutations — clinician/admin only (staff cannot modify PHI)
		healthMut := authed.Group("/health")
		healthMut.Use(middleware.RequireRole(models.RoleClinician, models.RoleAdmin))
		{
			healthMut.POST("/update", healthHandler.UpdateHealthRecord)
		}

		// Clinician routes
		clinician := authed.Group("/clinician")
		clinician.Use(middleware.RequireRole(models.RoleClinician, models.RoleAdmin))
		{
			clinician.GET("", healthHandler.ClinicianPage)
			clinician.POST("/encounter", healthHandler.CreateEncounter)
			clinician.POST("/vitals", healthHandler.RecordVitals)
		}

		// Booking page routes (browser)
		authed.GET("/bookings", bookingHandler.BookingPage)
		authed.POST("/bookings", bookingHandler.CreateBooking)
		authed.POST("/bookings/:id/transition", bookingHandler.TransitionBooking)
		authed.GET("/bookings/:id/audit", bookingHandler.BookingAudit)

		// Menu page routes (browser)
		authed.GET("/menu", menuHandler.MenuPage)
		authed.POST("/menu/order", menuHandler.CreateOrder)

		// Menu management — staff/admin only
		menuManage := authed.Group("/menu/manage")
		menuManage.Use(middleware.RequireRole(models.RoleStaff, models.RoleAdmin))
		{
			menuManage.GET("", menuHandler.MenuManagePage)
			menuManage.POST("/category", menuHandler.CreateCategory)
			menuManage.POST("/item", menuHandler.CreateMenuItem)
			menuManage.POST("/item/:id/sold-out", menuHandler.ToggleSoldOut)
			menuManage.POST("/item/:id/sell-windows", menuHandler.SetSellWindows)
			menuManage.POST("/item/:id/substitutes", menuHandler.SetSubstitutes)
			menuManage.POST("/item/:id/choices", menuHandler.AddChoice)
			menuManage.POST("/blackout", menuHandler.CreateBlackout)
			menuManage.POST("/blackout/:id/delete", menuHandler.DeleteBlackout)
			menuManage.POST("/promotion", menuHandler.CreatePromotion)
		}

		// Admin routes
		admin := authed.Group("/admin")
		admin.Use(middleware.RequireRole(models.RoleAdmin))
		{
			admin.GET("/users", authHandler.UsersPage)
			admin.GET("/register", authHandler.RegisterPage)
			admin.POST("/register", authHandler.Register)
			admin.POST("/users/:id/toggle", authHandler.ToggleUser)
			admin.POST("/users/:id/role", authHandler.ChangeRole)
			admin.POST("/users/:id/temp-access", authHandler.GrantTempAccess)
			admin.POST("/users/:id/reset-password", authHandler.ResetPassword)
			admin.GET("/performance", adminHandler.PerformancePage)
			admin.POST("/refresh-views", adminHandler.RefreshViews)
			admin.GET("/webhooks", adminHandler.WebhooksPage)
			admin.POST("/webhooks", adminHandler.RegisterWebhook)
			admin.GET("/bookings", bookingHandler.AllBookingsPage)
			admin.POST("/reports", adminHandler.SubmitReport)
			admin.GET("/reports/:id", adminHandler.GetReportStatus)
		}
	}

	// ===================== REST API (token + HMAC) routes =====================
	// Per prompt: "all API access uses locally issued tokens, HMAC request signing, and rate limiting"
	api := r.Group("/api")
	api.Use(middleware.APITokenRequired(authSvc))
	api.Use(middleware.HMACAuth(cfg.HMACSecret))
	api.Use(middleware.DataScope())
	{
		api.GET("/slots", bookingHandler.GetSlots)
		api.GET("/match-partners", bookingHandler.MatchPartners)
		api.GET("/check-conflicts", bookingHandler.CheckConflicts)
		api.GET("/price", menuHandler.CalculatePrice)
	}

	// ===================== Internal routes (token + HMAC + admin RBAC) =====================
	internal := r.Group("/api/internal")
	internal.Use(middleware.APITokenRequired(authSvc))
	internal.Use(middleware.HMACAuth(cfg.HMACSecret))
	internal.Use(middleware.DataScope())
	internal.Use(middleware.RequireRole(models.RoleAdmin))
	{
		internal.GET("/clinic-utilization", adminHandler.APIClinicUtilization)
		internal.GET("/booking-fill-rates", adminHandler.APIBookingFillRates)
		internal.GET("/menu-sell-through", adminHandler.APIMenuSellThrough)
		internal.POST("/webhooks/receive", func(c *gin.Context) {
			var payload map[string]interface{}
			if err := c.BindJSON(&payload); err != nil {
				c.JSON(400, gin.H{"error": "invalid payload"})
				return
			}
			eventType, _ := payload["event_type"].(string)

			// Validate against known event types
			knownEvents := make(map[string]bool)
			for _, et := range services.AllEventTypes() {
				knownEvents[et] = true
			}
			if !knownEvents[eventType] {
				log.Printf("Webhook received: unknown event_type=%s", eventType)
				c.JSON(400, gin.H{"error": "unknown event type: " + eventType})
				return
			}

			log.Printf("Webhook received: event_type=%s", eventType)
			c.JSON(200, gin.H{"status": "received", "event_type": eventType})
		})
	}

	log.Printf("Campus Portal starting on :%s", cfg.ServerPort)
	if err := r.Run(":" + cfg.ServerPort); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
