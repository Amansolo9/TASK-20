# ---- Build stage ----
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git

# Install templ CLI for code generation
RUN go install github.com/a-h/templ/cmd/templ@latest

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Generate Templ components and build the binary
RUN templ generate ./internal/views/
RUN CGO_ENABLED=0 GOOS=linux go build -o /campus-portal ./cmd/server

# ---- Runtime stage ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /campus-portal /app/campus-portal

# Copy templates, static assets, and migration files
COPY --from=builder /app/internal/templates/ /app/internal/templates/
COPY --from=builder /app/static/ /app/static/
COPY --from=builder /app/migrations/ /app/migrations/

# Create directories the app expects at runtime
RUN mkdir -p /app/uploads /app/watched_folder

EXPOSE 8080

ENTRYPOINT ["/app/campus-portal"]
