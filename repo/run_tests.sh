#!/bin/bash
# Campus Wellness & Training Portal — Test Runner
#
# Test organization:
#   tests/unit_tests/   — Middleware, crypto, config (no DB required)
#   tests/api_tests/    — Auth, handlers, booking, menu, webhook, health,
#                         SSO sync, CSV watcher, audit, integration contracts
#                         (requires DB)
#
# The actual runnable test files live in internal/ (Go requirement).
# The tests/ directory contains classified copies for reference.
#
# Usage:
#   bash run_tests.sh              # Run ALL tests
#   bash run_tests.sh --unit       # Unit tests only (no DB)
#   bash run_tests.sh --api        # API/integration tests only (requires DB)
#   bash run_tests.sh --coverage   # Full suite with coverage report
#   bash run_tests.sh --docker     # Run tests inside Docker (self-contained)

set -e

# Ensure encryption key is set for crypto tests
if [ -z "$FIELD_ENCRYPTION_KEY" ]; then
    export FIELD_ENCRYPTION_KEY=$(openssl rand -base64 32 2>/dev/null || \
        python3 -c "import base64,os;print(base64.b64encode(os.urandom(32)).decode())" 2>/dev/null || \
        echo "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
fi

# Generate Templ components if templ CLI is available
if command -v templ &>/dev/null; then
    templ generate ./internal/views/
fi

# ── Test packages mapped to tests/ classification ──
#
# tests/unit_tests/ sources:
UNIT_MIDDLEWARE="./internal/middleware/"   # middleware_test.go
UNIT_SERVICES="./internal/services/"      # crypto_test.go
UNIT_CONFIG="./internal/config/"          # config_test.go
UNIT_PATTERN="TestRequire|TestData|TestHMAC|TestRate|TestCSRF|TestEncrypt|TestDecrypt|TestEnforce|TestIsInternal|TestMaskSSN|TestMaskEmail|TestComputeFingerprint"
#
# tests/api_tests/ sources:
API_HANDLERS="./internal/handlers/"       # integration_test.go, e2e_test.go
API_AUTH="./internal/auth/"               # auth_test.go
API_SERVICES="./internal/services/"       # booking_test.go, menu_test.go, webhook_test.go,
                                          # health_test.go, audit_test.go, sso_sync_test.go,
                                          # csv_watcher_test.go, integration_contracts_test.go

case "${1:-all}" in
    --unit)
        echo "=========================================="
        echo "  UNIT TESTS  (tests/unit_tests/)"
        echo "  No database required"
        echo "=========================================="
        echo ""
        echo "Source: internal/middleware/middleware_test.go"
        go test $UNIT_MIDDLEWARE -v -count=1
        echo ""
        echo "Source: internal/services/crypto_test.go + PII masking"
        go test $UNIT_SERVICES -v -count=1 -run "$UNIT_PATTERN"
        echo ""
        echo "Source: internal/config/config_test.go"
        go test $UNIT_CONFIG -v -count=1
        echo ""
        echo "=== Unit tests complete ==="
        ;;

    --api)
        echo "=========================================="
        echo "  API / INTEGRATION TESTS  (tests/api_tests/)"
        echo "  Requires PostgreSQL"
        echo "=========================================="
        echo ""
        echo "Source: internal/auth/auth_test.go"
        go test $API_AUTH -v -count=1
        echo ""
        echo "Source: internal/handlers/integration_test.go + e2e_test.go"
        go test $API_HANDLERS -v -count=1
        echo ""
        echo "Source: internal/services/{booking,menu,webhook,health,audit,sso_sync,csv_watcher,integration_contracts}_test.go"
        go test $API_SERVICES -v -count=1
        echo ""
        echo "=== API tests complete ==="
        ;;

    --coverage)
        echo "=========================================="
        echo "  FULL SUITE WITH COVERAGE"
        echo "=========================================="
        echo ""
        go test ./... -v -count=1 -coverprofile=coverage.out
        go tool cover -func=coverage.out | tail -1
        echo ""
        echo "Coverage report: coverage.out"
        echo "View HTML: go tool cover -html=coverage.out"
        echo ""
        echo "=== Coverage run complete ==="
        ;;

    --docker)
        echo "=========================================="
        echo "  DOCKER-CONTAINED TEST SUITE"
        echo "  Self-contained: starts DB + runs tests"
        echo "=========================================="
        echo ""
        # Start postgres in the background, run tests, then tear down
        docker compose up -d postgres
        echo "Waiting for PostgreSQL to be ready..."
        for i in $(seq 1 30); do
            if docker compose exec -T postgres pg_isready -U campus_admin -d campus_portal >/dev/null 2>&1; then
                echo "PostgreSQL is ready."
                break
            fi
            sleep 1
        done
        echo ""
        echo "Running full test suite..."
        export TEST_DATABASE_URL="host=localhost port=5432 user=campus_admin password=campus_secret dbname=campus_portal sslmode=disable"
        go test ./... -v -count=1 -coverprofile=coverage.out
        go tool cover -func=coverage.out | tail -1
        echo ""
        docker compose stop postgres
        echo "=== Docker test run complete ==="
        ;;

    all|"")
        echo "=========================================="
        echo "  FULL TEST SUITE"
        echo "=========================================="
        echo ""
        echo "Unit tests   → tests/unit_tests/"
        echo "API tests    → tests/api_tests/"
        echo ""
        echo "── Unit tests (middleware, crypto, config) ──"
        go test $UNIT_MIDDLEWARE -v -count=1
        go test $UNIT_SERVICES -v -count=1 -run "$UNIT_PATTERN"
        go test $UNIT_CONFIG -v -count=1
        echo ""
        echo "── API tests (auth, handlers, services) ──"
        go test $API_AUTH -v -count=1
        go test $API_HANDLERS -v -count=1
        go test $API_SERVICES -v -count=1
        echo ""
        echo "=== All tests complete ==="
        ;;

    *)
        echo "Usage: bash run_tests.sh [--unit|--api|--coverage|--docker]"
        echo ""
        echo "  --unit       Unit tests only (no DB required)"
        echo "  --api        API/integration tests only (requires PostgreSQL)"
        echo "  --coverage   Full suite with coverage report"
        echo "  --docker     Self-contained: starts DB in Docker, runs all tests"
        echo "  (no flag)    Run all tests"
        echo ""
        echo "Test classification:"
        echo "  tests/unit_tests/  — middleware, crypto, config, PII masking"
        echo "  tests/api_tests/   — auth, handlers, booking, menu, webhook,"
        echo "                       health, audit, SSO sync, CSV watcher,"
        echo "                       integration contracts, e2e flows"
        exit 1
        ;;
esac
