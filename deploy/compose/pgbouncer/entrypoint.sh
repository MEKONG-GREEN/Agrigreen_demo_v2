#!/bin/sh
# Sinh userlist.txt cho các vai ứng dụng từ biến môi trường rồi chạy PgBouncer
# ở transaction mode (mục 4, R-30). Chỉ dùng cho Local/Dev.
set -eu
mkdir -p /etc/pgbouncer
cp /etc/pgbouncer-src/pgbouncer.ini /etc/pgbouncer/pgbouncer.ini
: > /etc/pgbouncer/userlist.txt
for role in platform_api worker_platform party_app masterdata_app geo_app extension_app content_app mechanization_app mrv_app sync_app accounts_app notify_app reports_app cropcycle_app waterway_app simulation_app fieldops_app hub_app mkg_api measurement_app ingest_mkg worker_internal ingest_ts ts_reader metrics_app logistics_app orders_app costing_app acctbridge_app apiclient_app metering_app public_api mkg_monitor ts_monitor vault_app ag_outbox_publisher ag_ops_jobs app_platform app_worker app_public app_sync; do
  var="AG_DB_PASSWORD_$(echo "$role" | tr '[:lower:]' '[:upper:]')"
  eval "pw=\${$var:-\${AG_DB_APP_PASSWORD:-}}"
  if [ -z "$pw" ]; then echo "thiếu mật khẩu cho $role" >&2; exit 1; fi
  printf '"%s" "%s"\n' "$role" "$pw" >> /etc/pgbouncer/userlist.txt
done
exec pgbouncer /etc/pgbouncer/pgbouncer.ini
