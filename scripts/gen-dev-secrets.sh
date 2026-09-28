#!/bin/sh
# Sinh deploy/compose/.env với bí mật ngẫu nhiên cho Docker Compose Local/Dev.
# Dùng: sh scripts/gen-dev-secrets.sh [--force]
# Không bao giờ commit .env (đã có trong .gitignore). Không in bí mật ra màn hình.
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
target="$root/deploy/compose/.env"
if [ -f "$target" ] && [ "${1:-}" != "--force" ]; then
  # Bổ sung bí mật mới thêm sau (không đổi bí mật cũ, không cần down -v).
  if ! grep -q '^AG_KC_CLIENT_SECRET=' "$target"; then
    [ -z "$(tail -c1 "$target")" ] || echo >> "$target"
    echo "AG_KC_CLIENT_SECRET=$(head -c 32 /dev/urandom | od -An -v -tx1 | tr -d ' 
')" >> "$target"
    echo "Đã bổ sung AG_KC_CLIENT_SECRET vào $target (giá trị không in ra). Tạo lại container keycloak để nhập lại realm."
  fi
  echo "Đã có $target — giữ nguyên (dùng --force để sinh lại; khi đó chạy 'docker compose down -v' vì mật khẩu DB đã đổi)."
  exit 0
fi

# Mật khẩu dạng hex: an toàn khi ghép vào URL (postgres://…, amqp://…).
hex() { head -c "$1" /dev/urandom | od -An -v -tx1 | tr -d ' \n'; }
b64() { head -c "$1" /dev/urandom | base64 | tr -d '\n'; }

umask 077
tmp="$target.tmp.$$"
{
  echo "# Sinh bởi scripts/gen-dev-secrets.sh — CHỈ cho Local/Dev. KHÔNG commit."
  echo "PG_SUPERUSER_PASSWORD=$(hex 24)"
  echo "AG_DB_APP_PASSWORD=$(hex 24)"
  echo "AG_DB_PASSWORD_VAULT_APP=$(hex 24)"
  echo "AMQP_PASSWORD=$(hex 24)"
  echo "KEYCLOAK_ADMIN_PASSWORD=$(hex 24)"
  echo "MINIO_ROOT_PASSWORD=$(hex 24)"
  echo "S3_APP_SECRET_KEY=$(hex 24)"
  echo "AG_SYNC_CURSOR_KEY=$(hex 32)"
  echo "AG_VAULT_KEK='$(b64 32)'"
  echo "AG_VAULT_BLIND_KEY='$(b64 32)'"
  echo "AG_ANCHOR_SIGNING_KEY='$(b64 32)'"
  echo "AG_KC_CLIENT_SECRET=$(hex 32)"
} > "$tmp"
mv "$tmp" "$target"
echo "Đã ghi $target (giá trị không in ra)."
