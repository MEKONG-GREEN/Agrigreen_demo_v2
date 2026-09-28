#!/bin/sh
# CHỈ cho Local/CI diễn tập phục hồi (docs/runbooks/restore-drill.md): tạo client
# dịch vụ "agrigreen-drill-seed" trong realm agrigreen (qua kcadm trong container
# keycloak — không sửa tệp realm), rồi in access token cho cmd/seed-synthetic ra stdout.
#
# Token mang: aud agrigreen-api, tenant_id cố định của diễn tập, amr [pwd, otp]
# (Admin API bắt buộc MFA — tài khoản dịch vụ không có yếu tố thứ hai thật, nên
# claim này chỉ được phép ở realm Local/CI, KHÔNG BAO GIỜ ở production) và các vai
# bộ sinh cần. Chạy lại an toàn.
#
# Dùng: TENANT=<uuid> sh scripts/drill/seed-token.sh > token.txt
set -eu
: "${TENANT:?đặt TENANT=<uuid tenant diễn tập>}"
: "${KEYCLOAK_ADMIN_PASSWORD:?nạp deploy/compose/.env trước}"
COMPOSE=${COMPOSE:-docker compose -f deploy/compose/docker-compose.yml}
CLIENT=agrigreen-drill-seed
SECRET=${DRILL_CLIENT_SECRET:-$(head -c 24 /dev/urandom | od -An -v -tx1 | tr -d ' \n')}

kc() { $COMPOSE exec -T keycloak /opt/keycloak/bin/kcadm.sh "$@"; }
kc config credentials --server http://localhost:8080 --realm master --user admin --password "$KEYCLOAK_ADMIN_PASSWORD" >/dev/null 2>&1

CID=$(kc get clients -r agrigreen -q clientId=$CLIENT --fields id --format csv --noquotes | tr -d '\r')
if [ -z "$CID" ]; then
  kc create clients -r agrigreen -s clientId=$CLIENT -s enabled=true -s publicClient=false \
    -s serviceAccountsEnabled=true -s standardFlowEnabled=false -s directAccessGrantsEnabled=false \
    -s 'attributes."access.token.lifespan"=3600' >/dev/null
  CID=$(kc get clients -r agrigreen -q clientId=$CLIENT --fields id --format csv --noquotes | tr -d '\r')
  mapper() { kc create "clients/$CID/protocol-mappers/models" -r agrigreen -s protocol=openid-connect "$@" >/dev/null; }
  mapper -s name=aud -s protocolMapper=oidc-audience-mapper \
    -s 'config."included.client.audience"=agrigreen-api' -s 'config."access.token.claim"=true'
  mapper -s name=tenant_id -s protocolMapper=oidc-hardcoded-claim-mapper \
    -s 'config."claim.name"=tenant_id' -s "config.\"claim.value\"=$TENANT" \
    -s 'config."jsonType.label"=String' -s 'config."access.token.claim"=true'
  mapper -s name=amr-drill -s protocolMapper=oidc-hardcoded-claim-mapper \
    -s 'config."claim.name"=amr' -s 'config."claim.value"="[\"pwd\",\"otp\"]"' \
    -s 'config."jsonType.label"=JSON' -s 'config."access.token.claim"=true'
  kc add-roles -r agrigreen --uusername "service-account-$CLIENT" \
    --rolename platform_admin --rolename masterdata_admin --rolename registry_admin \
    --rolename vault_registrar --rolename kn_province_admin --rolename cgh_admin >/dev/null
fi
kc update "clients/$CID" -r agrigreen -s "secret=$SECRET" >/dev/null

curl -fsS -X POST http://127.0.0.1:8180/realms/agrigreen/protocol/openid-connect/token \
  -d grant_type=client_credentials -d client_id=$CLIENT --data-urlencode "client_secret=$SECRET" \
  | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p'
