# Campus Wellness & Training Operations Portal - Clarification Questions

---

## 1. Role-Based Dashboard Rendering Strategy

**Question:** The prompt says users land on a "Templ-rendered web dashboard tailored by role," but does not specify whether each role gets a completely separate page template or a single page that conditionally shows/hides sections.

**My Understanding:** A single dashboard route should handle all roles, with the backend resolving the user's role from their session and passing role-appropriate data to a shared Templ layout that conditionally renders sections (health summary for students/faculty, encounter forms for clinicians, booking management for athletics staff, etc.).

**Solution:** Implemented a unified `DashboardPage` handler in `health_handlers.go` that reads the authenticated user's role from the Gin context, queries role-relevant data (health records, vitals, encounters), and passes it to a shared `Layout` Templ component. The `Layout` component in `views/layout.templ` renders a role-aware navbar with navigation links filtered by role (e.g., clinician routes, staff routes, admin routes), so each user sees only the features they are authorized to use.

---

## 2. Clinician Cross-Patient Record Access Scope

**Question:** The prompt states clinicians "use structured, template-driven forms to record encounters and vitals" but does not clarify whether clinicians can view records for any patient in the system or only patients within their own department.

**My Understanding:** Clinicians should be scoped to their own department — they can view and modify records for patients belonging to the same department but not across departments. Admins retain organization-wide access.

**Solution:** Implemented a `DataScope` middleware in `middleware.go` that sets a scope type per role: `"self"` for students/faculty, `"department"` for clinicians/staff, and `"organization"` for admins. An `EnforceDeptScope` middleware function performs a database lookup to verify that the target patient's `DepartmentID` matches the clinician's own department before granting access. The `DashboardPage` handler accepts an optional `user_id` query parameter that lets clinicians view other patients' records, but only if the department scope check passes.

---

## 3. Unsaved Form Changes Across Department Views

**Question:** The prompt says clinicians "can switch between department views without losing unsaved changes," but does not specify how form state should be preserved given that this is a server-rendered Templ application without a client-side SPA framework.

**My Understanding:** Client-side draft persistence using the browser's `sessionStorage` is the most pragmatic approach for a server-rendered app, saving form inputs before navigation and restoring them when the user returns.

**Solution:** Implemented `saveDraft(formId)` and `restoreDraft(formId)` functions in `static/js/app.js` that serialize all form inputs (by name/value pairs) into `sessionStorage` keyed by form ID. When a clinician navigates away and returns, the draft is automatically restored into the form fields. Drafts are explicitly cleared on logout to prevent PII leakage across sessions.

---

## 4. File Upload Validation — Client-Side vs. Server-Side Boundary

**Question:** The prompt requires "immediate client-side type/size validation and clear success/error feedback" for document uploads, but does not specify whether the server should also independently validate, or whether client-side checks are sufficient.

**My Understanding:** Client-side validation provides immediate UX feedback, but the server must independently re-validate because client-side checks can be bypassed. Both layers are needed.

**Solution:** Implemented dual-layer validation. On the client side, `app.js` validates file size (10 MB max) and MIME type before sending the upload via a `fetch` request with `FormData`, displaying success or error messages immediately. On the server side, `health_handlers.go` enforces a 10 MB limit from `config.MaxUploadMB`, checks the declared MIME type against an allowlist (PDF, JPEG, PNG, GIF), and reads the first 512 bytes to perform magic-byte detection comparing the actual file type against the declared type — rejecting mismatches. The file is then stored on disk with a SHA-256 fingerprint recorded in the database alongside the attachment metadata.

---

## 5. SHA-256 Fingerprint Purpose and Integrity Checking

**Question:** The prompt requires "SHA-256 fingerprints" for attachments but does not specify when or how the fingerprint should be used beyond storage — for example, whether it should be verified on download or used for deduplication.

**My Understanding:** The SHA-256 fingerprint serves as a tamper-detection mechanism. It should be computed at upload time and stored alongside the file reference, providing an integrity audit trail. Download-time re-verification is a future enhancement but not strictly required for the initial implementation.

**Solution:** Implemented SHA-256 computation in the `SaveAttachment` method of `health.go`, which hashes the file content during upload and stores the hex-encoded fingerprint in the `Attachment` model's `SHA256Hash` field. The `Attachment` record also stores `ContentType`, `FilePath`, and `FileSize`. The download handler in `health_handlers.go` verifies user scope authorization before serving the file and sets headers to prevent header injection.

---

## 6. Booking Slot Duration and Calendar Time Range

**Question:** The prompt specifies "available 30-minute slots" but does not define the daily time range over which slots should be generated (e.g., business hours only vs. full 24 hours).

**My Understanding:** Slots should be generated during reasonable operating hours (8:00 AM to 8:00 PM) to reflect a campus athletics facility schedule, rather than offering 24-hour availability.

**Solution:** Implemented `GetAvailableSlots` in `booking.go`, which generates 30-minute slot intervals from 8:00 AM to 8:00 PM on a given date. The method queries existing bookings for the selected venue and date, excluding slots that are already booked (filtering out canceled and refunded statuses), and returns only the remaining available time windows.

---

## 7. Virtual Session Venue Conflict Rules

**Question:** The prompt lists "virtual session" as a location type but does not specify whether virtual sessions should enforce capacity limits or overlap restrictions like physical rooms.

**My Understanding:** Virtual sessions have no physical capacity constraint, so multiple bookings can overlap on a virtual venue. Only on-site rooms should enforce capacity-based conflict detection.

**Solution:** Implemented differentiated conflict logic in the `CheckConflicts` method of `booking.go`. The method checks three dimensions: requester time conflicts, partner time conflicts, and venue capacity. For venue conflicts, it only enforces capacity limits when the venue's `RoomType` is `"onsite"` — virtual venues (`RoomType: "virtual"`) are excluded from overlap checks, allowing unlimited concurrent virtual sessions.

---

## 8. Partner Matching Criteria Configurability

**Question:** The prompt mentions "configurable criteria like skill level bands, weight classes in lb, and primary style" for partner matching, but does not define the matching tolerance (e.g., exact match vs. range-based) or whether all criteria must match simultaneously.

**My Understanding:** Skill level and weight class should use range-based matching (within configurable bands), while primary style should use exact matching. Criteria should be applied as filters — all specified criteria must be satisfied, but optional criteria (like style) can be omitted to broaden results.

**Solution:** Implemented `MatchPartners` in `booking.go` with configurable `skillRange` and `weightRange` parameters. The method queries `TrainerProfile` records where the candidate's `SkillLevel` is within `±skillRange` of the requester and `WeightClass` is within `±weightRange` (in lb). If a `PrimaryStyle` filter is provided, it requires an exact match. The requester is excluded from results. The handler in `booking_handlers.go` exposes this as a JSON API for the calendar UI to display compatible partners.

---

## 9. Booking Cancellation Inventory/Slot Rollback

**Question:** The prompt says "users can cancel up to 2 hours before start" but does not specify what happens to the slot after cancellation — whether it becomes immediately re-bookable or enters a cooldown.

**My Understanding:** Canceled bookings should immediately free the slot for rebooking. The 2-hour rule is a hard cutoff: cancellations within 2 hours of start time are rejected outright.

**Solution:** Implemented cancellation logic in the `TransitionBooking` method of `booking.go`. When a booking transitions to `"canceled"` status, the method checks if `time.Now()` is less than 2 hours before `SlotStart` — if so, it returns an error rejecting the cancellation. On successful cancellation, the booking's status is updated in a database transaction with an audit record. The `GetAvailableSlots` method explicitly excludes canceled and refunded bookings from its conflict query, so the slot becomes immediately re-bookable.

---

## 10. Booking Order Lifecycle Status Display

**Question:** The prompt requires "on-screen status badges and audit notes explaining who changed what and when" but does not specify the exact lifecycle states or how the audit trail should be presented.

**My Understanding:** The booking lifecycle should follow a state machine with clear states (initiated, confirmed, canceled, refunded), each transition logged with the actor's identity, timestamp, and a human-readable note. The UI should display the current status as a badge and provide access to the full audit history.

**Solution:** Implemented a `BookingStatus` type with four states: `initiated`, `confirmed`, `canceled`, and `refunded`. Valid transitions are defined in a map within `TransitionBooking` (e.g., initiated can go to confirmed or canceled; canceled can go to refunded). Each transition creates a `BookingAudit` record containing `BookingID`, `ChangedBy` (user ID), `OldStatus`, `NewStatus`, `Note`, and `Timestamp`. The `BookingAudit` handler returns the chronological history for display, and authorization ensures only the requester, partner, or staff/admin can modify a booking's status.

---

## 11. Sell Window Time Boundaries and Holiday Blackouts

**Question:** The prompt gives an example sell window of "weekdays 6:30 AM–2:00 PM" and mentions "holiday blackouts," but does not specify whether blackouts override sell windows or are checked independently, nor whether items without sell windows are always available or never available.

**My Understanding:** Holiday blackouts should take precedence — if today is a blackout date, no items are available regardless of sell windows. Items with no sell windows defined should default to always available. Items with sell windows are only available during those windows on matching days.

**Solution:** Implemented `IsWithinSellWindow` in `menu.go` with a three-step check: (1) query the `HolidayBlackout` table for today's date — if a blackout exists, return unavailable immediately; (2) query `SellWindow` records for the item's ID and current day of week; (3) if no windows are defined, return available (always-on default); if windows exist, check whether the current time (in HH:MM format) falls within any defined window's `OpenTime`/`CloseTime` range.

---

## 12. Menu Pricing Rule Application Order

**Question:** The prompt lists "dine-in vs takeout, member discounts, and time-bound promotions" as pricing rules, but does not specify the order in which discounts should be applied or whether they stack.

**My Understanding:** Pricing should start from the base price for the order type (dine-in or takeout), then apply the member discount percentage, then apply any active time-bound promotions. Discounts should stack multiplicatively (each applies to the already-discounted price), and the final price should be floored at zero.

**Solution:** Implemented `CalculatePrice` in `menu.go` with the following pipeline: (1) select `BasePriceDineIn` or `BasePriceTakeout` based on order type; (2) if `IsMember` is true, apply the item's `MemberDiscount` percentage; (3) query `Promotion` records that are active, not expired, and linked to the item, then apply each promotion's `DiscountPct` multiplicatively; (4) floor the result to zero if negative. The `MenuPage` handler pre-computes final prices for display so totals are visible before a request is saved.

---

## 13. Sold-Out Items and Substitute Suggestions

**Question:** The prompt says staff can "mark items sold out with substitute suggestions," but does not specify whether sold-out items should be hidden from the menu, still visible with a label, or whether substitutes should be auto-applied.

**My Understanding:** Sold-out items should remain visible on the menu with a sold-out indicator and linked substitute suggestions displayed alongside. They should not be hidden (so users know what exists) but orders for sold-out items should be rejected.

**Solution:** Implemented the `SoldOut` boolean field on `MenuItem` and an `ItemSubstitute` join model linking a sold-out item to alternative `MenuItem` records. The `MenuPage` handler enriches each item with substitute information when `SoldOut` is true. The `ToggleSoldOut` handler lets staff toggle the flag with an audit log entry. The `CreateOrder` method in `menu.go` validates every item in the order and returns an error if any item is marked sold out, preventing sold-out items from being ordered.

---

## 14. Multi-Level Menu Categories and SKU Uniqueness

**Question:** The prompt mentions "multi-level categories and SKUs for dishes, combos, add-ons, prep and flavor choices" but does not define how deep the category hierarchy can go or whether SKUs must be globally unique.

**My Understanding:** Categories should support a parent-child hierarchy (at least two levels) using a self-referential parent ID. SKUs must be globally unique across all menu items to serve as unambiguous identifiers. Choices (prep, flavor) should be modeled as item-level options with optional extra pricing.

**Solution:** Implemented `MenuCategory` with a nullable `ParentID` field enabling arbitrary nesting depth, plus a `SortOrder` field for display ordering. `MenuItem` has a unique `SKU` field enforced at the database level. `MenuItemChoice` records are linked to menu items with `ChoiceType` (e.g., prep, flavor, size) and an `ExtraPrice` field for upcharges. The `ItemType` field on `MenuItem` distinguishes between `dish`, `combo`, and `addon`, with validation in `CreateMenuItem`.

---

## 15. Temporary Elevated Access Expiration Mechanism

**Question:** The prompt says administrators can "grant temporary elevated access that expires automatically after a set duration (default 8 hours)," but does not specify how expiration should be enforced — via a background job, on next request, or via database trigger.

**My Understanding:** A background goroutine should periodically check for expired temporary access grants and automatically revert the user's role to their original role, ensuring expiration happens even if the user is not actively making requests.

**Solution:** Implemented a `TempAccess` model storing `UserID`, `GrantedRole`, `OriginalRole`, `ExpiresAt`, and `Reverted` boolean. The `GrantTempAccess` method in `auth.go` creates this record and immediately updates the user's role. In `main.go`, a background goroutine runs `RevertExpiredAccess` every minute via a `time.Ticker`, which queries for non-reverted `TempAccess` records past their expiration, reverts each user's role to `OriginalRole`, marks the record as `Reverted`, and invalidates active sessions to force re-authentication with the restored role.

---

## 16. Versioned Change History and Immutable Revisions

**Question:** The prompt requires "each record update creates an immutable revision with editor identity, timestamp, and a human-readable reason," but does not specify the storage mechanism — whether to use a separate audit table, JSON snapshots, or a temporal/versioned table pattern.

**My Understanding:** A centralized audit log table with JSON snapshots of the changed record provides the best balance of simplicity and completeness. Each entry should be append-only (immutable) with a tamper-detection fingerprint.

**Solution:** Implemented an `AuditLog` model in `models.go` with fields for `TableName`, `RecordID`, `Action` (create/update/delete/role_change), `EditorID`, `Reason`, `Snapshot` (JSON-marshaled record state), `Timestamp`, and a `Fingerprint` computed as SHA-256 of the concatenation of table name, record ID, action, editor ID, and snapshot. The `LogChange` method in `audit.go` is called after every data modification across all domains (health records, vitals, encounters, menu items, user management). Entries are append-only — no update or delete operations exist on the audit log.

---

## 17. CSV Import File Processing and Error Handling

**Question:** The prompt mentions "scheduled imports from on-prem SSO and academic enrollment exports (CSV dropped into a watched folder)" but does not specify how to distinguish between different CSV types, or how to handle malformed files.

**My Understanding:** The CSV type should be inferred from the filename (e.g., files containing "enrollment" vs. "org" in the name). Malformed or failed files should be moved to an errors directory rather than deleted, so they can be inspected and retried.

**Solution:** Implemented a `CSVWatcher` in `integration.go` that polls a watched directory every 10 seconds for `.csv` files. It distinguishes file types by filename: files matching enrollment patterns are processed as user imports (username, full_name, email, role with default "student"), while org-structure files create organizations and departments. On successful processing, files are moved to a `processed/` subdirectory; on failure, they are moved to an `errors/` subdirectory with the error logged. Role values are validated against the allowed role set.

---

## 18. Webhook Delivery Reliability Without External Dependencies

**Question:** The prompt requires "outbound webhook deliveries to other internal systems" but specifies "without any external network dependency." It does not clarify retry behavior or how delivery failures should be handled.

**My Understanding:** Webhooks should be delivered asynchronously with retry logic and exponential backoff. All delivery attempts should be logged for auditability. Since the system is internal-network only, the HTTP client should use locally issued HMAC signatures for authentication.

**Solution:** Implemented `WebhookService` in `integration.go` with a `Dispatch` method that fans out delivery to all active webhook endpoints asynchronously via goroutines. The `deliver` method retries up to 3 times with exponential backoff (1s, 2s, 3s). Each delivery is signed with `X-Webhook-Signature` (HMAC-SHA256 of the payload using the endpoint's stored secret). Every attempt is logged in the `WebhookDelivery` table with status code, response body, and attempt count. Webhook registration in `admin_handlers.go` validates URL format (http/https with valid host).

---

## 19. HMAC Request Signing and Token-Based API Access

**Question:** The prompt requires "locally issued tokens, HMAC request signing, and rate limiting" for API access, but does not specify the HMAC signing scheme (what is signed, header format, replay protection).

**My Understanding:** HMAC should sign a canonical string combining the HTTP method, path, timestamp, and body hash to prevent tampering and replay attacks. A timestamp window (e.g., 5 minutes) provides replay protection. Rate limiting should apply per-client IP.

**Solution:** Implemented `HMACAuth` middleware in `middleware.go` for `/api/internal/*` routes. The signature is computed as HMAC-SHA256 of `METHOD:PATH:TIMESTAMP:BODY_SHA256` using a shared secret from the `HMAC_SECRET` environment variable. The middleware validates the `X-HMAC-Signature` header, checks that the `X-HMAC-Timestamp` is within a 5-minute window, and verifies the `X-Body-SHA256` header matches the actual request body hash. Rate limiting is implemented separately as a per-IP token bucket middleware allowing 60 requests per minute (configurable), returning HTTP 429 with a `Retry-After` header when exceeded.

---

## 20. Materialized View Refresh Strategy for Reporting

**Question:** The prompt requires "materialized views refreshed on a schedule" for high-volume reporting but does not specify the refresh mechanism or how to handle concurrent reads during refresh.

**My Understanding:** Materialized views should be refreshed in a background goroutine on a configurable interval. Concurrent refresh (which allows reads during refresh) should be preferred where possible, with a fallback to blocking refresh if the database does not support concurrent refresh for a given view.

**Solution:** Implemented three materialized views in `database.go`: `mv_clinic_utilization` (encounters by day/department), `mv_booking_fill_rates` (bookings by day/venue with confirmed/canceled counts), and `mv_menu_sell_through` (items by SKU with total sold and revenue). A background goroutine in `main.go` refreshes all views every 15 minutes (configurable via `MVRefreshInterval`). The refresh logic in `integration.go` first attempts `REFRESH MATERIALIZED VIEW CONCURRENTLY` (non-blocking for readers) and falls back to a standard blocking `REFRESH MATERIALIZED VIEW` if concurrent refresh fails. Each refresh is logged with timing information.

---

## 21. Result Caching with TTL for Repeated Report Queries

**Question:** The prompt specifies "result caching with TTL (e.g., 5 minutes) for repeated queries" but does not clarify whether the cache should be in-memory, in Redis, or at the database level, nor how cache invalidation should work.

**My Understanding:** Given the fully local deployment with no external dependencies, an in-memory cache with time-based expiration is the simplest and most appropriate approach. Cache entries expire naturally after the TTL; no explicit invalidation is needed since materialized views are refreshed on their own schedule.

**Solution:** Implemented an in-memory cache in `ReportingService` (`integration.go`) using a Go `map[string]cachedResult` protected by a `sync.RWMutex`. The `cachedQuery` method checks if a cached result exists and is within the 5-minute TTL before executing the database query. Cache reads use `RLock` for concurrent access; cache writes use a full `Lock`. This avoids any external dependency while providing efficient repeated-query performance for the admin reporting dashboard.

---

## 22. Slow Query Detection Threshold and Admin Visibility

**Question:** The prompt says "the system records slow queries over 500 ms" and "surfaces performance dashboards to admins," but does not specify how slow queries should be captured (application-level vs. database-level) or how long logs should be retained.

**My Understanding:** Slow query detection should happen at the application level via an ORM callback, capturing the query text, duration, and caller. Logs should be persisted to a database table with a retention policy, and exposed to admins through a dedicated dashboard endpoint.

**Solution:** Implemented a `SlowQueryLogger` in `middleware.go` that installs a GORM callback on every query. When a query exceeds the 500 ms threshold (configurable via `SlowQueryMs`), it inserts a record into the `slow_query_logs` table with the query text, duration in milliseconds, caller information, and timestamp. A background cleanup goroutine in `integration.go` (`CleanupSlowQueryLogs`) runs every 15 minutes and deletes logs older than 30 days (configurable retention). The admin dashboard in `admin_handlers.go` exposes `GetSlowQueries` to surface recent slow queries for performance monitoring.

---

## 23. Vitals Table Partitioning Strategy

**Question:** The prompt requires "partitioning for large time-series vitals by month" but does not specify the partitioning mechanism (range, list, hash) or how new partitions should be provisioned.

**My Understanding:** PostgreSQL native range partitioning on the `recorded_at` timestamp column is the natural fit for monthly time-series data. Partitions should be pre-created for a reasonable future window to avoid runtime partition creation overhead.

**Solution:** Implemented range partitioning in `database.go` during the `AutoMigrate` process. The vitals table is created with `PARTITION BY RANGE (recorded_at)`, and monthly partitions are pre-created for 2025 through 2027 (36 partitions). If the table already exists as non-partitioned (e.g., from GORM auto-migrate), the migration backs up existing data, drops the table, recreates it as partitioned, and restores the data. Indexes are created on both `recorded_at` (partition key) and `user_id` for query performance.

---

## 24. Field-Level Encryption and PII Masking Scope

**Question:** The prompt requires "encrypts sensitive fields at rest with masked display defaults (e.g., partial SSN if stored)" but does not specify which fields should be encrypted or which roles can see unmasked data.

**My Understanding:** SSN and health record sensitive fields (allergies, conditions, medications) should be encrypted at rest using AES-256-GCM. Display masking should be role-based: admins see full values, while other roles see masked versions (e.g., partial SSN, obscured email).

**Solution:** Implemented AES-256-GCM encryption in `crypto.go` with a 32-byte key from the `FIELD_ENCRYPTION_KEY` environment variable (base64-encoded). The `Encrypt` and `Decrypt` functions use random nonces for each encryption (ensuring identical plaintext produces different ciphertext). `SetUserSSN` in `auth.go` encrypts SSN before storage. `UpsertHealthRecord` in `health.go` encrypts allergies, conditions, and medications before database writes, and `GetHealthRecord` decrypts them on read (with graceful fallback on decryption error). PII masking in `integration.go` provides `MaskSSN` (returns `***-**-LAST4` for non-admins) and `MaskEmail` (returns `XX***@domain` for students/staff), with admins and clinicians seeing full values.

---

## 25. Account Activation/Deactivation and Session Invalidation

**Question:** The prompt says "administrators can activate/deactivate accounts" but does not specify whether deactivation should immediately terminate active sessions or only prevent future logins.

**My Understanding:** Deactivating an account should immediately invalidate all active sessions for that user, forcing them out of the system. This prevents a deactivated user from continuing to use an existing session.

**Solution:** Implemented `ToggleUserActive` in `auth.go` which flips the user's `Active` boolean, paired with `InvalidateUserSessions` which deletes all session records for that user from the `sessions` table. The `ToggleUser` handler in `auth_handlers.go` calls both methods in sequence and creates an audit log entry. Additionally, `ValidateSession` checks the user's `Active` status on every request, so even if a session record somehow persists, the authentication middleware will reject the request.

---

## 26. Templ Migration for Server-Rendered Pages

**Question:** The codebase uses both Go `html/template` and Templ components for rendering pages. Should all core pages be migrated to Templ for consistency, or is a hybrid approach acceptable?

**My Understanding:** All core pages should use Templ components for type safety, compile-time checks, and consistency with the dashboard (which already uses Templ). The `html/template` approach with `gin.H` maps loses type safety and can lead to runtime template errors.

**Solution:** Migrated all core pages to Templ components in `internal/views/`:
- `bookings.templ` — unified booking page (user + admin views via `IsAdmin` flag)
- `menu.templ` — dining menu with category nav, items, order form
- `clinician.templ` — clinician dashboard with encounter/vitals forms and draft persistence
- `admin_users.templ` — user management table + registration form (with `RegisterPage`)
- `admin_performance.templ` — performance dashboard with materialized view data
- `admin_webhooks.templ` — webhook management with updated event type options

Each handler constructs a typed data struct (e.g., `views.BookingsData`, `views.MenuData`) and calls `views.Render()`. This replaces `c.HTML()` with `gin.H` maps, providing compile-time guarantees that all template data is correctly typed and populated. The legacy `html/template` bootstrapping in `main.go` is retained only for `menu_manage.html`.

---

## 27. On-Prem SSO Sync vs. CSV Enrollment

**Question:** The prompt mentions "scheduled imports from on-prem SSO" alongside "academic enrollment exports (CSV dropped into a watched folder)" but does not clarify whether these are the same mechanism or separate integration paths.

**My Understanding:** These are separate systems. CSV enrollment handles batch imports from academic systems (registrar, HR), while SSO sync handles real-time user provisioning from the campus identity provider. They should be independent services with distinct configuration.

**Solution:** Implemented `SSOSyncService` in `internal/services/sso_sync.go` as a separate service from the `CSVWatcher`:
- Defines `SSOSource` interface for pluggable SSO backends (file-based `FileSSOSource` included)
- Runs on a configurable interval (default 15 min) via background goroutine
- Syncs user create/update/deactivate with full audit logging (`sso_sync_created`, `sso_sync_updated` actions)
- Auto-creates departments from SSO data when they don't exist in the portal
- Configured via `SSO_SYNC_ENABLED`, `SSO_SYNC_INTERVAL`, `SSO_SOURCE_PATH` environment variables
- Started in `main.go` alongside (not replacing) the CSV watcher

---

## 28. Audit Immutability Enforcement

**Question:** The prompt requires "immutable revisions" for audit records, but does not specify whether immutability should be enforced at the application level (no delete/update code paths) or at the database level (triggers/constraints).

**My Understanding:** Application-level enforcement alone is insufficient — bugs, ORM misuse, or raw SQL could bypass it. Database-level enforcement via triggers provides a defense-in-depth guarantee that audit records cannot be modified regardless of how the database is accessed.

**Solution:** Implemented PostgreSQL triggers in `internal/models/database.go` during `AutoMigrate`:
- `prevent_audit_modifications()` — trigger function that raises an exception with the table name on any UPDATE or DELETE attempt
- `trg_audit_logs_immutable` — BEFORE UPDATE OR DELETE trigger on `audit_logs`
- `trg_booking_audits_immutable` — BEFORE UPDATE OR DELETE trigger on `booking_audits`
- Both triggers use `IF NOT EXISTS` checks for idempotent migration
- Test cleanup disables triggers temporarily (`ALTER TABLE ... DISABLE TRIGGER`) before re-enabling

---

## 29. Named Integration Contracts for External Systems

**Question:** The prompt references integration with "competition platforms" and "data warehouse" systems, but the webhook plumbing only supports generic string event types with no structured payload contracts.

**My Understanding:** Named integration systems should have explicit event type constants, typed payload schemas, and typed dispatcher helpers to ensure payload structure correctness and make the integration contracts self-documenting.

**Solution:** Implemented `internal/services/integration_contracts.go` with:
- Event type constants for all systems: `EventCompetitionResult`, `EventWarehouseExportReady`, etc.
- Typed payload structs: `CompetitionResultPayload`, `CompetitionRegistrationPayload`, `CompetitionScoreUpdatePayload`, `WarehouseExportReadyPayload`, `WarehouseSyncCompletePayload`, `WarehouseSchemaChangePayload`
- Typed dispatcher methods on `WebhookService`: `DispatchCompetitionResult(...)`, `DispatchWarehouseExportReady(...)`, etc. — these set the event type and call `DispatchForOrg` with the correct payload
- `AllEventTypes()` registry for validation — the internal webhook receiver rejects unknown event types
- Updated admin webhooks UI (`admin_webhooks.templ`) to include competition and warehouse event options in the registration dropdown
