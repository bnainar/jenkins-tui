#!/usr/bin/env bash
set -euo pipefail

JENKINS_URL="${JENKINS_URL:-http://localhost:8080}"
JENKINS_HOST="${JENKINS_HOST:-http://host.docker.internal:8080}"
JENKINS_USER="${JENKINS_USER:-admin}"
JENKINS_PASS="${JENKINS_PASS:-admin}"
TOKEN_NAME="${TOKEN_NAME:-jenkins-tui-local}"
cookie_jar="$(mktemp)"
trap 'rm -f "$cookie_jar"' EXIT

crumb_json=$(curl -fsS -c "$cookie_jar" -u "$JENKINS_USER:$JENKINS_PASS" "$JENKINS_URL/crumbIssuer/api/json")
crumb_field=$(echo "$crumb_json" | sed -E 's/.*"crumbRequestField"\s*:\s*"([^"]+)".*/\1/')
crumb_value=$(echo "$crumb_json" | sed -E 's/.*"crumb"\s*:\s*"([^"]+)".*/\1/')

resp=$(curl -fsS -b "$cookie_jar" -u "$JENKINS_USER:$JENKINS_PASS" \
  -H "$crumb_field: $crumb_value" \
  -X POST "$JENKINS_URL/user/$JENKINS_USER/descriptorByName/jenkins.security.ApiTokenProperty/generateNewToken" \
  --data-urlencode "newTokenName=$TOKEN_NAME")

token=$(echo "$resp" | sed -E 's/.*"tokenValue"\s*:\s*"([^"]+)".*/\1/')

echo "Generated token for $JENKINS_USER" >&2
cat <<YAML
jenkins:
  - name: local
    host: $JENKINS_HOST
    username: $JENKINS_USER
    token: $token
YAML
