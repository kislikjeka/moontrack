#!/bin/bash
# Script to sync root .env to backend and frontend directories
# This ensures compatibility with tools that expect .env in specific locations

set -e

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROOT_ENV="$ROOT_DIR/.env"

# Check if root .env exists
if [ ! -f "$ROOT_ENV" ]; then
    echo "❌ Root .env file not found at: $ROOT_ENV"
    echo "Run: just env-init"
    exit 1
fi

# Create backend .env with backend-specific variables
echo "📝 Creating backend .env..."
cat > "$ROOT_DIR/apps/backend/.env" << EOF
# This file is auto-generated from root .env
# DO NOT EDIT - edit root .env instead and run: scripts/sync-env.sh

# Source root .env variables
$(cat "$ROOT_ENV")
EOF

# Create frontend .env with frontend-specific variables (VITE_ prefix)
echo "📝 Creating frontend .env..."
cat > "$ROOT_DIR/apps/frontend/.env" << EOF
# This file is auto-generated from root .env
# DO NOT EDIT - edit root .env instead and run: scripts/sync-env.sh

# Vite requires VITE_ prefix for environment variables
VITE_API_BASE_URL=http://\${API_HOST}:\${API_PORT}/api/v1
EOF

# Also add specific Vite vars from root .env
grep "^VITE_" "$ROOT_ENV" >> "$ROOT_DIR/apps/frontend/.env" 2>/dev/null || true

echo "✅ Environment files synced:"
echo "   - apps/backend/.env"
echo "   - apps/frontend/.env"
