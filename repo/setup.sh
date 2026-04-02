#!/bin/bash
# Optional setup script — generates .env with production-grade secrets and starts the portal.
# Not required for development: `docker compose up --build` works without it.
# Usage: bash setup.sh

set -e

ENV_FILE=".env"

if [ ! -f "$ENV_FILE" ]; then
    echo "Generating $ENV_FILE with random secrets..."

    # Generate a 32-byte base64-encoded encryption key
    if command -v openssl &>/dev/null; then
        KEY=$(openssl rand -base64 32)
    elif command -v python3 &>/dev/null; then
        KEY=$(python3 -c "import base64,os;print(base64.b64encode(os.urandom(32)).decode())")
    else
        echo "ERROR: Need openssl or python3 to generate encryption key."
        echo "Create .env manually from .env.example"
        exit 1
    fi

    cat > "$ENV_FILE" <<EOF
# Auto-generated secrets. For production, regenerate all values.
FIELD_ENCRYPTION_KEY=$KEY
EOF
    echo "Created $ENV_FILE"
else
    echo "$ENV_FILE already exists, using existing secrets."
fi

echo ""
echo "Starting Campus Wellness Portal..."
echo ""
docker compose up --build
