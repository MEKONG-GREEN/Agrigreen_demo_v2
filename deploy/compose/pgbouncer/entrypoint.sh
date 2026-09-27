#!/bin/sh
# Sinh userlist.txt cho các vai ứng dụng từ biến môi trường rồi chạy PgBouncer
# ở transaction mode (mục 4, R-30). Chỉ dùng cho Local/Dev.
set -eu
mkdir -p /etc/pgbouncer
cp /etc/pgbouncer-src/pgbouncer.ini /etc/pgbouncer/pgbouncer.ini
: > /etc/pgbouncer/userlist.txt
for role in platform_api worker_platform party_app masterdata_app geo_app extension_app content_app mechanization_app mrv_app sync_app accounts_app notify_app reports_app fieldops_app vault_app ag_outbox_publisher; do
  var="AG_DB_PASSWORD_$(echo "$role" | tr '[:lower:]' '[:upper:]')"
  eval "pw=\${$var:-\${AG_DB_APP_PASSWORD:-}}"
  if [ -z "$pw" ]; then echo "thiếu mật khẩu cho $role" >&2; exit 1; fi
  printf '"%s" "%s"\n' "$role" "$pw" >> /etc/pgbouncer/userlist.txt
done
exec pgbouncer /etc/pgbouncer/pgbouncer.ini
