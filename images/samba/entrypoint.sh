#!/bin/sh
set -e

SMB_USER="${SMB_USER:-lex}"

# Create system user for Samba (no home, no shell)
adduser -D -H -s /bin/false "$SMB_USER" 2>/dev/null || true

# Set Samba password from environment
if [ -z "$SMB_PASSWORD" ]; then
    echo "ERROR: SMB_PASSWORD environment variable is required" >&2
    exit 1
fi
printf '%s\n%s\n' "$SMB_PASSWORD" "$SMB_PASSWORD" | smbpasswd -a -s "$SMB_USER"

exec "$@"
