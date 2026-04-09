# API & Integration Tests

Tests that require a **running PostgreSQL database**. Cover HTTP handlers, authentication, RBAC, tenant isolation, and business logic.

## Files in this directory

| File | Package | Source of truth |
|------|---------|-----------------|
| `integration_test.go` | `handlers` | `internal/handlers/integration_test.go` |
| `auth_test.go` | `auth` | `internal/auth/auth_test.go` |
| `booking_test.go` | `services` | `internal/services/booking_test.go` |
| `menu_test.go` | `services` | `internal/services/menu_test.go` |
| `webhook_test.go` | `services` | `internal/services/webhook_test.go` |
| `testdb_test.go` | `services` | `internal/services/testdb_test.go` (shared DB helper) |

> **Note:** Go requires `_test.go` files to live alongside their package source for `go test` to work. The files here are **copies** for organizational reference. The canonical (runnable) versions are in `internal/`. Always edit the originals.

## Running

```bash
# API tests only
bash run_tests.sh --api

# Full suite
bash run_tests.sh

# With coverage
bash run_tests.sh --coverage
```

## Prerequisites

- PostgreSQL on `localhost:5432` — database `campus_portal`, user `campus_admin`
- Or: `docker compose up -d` to start required Postgres

## Test Count: 76+

### auth_test.go — 12 tests (authentication service)

| Test | What it verifies |
|------|------------------|
| `TestRegister` | User registration succeeds |
| `TestRegister_DuplicateUsername` | Duplicate username rejected |
| `TestLogin_Success` | Valid credentials return session |
| `TestLogin_WrongPassword` | Bad password rejected |
| `TestLogin_NonExistentUser` | Unknown user rejected |
| `TestValidateSession` | Valid session returns user |
| `TestValidateSession_Expired` | Expired session rejected |
| `TestValidateSession_Invalid` | Fake session rejected |
| `TestLogout` | Session destroyed |
| `TestGrantTempAccess` | Temporary elevated role stored |
| `TestRevertExpiredAccess` | Auto-revert after expiry |
| `TestChangeUserRole` | Admin role change persists |

### integration_test.go — 55+ tests (full HTTP stack)

**Auth & sessions (8):**
`TestLoginPage_Returns200`, `TestLogin_ValidCredentials_RedirectsToDashboard`, `TestLogin_InvalidCredentials_ShowsError`, `TestLogin_MissingCSRF_ShowsError`, `TestDashboard_WithoutAuth_RedirectsToLogin`, `TestDashboard_WithValidSession_Returns200`, `TestDashboard_WithExpiredSession_RedirectsToLogin`, `TestLogout_ClearsSession`

**RBAC (4):**
`TestAdminEndpoint_DeniedForStudent`, `TestAdminEndpoint_AllowedForAdmin`, `TestStaffCannotUpload`, `TestStaffCannotUpdateHealthRecord`

**Upload & scope (3):**
`TestUpload_RejectsOversizedFile`, `TestUpload_RejectsDisallowedType`, `TestStudentCanUploadToSelf`, `TestStudentCannotUploadToOtherUser`, `TestStudentCannotViewOtherStudentDashboard`

**API token + HMAC (5):**
`TestAPIRoute_ValidTokenAndHMAC_Succeeds`, `TestAPIRoute_TokenWithoutHMAC_Fails`, `TestAPIRoute_MissingToken_Fails`, `TestAPIRoute_InvalidToken_Fails`, `TestAPIRoute_SessionCookieAlone_Rejected`

**CSRF integration (2):**
`TestCSRF_IntegrationRouter_PostWithValidCSRF`, `TestCSRF_IntegrationRouter_PostWithoutCSRF_Fails`

**Org isolation (2):**
`TestAdminUsersPage_OnlyShowsSameOrg`, `TestAdminRoleChange_ProducesAuditLog`

**Internal API — auth + RBAC + org (10):**
`TestInternalAPI_MissingToken_Returns401`, `TestInternalAPI_TokenWithoutHMAC_Returns401`, `TestInternalAPI_InvalidHMAC_Returns401`, `TestInternalAPI_NonAdminRole_Returns403`, `TestInternalAPI_AdminRole_Returns200`, `TestInternalAPI_OrgScopedReports_ClinicUtilization`, `TestInternalAPI_OrgScopedReports_BookingFillRates`, `TestInternalAPI_OrgScopedReports_MenuSellThrough`, `TestInternalAPI_WebhookEndpoints_OrgScoped`

**Clinician encounters (3):**
`TestClinicianEncounters_OrgIsolation_NoCrossOrgLeak`, `TestClinicianEncounters_SameOrgAuthorizedDept_Succeeds`, `TestClinicianEncounters_DifferentDept_ReturnsEmpty`

**Reporting service (3):**
`TestReportingService_QueriesMaterializedViews`, `TestReportingService_RefreshTargetsCorrectViews`, `TestReportingService_OrgIsolation_NoLeakAcrossOrgs`

### booking_test.go — 8 tests

`TestTransitionBooking_ValidTransitions`, `TestTransitionBooking_InvalidTransitions`, `TestTransitionBooking_TwoHourCancellationRule`, `TestCheckConflicts_RequesterConflict`, `TestCheckConflicts_VirtualVenueNoOverlap`, `TestGetAvailableSlots`, `TestBookingAuditTrail`, `TestMatchPartners`

### menu_test.go — 8 tests

`TestCalculatePrice_DineIn`, `TestCalculatePrice_Takeout`, `TestCalculatePrice_MemberDiscount`, `TestCalculatePrice_WithPromotion`, `TestCalculatePrice_NotFound`, `TestIsWithinSellWindow_NoWindowsAlwaysAvailable`, `TestIsWithinSellWindow_HolidayBlackout`, `TestCreateOrder_SoldOutReject`, `TestCreateOrder_Success`, `TestSubstitutes`

### webhook_test.go — 3 tests

`TestDispatchForOrg_OrgIsolation`, `TestDeliver_RetryBodyIntegrity`, `TestDeliver_AllRetriesFail`
