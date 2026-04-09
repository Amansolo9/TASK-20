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

### Webhooks
- Internal-only destinations (RFC1918/loopback/.local/.internal/.corp)
- Org-scoped: events dispatch only to same-org endpoints
- HMAC-signed payloads
- 3 retries with fresh request body per attempt
- Delivery logging with status/attempt tracking

## Security Hardening

| Control | Implementation |
|---------|---------------|
| Encryption at rest | AES-256-GCM (dev fallback key; required in `GIN_MODE=release`) |
| Session security | SameSite=Lax, HttpOnly, configurable Secure flag |
| CSRF | Double-submit cookie + origin validation |
| Rate limiting | 60 req/min per IP, sliding window |
| File validation | Allowlisted MIME + magic byte detection |
| Audit immutability | Append-only audit_logs table |
| Auth failure logging | AUTH_FAILURE, SESSION_INVALID, RBAC_DENIED events |
| Slow query tracking | GORM callbacks log queries >500ms |

## Database Schema

Authoritative schema managed by GORM AutoMigrate in `internal/models/database.go`.

Key tables: organizations, departments, users, sessions, temp_accesses, health_records, vitals (monthly partitioned), encounters, attachments, audit_logs, venues, trainer_profiles, bookings, booking_audits, menu_categories, menu_items, menu_item_choices, item_substitutes, sell_windows, holiday_blackouts, promotions, menu_orders, menu_order_items, api_tokens, webhook_endpoints, webhook_deliveries, report_jobs, slow_query_logs.

3 materialized views: `mv_clinic_utilization`, `mv_booking_fill_rates`, `mv_menu_sell_through` — all include `organization_id` for tenant-scoped reads.
