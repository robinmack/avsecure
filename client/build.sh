#!/bin/bash
set -e
cd "$(dirname "$0")"

# ── Credential source (priority order) ───────────────────────────────────────
# 1. /etc/avsecure/secrets  — recommended for servers (chmod 600, outside repo)
# 2. client/.env            — fallback for local dev (gitignored)

SYSTEM_SECRETS="/etc/avsecure/secrets"
LOCAL_ENV=".env"

if [ -f "$SYSTEM_SECRETS" ]; then
  # shellcheck source=/dev/null
  source "$SYSTEM_SECRETS"
  echo "Using secrets from $SYSTEM_SECRETS"
elif [ -f "$LOCAL_ENV" ]; then
  # shellcheck source=/dev/null
  source "$LOCAL_ENV"
  echo "Using secrets from $LOCAL_ENV"
else
  echo "ERROR: No credentials file found."
  echo "       Option A (server):    sudo cp .env.example $SYSTEM_SECRETS"
  echo "                             sudo chmod 600 $SYSTEM_SECRETS"
  echo "                             Then edit $SYSTEM_SECRETS with your TURN credentials."
  echo "       Option B (local dev): cp .env.example $LOCAL_ENV"
  echo "                             Then edit $LOCAL_ENV with your TURN credentials."
  exit 1
fi

# ── Credential guard ──────────────────────────────────────────────────────────
if [ -z "$REACT_APP_TURN_USERNAME" ]; then
  echo "ERROR: REACT_APP_TURN_USERNAME is not set"
  exit 1
fi

if [[ "$REACT_APP_TURN_USERNAME" == /* || "$REACT_APP_TURN_USERNAME" == *"/"* ]]; then
  echo "ERROR: REACT_APP_TURN_USERNAME looks like a file path: $REACT_APP_TURN_USERNAME"
  echo "       Set it to your metered.ca username (a hex string), not a file path."
  exit 1
fi

if [ -z "$REACT_APP_TURN_CREDENTIAL" ]; then
  echo "ERROR: REACT_APP_TURN_CREDENTIAL is not set"
  exit 1
fi

echo "✓ TURN username:   ${REACT_APP_TURN_USERNAME:0:8}... (${#REACT_APP_TURN_USERNAME} chars)"
echo "✓ TURN credential: ${REACT_APP_TURN_CREDENTIAL:0:6}... (${#REACT_APP_TURN_CREDENTIAL} chars)"
echo ""

# ── Build ─────────────────────────────────────────────────────────────────────
npm run build

# ── Deploy ────────────────────────────────────────────────────────────────────
sudo rm -rf /var/www/html/*
echo "Removed previous contents of /var/www/html"
sudo cp -r build/* /var/www/html/
echo "Copied new build"
sudo chmod -R 755 /var/www/html/*
sudo chown -R nginx /var/www/html/*
echo "Deployed at $(date)"
