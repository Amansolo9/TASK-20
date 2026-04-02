.PHONY: run test build docker generate setup-db

# Generate Templ components
generate:
	templ generate ./internal/views/

# Build the server binary
build: generate
	go build -o campus-portal ./cmd/server

# Run locally (requires PostgreSQL and env vars)
run: generate
	go run ./cmd/server

# Run all tests
test:
	go test ./... -v -count=1

# Run tests that don't require PostgreSQL
test-unit:
	go test ./internal/middleware/ ./internal/services/ -v -count=1 -run "TestRequire|TestData|TestHMAC|TestRate|TestEncrypt|TestDecrypt|TestCSRF"

# Build and start with Docker Compose
docker:
	docker compose up --build

# Create local PostgreSQL database (run once)
setup-db:
	@echo "Creating database..."
	createdb -U campus_admin campus_portal 2>/dev/null || echo "Database may already exist"
	@echo "Set these env vars:"
	@echo "  export DB_HOST=localhost DB_PORT=5432 DB_USER=campus_admin DB_PASSWORD=campus_secret DB_NAME=campus_portal"
	@echo "  export FIELD_ENCRYPTION_KEY=$$(openssl rand -base64 32)"
	@echo "Then run: make run"
