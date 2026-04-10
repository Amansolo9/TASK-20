package models

import (
	"fmt"
	"log"
	"time"

	"campus-portal/internal/config"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDB(cfg *config.Config) *gorm.DB {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode,
	)

	var db *gorm.DB
	var err error

	// Retry loop — wait for Postgres to become ready (critical for Docker Compose)
	for i := 0; i < 30; i++ {
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Warn),
		})
		if err == nil {
			sqlDB, _ := db.DB()
			if pingErr := sqlDB.Ping(); pingErr == nil {
				break
			}
		}
		log.Printf("Waiting for database to be ready... (%d/30)", i+1)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		log.Fatalf("Failed to connect to database after retries: %v", err)
	}

	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	return db
}

func AutoMigrate(db *gorm.DB) {
	err := db.AutoMigrate(
		&Organization{},
		&DepartmentRecord{},
		&User{},
		&TempAccess{},
		&Session{},
		&HealthRecord{},
		&Vital{},
		&Encounter{},
		&Attachment{},
		&AuditLog{},
		&Venue{},
		&TrainerProfile{},
		&Booking{},
		&BookingAudit{},
		&MenuCategory{},
		&MenuItem{},
		&MenuItemChoice{},
		&ItemSubstitute{},
		&SellWindow{},
		&HolidayBlackout{},
		&Promotion{},
		&MenuOrder{},
		&MenuOrderItem{},
		&APIToken{},
		&WebhookEndpoint{},
		&WebhookDelivery{},
		&ReportJob{},
		&SlowQueryLog{},
	)
	if err != nil {
		log.Fatalf("AutoMigrate failed: %v", err)
	}

	// Create vitals partitioning by month.
	// GORM creates a regular vitals table; we convert it to partitioned if it isn't already.
	db.Exec(`
		DO $$
		DECLARE
			month_start DATE;
			month_end DATE;
			part_name TEXT;
			is_partitioned BOOLEAN;
		BEGIN
			-- Check if the vitals table is already partitioned
			SELECT EXISTS(
				SELECT 1 FROM pg_partitioned_table
				WHERE partrelid = 'vitals'::regclass
			) INTO is_partitioned;

			IF NOT is_partitioned THEN
				-- Backup, drop, and recreate as partitioned
				CREATE TABLE IF NOT EXISTS vitals_backup AS SELECT * FROM vitals;
				DROP TABLE IF EXISTS vitals CASCADE;
				CREATE TABLE vitals (
					id SERIAL,
					user_id INTEGER NOT NULL,
					weight_lb NUMERIC(6,1),
					bp_systolic INTEGER,
					bp_diastolic INTEGER,
					temperature_f NUMERIC(5,1),
					heart_rate INTEGER,
					recorded_by INTEGER NOT NULL,
					recorded_at TIMESTAMP NOT NULL,
					created_at TIMESTAMP DEFAULT NOW(),
					PRIMARY KEY (id, recorded_at)
				) PARTITION BY RANGE (recorded_at);

				-- Restore data if any existed
				INSERT INTO vitals (user_id, weight_lb, bp_systolic, bp_diastolic,
					temperature_f, heart_rate, recorded_by, recorded_at, created_at)
				SELECT user_id, weight_lb, bp_systolic, bp_diastolic,
					temperature_f, heart_rate, recorded_by, recorded_at, created_at
				FROM vitals_backup
				WHERE recorded_at IS NOT NULL;
				DROP TABLE vitals_backup;
			END IF;

			-- Rolling partition: create partitions from 2025 through 2 years in the future
			FOR yr IN 2025..(EXTRACT(YEAR FROM NOW())::INT + 2) LOOP
				FOR m IN 1..12 LOOP
					month_start := make_date(yr, m, 1);
					month_end := month_start + '1 month'::INTERVAL;
					part_name := 'vitals_' || TO_CHAR(month_start, 'YYYY_MM');

					IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = part_name) THEN
						EXECUTE format(
							'CREATE TABLE %I PARTITION OF vitals FOR VALUES FROM (%L) TO (%L)',
							part_name, month_start, month_end
						);
					END IF;
				END LOOP;
			END LOOP;

			-- Create indexes
			IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_vitals_recorded_at') THEN
				CREATE INDEX idx_vitals_recorded_at ON vitals (recorded_at);
			END IF;
			IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = 'idx_vitals_user_id') THEN
				CREATE INDEX idx_vitals_user_id ON vitals (user_id);
			END IF;
		END $$;
	`)

	// Create org-scoped materialized views for reporting.
	// Drop and recreate to ensure organization_id column is present (migration from
	// earlier schema that lacked the org dimension).
	db.Exec(`DROP MATERIALIZED VIEW IF EXISTS mv_clinic_utilization`)
	db.Exec(`DROP MATERIALIZED VIEW IF EXISTS mv_booking_fill_rates`)
	db.Exec(`DROP MATERIALIZED VIEW IF EXISTS mv_menu_sell_through`)

	db.Exec(`
		CREATE MATERIALIZED VIEW mv_clinic_utilization AS
		SELECT
			date_trunc('day', e.encounter_date) AS day,
			e.department,
			u.organization_id,
			COUNT(*) AS encounter_count
		FROM encounters e
		JOIN users u ON u.id = e.clinician_id
		GROUP BY day, e.department, u.organization_id
		ORDER BY day DESC;
	`)

	db.Exec(`
		CREATE MATERIALIZED VIEW mv_booking_fill_rates AS
		SELECT
			date_trunc('day', b.slot_start) AS day,
			b.venue_id,
			b.organization_id,
			COUNT(*) AS total_bookings,
			COUNT(*) FILTER (WHERE b.status = 'confirmed') AS confirmed,
			COUNT(*) FILTER (WHERE b.status = 'canceled') AS canceled
		FROM bookings b
		GROUP BY day, b.venue_id, b.organization_id
		ORDER BY day DESC;
	`)

	db.Exec(`
		CREATE MATERIALIZED VIEW mv_menu_sell_through AS
		SELECT
			mi.sku,
			mi.name,
			mi.organization_id,
			COALESCE(SUM(moi.quantity), 0) AS total_sold,
			COALESCE(SUM(moi.quantity * moi.unit_price), 0) AS total_revenue
		FROM menu_items mi
		LEFT JOIN menu_order_items moi ON moi.menu_item_id = mi.id
		GROUP BY mi.id, mi.sku, mi.name, mi.organization_id
		ORDER BY total_sold DESC;
	`)

	// Create unique indexes on materialized views (required for REFRESH CONCURRENTLY)
	db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS mv_clinic_util_idx ON mv_clinic_utilization (day, department, organization_id)`)
	db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS mv_booking_fill_idx ON mv_booking_fill_rates (day, venue_id, organization_id)`)
	db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS mv_menu_sell_idx ON mv_menu_sell_through (sku, organization_id)`)

	// Create audit log trigger function
	db.Exec(`
		CREATE OR REPLACE FUNCTION create_unique_index_if_not_exists(idx_name text, tbl text, col text)
		RETURNS void AS $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = idx_name) THEN
				EXECUTE format('CREATE UNIQUE INDEX %I ON %I (%s)', idx_name, tbl, col);
			END IF;
		END $$ LANGUAGE plpgsql;
	`)

	// Enforce audit immutability at DB level: prevent UPDATE and DELETE on audit tables.
	// This ensures audit records cannot be tampered with even by application-level bugs.
	db.Exec(`
		CREATE OR REPLACE FUNCTION prevent_audit_modifications()
		RETURNS TRIGGER AS $$
		BEGIN
			RAISE EXCEPTION 'Audit records are immutable — UPDATE and DELETE operations are not permitted on %', TG_TABLE_NAME;
			RETURN NULL;
		END;
		$$ LANGUAGE plpgsql;
	`)

	// Protect audit_logs table
	db.Exec(`
		DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_trigger WHERE tgname = 'trg_audit_logs_immutable'
			) THEN
				CREATE TRIGGER trg_audit_logs_immutable
				BEFORE UPDATE OR DELETE ON audit_logs
				FOR EACH ROW EXECUTE FUNCTION prevent_audit_modifications();
			END IF;
		END $$;
	`)

	// Protect booking_audits table
	db.Exec(`
		DO $$
		BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_trigger WHERE tgname = 'trg_booking_audits_immutable'
			) THEN
				CREATE TRIGGER trg_booking_audits_immutable
				BEFORE UPDATE OR DELETE ON booking_audits
				FOR EACH ROW EXECUTE FUNCTION prevent_audit_modifications();
			END IF;
		END $$;
	`)

	log.Println("Database migration completed successfully")
}

func SeedDefaults(db *gorm.DB) {
	// Seed default organization
	var orgCount int64
	db.Model(&Organization{}).Count(&orgCount)
	if orgCount == 0 {
		db.Create(&Organization{Name: "Campus University"})
		db.Create(&DepartmentRecord{Name: "General Medicine", OrganizationID: 1})
		db.Create(&DepartmentRecord{Name: "Laboratory", OrganizationID: 1})
		db.Create(&DepartmentRecord{Name: "Pharmacy", OrganizationID: 1})
		db.Create(&DepartmentRecord{Name: "Athletics", OrganizationID: 1})
		db.Create(&DepartmentRecord{Name: "Dining Services", OrganizationID: 1})
	}

	// Seed default venues
	var venueCount int64
	db.Model(&Venue{}).Count(&venueCount)
	if venueCount == 0 {
		db.Create(&Venue{Name: "Training Room A", RoomType: "onsite", Capacity: 20})
		db.Create(&Venue{Name: "Training Room B", RoomType: "onsite", Capacity: 15})
		db.Create(&Venue{Name: "Main Gym", RoomType: "onsite", Capacity: 50})
		db.Create(&Venue{Name: "Virtual Session", RoomType: "virtual", Capacity: 100})
	}

	// Seed default users (one per role) so the portal is usable on first boot
	var userCount int64
	db.Model(&User{}).Count(&userCount)
	if userCount == 0 {
		hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		pw := string(hash)

		deptGeneral := uint(1)
		deptLab := uint(2)
		deptAthletics := uint(4)
		deptDining := uint(5)

		seedUsers := []User{
			{Username: "admin", PasswordHash: pw, FullName: "System Administrator", Email: "admin@campus.local", Role: RoleAdmin, OrganizationID: 1, Active: true},
			{Username: "student", PasswordHash: pw, FullName: "Jane Student", Email: "student@campus.local", Role: RoleStudent, OrganizationID: 1, Active: true},
			{Username: "faculty", PasswordHash: pw, FullName: "Dr. Robert Faculty", Email: "faculty@campus.local", Role: RoleFaculty, OrganizationID: 1, Active: true},
			{Username: "clinician", PasswordHash: pw, FullName: "Dr. Sarah Clinician", Email: "clinician@campus.local", Role: RoleClinician, OrganizationID: 1, DepartmentID: &deptGeneral, Active: true},
			{Username: "labtech", PasswordHash: pw, FullName: "Mike LabTech", Email: "labtech@campus.local", Role: RoleClinician, OrganizationID: 1, DepartmentID: &deptLab, Active: true},
			{Username: "staff", PasswordHash: pw, FullName: "Emily Staff", Email: "staff@campus.local", Role: RoleStaff, OrganizationID: 1, DepartmentID: &deptDining, Active: true},
			{Username: "trainer", PasswordHash: pw, FullName: "Chris Trainer", Email: "trainer@campus.local", Role: RoleStaff, OrganizationID: 1, DepartmentID: &deptAthletics, Active: true},
		}
		for i := range seedUsers {
			db.Create(&seedUsers[i])
		}

		// Seed trainer profiles for partner matching demo
		db.Create(&TrainerProfile{UserID: 2, SkillLevel: 3, WeightClass: 145, PrimaryStyle: "boxing"})
		db.Create(&TrainerProfile{UserID: 3, SkillLevel: 5, WeightClass: 180, PrimaryStyle: "jiu-jitsu"})
		db.Create(&TrainerProfile{UserID: 7, SkillLevel: 7, WeightClass: 170, PrimaryStyle: "muay-thai"})

		// Seed sample menu categories and items (org-scoped)
		db.Create(&MenuCategory{OrganizationID: 1, Name: "Entrees", SortOrder: 1})
		db.Create(&MenuCategory{OrganizationID: 1, Name: "Sides", SortOrder: 2})
		db.Create(&MenuCategory{OrganizationID: 1, Name: "Beverages", SortOrder: 3})
		db.Create(&MenuCategory{OrganizationID: 1, Name: "Combos", SortOrder: 4})

		db.Create(&MenuItem{OrganizationID: 1, CategoryID: 1, SKU: "ENT-001", Name: "Grilled Chicken Wrap", Description: "Seasoned chicken with fresh veggies", ItemType: "dish", BasePriceDineIn: 8.99, BasePriceTakeout: 9.49, MemberDiscount: 10})
		db.Create(&MenuItem{OrganizationID: 1, CategoryID: 1, SKU: "ENT-002", Name: "Veggie Burger", Description: "Plant-based patty with house sauce", ItemType: "dish", BasePriceDineIn: 7.99, BasePriceTakeout: 8.49, MemberDiscount: 10})
		db.Create(&MenuItem{OrganizationID: 1, CategoryID: 2, SKU: "SID-001", Name: "Sweet Potato Fries", Description: "Crispy baked sweet potato fries", ItemType: "dish", BasePriceDineIn: 3.99, BasePriceTakeout: 4.49})
		db.Create(&MenuItem{OrganizationID: 1, CategoryID: 3, SKU: "BEV-001", Name: "Fresh Smoothie", Description: "Mixed berry protein smoothie", ItemType: "dish", BasePriceDineIn: 4.99, BasePriceTakeout: 5.49, MemberDiscount: 5})
		db.Create(&MenuItem{OrganizationID: 1, CategoryID: 4, SKU: "CMB-001", Name: "Training Meal Deal", Description: "Wrap + Fries + Smoothie", ItemType: "combo", BasePriceDineIn: 14.99, BasePriceTakeout: 15.99, MemberDiscount: 15})

		log.Println("Seed users and demo data created. See README for default credentials.")
	}
}
