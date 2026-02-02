# Environment Configuration Guide

## Overview

MoonTrack uses a **centralized `.env` file** in the project root for all configuration. This single file is used by:

- **Docker Compose** - PostgreSQL and Redis containers
- **Just commands** - All development commands
- **Backend** - Go API server (auto-synced)
- **Frontend** - React app (auto-synced)

## Quick Start

### 1. Initialize .env file

```bash
# Create .env from template
just env-init

# Or manually
cp .env.example .env
```

### 2. Edit configuration

Edit `.env` and update these values:

```env
# Change the password!
POSTGRES_PASSWORD=your_secure_password

# Change the JWT secret (min 32 chars)
JWT_SECRET=your-very-long-and-secure-jwt-secret-key-here

# Get free API key from https://www.coingecko.com/en/api
COINGECKO_API_KEY=your-coingecko-api-key
```

### 3. Verify configuration

```bash
# Check .env values (passwords hidden)
just env

# Validate configuration
just env-validate
```

### 4. Start infrastructure

```bash
# This automatically syncs .env to apps/backend and apps/frontend
just up
```

## File Structure

```
moontrack/
├── .env                    # ⭐ MAIN configuration file (edit this!)
├── .env.example            # Template with all variables
├── apps/
│   ├── backend/
│   │   └── .env            # Auto-generated, DO NOT EDIT
│   └── frontend/
│       └── .env            # Auto-generated, DO NOT EDIT
└── docker-compose.yml      # Uses root .env
```

## Environment Variables

### Database (PostgreSQL)

```env
POSTGRES_HOST=localhost         # Database host
POSTGRES_PORT=5432             # Database port
POSTGRES_DB=moontrack_dev      # Database name
POSTGRES_USER=postgres         # Database user
POSTGRES_PASSWORD=postgres     # ⚠️ CHANGE THIS!
```

**Full connection URL** (auto-generated):
```env
DATABASE_URL=postgres://postgres:postgres@localhost:5432/moontrack_dev?sslmode=disable
```

### Redis

```env
REDIS_HOST=localhost           # Redis host
REDIS_PORT=6379               # Redis port
REDIS_PASSWORD=               # Optional password (leave empty for no auth)
```

### Backend API

```env
API_HOST=localhost            # API server host
API_PORT=8080                # API server port
ENV=development              # Environment (development/staging/production)
LOG_LEVEL=info               # Logging level (debug/info/warn/error)
JWT_SECRET=...               # ⚠️ JWT secret (min 32 chars, CHANGE THIS!)
```

### External Services

```env
COINGECKO_API_KEY=           # CoinGecko API key
                             # Get free: https://www.coingecko.com/en/api
```

### Frontend

```env
VITE_API_BASE_URL=http://localhost:8080/api/v1
```

**Note**: Vite requires `VITE_` prefix for env vars to be exposed to the client.

## Common Tasks

### View current configuration

```bash
just env
```

Output:
```
📋 Environment Configuration:

Database:
  POSTGRES_HOST=localhost
  POSTGRES_PORT=5432
  POSTGRES_DB=moontrack_dev
  POSTGRES_USER=postgres
  POSTGRES_PASSWORD=***hidden***

Redis:
  REDIS_HOST=localhost
  REDIS_PORT=6379
  REDIS_PASSWORD=<empty>

API:
  API_HOST=localhost
  API_PORT=8080
  ENV=development
```

### Sync .env to apps

```bash
# Manually sync (usually automatic)
just env-sync
```

### Validate configuration

```bash
just env-validate
```

Checks:
- `.env` file exists
- No default passwords in production
- Required variables are set

### Change database password

1. Edit `.env`:
   ```env
   POSTGRES_PASSWORD=new_secure_password
   ```

2. Restart infrastructure:
   ```bash
   just down
   just up
   ```

3. Database URL is automatically updated with new password

## Docker Compose Integration

`docker-compose.yml` automatically reads from `.env`:

```yaml
services:
  postgres:
    environment:
      POSTGRES_DB: ${POSTGRES_DB}
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    env_file:
      - .env
```

No need to pass environment variables manually!

## Just Integration

Justfile loads `.env` automatically:

```justfile
# Load environment variables from .env file
set dotenv-load
```

All commands can use env vars:

```bash
just db-connect    # Uses ${POSTGRES_USER} and ${POSTGRES_DB}
just migrate-up    # Uses ${DATABASE_URL}
```

## Security Best Practices

### ✅ DO

- Keep `.env` in `.gitignore` (already configured)
- Use strong passwords (min 16 chars)
- Use long JWT secrets (min 32 chars, 64+ recommended)
- Rotate secrets regularly
- Use different values for dev/staging/prod

### ❌ DON'T

- Don't commit `.env` to git
- Don't share `.env` files
- Don't use default passwords in production
- Don't hardcode secrets in code

## Production Setup

For production, use stronger configuration:

```env
# Production example
POSTGRES_PASSWORD=very-long-secure-random-password-here-min-32-chars
JWT_SECRET=extremely-long-and-random-jwt-secret-key-at-least-64-characters-for-production
ENV=production
LOG_LEVEL=warn

# Production API (if deployed)
API_HOST=api.moontrack.app
VITE_API_BASE_URL=https://api.moontrack.app/api/v1
```

## Troubleshooting

### "Database connection failed"

1. Check `.env` password matches Docker container:
   ```bash
   just env              # View config
   just down && just up  # Restart with new config
   ```

### "Environment variable not found"

1. Ensure `.env` exists:
   ```bash
   just env-validate
   ```

2. Sync to apps:
   ```bash
   just env-sync
   ```

### "Docker can't read .env"

1. Check `.env` is in project root (not in `apps/`)
2. Ensure no syntax errors in `.env`
3. Restart Docker:
   ```bash
   just restart
   ```

### Apps can't find .env

Run sync command:
```bash
just env-sync
```

This creates `.env` files in `apps/backend/` and `apps/frontend/` from root `.env`.

## Migration from Old Setup

If you had separate `.env` files in `apps/backend/` and `apps/frontend/`:

1. **Merge configurations** into root `.env`
2. **Delete old files**:
   ```bash
   rm apps/backend/.env apps/frontend/.env
   ```
3. **Sync from root**:
   ```bash
   just env-sync
   ```

The new setup is already configured! ✅

## FAQ

**Q: Where do I edit environment variables?**
A: Edit the root `.env` file only. Apps sync automatically.

**Q: Do I need to restart after changing .env?**
A: Yes, for Docker: `just restart`. For backend/frontend: restart dev servers.

**Q: Can I use .env.local?**
A: Yes, create `.env.local` for local overrides. Add to `.gitignore`.

**Q: How do I add new variables?**
A: Add to root `.env`, then run `just env-sync`.

**Q: What if I want different configs for each app?**
A: Use the root `.env` and prefix variables:
```env
BACKEND_FEATURE_X=enabled
FRONTEND_FEATURE_Y=enabled
```

## References

- Justfile: `justfile` - All commands with env integration
- Docker Compose: `docker-compose.yml` - Infrastructure with env vars
- Example: `.env.example` - Template with all variables
- Sync Script: `scripts/sync-env.sh` - Auto-sync mechanism
