# Campus Wellness & Training Operations Portal - API Specification

## Base URL
```
http://localhost:8080
```

## Authentication
All endpoints except login require a valid `session_id` cookie. State-changing requests (POST/PUT/DELETE) require a CSRF token via form field `csrf_token` or `X-CSRF-Token` header.

Internal API routes (`/api/internal/*`) require HMAC authentication via:
- `X-HMAC-Signature`: HMAC-SHA256 of `METHOD:PATH:TIMESTAMP:BODY_SHA256`
- `X-HMAC-Timestamp`: Unix timestamp (within 5-minute window)
- `X-Body-SHA256`: SHA-256 hex digest of request body

Rate limit: 60 requests/minute per client IP. Exceeding returns `429 Too Many Requests` with `Retry-After` header.

---

## Public Endpoints

### `GET /login`
Renders the login page.

### `POST /login`
Authenticates a user and creates a session.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| username | string | Yes | Local username |
| password | string | Yes | User password |

**Responses:**
- `302` → Redirect to `/dashboard` (on success, sets `session_id` cookie)
- `200` → Re-renders login page with error message (on failure)

---

## Authenticated Endpoints

### Dashboard & Health Records

#### `GET /dashboard`
Renders role-tailored dashboard with health record summary, recent vitals, encounters, and attachments.

**Query Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| user_id | uint | No | View another user's records (clinician/admin only, department-scoped) |

**Responses:**
- `200` → HTML dashboard page

#### `POST /health/update`
Updates a user's health record (allergies, conditions, medications, blood type). Fields encrypted at rest.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| user_id | uint | Yes | Target user ID |
| allergies | string | No | Allergies (max 10KB) |
| conditions | string | No | Medical conditions (max 10KB) |
| medications | string | No | Current medications (max 10KB) |
| blood_type | string | No | Blood type |
| reason | string | No | Reason for update (audit log) |

**Responses:**
- `302` → Redirect to dashboard
- `400` → Validation error
- `403` → Scope violation

#### `POST /health/upload`
Uploads a supporting document (PDF or image).

**Form Parameters (multipart):**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| user_id | uint | Yes | Target user ID |
| file | file | Yes | PDF, JPEG, PNG, or GIF (max 10 MB) |

**Headers:**
- `X-CSRF-Token`: CSRF token (required for multipart)

**Responses:**
- `200` → JSON `{"message": "File uploaded successfully", "attachment_id": <id>}`
- `400` → Invalid file type, size exceeded, or MIME mismatch
- `403` → Scope violation

#### `GET /health/download/:id`
Downloads an attachment by ID. Scope-checked before serving.

**Responses:**
- `200` → File content with appropriate Content-Type and Content-Disposition headers
- `403` → Scope violation
- `404` → Attachment not found

#### `GET /health/history`
Returns audit log history for a table/record.

**Query Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| table | string | Yes | Table name (allowlisted) |
| record_id | uint | Yes | Record ID |

**Responses:**
- `200` → JSON array of audit log entries
- `403` → Role/scope restriction

---

### Clinician Endpoints

#### `GET /clinician`
Renders clinician workspace with patient list and encounter/vitals forms.

#### `POST /clinician/encounter`
Creates a clinical encounter record.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| user_id | uint | Yes | Patient user ID |
| department | string | Yes | Department (lab, pharmacy, general, nursing) |
| chief_complaint | string | Yes | Reason for visit |
| notes | string | No | Clinical notes |
| diagnosis | string | No | Diagnosis |
| treatment | string | No | Treatment plan |

**Responses:**
- `302` → Redirect to clinician page
- `400` → Validation error
- `403` → Department scope violation

#### `POST /clinician/vitals`
Records patient vitals.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| user_id | uint | Yes | Patient user ID |
| weight_lb | float | No | Weight in pounds (1-1000) |
| bp_systolic | int | No | Systolic BP (40-300) |
| bp_diastolic | int | No | Diastolic BP (20-200, must be < systolic) |
| temperature_f | float | No | Temperature in Fahrenheit (85-115) |
| heart_rate | int | No | Heart rate in bpm (20-300) |

**Responses:**
- `302` → Redirect to clinician page
- `400` → Validation error (out of range)

---

### Booking Endpoints

#### `GET /bookings`
Renders booking page with user's bookings (as requester or partner).

#### `POST /bookings`
Creates a new booking.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| venue_id | uint | Yes | Venue ID |
| partner_id | uint | No | Partner user ID |
| slot_start | string | Yes | Start time (RFC3339 or YYYY-MM-DDTHH:MM) |

**Responses:**
- `302` → Redirect to bookings page
- `400` → Conflict detected or validation error

#### `POST /bookings/:id/transition`
Transitions a booking to a new status.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| status | string | Yes | Target status (confirmed, canceled, refunded) |
| note | string | No | Reason for transition |

**Authorization:** Requester, partner, staff, or admin only.

**Responses:**
- `302` → Redirect to bookings page
- `400` → Invalid transition or cancellation within 2-hour window
- `403` → Not authorized

#### `GET /bookings/:id/audit`
Returns booking audit trail.

**Responses:**
- `200` → JSON array of BookingAudit entries (booking_id, changed_by, old_status, new_status, note, timestamp)

#### `GET /api/slots`
Returns available 30-minute slots for a venue on a given date.

**Query Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| venue_id | uint | Yes | Venue ID |
| date | string | Yes | Date (YYYY-MM-DD) |

**Responses:**
- `200` → JSON array of `{"start": "<RFC3339>", "end": "<RFC3339>"}`

#### `GET /api/match-partners`
Returns compatible training partners based on configurable criteria.

**Query Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| skill_range | float | No | Skill level tolerance (default varies) |
| weight_range | float | No | Weight class tolerance in lb |
| style | string | No | Primary style filter (exact match) |

**Responses:**
- `200` → JSON array of trainer profiles with user details

#### `GET /api/check-conflicts`
Checks for booking conflicts before submission.

**Query Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| venue_id | uint | Yes | Venue ID |
| slot_start | string | Yes | Proposed start time |
| partner_id | uint | No | Partner user ID |

**Responses:**
- `200` → JSON array of conflict description strings (empty if no conflicts)

---

### Menu Endpoints

#### `GET /menu`
Renders menu page with items enriched with availability, computed prices, substitutes, and choices.

#### `POST /menu/order`
Creates a menu order. Validates all items are within sell windows and not sold out.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| order_type | string | Yes | "dine_in" or "takeout" |
| is_member | bool | No | Member pricing flag |
| items | array | Yes | Array of {menu_item_id, quantity, choices} |

**Responses:**
- `302` → Redirect to menu page with success message
- `400` → Item unavailable, sold out, or outside sell window

#### `GET /api/price`
Calculates final price for an item with all applicable discounts.

**Query Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| item_id | uint | Yes | Menu item ID |
| order_type | string | Yes | "dine_in" or "takeout" |
| is_member | bool | No | Apply member discount |

**Responses:**
- `200` → JSON `{"price": <float64>}`

---

### Menu Management Endpoints (Staff/Admin)

#### `GET /menu/manage`
Renders menu management page with all categories, items, blackouts, and promotions.

#### `POST /menu/manage/category`
Creates a menu category.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| name | string | Yes | Category name |
| parent_id | uint | No | Parent category ID (for subcategories) |
| sort_order | int | No | Display order |

#### `POST /menu/manage/item`
Creates a menu item.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| category_id | uint | Yes | Category ID |
| sku | string | Yes | Unique SKU |
| name | string | Yes | Item name |
| description | string | No | Item description |
| item_type | string | Yes | "dish", "combo", or "addon" |
| base_price_dine_in | float | Yes | Dine-in price (>= 0) |
| base_price_takeout | float | Yes | Takeout price (>= 0) |
| member_discount | float | No | Member discount % (0-100) |

#### `POST /menu/manage/item/:id/sold-out`
Toggles the sold-out status of a menu item. Creates audit log entry.

#### `POST /menu/manage/item/:id/sell-windows`
Sets sell windows for a menu item (replaces existing windows).

**Form Parameters (arrays):**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| day_of_week[] | int | Yes | Day of week (0=Sunday, 6=Saturday) |
| open_time[] | string | Yes | Opening time (HH:MM) |
| close_time[] | string | Yes | Closing time (HH:MM, must be > open_time) |

#### `POST /menu/manage/item/:id/substitutes`
Sets substitute suggestions for a menu item (replaces existing substitutes).

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| substitute_ids[] | uint | Yes | Array of substitute menu item IDs |

#### `POST /menu/manage/item/:id/choices`
Adds a choice option to a menu item.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| choice_type | string | Yes | Type (e.g., prep, flavor, size) |
| name | string | Yes | Choice name |
| extra_price | float | No | Additional price |

#### `POST /menu/manage/blackout`
Creates a holiday blackout date.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| date | string | Yes | Blackout date (YYYY-MM-DD) |
| description | string | No | Holiday description |

#### `POST /menu/manage/blackout/:id/delete`
Deletes a holiday blackout date.

#### `POST /menu/manage/promotion`
Creates a time-bound promotion for a menu item.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| menu_item_id | uint | Yes | Target item ID |
| discount_pct | float | Yes | Discount percentage (0-100) |
| starts_at | string | Yes | Start datetime |
| ends_at | string | Yes | End datetime (must be after starts_at) |

---

### Admin Endpoints

#### `GET /admin/users`
Lists all users with roles and active status.

#### `GET /admin/register`
Renders the user registration form.

#### `POST /admin/register`
Creates a new user account (admin only).

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| username | string | Yes | Unique username |
| password | string | Yes | Password (bcrypt hashed) |
| full_name | string | Yes | Display name |
| email | string | No | Email address |
| role | string | Yes | Role (student, faculty, clinician, staff, admin) |
| department_id | uint | No | Department assignment |

#### `POST /admin/users/:id/toggle`
Activates or deactivates a user account. Invalidates all sessions on deactivation.

#### `POST /admin/users/:id/role`
Permanently changes a user's role. Invalidates sessions.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| role | string | Yes | New role |

#### `POST /admin/users/:id/temp-access`
Grants temporary elevated access.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| role | string | Yes | Temporary role to grant |
| duration_hours | int | No | Duration in hours (default: 8) |

#### `GET /admin/performance`
Renders admin performance dashboard with slow queries and system metrics.

#### `POST /admin/refresh-views`
Manually triggers materialized view refresh.

#### `GET /admin/webhooks`
Lists webhook endpoints and recent delivery history.

#### `POST /admin/webhooks`
Registers a new webhook endpoint.

**Form Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| url | string | Yes | Webhook URL (http/https with valid host) |
| event_type | string | Yes | Event type to subscribe to |
| secret | string | Yes | HMAC signing secret |

#### `GET /admin/bookings`
Lists all bookings system-wide (admin view).

---

### Internal API Endpoints (HMAC-Authenticated)

#### `GET /api/internal/clinic-utilization`
Returns clinic utilization report from materialized view (cached with 5-min TTL).

**Responses:**
- `200` → JSON array of `{day, department, encounter_count}`

#### `GET /api/internal/booking-fill-rates`
Returns booking fill rate report from materialized view (cached with 5-min TTL).

**Responses:**
- `200` → JSON array of `{day, venue_id, total_bookings, confirmed_count, canceled_count}`

#### `GET /api/internal/menu-sell-through`
Returns menu sell-through report from materialized view (cached with 5-min TTL).

**Responses:**
- `200` → JSON array of `{sku, item_name, total_sold, total_revenue}`

#### `POST /api/internal/webhooks/receive`
Receives inbound webhooks from other internal systems. Logs payload.

**Responses:**
- `200` → JSON `{"status": "received"}`
