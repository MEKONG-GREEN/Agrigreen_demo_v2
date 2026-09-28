#!/bin/sh
# CHỈ Local/Dev: tạo người dùng demo trong realm agrigreen của Keycloak Docker Compose.
#
# Dùng: sh scripts/dev-users.sh [tenant_id] [org_ref]      (cần curl và jq)
#   KEYCLOAK_URL mặc định http://localhost:8180
#
# Gọi Keycloak Admin REST bằng tài khoản bootstrap "admin" (KEYCLOAK_ADMIN_PASSWORD trong
# deploy/compose/.env). Mật khẩu NGẪU NHIÊN in ra MỘT LẦN; người dùng đã có thì bỏ qua.
# Vai [MFA] → required action CONFIGURE_TOTP (cấu hình ở lần đăng nhập đầu qua trình duyệt).
# Không bao giờ chạy với Keycloak staging/production.
set -eu

case "${AG_ENV:-}" in prod|production) echo "AG_ENV=production — từ chối" >&2; exit 1 ;; esac
kc="${KEYCLOAK_URL:-http://localhost:8180}"
case "$kc" in http://localhost*|http://127.0.0.1*) ;; *) echo "Chỉ chạy với Keycloak Local/Dev" >&2; exit 1 ;; esac
realm=agrigreen
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
envf="$root/deploy/compose/.env"
[ -f "$envf" ] || { echo "Không thấy $envf — chạy scripts/gen-dev-secrets.sh trước" >&2; exit 1; }
adminpw=$(sed -n "s/^KEYCLOAK_ADMIN_PASSWORD=//p" "$envf" | tr -d "'\"")
tenant="${1:-$(cat /proc/sys/kernel/random/uuid 2>/dev/null || uuidgen | tr 'A-Z' 'a-z')}"
org="${2:-}"

tok=$(curl -fsS -X POST "$kc/realms/master/protocol/openid-connect/token" \
  --data-urlencode grant_type=password --data-urlencode client_id=admin-cli \
  --data-urlencode username=admin --data-urlencode "password=$adminpw" | jq -r .access_token)
api="$kc/admin/realms/$realm"
auth="Authorization: Bearer $tok"
mfa=" platform_admin param_setter param_approver masterdata_admin registry_admin cgh_admin geo_admin device_admin sync_conflict_resolver vault_eraser account_admin account_admin_province "

newpw() { printf '%s#7' "$(head -c 64 /dev/urandom | tr -dc 'a-km-zA-HJ-NP-Z2-9' | head -c 20)"; }

create() { # username first last roles...
  u=$1; first=$2; last=$3; shift 3
  if [ "$(curl -fsS -H "$auth" "$api/users?username=$u&exact=true" | jq length)" -gt 0 ]; then
    echo "$u: đã có — bỏ qua (mật khẩu không in lại)"; return
  fi
  pw=$(newpw); actions='[]'
  for r in "$@"; do case "$mfa" in *" $r "*) actions='["CONFIGURE_TOTP"]' ;; esac; done
  attrs=$(jq -n --arg t "$tenant" --arg o "$org" '{tenant_id:[$t]} + (if $o == "" then {} else {org_ref:[$o]} end)')
  body=$(jq -n --arg u "$u" --arg f "$first" --arg l "$last" --arg p "$pw" --argjson a "$attrs" --argjson ra "$actions" \
    '{username:$u, email:($u+"@dev.agrigreen.local"), emailVerified:true, enabled:true, firstName:$f, lastName:$l,
      attributes:$a, requiredActions:$ra, credentials:[{type:"password", value:$p, temporary:false}]}')
  loc=$(curl -fsS -D - -o /dev/null -H "$auth" -H 'Content-Type: application/json' -d "$body" "$api/users" | tr -d '\r' | sed -n 's/^[Ll]ocation: //p')
  id=${loc##*/}
  reps='[]'
  for r in "$@"; do reps=$(echo "$reps" | jq --argjson x "$(curl -fsS -H "$auth" "$api/roles/$r" | jq '{id,name}')" '. + [$x]'); done
  curl -fsS -H "$auth" -H 'Content-Type: application/json' -d "$reps" "$api/users/$id/role-mappings/realm" >/dev/null
  note=""; [ "$actions" = '[]' ] || note=" — cần cấu hình TOTP ở lần đăng nhập đầu"
  echo "$u / $pw  (vai: $*)$note"
}

echo "tenant_id = $tenant"
create dev-admin "Quản trị" Dev platform_admin masterdata_admin registry_admin vault_registrar kn_province_admin cgh_admin account_admin
create dev-accounts "Tài khoản" Dev account_admin
create dev-viewer "Cục KTHT" Dev cgh_viewer
echo "Mật khẩu trên chỉ in MỘT LẦN. Không dán vào issue, chat hay commit."
