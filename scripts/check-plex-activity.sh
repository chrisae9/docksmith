#!/bin/bash
# Plex Activity Checker - Pre-Update Script
# Checks Tautulli API for active Plex sessions before allowing container updates

# Tautulli server configuration
TAUTULLI_URL="${TAUTULLI_URL:-https://tautulli.ts.chis.dev}"
API_KEY="${TAUTULLI_API_KEY:-92f92962f1c846c2beafab9c3d81ce06}"

# Construct the API URL
URL="${TAUTULLI_URL}/api/v2?apikey=${API_KEY}&cmd=get_activity"

# Make the API request
response=$(curl -s -f "$URL")
curl_exit=$?

# Check if curl succeeded
if [ $curl_exit -ne 0 ]; then
    echo "Update blocked: Cannot connect to Tautulli API (curl exit code: $curl_exit)"
    exit 1  # Block update on API connection failure (fail-safe)
fi

# Parse JSON response and check for active sessions
if ! command -v jq &> /dev/null; then
    echo "Update blocked: jq is not installed (required for JSON parsing)"
    exit 1
fi

# Extract session count and user list
session_count=$(echo "$response" | jq -r '.response.data.sessions | length')
parse_exit=$?

if [ $parse_exit -ne 0 ] || [ -z "$session_count" ]; then
    echo "Update blocked: Cannot parse Tautulli API response (invalid JSON)"
    exit 1  # Block update on parsing failure (fail-safe)
fi

# Check if there are active sessions
if [ "$session_count" -gt 0 ]; then
    # Extract usernames from active sessions
    users=$(echo "$response" | jq -r '.response.data.sessions[].user' | tr '\n' ', ' | sed 's/,$//')
    echo "Update blocked: ${session_count} active Plex session(s) - ${users}"
    exit 1  # Block update (users are watching)
else
    echo "Update allowed: No active Plex sessions"
    exit 0  # Allow update (safe to update)
fi
