# Campus Wellness & Training Operations Portal — System Design

## Overview

A multi-tenant, internally deployed portal for university health clinics and athletics departments. The system manages health records, training session bookings, dining menus, and operational reporting — all running on an internal network with no external dependencies.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Gin HTTP Server                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │ Public Routes │  │Session Routes│  │  API Routes (Token+  │  │
│  │  /login       │  │ /dashboard   │  │  HMAC)               │  │
│  │               │  │ /bookings    │  │  /api/slots          │  │
│  │               │  │ /menu        │  │  /api/match-partners │  │
│  │               │  │ /clinician   │  │  /api/check-conflicts│  │
│  │               │  │ /admin       │  │  /api/price          │  │
│  │               │  │ /health      │  │  /api/internal/*     │  │
│  └──────────────┘  └──────────────┘  └──────────────────────┘  │
│                              │                                   │
│  ┌───────────────────────────┴──────────────────────────────┐   │
│  │              Middleware Stack (per route group)           │   │
│  │  Rate Limiter → Slow Query Logger → Auth → DataScope →   │   │
│  │  CSRF → RBAC → HMAC (API only)                          │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
         │                    │                    │
    ┌────┴────┐         ┌────┴────┐         ┌────┴────┐
    │ Health  │         │Booking  │         │  Menu   │
    │ Service │         │Service  │         │ Service │
    └────┬────┘         └────┬────┘         └────┬────┘
         │                    │                    │
    ┌────┴────────────────────┴────────────────────┴────┐
    │              PostgreSQL 16 (GORM ORM)              │
    │  27 tables + 3 materialized views + partitioning   │
    └───────────────────────────────────────────────────┘
```

## Multi-Tenancy Model

Every tenant-owned entity carries an `organization_id` column. All queries filter by the authenticated user's org.

| Layer | Enforcement |
|-------|-------------|
| **Middleware** | `DataScope()` sets `orgID` in context from authenticated user |
| **Handlers** | Extract `orgID` from context, pass to service methods |
| **Services** | Every query includes `WHERE organization_id = ?` |
| **Models** | `OrganizationID uint` on Booking, MenuItem, MenuCategory, MenuOrder, HolidayBlackout, WebhookEndpoint |

### Scope Hierarchy

```
Organization (org_id)
  └── Department (dept_id)
       └── User (user_id)
            └── Records (health, bookings, orders)
```

- **Students/Faculty**: self-only access
- **Clinicians**: department-scoped within org (fail-closed if dept is nil)
- **Staff**: org-scoped for menu/booking management
- **Admin**: org-scoped for all administrative functions

## Authentication & Authorization

### Browser Sessions
- bcrypt password hashing
- 24-hour session cookies (HttpOnly, SameSite=Lax)
- CSRF double-submit cookie pattern
- Login CSRF via separate `login_csrf` cookie

### API Access
All `/api/*` routes require:
1. **Bearer token** — locally issued, 24h expiry, bound to user + org
2. **HMAC signature** — `X-HMAC-Signature` over `method:path:timestamp:bodyHash`
3. **Timestamp validation** — ±5 minute absolute skew
4. **Body verification** — SHA-256 of actual request body bytes
5. **Rate limiting** — 60 req/min per client IP

### Internal API (`/api/internal/*`)
All of the above, plus:
- Admin role required
- Responses are org-scoped

## Data Modules

### Health Records
- Encrypted at rest (AES-256-GCM) for allergies, conditions, medications
- SSN field encrypted separately
- PII masking by role (partial SSN for non-admin)
- File uploads: PDF/JPEG/PNG/GIF only, 10MB max, SHA-256 fingerprint, magic byte validation
- Students/faculty can self-upload; clinicians/admin can upload within scope; staff blocked

### Athletics Bookings
- 30-minute slots, 8AM–8PM
- Partner matching by skill level, weight class, style — org-scoped
- Conflict detection: requester, partner, venue — all org-scoped
- State machine: initiated → confirmed → canceled → refunded
- 2-hour cancellation rule
- Audit trail on every state change (requires human-readable note)

### Menu Management
- Multi-level categories with org isolation
- SKU-based items with dine-in/takeout pricing
- Member discounts, time-bound promotions
- Sell windows (day + time), holiday blackouts (org-scoped)
- Sold-out toggles with substitute suggestions
- Order validation: sell window + blackout + sold-out + org ownership

### Reporting
- Materialized views with org dimension (refreshed every 15 min)
- In-memory cache with 5-minute TTL
- Async report jobs (pending → running → completed/failed)
- Slow query logging (>500ms)

## Integration

### CSV Import (Watched Folder)
- Enrollment files: username, full_name, email, role, organization, department, eligible
- Eligibility-based activation/deactivation
- Department sync from CSV within org
- Placeholder password hash (requires admin reset)

### On-Prem SSO Sync
Separate from CSV enrollment, the portal can synchronize users and groups from an on-prem SSO directory (JSON file on shared network path).

- **Service**: `SSOSyncService` in `internal/services/sso_sync.go`
- **Interface**: `SSOSource` — pluggable backend (file-based `FileSSOSource` included)
- **Scheduler**: background goroutine with configurable interval (default 15 min)
- **Operations**: user create, update (name/email/role/dept), deactivate
- **Dept auto-create**: departments referenced in SSO data are auto-created if missing
- **Audit trail**: every SSO-driven change logged via `AuditService` with `sso_sync_*` actions
- **Config**: `SSO_SYNC_ENABLED`, `SSO_SYNC_INTERVAL`, `SSO_SOURCE_PATH` env vars

### Webhooks
- Internal-only destinations (RFC1918/loopback/.local/.internal/.corp)
- Org-scoped: events dispatch only to same-org endpoints
- HMAC-signed payloads
- 3 retries with fresh request body per attempt
- Delivery logging with status/attempt tracking

### Named Integration Contracts
Explicit event type constants and typed payload schemas are defined in `internal/services/integration_contracts.go` for all named integration systems:

| System | Event Types |
|--------|-------------|
| **Bookings** | `booking.created`, `booking.confirmed`, `booking.canceled` |
| **Encounters** | `encounter.created` |
| **Users** | `user.created` |
| **Orders** | `order.created` |
| **E-Signature** | `esignature.request` |
| **Plagiarism** | `plagiarism.check` |
| **Competition Platform** | `competition.result`, `competition.registration`, `competition.score_update` |
| **Data Warehouse** | `warehouse.export_ready`, `warehouse.sync_complete`, `warehouse.schema_change` |

Typed dispatcher helpers on `WebhookService` (e.g., `DispatchCompetitionResult(...)`, `DispatchWarehouseExportReady(...)`) ensure payload structure correctness. The internal webhook receiver validates incoming events against the `AllEventTypes()` registry and rejects unknown event types.

## Security Hardening

| Control | Implementation |
|---------|---------------|
| Encryption at rest | AES-256-GCM (dev fallback key; required in `GIN_MODE=release`) |
| Session security | SameSite=Lax, HttpOnly, configurable Secure flag |
| CSRF | Double-submit cookie + origin validation |
| Rate limiting | 60 req/min per IP, sliding window |
| File validation | Allowlisted MIME + magic byte detection |
| Audit immutability | DB-level triggers block UPDATE/DELETE on audit_logs and booking_audits |
| Auth failure logging | AUTH_FAILURE, SESSION_INVALID, RBAC_DENIED events |
| Slow query tracking | GORM callbacks log queries >500ms |

## Database Schema

Authoritative schema managed by GORM AutoMigrate in `internal/models/database.go`.

Key tables: organizations, departments, users, sessions, temp_accesses, health_records, vitals (monthly partitioned), encounters, attachments, audit_logs, venues, trainer_profiles, bookings, booking_audits, menu_categories, menu_items, menu_item_choices, item_substitutes, sell_windows, holiday_blackouts, promotions, menu_orders, menu_order_items, api_tokens, webhook_endpoints, webhook_deliveries, report_jobs, slow_query_logs.

3 materialized views: `mv_clinic_utilization`, `mv_booking_fill_rates`, `mv_menu_sell_through` — all include `organization_id` for tenant-scoped reads.

## Templ Migration

All core pages are rendered using Templ components (`internal/views/*.templ`) instead of Go `html/template`. This provides type-safe, composable server-side rendering with compile-time checks.

| Page | Templ File | Handler |
|------|-----------|---------|
| Dashboard | `dashboard.templ` | `health_handlers.go:DashboardPage` |
| Bookings | `bookings.templ` | `booking_handlers.go:BookingPage`, `AllBookingsPage` |
| Menu | `menu.templ` | `menu_handlers.go:MenuPage` |
| Clinician | `clinician.templ` | `health_handlers.go:ClinicianPage` |
| Admin Users | `admin_users.templ` | `auth_handlers.go:UsersPage`, `RegisterPage` |
| Performance | `admin_performance.templ` | `admin_handlers.go:PerformancePage` |
| Webhooks | `admin_webhooks.templ` | `admin_handlers.go:WebhooksPage` |
| Login/Error | `layout.templ` | `auth_handlers.go:LoginPage` |

Each handler constructs a typed data struct, populates it from service calls, and passes it to `views.Render()`. The legacy `html/template` bootstrapping in `main.go` is retained only for `menu_manage.html`.

## Audit Immutability

Audit tables are protected at the PostgreSQL level to ensure records cannot be tampered with:

- `prevent_audit_modifications()` — trigger function that raises an exception on UPDATE or DELETE
- `trg_audit_logs_immutable` — trigger on `audit_logs` table
- `trg_booking_audits_immutable` — trigger on `booking_audits` table

Both triggers are created idempotently during `AutoMigrate` in `database.go`. Even application-level bugs or SQL injection cannot modify or delete audit records through the app's database connection.
