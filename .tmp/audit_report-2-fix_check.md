# Full Follow-up Report (4 Problems)

Date: 2026-04-09  
Type: Follow-up verification report (not an audit)  
Boundary: Static verification only (no runtime execution, no tests run)

## Source of Follow-up Items
Problems verified here are the 4 items listed in the previous inspection report (`.tmp/audit-report.md`, Section 5 Issues 1-4).

## Summary
- Total problems checked: 4
- Fixed: 4
- Not fixed: 0

## Problem-by-Problem Verification

### Problem 1
- Original issue: Client conflict-check failure path auto-submitted booking.
- Current status: **Fixed**
- Evidence:
  - Error path now shows warning + retry UI only: `static/js/app.js:366`, `static/js/app.js:369`, `static/js/app.js:371`, `static/js/app.js:373`
  - `form.submit()` remains only on success/no-conflict path: `static/js/app.js:359`, `static/js/app.js:362`
- Verification conclusion: API pre-check failure no longer forces automatic submission.

### Problem 2
- Original issue: README `GIN_MODE` default mismatched compose default.
- Current status: **Fixed**
- Evidence:
  - README config table shows `GIN_MODE` default `debug`: `README.md:548`
  - Compose default is `debug`: `docker-compose.yml:43`
- Verification conclusion: documentation and compose defaults are aligned.

### Problem 3
- Original issue: No static test evidence for org-scoped webhook dispatch + retry body integrity.
- Current status: **Fixed**
- Evidence:
  - Org isolation dispatch test exists: `internal/services/webhook_test.go:22`, `internal/services/webhook_test.go:24`, `internal/services/webhook_test.go:65`, `internal/services/webhook_test.go:74`, `internal/services/webhook_test.go:70`, `internal/services/webhook_test.go:71`
  - Retry payload/signature integrity test exists: `internal/services/webhook_test.go:82`, `internal/services/webhook_test.go:86`, `internal/services/webhook_test.go:157`, `internal/services/webhook_test.go:160`, `internal/services/webhook_test.go:162`
  - All-retries-fail logging test exists: `internal/services/webhook_test.go:176`, `internal/services/webhook_test.go:178`, `internal/services/webhook_test.go:215`, `internal/services/webhook_test.go:217`
- Verification conclusion: previously missing regression coverage has been added.

### Problem 4
- Original issue: Legacy broad webhook `Dispatch` API remained available and could be reused.
- Current status: **Fixed**
- Evidence:
  - Legacy method not found in `integration.go` (search result): `NOT_FOUND`
  - Org-scoped dispatch method is the available API: `internal/services/integration.go:288`
  - Safety guard blocks `orgID == 0`: `internal/services/integration.go:289`, `internal/services/integration.go:291`
  - Tenant-scoped endpoint query enforced: `internal/services/integration.go:294`
- Verification conclusion: broad dispatch path is removed; tenant-safety guard is in place.

## Final Follow-up Conclusion
All 4 previously reported problems are fixed based on current static code and test evidence.
