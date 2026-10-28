#!/bin/sh
# Isolated project/volumes only. No deployment credentials or host daemon changes.
set -eu
[ "${AG_P0_SMOKE_DISPOSABLE:-}" = 1 ] || { echo "requires AG_P0_SMOKE_DISPOSABLE=1" >&2; exit 2; }
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
# Match this branch's declared SDK rather than the host's default Go version.
GOTOOLCHAIN=$(awk '$1 == "toolchain" { print $2; exit }' go.mod)
[ -n "$GOTOOLCHAIN" ] || { echo "go.mod must declare toolchain" >&2; exit 2; }
export GOTOOLCHAIN
export COMPOSE_PROJECT_NAME="agrigreen-p0-${GITHUB_RUN_ID:-local}"
C="-f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.vps.yml -f deploy/compose/docker-compose.vps-smoke.yml"
dc() { docker compose $C "$@"; }
cleanup() {
 status=$?
 trap - EXIT
 if [ "$status" -ne 0 ]; then
  # Only migration jobs; never config/env, API logs, reset links or Keycloak logs.
  # Missing env/sanitizer failure prints a fixed message, never raw output.
  dc logs --no-color --no-log-prefix --tail 80 migrate-up migrate-bootstrap 2>&1 |
   python3 deploy/vps/redact.py --env-file deploy/compose/.env ||
   printf '%s\n' 'Migration diagnostics unavailable; raw logs suppressed.' >&2
 fi
 dc down -v --remove-orphans >/dev/null 2>&1 || true
 exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
sh deploy/vps/prepare.sh vps.example.test ops@example.test "203.0.113.10/32"
# Generated test-only service key; values never appear in logs or artifacts.
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:2048 -out deploy/compose/.vps/notify/fcm.key 2>/dev/null
python3 - <<'PY'
import json
from pathlib import Path
root=Path('deploy/compose/.vps/notify')
(root/'fcm.json').write_text(json.dumps({'project_id':'p0-smoke','client_email':'p0-smoke@example.test','private_key':(root/'fcm.key').read_text(),'token_uri':'http://fcm-smoke:8080/token'}))
(root/'fcm.json').chmod(0o644)
PY
export AG_SMTP_HOST=smtp AG_SMTP_PORT=1025 AG_SMTP_FROM=no-reply@example.test
export AG_FCM_PROJECT_ID=p0-smoke AG_FCM_CREDENTIALS_FILE=/run/secrets/notify/fcm.json
export AG_FCM_ENDPOINT=http://fcm-smoke:8080 AG_FCM_TOKEN_URL=http://fcm-smoke:8080/token
# Do not print rendered Compose: it contains generated credentials.
dc config --quiet
printf '%s\n' '--- PASS: merged VPS Compose configuration'
dc config --format json | python3 -c '
import json,sys
c=json.load(sys.stdin)
for name,s in c["services"].items():
 assert float(s.get("mem_limit",0))>0 and float(s.get("cpus",0))>0, name
for name in ("api-platform","worker"):
 e=c["services"][name]["environment"]
 for key in ("AG_SMTP_HOST","AG_SMTP_FROM","AG_FCM_PROJECT_ID","AG_FCM_CREDENTIALS_FILE","AG_WEB_OPS_URL","AG_REPORT_LINK_BASE"):
  assert e.get(key), name+":"+key
 assert e["AG_S3_PUBLIC_ENDPOINT"]=="files.vps.example.test"
 assert e["AG_SMTP_ALLOW_PLAIN"]=="false"
print("--- PASS: merged containers capped; notify env and public URLs wired")'
# Production-VPS API and worker dependencies; caddy-smoke only substitutes
# external TLS with loopback HTTP for SigV4 verification inside isolated CI.
dc up -d --build --wait --wait-timeout 1200 smtp fcm-smoke caddy-smoke api-platform worker
(
 set -a
 . deploy/compose/.env
 set +a
 export AG_S3_ENDPOINT=127.0.0.1:9000 AG_S3_USE_TLS=false AG_S3_BUCKET=agrigreen AG_S3_ACCESS_KEY=agrigreen-app
 export AG_S3_SECRET_KEY="$S3_APP_SECRET_KEY" AG_S3_REGION=us-east-1
 export AG_S3_PUBLIC_ENDPOINT=files.vps.example.test:9080 AG_S3_PUBLIC_USE_TLS=false
 go test -v -count=1 -tags=vpssmoke ./internal/shared/objstore -run '^TestVPSPublicS3Proxy$'
)
python3 deploy/vps/smoke.py
docker run --rm -e AG_DOMAIN=vps.example.test -e ACME_EMAIL=ops@example.test -e ADMIN_ALLOW_CIDR=203.0.113.10/32 \
 -v "$PWD/deploy/compose/caddy/Caddyfile:/etc/caddy/Caddyfile:ro" caddy:2.10-alpine \
 caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
printf '%s\n' '--- PASS: production Caddy TLS/proxy configuration accepted'
# No log dumps, container env, reset mail or signed URLs.
dc ps
