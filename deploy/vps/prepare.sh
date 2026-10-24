#!/bin/sh
# Chuẩn bị một VPS cho docker-compose.vps.yml (xem docs/runbooks/vps-deploy.md).
#
# Dùng: sh deploy/vps/prepare.sh <domain> <email-acme> "<cidr-quản-trị> [cidr…]"
#   vd:  sh deploy/vps/prepare.sh demo.agrigreen.vn ops@mekonggreen.vn "203.0.113.10/32"
#
# - Sinh deploy/compose/.env nếu chưa có (scripts/gen-dev-secrets.sh), bổ sung KC_DB_PASSWORD,
#   ghi AG_DOMAIN / ACME_EMAIL / ADMIN_ALLOW_CIDR. Không in bí mật.
# - Sinh realm Keycloak cho VPS vào deploy/compose/.vps/keycloak/ từ realm dev: redirect/origin theo
#   tên miền, client CHỈ-DEV agrigreen-seed TẮT (bật tạm khi seed, xem runbook).
# Chạy lại an toàn; đổi tên miền sau lần dựng đầu phải sửa client trong Keycloak (realm chỉ nhập một lần).
set -eu

[ $# -eq 3 ] || { sed -n '4,5p' "$0" >&2; exit 2; }
domain=$1; email=$2; cidr=$3
case "$domain" in *[!a-z0-9.-]*|.*|*.|"") echo "Tên miền không hợp lệ: $domain" >&2; exit 2 ;; esac
case "$email" in *@*.*) ;; *) echo "Email ACME không hợp lệ: $email" >&2; exit 2 ;; esac
case "$cidr" in *[!0-9a-fA-F.:/\ ]*|"") echo "CIDR không hợp lệ: $cidr" >&2; exit 2 ;; esac

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
envf="$root/deploy/compose/.env"
sh "$root/scripts/gen-dev-secrets.sh"

umask 077
setvar() { # key value — thay dòng cũ hoặc thêm mới
  tmp="$envf.tmp.$$"
  grep -v "^$1=" "$envf" > "$tmp" || true
  printf '%s=%s\n' "$1" "$2" >> "$tmp"
  mv "$tmp" "$envf"
}
grep -q '^KC_DB_PASSWORD=' "$envf" || setvar KC_DB_PASSWORD "$(head -c 24 /dev/urandom | od -An -v -tx1 | tr -d ' \n')"
setvar AG_DOMAIN "$domain"
setvar ACME_EMAIL "$email"
setvar ADMIN_ALLOW_CIDR "'$cidr'"

out="$root/deploy/compose/.vps/keycloak"
mkdir -p "$out" "$root/deploy/compose/.vps/notify"
sed -e "s#http://localhost:3000#https://app.$domain#g" \
    -e "s#http://127.0.0.1:3000#https://app.$domain#g" \
    -e "s#http://localhost:8082#https://admin.$domain#g" \
    -e "s#http://localhost:3001#https://web-internal.invalid#g" \
    -e "s#http://127.0.0.1:3001#https://web-internal.invalid#g" \
    "$root/deploy/compose/keycloak/agrigreen-realm.json" > "$out/agrigreen-realm.json.tmp"
# Tắt client chỉ-dev: ROPC agrigreen-seed, và agrigreen-web-internal (VPS không phục vụ web nội bộ MKG — Caddy chỉ
# có app/api/admin/auth; URL 3001 đổi sang .invalid ở trên). Dòng "enabled" ngay sau clientId.
awk '/"clientId": "agrigreen-(seed|web-internal)"/ { off = 1 }
     off && /"enabled": true/ { sub(/"enabled": true/, "\"enabled\": false"); off = 0 }
     { print }' "$out/agrigreen-realm.json.tmp" > "$out/agrigreen-realm.json"
rm -f "$out/agrigreen-realm.json.tmp"
# D24/D25: realm quản trị riêng của hệ KN (mật khẩu 14 ký tự/không trùng 10, TOTP bắt buộc).
sed -e "s#http://localhost:3000#https://app.$domain#g" \
    -e "s#http://127.0.0.1:3000#https://app.$domain#g" \
    "$root/deploy/compose/keycloak/agrigreen-kn-admin-realm.json" > "$out/agrigreen-kn-admin-realm.json"
for f in agrigreen-realm.json agrigreen-kn-admin-realm.json; do
  if grep -q 'localhost' "$out/$f"; then
    echo "Realm VPS $f vẫn còn 'localhost' — kiểm tra lại realm dev" >&2; exit 1
  fi
done
# Keycloak trong container chạy bằng uid khác → phải đọc được (realm không chứa bí mật,
# chỉ placeholder ${AG_KC_CLIENT_SECRET}; umask 077 ở trên chỉ dành cho .env).
chmod 755 "$root/deploy/compose/.vps" "$out"
chmod 644 "$out/agrigreen-realm.json" "$out/agrigreen-kn-admin-realm.json"

echo "Xong: $envf (đã thêm biến VPS), $out/agrigreen-realm.json, $out/agrigreen-kn-admin-realm.json"
echo "Cần bản ghi DNS A: app.$domain api.$domain auth.$domain admin.$domain files.$domain → IP của VPS"
