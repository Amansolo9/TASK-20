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

	// Temp access revert ticker
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			authSvc.RevertExpiredAccess()
		}
	}()

	// Materialized view refresh + slow query cleanup ticker
	go func() {
		ticker := time.NewTicker(cfg.MVRefreshInterval)
		defer ticker.Stop()
		for range ticker.C {
			reportingSvc.RefreshMaterializedViews()
			reportingSvc.CleanupSlowQueryLogs(30) // Keep 30 days
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

	// Authenticated routes
	authed := r.Group("/")
	authed.Use(middleware.AuthRequired(authSvc))
	authed.Use(middleware.DataScope())
	authed.Use(middleware.CSRFProtect())
	{
		authed.GET("/", func(c *gin.Context) { c.Redirect(302, "/dashboard") })
		authed.GET("/logout", authHandler.Logout)

		// Dashboard - all roles
		authed.GET("/dashboard", healthHandler.DashboardPage)
		authed.POST("/health/update", healthHandler.UpdateHealthRecord)
		authed.POST("/health/upload", healthHandler.UploadAttachment)
		authed.GET("/health/download/:id", healthHandler.DownloadAttachment)
		authed.GET("/health/history", healthHandler.RecordHistory)

		// Clinician routes
		clinician := authed.Group("/clinician")
		clinician.Use(middleware.RequireRole(models.RoleClinician, models.RoleAdmin))
		{
			clinician.GET("", healthHandler.ClinicianPage)
			clinician.POST("/encounter", healthHandler.CreateEncounter)
			clinician.POST("/vitals", healthHandler.RecordVitals)
		}

		// Booking routes - students, faculty, staff, admin
		authed.GET("/bookings", bookingHandler.BookingPage)
		authed.POST("/bookings", bookingHandler.CreateBooking)
		authed.POST("/bookings/:id/transition", bookingHandler.TransitionBooking)
		authed.GET("/bookings/:id/audit", bookingHandler.BookingAudit)
		authed.GET("/api/slots", bookingHandler.GetSlots)
		authed.GET("/api/match-partners", bookingHandler.MatchPartners)
		authed.GET("/api/check-conflicts", bookingHandler.CheckConflicts)

		// Menu routes - all roles can view
		authed.GET("/menu", menuHandler.MenuPage)
		authed.POST("/menu/order", menuHandler.CreateOrder)
		authed.GET("/api/price", menuHandler.CalculatePrice)

		// Menu management - staff and admin only
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
			admin.GET("/performance", adminHandler.PerformancePage)
			admin.POST("/refresh-views", adminHandler.RefreshViews)
			admin.GET("/webhooks", adminHandler.WebhooksPage)
			admin.POST("/webhooks", adminHandler.RegisterWebhook)
			admin.GET("/bookings", bookingHandler.AllBookingsPage)
		}

	}

	// Internal API routes (HMAC-signed) — outside authed group
	internal := r.Group("/api/internal")
	internal.Use(middleware.HMACAuth(cfg.HMACSecret))
	{
		internal.GET("/clinic-utilization", adminHandler.APIClinicUtilization)
		internal.GET("/booking-fill-rates", adminHandler.APIBookingFillRates)
		internal.GET("/menu-sell-through", adminHandler.APIMenuSellThrough)

		// Webhook receiver — now HMAC-protected
		internal.POST("/webhooks/receive", func(c *gin.Context) {
			var payload map[string]interface{}
			if err := c.BindJSON(&payload); err != nil {
				c.JSON(400, gin.H{"error": "invalid payload"})
				return
			}
			// Log event type only — avoid logging potentially sensitive payload data
			eventType, _ := payload["event_type"].(string)
			log.Printf("Webhook received: event_type=%s", eventType)
			c.JSON(200, gin.H{"status": "received"})
		})
	}

	log.Printf("Campus Portal starting on :%s", cfg.ServerPort)
	if err := r.Run(":" + cfg.ServerPort); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
