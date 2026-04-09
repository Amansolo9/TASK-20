# Campus Wellness & Training Portal — API Specification

## Authentication Methods

| Method | Routes | Mechanism |
|--------|--------|-----------|
| **Session cookie** | All browser routes (`/dashboard`, `/bookings`, `/menu`, `/admin`, `/clinician`, `/health`) | `session_id` HttpOnly cookie (24h), CSRF double-submit |
| **Token + HMAC** | `/api/*` (including `/api/internal/*`) | Bearer token + HMAC-SHA256 signature + timestamp + body hash |
| **Token + HMAC + Admin** | `/api/internal/*` | All of the above + admin role required + org-scoped responses |

### HMAC Signing Protocol

Every `/api/*` request must include:

| Header | Description |
|--------|-------------|
| `Authorization` | `Bearer <token>` — locally issued, 24h expiry |
| `X-HMAC-Signature` | HMAC-SHA256 of `{METHOD}:{PATH}:{TIMESTAMP}:{BODY_HASH}` |
| `X-HMAC-Timestamp` | RFC3339 timestamp (±5 min skew allowed) |
| `X-Body-SHA256` | Optional claimed body hash (verified against actual bytes) |

**Signing message format:** `GET:/api/slots:2026-04-09T12:00:00Z:e3b0c44...`

The server computes SHA-256 of the actual request body and uses that in the signature verification, regardless of the `X-Body-SHA256` header value.

---

## Public Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/login` | None | Login page |
| `POST` | `/login` | CSRF | Authenticate with username/password |

---

## Session-Authenticated Endpoints (Browser)

All require valid `session_id` cookie + CSRF token on POST requests.

### All Roles

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Redirect to `/dashboard` |
| `GET` | `/dashboard` | Role-tailored health dashboard (Templ-rendered) |
| `GET` | `/logout` | Destroy session |
| `POST` | `/api/tokens` | Issue API token (returns token + HMAC secret) |
| `GET` | `/health/download/:id` | Download attachment |
| `GET` | `/health/history` | Audit log for health records (allowlisted tables, scope-checked) |
| `POST` | `/health/upload` | Upload document — students/faculty: self only; clinicians/admin: scoped; staff: denied |
| `GET` | `/bookings` | View user's bookings |
| `POST` | `/bookings` | Create booking (org-scoped, conflict-checked) |
| `POST` | `/bookings/:id/transition` | Change booking status (requires note, org-checked) |
| `GET` | `/bookings/:id/audit` | View booking audit trail (org-checked) |
| `GET` | `/menu` | Browse menu (org-scoped items) |
| `POST` | `/menu/order` | Place order (validates all item org ownership) |

### Clinician + Admin Only

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/health/update` | Update health record (requires reason) |
| `GET` | `/clinician` | Clinician dashboard (dept + org scoped encounters) |
| `POST` | `/clinician/encounter` | Record encounter |
| `POST` | `/clinician/vitals` | Record vitals (requires reason) |

### Staff + Admin Only (Menu Management)

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/menu/manage` | Menu management page |
| `POST` | `/menu/manage/category` | Create category (org-scoped, parent validated) |
| `POST` | `/menu/manage/item` | Create menu item (category org validated) |
| `POST` | `/menu/manage/item/:id/sold-out` | Toggle sold-out (item org verified) |
| `POST` | `/menu/manage/item/:id/sell-windows` | Set sell windows (item org verified) |
| `POST` | `/menu/manage/item/:id/substitutes` | Set substitutes (all IDs org verified) |
| `POST` | `/menu/manage/item/:id/choices` | Add choice (item org verified) |
| `POST` | `/menu/manage/blackout` | Create holiday blackout (org-scoped) |
| `POST` | `/menu/manage/blackout/:id/delete` | Delete blackout (org verified) |
| `POST` | `/menu/manage/promotion` | Create promotion (item org verified) |

### Admin Only

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/admin/users` | List org users |
| `GET` | `/admin/register` | Registration form |
| `POST` | `/admin/register` | Create user (org from admin, dept validated) |
| `POST` | `/admin/users/:id/toggle` | Activate/deactivate (same-org only) |
| `POST` | `/admin/users/:id/role` | Change role (same-org only) |
| `POST` | `/admin/users/:id/temp-access` | Grant temporary elevated access (same-org only) |
| `POST` | `/admin/users/:id/reset-password` | Reset password (same-org only, audited) |
| `GET` | `/admin/performance` | Performance dashboard (org-scoped reports) |
| `POST` | `/admin/refresh-views` | Refresh materialized views |
| `GET` | `/admin/webhooks` | List webhooks (org-scoped) |
| `POST` | `/admin/webhooks` | Register webhook (internal-only URL, org-scoped) |
| `GET` | `/admin/bookings` | All org bookings |
| `POST` | `/admin/reports` | Submit async report job |
| `GET` | `/admin/reports/:id` | Poll async report status (org-scoped) |

---

## REST API Endpoints (Token + HMAC Required)

All require Bearer token + valid HMAC signature.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/slots?venue_id=&date=` | Available 30-min slots (org-scoped occupancy) |
| `GET` | `/api/match-partners?skill_range=&weight_range=&style=` | Partner matching (org-scoped) |
| `GET` | `/api/check-conflicts?venue_id=&slot_start=&partner_id=` | Pre-submit conflict check (org-scoped, partner org validated) |
| `GET` | `/api/price?item_id=&order_type=&is_member=` | Calculate price (item org verified) |

---

## Internal API Endpoints (Token + HMAC + Admin)

All require Bearer token + valid HMAC + admin role. All responses are org-scoped.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/internal/clinic-utilization` | Clinic report (from materialized view, org-filtered) |
| `GET` | `/api/internal/booking-fill-rates` | Booking report (from materialized view, org-filtered) |
| `GET` | `/api/internal/menu-sell-through` | Menu report (from materialized view, org-filtered) |
| `POST` | `/api/internal/webhooks/receive` | Inbound webhook receiver |

---

## Error Responses

All error responses use consistent JSON format:

```json
{
  "error": "human-readable error message"
}
```

| Status | Meaning |
|--------|---------|
| `400` | Bad request (validation failure, invalid input) |
| `401` | Unauthorized (missing/invalid token, expired session, bad HMAC) |
| `403` | Forbidden (RBAC denial, cross-org access, scope violation) |
| `404` | Not found |
| `429` | Rate limited (retry after N seconds) |
| `500` | Internal server error |

---

## Webhook Delivery Format

Outbound webhook payloads are JSON, signed with HMAC-SHA256.

**Headers:**
- `Content-Type: application/json`
- `X-Webhook-Signature: <HMAC-SHA256 hex>`
- `X-Webhook-Event: <event_type>`

**Event types:** `booking.created`, `booking.confirmed`, `booking.canceled`, `booking.refunded`

**Delivery behavior:**
- Up to 3 attempts with exponential backoff (1s, 2s, 3s)
- Fresh HTTP request constructed per attempt
- Delivery logged with status code, response, and attempt count
- Org-scoped: events dispatch only to endpoints in the same organization

---

## Audit Log Format

Every mutable operation produces an immutable audit entry:

```json
{
  "table_name": "health_records",
  "record_id": 42,
  "action": "update",
  "editor_id": 4,
  "reason": "Updated allergies after patient report",
  "fingerprint": "sha256-of-snapshot",
  "snapshot": "{...json...}",
  "timestamp": "2026-04-09T12:00:00Z"
}
```

**Reason enforcement:** Health record updates, vitals recordings, and booking state transitions require a non-empty human-readable reason/note. Empty or whitespace-only reasons are rejected with 400.
