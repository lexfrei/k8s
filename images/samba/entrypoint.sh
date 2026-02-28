#!/bin/sh
set -e

USERS_FILE="${SMB_USERS_FILE:-/etc/samba/users.json}"

if [ -f "$USERS_FILE" ]; then
    jq -r '.users.all_entries[] | "\(.name) \(.uid) \(.gid) \(.password)"' "$USERS_FILE" | \
    while read -r name uid gid password; do
        addgroup -g "$gid" "$name" 2>/dev/null || true
        adduser -D -H -s /bin/false -u "$uid" -G "$name" "$name" 2>/dev/null || true
        printf '%s\n%s\n' "$password" "$password" | smbpasswd -a -s "$name"
        echo "User '$name' (uid=$uid, gid=$gid) configured"
    done
else
    echo "Warning: users file '$USERS_FILE' not found, starting without users"
fi

exec "$@"
