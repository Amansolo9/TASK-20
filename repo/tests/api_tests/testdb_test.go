//go:build ignore

package services

import (
	"os"
	"testing"

	"campus-portal/internal/models"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// getTestDB returns a GORM DB connected to a real Postgres instance.
// Set TEST_DATABASE_URL env var to run DB-dependent tests.
// Falls back to the Docker Compose default if the DB is reachable.
func getTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "host=localhost port=5432 user=campus_admin password=campus_secret dbname=campus_portal sslmode=disable"
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("Skipping DB test — Postgres not available: %v", err)
	}

	sqlDB, _ := db.DB()
	if err := sqlDB.Ping(); err != nil {
		t.Skipf("Skipping DB test — cannot ping Postgres: %v", err)
	}

	// Auto-migrate test tables
	db.AutoMigrate(
		&models.Organization{},
		&models.DepartmentRecord{},
		&models.User{},
		&models.Session{},
		&models.TempAccess{},
		&models.Venue{},
		&models.Booking{},
		&models.BookingAudit{},
		&models.TrainerProfile{},
		&models.AuditLog{},
		&models.MenuCategory{},
		&models.MenuItem{},
		&models.MenuItemChoice{},
		&models.ItemSubstitute{},
		&models.SellWindow{},
		&models.HolidayBlackout{},
		&models.Promotion{},
		&models.MenuOrder{},
		&models.MenuOrderItem{},
		&models.HealthRecord{},
		&models.Vital{},
		&models.Encounter{},
		&models.WebhookEndpoint{},
		&models.WebhookDelivery{},
	)

	return db
}

// cleanupTestData removes test data created during tests.
func cleanupTestData(db *gorm.DB) {
	db.Exec("DELETE FROM booking_audits")
	db.Exec("DELETE FROM bookings")
	db.Exec("DELETE FROM trainer_profiles")
	db.Exec("DELETE FROM menu_order_items")
	db.Exec("DELETE FROM menu_orders")
	db.Exec("DELETE FROM promotions")
	db.Exec("DELETE FROM sell_windows")
	db.Exec("DELETE FROM holiday_blackouts")
	db.Exec("DELETE FROM item_substitutes")
	db.Exec("DELETE FROM menu_item_choices")
	db.Exec("DELETE FROM menu_items")
	db.Exec("DELETE FROM menu_categories")
	db.Exec("DELETE FROM audit_logs")
	db.Exec("DELETE FROM temp_accesses")
	db.Exec("DELETE FROM sessions")
	db.Exec("DELETE FROM webhook_deliveries")
	db.Exec("DELETE FROM webhook_endpoints")
}
