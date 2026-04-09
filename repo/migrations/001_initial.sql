-- Campus Wellness & Training Operations Portal
-- LEGACY REFERENCE ONLY — NOT AUTHORITATIVE
-- The authoritative schema is managed by GORM AutoMigrate in internal/models/database.go.
-- This file is retained for documentation purposes. Do not run it directly.

-- Enable extensions
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Organizations
CREATE TABLE IF NOT EXISTS organizations (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) UNIQUE NOT NULL
);

-- Departments
CREATE TABLE IF NOT EXISTS departments (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    organization_id INTEGER NOT NULL REFERENCES organizations(id)
);

-- Users
CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(100) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    full_name VARCHAR(255) NOT NULL,
    email VARCHAR(255),
    role VARCHAR(20) NOT NULL DEFAULT 'student',
    organization_id INTEGER NOT NULL REFERENCES organizations(id),
    department_id INTEGER REFERENCES departments(id),
    active BOOLEAN DEFAULT true,
    ssn VARCHAR(255),
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX idx_users_role ON users(role);
CREATE INDEX idx_users_org ON users(organization_id);

-- Sessions
CREATE TABLE IF NOT EXISTS sessions (
    id VARCHAR(64) PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_sessions_user ON sessions(user_id);

-- Temporary Access
CREATE TABLE IF NOT EXISTS temp_accesses (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    granted_role VARCHAR(20) NOT NULL,
    original_role VARCHAR(20) NOT NULL,
    granted_by INTEGER NOT NULL REFERENCES users(id),
    expires_at TIMESTAMP NOT NULL,
    reverted BOOLEAN DEFAULT false,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_temp_access_user ON temp_accesses(user_id);
CREATE INDEX idx_temp_access_expires ON temp_accesses(expires_at);

-- Health Records
CREATE TABLE IF NOT EXISTS health_records (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    allergies TEXT,
    conditions TEXT,
    medications TEXT,
    blood_type VARCHAR(10),
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_health_user ON health_records(user_id);

-- Vitals (partitioned by month)
CREATE TABLE IF NOT EXISTS vitals (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    weight_lb NUMERIC(6,1),
    bp_systolic INTEGER,
    bp_diastolic INTEGER,
    temperature_f NUMERIC(5,1),
    heart_rate INTEGER,
    recorded_by INTEGER NOT NULL,
    recorded_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
) PARTITION BY RANGE (recorded_at);

-- Create partitions for current year
DO $$
DECLARE
    month_start DATE;
    month_end DATE;
    partition_name TEXT;
BEGIN
    FOR m IN 1..12 LOOP
        month_start := DATE '2026-01-01' + ((m-1) || ' months')::INTERVAL;
        month_end := month_start + '1 month'::INTERVAL;
        partition_name := 'vitals_' || TO_CHAR(month_start, 'YYYY_MM');

        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF vitals
             FOR VALUES FROM (%L) TO (%L)',
            partition_name, month_start, month_end
        );
    END LOOP;
END $$;

CREATE INDEX idx_vitals_user ON vitals(user_id);
CREATE INDEX idx_vitals_recorded ON vitals(recorded_at);

-- Encounters
CREATE TABLE IF NOT EXISTS encounters (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    clinician_id INTEGER NOT NULL REFERENCES users(id),
    department VARCHAR(20) NOT NULL,
    chief_complaint TEXT,
    notes TEXT,
    diagnosis TEXT,
    treatment TEXT,
    encounter_date TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_encounters_user ON encounters(user_id);
CREATE INDEX idx_encounters_dept ON encounters(department);

-- Attachments
CREATE TABLE IF NOT EXISTS attachments (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    file_name VARCHAR(255) NOT NULL,
    file_path TEXT NOT NULL,
    file_size BIGINT NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    sha256 VARCHAR(64) NOT NULL,
    uploaded_by INTEGER NOT NULL REFERENCES users(id),
    created_at TIMESTAMP DEFAULT NOW()
);

-- Audit Log
CREATE TABLE IF NOT EXISTS audit_logs (
    id SERIAL PRIMARY KEY,
    table_name VARCHAR(100) NOT NULL,
    record_id INTEGER NOT NULL,
    action VARCHAR(20) NOT NULL,
    editor_id INTEGER NOT NULL,
    reason TEXT,
    fingerprint VARCHAR(64) NOT NULL,
    snapshot TEXT,
    timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_table_record ON audit_logs(table_name, record_id);
CREATE INDEX idx_audit_timestamp ON audit_logs(timestamp);

-- Venues
CREATE TABLE IF NOT EXISTS venues (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    room_type VARCHAR(50) NOT NULL,
    capacity INTEGER
);

-- Trainer Profiles
CREATE TABLE IF NOT EXISTS trainer_profiles (
    id SERIAL PRIMARY KEY,
    user_id INTEGER UNIQUE NOT NULL REFERENCES users(id),
    skill_level INTEGER NOT NULL DEFAULT 1,
    weight_class NUMERIC(6,1) NOT NULL,
    primary_style VARCHAR(50)
);

-- Bookings
CREATE TABLE IF NOT EXISTS bookings (
    id SERIAL PRIMARY KEY,
    requester_id INTEGER NOT NULL REFERENCES users(id),
    partner_id INTEGER REFERENCES users(id),
    venue_id INTEGER NOT NULL REFERENCES venues(id),
    slot_start TIMESTAMP NOT NULL,
    slot_end TIMESTAMP NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'initiated',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_bookings_requester ON bookings(requester_id);
CREATE INDEX idx_bookings_slot ON bookings(slot_start);
CREATE INDEX idx_bookings_venue_slot ON bookings(venue_id, slot_start);

-- Booking Audit
CREATE TABLE IF NOT EXISTS booking_audits (
    id SERIAL PRIMARY KEY,
    booking_id INTEGER NOT NULL REFERENCES bookings(id),
    changed_by INTEGER NOT NULL REFERENCES users(id),
    old_status VARCHAR(20),
    new_status VARCHAR(20) NOT NULL,
    note TEXT,
    timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

-- Menu Categories
CREATE TABLE IF NOT EXISTS menu_categories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    parent_id INTEGER REFERENCES menu_categories(id),
    sort_order INTEGER DEFAULT 0
);

-- Menu Items
CREATE TABLE IF NOT EXISTS menu_items (
    id SERIAL PRIMARY KEY,
    category_id INTEGER NOT NULL REFERENCES menu_categories(id),
    sku VARCHAR(50) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    item_type VARCHAR(20) NOT NULL DEFAULT 'dish',
    base_price_dine_in NUMERIC(10,2) NOT NULL,
    base_price_takeout NUMERIC(10,2) NOT NULL,
    member_discount NUMERIC(5,2) DEFAULT 0,
    sold_out BOOLEAN DEFAULT false,
    active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Menu Item Choices (prep, flavor, size)
CREATE TABLE IF NOT EXISTS menu_item_choices (
    id SERIAL PRIMARY KEY,
    menu_item_id INTEGER NOT NULL REFERENCES menu_items(id),
    choice_type VARCHAR(20) NOT NULL,
    name VARCHAR(255) NOT NULL,
    extra_price NUMERIC(10,2) DEFAULT 0
);

-- Item Substitutes (many-to-many)
CREATE TABLE IF NOT EXISTS item_substitutes (
    id SERIAL PRIMARY KEY,
    menu_item_id INTEGER NOT NULL REFERENCES menu_items(id),
    substitute_id INTEGER NOT NULL REFERENCES menu_items(id)
);

-- Sell Windows
CREATE TABLE IF NOT EXISTS sell_windows (
    id SERIAL PRIMARY KEY,
    menu_item_id INTEGER NOT NULL REFERENCES menu_items(id),
    day_of_week INTEGER NOT NULL,
    open_time VARCHAR(10) NOT NULL,
    close_time VARCHAR(10) NOT NULL
);

-- Holiday Blackouts
CREATE TABLE IF NOT EXISTS holiday_blackouts (
    id SERIAL PRIMARY KEY,
    date DATE UNIQUE NOT NULL,
    description TEXT
);

-- Promotions
CREATE TABLE IF NOT EXISTS promotions (
    id SERIAL PRIMARY KEY,
    menu_item_id INTEGER NOT NULL REFERENCES menu_items(id),
    discount_pct NUMERIC(5,2) NOT NULL,
    starts_at TIMESTAMP NOT NULL,
    ends_at TIMESTAMP NOT NULL,
    active BOOLEAN DEFAULT true
);

-- Menu Orders
CREATE TABLE IF NOT EXISTS menu_orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    order_type VARCHAR(20) NOT NULL,
    total_price NUMERIC(10,2) NOT NULL,
    is_member BOOLEAN DEFAULT false,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Menu Order Items
CREATE TABLE IF NOT EXISTS menu_order_items (
    id SERIAL PRIMARY KEY,
    order_id INTEGER NOT NULL REFERENCES menu_orders(id),
    menu_item_id INTEGER NOT NULL REFERENCES menu_items(id),
    quantity INTEGER NOT NULL DEFAULT 1,
    unit_price NUMERIC(10,2) NOT NULL,
    choices TEXT
);

-- Webhook Endpoints
CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id SERIAL PRIMARY KEY,
    url TEXT NOT NULL,
    event_type VARCHAR(50) NOT NULL,
    secret TEXT NOT NULL,
    active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Webhook Deliveries
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id SERIAL PRIMARY KEY,
    endpoint_id INTEGER NOT NULL REFERENCES webhook_endpoints(id),
    payload TEXT NOT NULL,
    status INTEGER NOT NULL,
    response TEXT,
    attempts INTEGER DEFAULT 1,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Slow Query Log
CREATE TABLE IF NOT EXISTS slow_query_logs (
    id SERIAL PRIMARY KEY,
    query TEXT NOT NULL,
    duration BIGINT NOT NULL,
    caller VARCHAR(255),
    created_at TIMESTAMP DEFAULT NOW()
);

-- Materialized Views for Reporting

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_clinic_utilization AS
SELECT
    date_trunc('day', e.encounter_date) AS day,
    e.department,
    COUNT(*) AS encounter_count
FROM encounters e
GROUP BY 1, 2
ORDER BY 1 DESC;

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_booking_fill_rates AS
SELECT
    date_trunc('day', b.slot_start) AS day,
    b.venue_id,
    COUNT(*) AS total_bookings,
    COUNT(*) FILTER (WHERE b.status = 'confirmed') AS confirmed,
    COUNT(*) FILTER (WHERE b.status = 'canceled') AS canceled
FROM bookings b
GROUP BY 1, 2
ORDER BY 1 DESC;

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_menu_sell_through AS
SELECT
    mi.sku,
    mi.name,
    COALESCE(SUM(moi.quantity), 0) AS total_sold,
    COALESCE(SUM(moi.quantity * moi.unit_price), 0) AS total_revenue
FROM menu_items mi
LEFT JOIN menu_order_items moi ON moi.menu_item_id = mi.id
GROUP BY mi.id, mi.sku, mi.name
ORDER BY total_sold DESC;

-- Seed Data
INSERT INTO organizations (name) VALUES ('Campus University') ON CONFLICT DO NOTHING;
INSERT INTO departments (name, organization_id) VALUES
    ('General Medicine', 1),
    ('Laboratory', 1),
    ('Pharmacy', 1),
    ('Athletics', 1),
    ('Dining Services', 1)
ON CONFLICT DO NOTHING;

INSERT INTO venues (name, room_type, capacity) VALUES
    ('Training Room A', 'onsite', 20),
    ('Training Room B', 'onsite', 15),
    ('Main Gym', 'onsite', 50),
    ('Virtual Session', 'virtual', 100)
ON CONFLICT DO NOTHING;
