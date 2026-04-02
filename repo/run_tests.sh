#!/bin/bash
# Run all tests for the Campus Wellness Portal.
#
# Usage:
#   bash run_tests.sh          # Run all tests (requires PostgreSQL)
#   bash run_tests.sh --unit   # Run only unit tests (no DB required)
#
# The test suite requires:
#   - FIELD_ENCRYPTION_KEY env var (or it will be set automatically below)
#   - PostgreSQL running on localhost:5432 for integration/service tests
#     (these skip gracefully if DB is not available)

set -e

# Ensure encryption key is set for crypto tests
if [ -z "$FIELD_ENCRYPTION_KEY" ]; then
    export FIELD_ENCRYPTION_KEY=$(openssl rand -base64 32 2>/dev/null || python3 -c "import base64,os;print(base64.b64encode(os.urandom(32)).decode())" 2>/dev/null || echo "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
fi

# Generate Templ components if templ CLI is available
if command -v templ &>/dev/null; then
    templ generate ./internal/views/
fi

if [ "$1" = "--unit" ]; then
    echo "=== Running unit tests only (no database required) ==="
    echo ""
    go test ./internal/middleware/ ./internal/services/ -v -count=1 \
        -run "TestRequire|TestData|TestHMAC|TestRate|TestCSRF|TestEncrypt|TestDecrypt"
    echo ""
    echo "=== Unit tests complete ==="
else
    echo "=== Running full test suite ==="
    echo ""
    echo "Packages: auth, handlers, middleware, services"
    echo "Note: DB-dependent tests skip if PostgreSQL is not available."
    echo ""
    go test ./... -v -count=1
    echo ""
    echo "=== All tests complete ==="
fi
