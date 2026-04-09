# Follow-up Check Update: Previous Inspection Issues

Date: 2026-04-09
Type: Targeted follow-up recheck (static-only)

## Checked Items
Rechecked only the issues from the previous follow-up check:
1. Reporting path should use org-scoped materialized views.
2. `/api/internal/*` integration coverage should exist for 401/403/200 and org-scoping.
3. README API/HMAC wording consistency.

## Recheck Results

| Item | Current Status | Evidence |
|---|---|---|
| Reporting path uses org-scoped materialized views | **Still Fixed** | MV definitions include `organization_id`: `internal/models/database.go:163-199`; unique indexes updated: `internal/models/database.go:204-206`; reporting methods query MVs with org filter: `internal/services/integration.go:342-353`, `internal/services/integration.go:358-361`; scheduled refresh remains: `internal/services/integration.go:327-333`. |
| `/api/internal/*` integration coverage (authz + org scope) | **Still Fixed** | Internal group/middleware in integration router: `internal/handlers/integration_test.go:132-143`; 401/403/200 tests: `internal/handlers/integration_test.go:716`, `:733`, `:767`, `:781`; org-scoped report tests: `internal/handlers/integration_test.go:839`, `:887`, `:923`; org-scoped webhook endpoint test: `internal/handlers/integration_test.go:957`; clinician encounter org-isolation test: `internal/handlers/integration_test.go:993-1044`. |
| README API/HMAC wording consistency | **Still Fixed** | Internal section aligned: `README.md:466-475`; auth model explicitly states token+HMAC on all `/api/*` and admin requirement on `/api/internal/*`: `README.md:486`, `README.md:490-492`. |

## Conclusion
All previously tracked issues remain fixed based on current static evidence.

## Boundary
No runtime execution was performed. Runtime behavior remains **Manual Verification Required**.
