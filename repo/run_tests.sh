#!/bin/bash
# Campus Wellness & Training Portal — Test Runner
#
# Test organization:
#   tests/unit_tests/   — Middleware, crypto (no DB required)
#   tests/api_tests/    — Auth, handlers, booking, menu, webhook (requires DB)
#
# The actual runnable test files live in internal/ (Go requirement).
# The tests/ directory contains classified copies for reference.
#
# Usage:
#   bash run_tests.sh              # Run ALL tests
#   bash run_tests.sh --unit       # Unit tests only (no DB)
#   bash run_tests.sh --api        # API/integration tests only (requires DB)
#   bash run_tests.sh --coverage   # Full suite with coverage report

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
UNIT_PATTERN="TestRequire|TestData|TestHMAC|TestRate|TestCSRF|TestEncrypt|TestDecrypt|TestEnforce|TestIsInternal"
#
# tests/api_tests/ sources:
API_HANDLERS="./internal/handlers/"       # integration_test.go
API_AUTH="./internal/auth/"               # auth_test.go
API_SERVICES="./internal/services/"       # booking_test.go, menu_test.go, webhook_test.go

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
        echo "Source: internal/services/crypto_test.go"
        go test $UNIT_SERVICES -v -count=1 -run "$UNIT_PATTERN"
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
        echo "Source: internal/handlers/integration_test.go"
        go test $API_HANDLERS -v -count=1
        echo ""
        echo "Source: internal/services/{booking,menu,webhook}_test.go"
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

    all|"")
        echo "=========================================="
        echo "  FULL TEST SUITE"
        echo "=========================================="
        echo ""
        echo "Unit tests   → tests/unit_tests/"
        echo "API tests    → tests/api_tests/"
        echo ""
        echo "── Unit tests (middleware, crypto) ──"
        go test $UNIT_MIDDLEWARE -v -count=1
        go test $UNIT_SERVICES -v -count=1 -run "$UNIT_PATTERN"
        echo ""
        echo "── API tests (auth, handlers, services) ──"
        go test $API_AUTH -v -count=1
        go test $API_HANDLERS -v -count=1
        go test $API_SERVICES -v -count=1
        echo ""
        echo "=== All tests complete ==="
        ;;

    *)
        echo "Usage: bash run_tests.sh [--unit|--api|--coverage]"
        echo ""
        echo "  --unit       Unit tests only (no DB required)"
        echo "  --api        API/integration tests only (requires PostgreSQL)"
        echo "  --coverage   Full suite with coverage report"
        echo "  (no flag)    Run all tests"
        echo ""
        echo "Test classification:"
        echo "  tests/unit_tests/  — middleware, crypto"
        echo "  tests/api_tests/   — auth, handlers, booking, menu, webhook"
        exit 1
        ;;
esac
