# Campus Wellness & Training Operations Portal

A university-run health clinic and athletics department operations portal designed to function entirely on an internal network. Built with Go (Gin), HTML/CSS/JS, and PostgreSQL.

---

## Table of Contents

- [Overview](#overview)
- [Tech Stack](#tech-stack)
- [Quick Start](#quick-start)
- [Default Login Credentials](#default-login-credentials)
- [Project Structure](#project-structure)
- [Architecture](#architecture)
- [Feature Inventory](#feature-inventory)
  - [Phase 1: Shared Core & Security](#phase-1-shared-core--security)
  - [Phase 2: Health & Clinician Module](#phase-2-health--clinician-module)
  - [Phase 3: Athletics & Training Scheduler](#phase-3-athletics--training-scheduler)
  - [Phase 4: Menu & Logistics Dining Hub](#phase-4-menu--logistics-dining-hub)
  - [Phase 5: System Integration & Performance](#phase-5-system-integration--performance)
- [Database Schema](#database-schema)
- [API Endpoints](#api-endpoints)
- [Security Features](#security-features)
- [Configuration](#configuration)
- [Original Prompt](#original-prompt)

---

## Overview

This portal serves as a centralized system for campus wellness operations spanning:

- **Health clinic** management (records, vitals, encounters, document uploads)
- **Athletics** training session booking with partner matching
- **Dining** menu management tied to training events
- **Administration** with role-based access control, audit logging, and performance monitoring

The system is designed for **fully offline/internal network** deployment with no external dependencies.

---

## Tech Stack

| Layer      | Technology                        |
|------------|-----------------------------------|
| Language   | Go 1.25                          |
| Web Server | Gin                               |
| Frontend   | Templ + Go html/template + vanilla JS/CSS |
| Database   | PostgreSQL 16                     |
| Auth       | bcrypt + session cookies          |
| Container  | Docker / Docker Compose           |

---

## Quick Start

```bash
docker compose up --build
```

Then open **http://localhost:8080** in your browser.

That single command:
1. Starts a PostgreSQL 16 container with healthcheck
2. Builds the Go 1.25 binary in a multi-stage Docker build (includes Templ code generation)
3. Auto-generates secrets for session signing, HMAC, and field encryption (with startup warnings)
4. Runs database migrations (GORM AutoMigrate + vitals partitioning + materialized views)
5. Seeds default organizations, departments, venues, users, menu categories, and menu items
6. Starts the Gin web server on port 8080

No `.env` file or manual setup is required. All secrets are auto-generated for development. For production, override them via environment variables (see [Configuration](#configuration)).

To stop:
```bash
docker compose down
```

To stop and wipe all data (fresh start):
```bash
docker compose down -v
```

### Running Tests

```bash
# Run all tests (requires PostgreSQL from Docker Compose to be running)
bash run_tests.sh

# Run only unit tests (no database required)
bash run_tests.sh --unit
```

### Local Development (without Docker)

1. **Install PostgreSQL 16** and create the database:
   ```sql
   CREATE USER campus_admin WITH PASSWORD 'campus_secret';
   CREATE DATABASE campus_portal OWNER campus_admin;
   ```

2. **Run the server** (all config has sensible defaults):
   ```bash
   go run ./cmd/server
   ```

3. **Run tests:**
   ```bash
   go test ./... -v
   ```

### Templ Integration

The project uses the `a-h/templ` library for component-based server-rendered HTML as specified by the prompt. Templ components are in `internal/views/*.templ` and compiled to Go code via the `templ` CLI during the Docker build. The login page and layout are Templ-rendered. Remaining pages use Go's `html/template` with the Templ rendering infrastructure (`views.Render()`) available for all handlers.

---

## Default Login Credentials

All seed accounts use the password **`password123`**

| Username    | Role       | Department       |
|-------------|------------|------------------|
| `admin`     | Admin      | —                |
| `student`   | Student    | —                |
| `faculty`   | Faculty    | —                |
| `clinician` | Clinician  | General Medicine |
| `labtech`   | Clinician  | Laboratory       |
| `staff`     | Staff      | Dining Services  |
| `trainer`   | Staff      | Athletics        |

---

## Project Structure

```
Task-20/
├── cmd/
│   └── server/
│       └── main.go                  # Entry point, router, background workers
├── internal/
│   ├── auth/
│   │   └── auth.go                  # Session auth, login, register, temp access
│   ├── config/
│   │   └── config.go                # Environment-based configuration
│   ├── handlers/
│   │   ├── admin_handlers.go        # Performance dashboard, webhooks, reporting APIs
│   │   ├── auth_handlers.go         # Login/logout pages, user management, temp access
│   │   ├── booking_handlers.go      # Training session booking, slots, partner matching
│   │   ├── health_handlers.go       # Health dashboard, vitals, encounters, file upload
│   │   └── menu_handlers.go         # Menu browsing, management, ordering, pricing
│   ├── middleware/
│   │   └── middleware.go            # AuthRequired, RBAC, DataScope, HMAC, rate limiter
│   ├── models/
│   │   ├── database.go              # DB init with retry, AutoMigrate, seed data
│   │   └── models.go                # All GORM models (27 tables)
│   ├── services/
│   │   ├── audit.go                 # Immutable audit log with SHA-256 fingerprints
│   │   ├── booking.go               # Slot engine, partner matching, conflict detection
│   │   ├── health.go                # Health records, vitals, encounters, file storage
│   │   ├── integration.go           # CSV watcher, webhooks, reporting, PII masking
│   │   └── menu.go                  # Categories, items, pricing engine, sell windows
│   └── templates/
│       ├── funcs.go                 # Custom template functions (divf, deref)
│       ├── layout.html              # Base layout with role-aware navbar
│       ├── login.html               # Login page
│       ├── dashboard.html           # Health dashboard (vitals, records, uploads)
│       ├── clinician.html           # Clinician encounter/vitals forms with draft save
│       ├── bookings.html            # Training session booking with calendar
│       ├── bookings_admin.html      # Admin view of all bookings
│       ├── menu.html                # Dining menu with ordering
│       ├── menu_manage.html         # Menu management (categories, items, sell windows)
│       ├── admin_users.html         # User management (activate, roles, temp access)
│       ├── admin_performance.html   # Performance dashboard (slow queries, reports)
│       ├── admin_webhooks.html      # Webhook endpoint management
│       ├── register.html            # New user registration form
│       └── error.html               # Error display page
├── static/
│   ├── css/
│   │   └── style.css                # Complete responsive stylesheet
│   └── js/
│       └── app.js                   # Client-side validation, drafts, AJAX helpers
├── migrations/
│   └── 001_initial.sql              # Full SQL schema (reference, not used at runtime)
├── db-init/
│   └── 01-extensions.sql            # Postgres init script (pgcrypto extension)
├── uploads/                         # Runtime: uploaded documents stored here
├── watched_folder/                  # Runtime: CSV drop folder for enrollment imports
├── internal/
│   └── views/
│       ├── layout.templ             # Templ components (login page, layout)
│       ├── layout_templ.go          # Generated Go code from Templ
│       └── render.go                # Templ-to-Gin rendering bridge
├── Dockerfile                       # Multi-stage Go 1.25 build with Templ generation
├── docker-compose.yml               # Postgres + App orchestration (zero-config)
├── .dockerignore                    # Build context exclusions
├── .gitignore                       # Git exclusions (.env, binaries)
├── Makefile                         # Build/test/run targets
├── setup.sh                         # Optional: generates .env with random secrets
├── run_tests.sh                     # Test runner (full suite or unit-only)
├── go.mod                           # Go module definition
└── go.sum                           # Dependency checksums
```

---

## Architecture

```
┌─────────────┐     ┌──────────────────────────────────────────────┐
│   Browser    │────▶│              Gin Router (port 8080)          │
│  (HTML/CSS/  │◀────│                                              │
│   JS)        │     │  Middleware Stack:                           │
└─────────────┘     │  ├─ Rate Limiter (60 req/min per IP)        │
                     │  ├─ HMAC Auth (internal API routes)         │
                     │  ├─ Session Auth (cookie-based)             │
                     │  ├─ RBAC (role check per route group)       │
                     │  └─ Data Scope (self/dept/org filtering)    │
                     │                                              │
                     │  Route Groups:                               │
                     │  ├─ Public: /login                          │
                     │  ├─ Authed: /dashboard, /bookings, /menu    │
                     │  ├─ Clinician: /clinician/*                 │
                     │  ├─ Staff: /menu/manage/*                   │
                     │  ├─ Admin: /admin/*                         │
                     │  └─ Internal API: /api/internal/* (HMAC)    │
                     └──────────────┬───────────────────────────────┘
                                    │
                     ┌──────────────▼───────────────────────────────┐
                     │           Service Layer                      │
                     │  ├─ AuthService     (sessions, temp access)  │
                     │  ├─ HealthService   (records, vitals, files) │
                     │  ├─ BookingService  (slots, matching, FSM)   │
                     │  ├─ MenuService     (pricing, sell windows)  │
                     │  ├─ AuditService    (immutable revisions)    │
                     │  ├─ WebhookService  (dispatch + delivery)    │
                     │  ├─ ReportingService(mat views, cache)       │
                     │  └─ CSVWatcher      (folder polling)         │
                     └──────────────┬───────────────────────────────┘
                                    │
                     ┌──────────────▼───────────────────────────────┐
                     │         PostgreSQL 16 (GORM)                 │
                     │  27 tables + 3 materialized views            │
                     │  Indexes on all foreign keys & query paths   │
                     └──────────────────────────────────────────────┘
```

**Background Workers (goroutines):**
- Temp access revert ticker (every 1 minute)
- Materialized view refresh ticker (every 15 minutes)
- CSV watched folder poller (every 10 seconds)

---

## Feature Inventory

### Phase 1: Shared Core & Security

| Feature | Status | Details |
|---------|--------|---------|
| Go project with Gin web server | Done | `cmd/server/main.go` |
| HTML templates for component-based UI | Done | 14 templates in `internal/templates/` |
| GORM for PostgreSQL ORM | Done | 27 model structs in `internal/models/models.go` |
| Manual dependency injection | Done | All services wired in `main.go` |
| Session-based login (local credentials) | Done | bcrypt hashing, 24h session cookie |
| RBAC middleware | Done | 5 roles: Student, Faculty, Clinician, Staff, Admin |
| Data scoping (self/dept/org) | Done | `DataScope()` middleware + `EnforceSelfScope()` |
| Temporary elevated access | Done | Admin grants role, auto-reverts after N hours (default 8) |
| HMAC request signing | Done | `X-HMAC-Signature` + `X-HMAC-Timestamp` on `/api/internal/*` |
| Rate limiting (60 req/min per IP) | Done | Sliding window with cleanup goroutine |
| Docker Compose deployment | Done | Single `docker compose up --build` command |
| Seed data on first boot | Done | 7 users, 4 venues, 4 menu categories, 5 menu items |

### Phase 2: Health & Clinician Module

| Feature | Status | Details |
|---------|--------|---------|
| HealthRecords table | Done | Allergies, conditions, medications, blood type per user |
| Vitals table | Done | Weight (lb), BP, temperature (F), heart rate, recorder ID |
| Encounters table | Done | Clinician encounters by department with diagnosis/treatment |
| Attachments table + disk storage | Done | SHA-256 fingerprint, content type allowlist, size tracking |
| Audit log (immutable revisions) | Done | SHA-256 fingerprint, editor ID, timestamp, reason, snapshot |
| Student/Faculty health dashboard | Done | `dashboard.html` — summary, vitals table, documents, encounters |
| File upload with client-side validation | Done | 10MB limit, PDF/JPEG/PNG/GIF only, immediate error/success feedback |
| Clinician encounter form | Done | `clinician.html` — structured form with department tabs |
| Clinician vitals recording | Done | Weight lb, BP, temperature F, heart rate fields |
| Draft save (unsaved changes preserved) | Done | sessionStorage-based draft save/restore across department switches, cleared on logout |
| Department view switching | Done | Tab navigation: General, Lab, Pharmacy, Nursing |

### Phase 3: Athletics & Training Scheduler

| Feature | Status | Details |
|---------|--------|---------|
| 30-minute slot generation | Done | 8 AM – 8 PM slots per venue per day |
| Venue support (onsite + virtual) | Done | 4 seeded venues including "Virtual Session" |
| Partner matching algorithm | Done | Filters by skill level band (±), weight class (lb ±), primary style |
| Conflict detection (pre-submit) | Done | Checks requester, partner, and venue overlaps |
| 2-hour cancellation rule | Done | Rejects cancel if < 2 hours before slot start |
| Booking state machine | Done | Initiated → Confirmed → Canceled → Refunded with validation |
| Status badges on UI | Done | Color-coded: blue/green/red/gray per status |
| Booking audit trail | Done | Who changed what, when, with notes — viewable via modal |
| Available slots API | Done | `GET /api/slots?venue_id=&date=` returns open slots |
| Conflict check API | Done | `GET /api/check-conflicts` returns conflict list |
| Partner matching API | Done | `GET /api/match-partners` with skill/weight/style filters |

### Phase 4: Menu & Logistics Dining Hub

| Feature | Status | Details |
|---------|--------|---------|
| Multi-level category system | Done | Parent/child categories with sort order |
| SKU-based menu items | Done | Dish, combo, add-on types with unique SKU codes |
| Dine-in vs takeout pricing | Done | Separate base prices per order type |
| Member discount (percentage) | Done | Per-item configurable discount percentage |
| Time-bound promotions | Done | Start/end datetime with discount percentage |
| Pricing engine (stacks discounts) | Done | Applies member discount + active promotions |
| Sell windows (day + time range) | Done | e.g., Weekdays 6:30 AM – 2:00 PM per item |
| Holiday blackouts | Done | Date-based blackout with description, blocks all sales |
| Sold-out toggle | Done | Per-item toggle with UI badge |
| Substitute suggestions | Done | Many-to-many relationship, shown when item is sold out |
| Prep/flavor/size choices | Done | Per-item configurable options with extra price |
| Order creation with validation | Done | Checks sell window + sold-out status before saving |
| Order total display | Done | Calculated price shown before submission |
| Menu management UI | Done | `menu_manage.html` — full CRUD for categories, items, windows, promos |

### Phase 5: System Integration & Performance

| Feature | Status | Details |
|---------|--------|---------|
| CSV watched folder importer | Done | Polls `/app/watched_folder/` every 10s, processes enrollment + org CSVs. Imported users get a placeholder password and require admin password reset before first login. Roles are validated against the allowed set (student/faculty/clinician/staff/admin). |
| Processed/error file handling | Done | Moves CSVs to `processed/` or `errors/` subdirectories |
| Webhook endpoint registration | Done | URL + event type + HMAC secret per endpoint |
| Webhook outbound delivery | Done | HMAC-signed POST with 3 retries + exponential backoff |
| Webhook delivery logging | Done | Status code, response, attempt count tracked |
| Webhook receiver endpoint | Done | `POST /webhooks/receive` for inbound events |
| Materialized views (3 reports) | Done | Clinic utilization, booking fill rates, menu sell-through |
| MV scheduled refresh | Done | Background ticker every 15 minutes |
| Result caching with 5-min TTL | Done | In-memory cache in ReportingService |
| Slow query logging (>500ms) | Done | Logged to `slow_query_logs` table with caller info |
| Admin performance dashboard | Done | Displays all 3 reports + slow query list |
| PII masking (SSN) | Done | `***-**-1234` format for non-admin roles |
| PII masking (email) | Done | `ja***@campus.local` format for student/faculty roles |
| Internal API endpoints (HMAC) | Done | `/api/internal/clinic-utilization`, `/booking-fill-rates`, `/menu-sell-through` |

---

## Database Schema

### Tables (27)

| Table | Purpose |
|-------|---------|
| `organizations` | Top-level org (multi-tenant support) |
| `departments` | Departments within an organization |
| `users` | All user accounts with role + org + dept |
| `sessions` | Active login sessions (64-char token) |
| `temp_accesses` | Temporary elevated role grants with expiry |
| `health_records` | Per-user health summary (allergies, conditions, meds) |
| `vitals` | Time-series vitals (weight lb, BP, temp F, HR) |
| `encounters` | Clinician encounter records by department |
| `attachments` | Uploaded file metadata + SHA-256 + disk path |
| `audit_logs` | Immutable revision history for all record changes |
| `venues` | Training venues (onsite rooms + virtual) |
| `trainer_profiles` | Skill level, weight class (lb), primary style |
| `bookings` | Training session bookings with state machine |
| `booking_audits` | Booking state change audit trail |
| `menu_categories` | Nested menu categories with sort order |
| `menu_items` | SKU-based items with dual pricing + member discount |
| `menu_item_choices` | Prep, flavor, size options per item |
| `item_substitutes` | Many-to-many sold-out substitution suggestions |
| `sell_windows` | Day-of-week + time range availability per item |
| `holiday_blackouts` | Date-based blackout days |
| `promotions` | Time-bound percentage discounts per item |
| `menu_orders` | Placed orders (dine-in/takeout, member flag) |
| `menu_order_items` | Line items per order with computed unit price |
| `webhook_endpoints` | Registered outbound webhook targets |
| `webhook_deliveries` | Delivery attempt log per endpoint |
| `slow_query_logs` | Queries exceeding 500ms threshold |

### Materialized Views (3)

| View | Purpose | Refresh |
|------|---------|---------|
| `mv_clinic_utilization` | Encounters per day per department | Every 15 min |
| `mv_booking_fill_rates` | Bookings per day per venue (confirmed/canceled) | Every 15 min |
| `mv_menu_sell_through` | Items sold + revenue per SKU | Every 15 min |

---

## API Endpoints

### Public

| Method | Path | Description |
|--------|------|-------------|
| GET | `/login` | Login page |
| POST | `/login` | Authenticate |

### Authenticated (all roles)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/dashboard` | Health dashboard |
| GET | `/logout` | End session |
| POST | `/health/update` | Update health record |
| POST | `/health/upload` | Upload document (multipart) |
| GET | `/health/history` | Get audit log for a record |
| GET | `/bookings` | View my bookings |
| POST | `/bookings` | Create booking |
| POST | `/bookings/:id/transition` | Change booking status |
| GET | `/bookings/:id/audit` | View booking audit trail |
| GET | `/api/slots` | Available time slots |
| GET | `/api/match-partners` | Find matching partners |
| GET | `/api/check-conflicts` | Check for booking conflicts |
| GET | `/menu` | Browse dining menu |
| POST | `/menu/order` | Place an order |
| GET | `/api/price` | Calculate item price |

### Clinician + Admin

| Method | Path | Description |
|--------|------|-------------|
| GET | `/clinician` | Clinician dashboard |
| POST | `/clinician/encounter` | Record encounter |
| POST | `/clinician/vitals` | Record vitals |

### Staff + Admin

| Method | Path | Description |
|--------|------|-------------|
| GET | `/menu/manage` | Menu management page |
| POST | `/menu/manage/category` | Add category |
| POST | `/menu/manage/item` | Add menu item |
| POST | `/menu/manage/item/:id/sold-out` | Toggle sold out |
| POST | `/menu/manage/item/:id/sell-windows` | Set sell windows |
| POST | `/menu/manage/item/:id/substitutes` | Set substitutes |
| POST | `/menu/manage/item/:id/choices` | Add choice option |
| POST | `/menu/manage/blackout` | Add holiday blackout |
| POST | `/menu/manage/blackout/:id/delete` | Remove blackout |
| POST | `/menu/manage/promotion` | Add promotion |

### Admin Only

| Method | Path | Description |
|--------|------|-------------|
| GET | `/admin/users` | User management |
| GET | `/admin/register` | Registration form |
| POST | `/admin/register` | Create user |
| POST | `/admin/users/:id/toggle` | Activate/deactivate |
| POST | `/admin/users/:id/role` | Change role |
| POST | `/admin/users/:id/temp-access` | Grant temporary access |
| GET | `/admin/performance` | Performance dashboard |
| POST | `/admin/refresh-views` | Refresh materialized views |
| GET | `/admin/webhooks` | Webhook management |
| POST | `/admin/webhooks` | Register webhook endpoint |
| GET | `/admin/bookings` | All bookings (admin view) |

### Internal (HMAC-signed)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/internal/clinic-utilization` | Clinic report data |
| GET | `/api/internal/booking-fill-rates` | Booking report data |
| GET | `/api/internal/menu-sell-through` | Menu report data |
| POST | `/webhooks/receive` | Inbound webhook receiver |

---

## Security Features

| Feature | Implementation |
|---------|----------------|
| Password hashing | bcrypt with default cost |
| Session tokens | 32 random bytes → 64-char hex string, 24h expiry, invalidated on role change/deactivation |
| RBAC | Per-route-group middleware checking user role |
| Data scoping | Students see own data only; clinicians see dept; admin sees org |
| Temporary access | Auto-reverts via background ticker (checked every 1 min) |
| HMAC request signing | SHA-256 HMAC of `method:path:timestamp`, 5-min window |
| Rate limiting | 60 requests/minute per client IP, sliding window |
| CSRF protection | Per-session token validated on all state-changing POST requests |
| File upload validation | Server-side: 10MB max, allowlisted MIME types only, scope-checked |
| File integrity | SHA-256 fingerprint stored for every uploaded file |
| Audit trail | Immutable log with SHA-256 fingerprint per revision, scope-enforced |
| At-rest encryption | AES-256-GCM for SSN, health record fields (allergies, conditions, medications) |
| PII masking | SSN: `***-**-1234`; Email: `ja***@domain` (role-based display) |
| Secret management | SESSION_KEY, HMAC_SECRET, FIELD_ENCRYPTION_KEY auto-generated with warnings if not set |
| SQL injection | Prevented by GORM parameterized queries |
| XSS | Prevented by Go html/template auto-escaping |
| Webhook security | Receiver endpoint moved under HMAC-protected `/api/internal/` prefix |
| Login CSRF | Separate `login_csrf` cookie + hidden form field for pre-auth CSRF protection |
| Session invalidation | All user sessions destroyed on role change or account deactivation |
| Draft data clearing | `sessionStorage` drafts cleared on logout; tab-scoped (clears on tab close) |
| File magic bytes | Server validates actual file content via `http.DetectContentType()`, not just Content-Type header |
| Input validation | Vitals ranges, menu prices/discounts, sell window times, text field lengths all validated server-side |
| Admin audit trail | Role changes, account toggles, temp access grants, user creation all produce immutable audit records |
| Auth failure logging | `AUTH_FAILURE`, `SESSION_INVALID`, `RBAC_DENIED` events logged with user/IP context |

---

## Configuration

All settings are configured via environment variables. Everything has sensible defaults for development — `docker compose up --build` works with zero configuration.

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `campus_admin` | Database username |
| `DB_PASSWORD` | `campus_secret` | Database password |
| `DB_NAME` | `campus_portal` | Database name |
| `DB_SSLMODE` | `disable` | SSL mode for DB connection |
| `SERVER_PORT` | `8080` | Web server port |
| `SESSION_KEY` | (auto-generated) | Session signing key (hex, 64 chars) |
| `HMAC_SECRET` | (auto-generated) | HMAC signing key (hex, 64 chars) |
| `FIELD_ENCRYPTION_KEY` | (auto-generated) | AES-256 key for PHI encryption (base64, 32 bytes) |
| `SECURE_COOKIES` | `false` | Set `true` when serving behind TLS |
| `UPLOAD_DIR` | `./uploads` | File upload storage path |
| `WATCHED_DIR` | `./watched_folder` | CSV import watch path |
| `GIN_MODE` | `release` | Gin framework mode |

**Production deployment:** All default credentials (`campus_admin`/`campus_secret`, seed password `password123`) must be changed. Generate secrets with `openssl rand -base64 32` for `FIELD_ENCRYPTION_KEY` and `openssl rand -hex 32` for `SESSION_KEY`/`HMAC_SECRET`. Pass them as environment variables or via a `.env` file.

---
