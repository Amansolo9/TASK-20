# Test Coverage Audit

## Scope and Method
- Audit type: static inspection only (no runtime execution).
- Endpoint source of truth: `repo/cmd/server/main.go` route registrations.
- Runnable test source of truth: `repo/internal/**/*_test.go`.
- `repo/tests/**/*_test.go` are non-runnable mirrors (`//go:build ignore`), evidenced at `repo/tests/api_tests/integration_test.go:1`, `repo/tests/unit_tests/middleware_test.go:1`.

## Backend Endpoint Inventory
Evidence source: `repo/cmd/server/main.go:99-209`.

| # | Endpoint |
|---|---|
| 1 | GET /login |
| 2 | POST /login |
| 3 | GET / |
| 4 | GET /logout |
| 5 | POST /api/tokens |
| 6 | GET /dashboard |
| 7 | GET /health/download/:id |
| 8 | GET /health/history |
| 9 | POST /health/upload |
| 10 | POST /health/update |
| 11 | GET /clinician |
| 12 | POST /clinician/encounter |
| 13 | POST /clinician/vitals |
| 14 | GET /bookings |
| 15 | POST /bookings |
| 16 | POST /bookings/:id/transition |
| 17 | GET /bookings/:id/audit |
| 18 | GET /menu |
| 19 | POST /menu/order |
| 20 | GET /menu/manage |
| 21 | POST /menu/manage/category |
| 22 | POST /menu/manage/item |
| 23 | POST /menu/manage/item/:id/sold-out |
| 24 | POST /menu/manage/item/:id/sell-windows |
| 25 | POST /menu/manage/item/:id/substitutes |
| 26 | POST /menu/manage/item/:id/choices |
| 27 | POST /menu/manage/blackout |
| 28 | POST /menu/manage/blackout/:id/delete |
| 29 | POST /menu/manage/promotion |
| 30 | GET /admin/users |
| 31 | GET /admin/register |
| 32 | POST /admin/register |
| 33 | POST /admin/users/:id/toggle |
| 34 | POST /admin/users/:id/role |
| 35 | POST /admin/users/:id/temp-access |
| 36 | POST /admin/users/:id/reset-password |
| 37 | GET /admin/performance |
| 38 | POST /admin/refresh-views |
| 39 | GET /admin/webhooks |
| 40 | POST /admin/webhooks |
| 41 | GET /admin/bookings |
| 42 | POST /admin/reports |
| 43 | GET /admin/reports/:id |
| 44 | GET /api/slots |
| 45 | GET /api/match-partners |
| 46 | GET /api/check-conflicts |
| 47 | GET /api/price |
| 48 | GET /api/internal/clinic-utilization |
| 49 | GET /api/internal/booking-fill-rates |
| 50 | GET /api/internal/menu-sell-through |
| 51 | POST /api/internal/webhooks/receive |

## API Test Mapping Table
All production endpoints are exercised by HTTP tests via the real Gin router + middleware + real handlers/services in `setupRouter` (`repo/internal/handlers/integration_test.go:57-228`).

| Endpoint | Covered | Test type | Test files | Evidence |
|---|---|---|---|---|
| GET /login | yes | true no-mock HTTP | `internal/handlers/integration_test.go`, `internal/handlers/e2e_test.go` | requests at `integration_test.go:271`, `e2e_test.go:26` |
| POST /login | yes | true no-mock HTTP | same | `integration_test.go:302`, `e2e_test.go:39` |
| GET / | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:132` |
| GET /logout | yes | true no-mock HTTP | both | `integration_test.go:407`, `e2e_test.go:65` |
| POST /api/tokens | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:150` |
| GET /dashboard | yes | true no-mock HTTP | both | `integration_test.go:358`, `e2e_test.go:49` |
| GET /health/download/:id | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:170` |
| GET /health/history | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:183`, `e2e_test.go:193` |
| POST /health/upload | yes | true no-mock HTTP | `internal/handlers/integration_test.go` | `integration_test.go:442`, `integration_test.go:462` |
| POST /health/update | yes | true no-mock HTTP | both | `integration_test.go:777`, `e2e_test.go:926` |
| GET /clinician | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:207`, `e2e_test.go:220` |
| POST /clinician/encounter | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:243` |
| POST /clinician/vitals | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:270`, `e2e_test.go:293` |
| GET /bookings | yes | true no-mock HTTP | both | `integration_test.go:502`, `e2e_test.go:57` |
| POST /bookings | yes | true no-mock HTTP | both | `integration_test.go` route exercised, explicit e2e at `e2e_test.go:89` |
| POST /bookings/:id/transition | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:107` |
| GET /bookings/:id/audit | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:118` |
| GET /menu | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:309` |
| POST /menu/order | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:326` |
| GET /menu/manage | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:340`, `e2e_test.go:353` |
| POST /menu/manage/category | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:370` |
| POST /menu/manage/item | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:401` |
| POST /menu/manage/item/:id/sold-out | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:423` |
| POST /menu/manage/item/:id/sell-windows | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:448` |
| POST /menu/manage/item/:id/substitutes | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:470` |
| POST /menu/manage/item/:id/choices | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:493` |
| POST /menu/manage/blackout | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:512` |
| POST /menu/manage/blackout/:id/delete | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:537` |
| POST /menu/manage/promotion | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:561` |
| GET /admin/users | yes | true no-mock HTTP | both | `integration_test.go:478`, `e2e_test.go:903` |
| GET /admin/register | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:577` |
| POST /admin/register | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:599` |
| POST /admin/users/:id/toggle | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:626` |
| POST /admin/users/:id/role | yes | true no-mock HTTP | `internal/handlers/integration_test.go` | `integration_test.go:560` |
| POST /admin/users/:id/temp-access | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:655` |
| POST /admin/users/:id/reset-password | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:679` |
| GET /admin/performance | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:693` |
| POST /admin/refresh-views | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:709` |
| GET /admin/webhooks | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:724` |
| POST /admin/webhooks | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:743` |
| GET /admin/bookings | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:757` |
| POST /admin/reports | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:774` |
| GET /admin/reports/:id | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:797` |
| GET /api/slots | yes | true no-mock HTTP | both | `integration_test.go:583`, `e2e_test.go:980` |
| GET /api/match-partners | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:812` |
| GET /api/check-conflicts | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:825` |
| GET /api/price | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:840` |
| GET /api/internal/clinic-utilization | yes | true no-mock HTTP | `internal/handlers/integration_test.go` | `integration_test.go:936` |
| GET /api/internal/booking-fill-rates | yes | true no-mock HTTP | `internal/handlers/integration_test.go` | `integration_test.go:983` |
| GET /api/internal/menu-sell-through | yes | true no-mock HTTP | `internal/handlers/integration_test.go` | `integration_test.go:1015` |
| POST /api/internal/webhooks/receive | yes | true no-mock HTTP | `internal/handlers/e2e_test.go` | `e2e_test.go:862`, `e2e_test.go:884` |

## API Test Classification
1. True No-Mock HTTP
- `repo/internal/handlers/integration_test.go`
- `repo/internal/handlers/e2e_test.go`
- Evidence: bootstrapped app/router and middleware stack in `setupRouter` (`integration_test.go:57-228`) and requests through `r.ServeHTTP`.

2. HTTP with Mocking
- None detected.

3. Non-HTTP (unit/integration without HTTP)
- `repo/internal/auth/auth_test.go`, `repo/internal/middleware/middleware_test.go`, `repo/internal/config/config_test.go`, `repo/internal/services/*_test.go`.

## Mock Detection
- No mocking/stubbing library usage found in tests.
- Static scan for `jest.mock`, `vi.mock`, `sinon.stub`, `gomock`, `testify/mock`, `sqlmock`, `httpmock`, `gock` returned no hits in runnable tests.

## Coverage Summary
- Total endpoints: 51
- Endpoints with HTTP tests: 51
- Endpoints with true no-mock HTTP tests: 51
- HTTP coverage: 100%
- True API coverage: 100%

## Unit Test Summary
Runnable unit/integration test modules:
- Handlers: `repo/internal/handlers/integration_test.go`, `repo/internal/handlers/e2e_test.go`
- Auth: `repo/internal/auth/auth_test.go`
- Middleware/guards/authn/authz: `repo/internal/middleware/middleware_test.go`
- Services: `repo/internal/services/{audit,booking,crypto,csv_watcher,health,integration_contracts,menu,sso_sync,webhook}_test.go`
- Config: `repo/internal/config/config_test.go`

Important modules not directly tested:
- `repo/cmd/server/main.go` startup/background worker orchestration (integration router mirrors behavior but does not execute `main()` lifecycle).
- Full browser automation layer (no Playwright/Cypress-style UI automation; tests are HTTP-level).

## API Observability Check
- Strong overall.
- Tests generally show method+path, payload/query, status, and response content.
- Evidence examples: `e2e_test.go:812-840` (`/api/match-partners`, `/api/check-conflicts`, `/api/price`), `e2e_test.go:862-884` (`/api/internal/webhooks/receive`).
- Residual weak spots: some gate tests still assert status only (auth-denial loops), but this is limited and acceptable for gate checks.

## Test Quality and Sufficiency
- Success paths: covered broadly across auth, health, booking, menu, admin, internal APIs.
- Failure/edge/validation paths: present (invalid CSRF, invalid dates, missing token/HMAC, invalid payload/event type, field limits, role denial).
- Auth/permissions: strong RBAC and API auth gate coverage.
- Integration boundaries: good DB-backed coverage in handler/service tests.
- Assertion quality: mostly meaningful, not superficial.

## Tests Check
- `run_tests.sh` includes Docker-contained mode (`--docker`) using Compose-managed Postgres (`repo/run_tests.sh:100-123`) -> OK.
- Same script also supports local dependency modes (`bash run_tests.sh`, `--api`, `--coverage`) that rely on host `go test` and reachable DB (`repo/run_tests.sh:75-81`, `135-142`) -> FLAG under strict Docker-only preference.

## End-to-End Expectations
- Project type: fullstack web app.
- FE?BE verification exists at HTTP/system-flow level (`repo/internal/handlers/e2e_test.go`), but there is no browser automation framework coverage.
- Given complete endpoint HTTP coverage and strong service/unit coverage, this is a reasonable partial substitute, but not equivalent to true browser E2E.

## Test Coverage Score (0-100)
- 91/100

## Score Rationale
- + Full endpoint HTTP coverage with real handler execution.
- + No over-mocking detected.
- + Strong security/validation path testing.
- - No browser-driven fullstack E2E automation.
- - `run_tests.sh` still includes local dependency execution modes.

## Key Gaps
- Browser automation tests are absent.
- Startup/background-worker orchestration in `main.go` is not directly test-executed.

## Confidence and Assumptions
- Confidence: high.
- Assumptions:
- Coverage classification is based on static evidence in test code; runtime pass/fail is out of scope.
- Production endpoint set is exactly what is registered in `repo/cmd/server/main.go`.

---

# README Audit

## Project Type Detection
- Declared at top: `Project type: Fullstack web application` (`repo/README.md:3`) -> PASS.

## README Location
- `repo/README.md` exists -> PASS.

## Hard Gates

### Formatting
- PASS. Structured sections, tables, and readable markdown.

### Startup Instructions
- PASS. Includes required literal `docker-compose up` command: `docker-compose up --build` (`repo/README.md:60`).
- Also includes v2 equivalent `docker compose up --build` (`repo/README.md:65`).

### Access Method
- PASS. URL + port documented: `http://localhost:8080` (`repo/README.md:68`).

### Verification Method
- PASS. Explicit post-start verification checklist and curl check are present (`repo/README.md:92-104`).

### Environment Rules (strict)
- PASS. No forbidden package-install instructions (`npm install`, `pip install`, `apt-get`) and no manual DB setup steps in README.

### Demo Credentials
- PASS. Auth exists and README provides credentials and roles (`repo/README.md:148-160`).

## Engineering Quality
- Tech stack clarity: strong.
- Architecture explanation: strong.
- Testing instructions: clear and actionable.
- Security/roles/workflows: detailed.
- Presentation quality: high.

## High Priority Issues
- None.

## Medium Priority Issues
- README test section still documents non-Docker test modes (`bash run_tests.sh`, `--api`, `--coverage`) that can depend on local environment; this is not a hard-gate failure but weakens strict reproducibility.

## Low Priority Issues
- Minor command duality (`docker-compose` and `docker compose`) is harmless but slightly redundant.

## Hard Gate Failures
- None.

## README Verdict
- PASS.

## Final Verdicts
- Test Coverage Audit Verdict: PASS.
- README Audit Verdict: PASS.
