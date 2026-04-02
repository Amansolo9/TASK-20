# Campus Wellness & Training Operations Portal - System Design Document

## 1. Architecture Overview

The system follows a layered monolithic architecture built in Go with Gin as the HTTP framework:

```
Client (Browser)
    │
    ▼
Gin Router (cmd/server/main.go)
    │
    ├─ Middleware Layer (middleware.go)
    │   ├─ AuthRequired      → Session validation
    │   ├─ CSRFProtect        → Double-submit cookie pattern
    │   ├─ RateLimit          → Per-IP token bucket (60 req/min)
    │   ├─ RequireRole        → RBAC enforcement
    │   ├─ DataScope          → Self/Department/Organization scoping
    │   └─ HMACAuth           → Internal API request signing
    │
    ├─ Handler Layer (handlers/)
    │   ├─ auth_handlers.go   → Login, registration, user management
    │   ├─ health_handlers.go → Dashboard, vitals, encounters, uploads
    │   ├─ booking_handlers.go→ Booking CRUD, conflicts, partner matching
    │   ├─ menu_handlers.go   → Menu display, management, orders
    │   └─ admin_handlers.go  → Performance, webhooks, reporting
    │
    ├─ Service Layer (services/)
    │   ├─ audit.go           → Immutable change tracking with fingerprints
    │   ├─ crypto.go          → AES-256-GCM field encryption
    │   ├─ health.go          → Health records, vitals, encounters, attachments
    │   ├─ booking.go         → Slot generation, conflict detection, state machine
    │   ├─ menu.go            → Categories, pricing, sell windows, orders
    │   └─ integration.go     → CSV watcher, webhooks, reporting, caching
    │
    ├─ Model Layer (models/)
    │   ├─ models.go          → All domain structs and enums
    │   └─ database.go        → DB init, migrations, partitioning, seeding
    │
    └─ View Layer (views/ + templates/ + static/)
        ├─ layout.templ       → Templ components (Layout, LoginPage, ErrorPage)
        ├─ render.go          → Templ render helper
        └─ static/js/app.js   → Client-side validation, CSRF, drafts, calendar
```

## 2. Data Model

### Core Entities

**User** - Central identity with role-based access (student, faculty, clinician, staff, admin). Belongs to an Organization and optionally a Department. SSN encrypted at rest.

**Organization / DepartmentRecord** - Hierarchical org structure. Organizations contain departments. Synced via CSV imports.

**Session** - 64-byte hex token with 24-hour TTL. Validated on every request. Invalidated on account deactivation or role change.

**TempAccess** - Tracks temporary role elevation. Stores original role for automatic reversion after configurable duration (default 8 hours).

### Health Domain

**HealthRecord** - Per-user record with encrypted fields (allergies, conditions, medications) and plaintext blood type.

**Vital** - Time-series data partitioned by month. Fields: weight (lb), blood pressure (systolic/diastolic), temperature (F), heart rate. Recorded by clinician ID.

**Encounter** - Clinician-created visit records scoped by department: chief complaint, diagnosis, treatment, notes.

**Attachment** - File metadata with SHA-256 hash, content type, file path. Physical files stored on local disk.

### Booking Domain

**Venue** - Physical rooms (onsite) or virtual session spaces. Capacity enforced only for onsite.

**TrainerProfile** - Per-user athletics profile: skill level (1-10), weight class (lb), primary style.

**Booking** - Links requester and optional partner to a venue and 30-minute time slot. Status follows state machine: initiated → confirmed → canceled → refunded.

**BookingAudit** - Separate audit trail for booking state transitions with actor identity and notes.

### Menu/Dining Domain

**MenuCategory** - Hierarchical (self-referential ParentID) with sort ordering.

**MenuItem** - Identified by unique SKU. Types: dish, combo, addon. Dual pricing (dine-in/takeout). Member discount percentage. Sold-out flag.

**MenuItemChoice** - Item-level options (prep, flavor, size) with optional extra pricing.

**ItemSubstitute** - Links sold-out items to alternative suggestions.

**SellWindow** - Per-item availability by day of week with open/close times (HH:MM).

**HolidayBlackout** - Date-based closure overriding all sell windows.

**Promotion** - Time-bound discount percentage per item with active flag.

**MenuOrder / MenuOrderItem** - Order with line items, total price, order type (dine_in/takeout), member status.

### System Domain

**AuditLog** - Immutable change log with JSON snapshot, SHA-256 fingerprint, editor ID, action, reason.

**WebhookEndpoint / WebhookDelivery** - Outbound webhook configuration and delivery tracking with retry history.

**SlowQueryLog** - Performance monitoring: query text, duration, caller, with 30-day retention.

## 3. Security Architecture

### Authentication Flow
1. User submits username/password to `POST /login`
2. Server verifies bcrypt hash, generates 64-byte hex session token
3. Session stored in DB with 24-hour expiry
4. HttpOnly cookie set with session ID
5. Every subsequent request validated via `AuthRequired` middleware

### Authorization Layers
- **Route-level RBAC**: `RequireRole` middleware on route groups (clinician, staff, admin)
- **Data scoping**: `DataScope` middleware sets self/department/organization scope per role
- **Object-level**: `EnforceSelfScope` and `EnforceDeptScope` verify record ownership before access
- **Booking authorization**: Requester/partner ownership check; staff/admin override

### Encryption at Rest
- AES-256-GCM with random nonce per encryption (via `crypto.go`)
- Applied to: SSN, allergies, conditions, medications
- Key: 32-byte base64-encoded from `FIELD_ENCRYPTION_KEY` env var
- Decryption errors fail gracefully with placeholder text

### PII Masking
- SSN: Full for admin, `***-**-LAST4` for others
- Email: Full for admin/clinician, `XX***@domain` for others

## 4. Performance Architecture

### Materialized Views
Three views refreshed every 15 minutes by background goroutine:
1. `mv_clinic_utilization` - Encounters aggregated by day and department
2. `mv_booking_fill_rates` - Bookings by day/venue with confirmed/canceled counts
3. `mv_menu_sell_through` - Menu items by SKU with total sold and revenue

Refresh uses `CONCURRENTLY` when possible (non-blocking reads), falls back to blocking refresh.

### Vitals Partitioning
- PostgreSQL range partitioning on `recorded_at` column
- Monthly partitions pre-created for 2025-2027 (36 partitions)
- Indexes on `recorded_at` (partition key) and `user_id`

### Result Caching
- In-memory map with `sync.RWMutex` protection
- 5-minute TTL per cache entry
- Used for materialized view query results in `ReportingService`

### Slow Query Detection
- GORM callback logs queries exceeding 500ms threshold
- Persisted to `slow_query_logs` table
- 30-day retention with automatic cleanup every 15 minutes
- Surfaced via admin performance dashboard

## 5. Integration Architecture

### CSV Import Pipeline
- `CSVWatcher` polls watched directory every 10 seconds
- File type inferred from filename (enrollment vs. org structure)
- Success → moved to `processed/` folder
- Failure → moved to `errors/` folder with logged error

### Webhook System
- Asynchronous dispatch via goroutines
- 3 retries with exponential backoff (1s, 2s, 3s)
- HMAC-SHA256 signature per delivery using endpoint secret
- Full delivery logging (status, response, attempts)

### Internal API Security
- HMAC request signing: `METHOD:PATH:TIMESTAMP:BODY_SHA256`
- 5-minute timestamp replay window
- Rate limiting: 60 requests/minute per client IP

## 6. Background Processes

Three goroutines launched in `main.go`:
1. **Temp Access Revert** - Runs every 1 minute, reverts expired role elevations
2. **Materialized View Refresh** - Runs every 15 minutes (configurable)
3. **CSV Watcher** - Polls every 10 seconds for import files

Plus auxiliary cleanup:
- **Rate Limit Cleanup** - Removes expired client windows every 1 minute
- **Slow Query Cleanup** - Deletes logs older than 30 days every 15 minutes

## 7. Database Configuration

- **Connection Pool**: MaxOpenConns=25, MaxIdleConns=10, ConnMaxLifetime=5min
- **Extensions**: pgcrypto
- **Indexes**: Covering all foreign keys, unique constraints (username, SKU), and query patterns (status, date ranges)
- **Migrations**: Auto-migrate via GORM with manual partitioning setup in `database.go`
