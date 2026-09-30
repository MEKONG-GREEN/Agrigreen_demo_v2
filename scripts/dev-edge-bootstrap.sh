#!/bin/sh
# CHỈ cho Local/Dev và CI compose-smoke (docs/runbooks/ingest.md mục 2): đăng ký thiết bị
# biên mô phỏng edge-sim vào sổ thiết bị của mkg/measurement qua Admin API THẬT (8083,
# MFA), đúng quy trình hai người (R-56, ADR-0019):
#
#   1. tạo (một lần) hai danh tính dịch vụ dev trong realm agrigreen qua kcadm — KHÔNG sửa
#      tệp realm, không dùng ở production:
#        agrigreen-dev-edge-proposer  vai scale_supervisor  (đề xuất)
#        agrigreen-dev-edge-approver  vai device_approver   (duyệt — sub khác người đề xuất)
#      Token mang aud agrigreen-api, tenant_id MKG dev cố định và amr [pwd, otp]: tài khoản
#      dịch vụ không có yếu tố thứ hai thật, claim này chỉ được phép ở realm Local/CI;
#   2. đọc device_id, epoch, khóa công khai, cert_sha256 từ `edge-agent info` trong container
#      edge-sim (khóa riêng không rời volume);
#   3. đề xuất kỳ (POST /v1/measurement/devices) rồi duyệt (…/epochs/<n>/decision);
#   4. (tùy chọn --wait <giây>) chờ tới khi ts_weigh.reading_batch có lô của thiết bị.
#
# Chạy lại an toàn: kỳ đã active cùng khóa + chứng thư → bỏ qua; khóa Idempotency-Key tất
# định theo (thiết bị, kỳ, key_id). Cần: docker compose (stack đang chạy), curl, jq.
#
#   sh scripts/dev-edge-bootstrap.sh [--wait 120]
#
# Biến: COMPOSE (mặc định docker compose -f deploy/compose/docker-compose.yml),
#       MKG_TENANT, EDGE_FACILITY_REF (UUID dev cố định), EDGE_SERVICE (edge-sim).
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
COMPOSE=${COMPOSE:-docker compose -f deploy/compose/docker-compose.yml}
TENANT=${MKG_TENANT:-01928a3c-0000-7000-8000-00000000e001}
FACILITY=${EDGE_FACILITY_REF:-01928a3c-0000-7000-8000-00000000f001}
SERVICE=${EDGE_SERVICE:-edge-sim}
POINT=${EDGE_POINT:-HUB_IN}
APP=${AG_MKG_APP_URL:-http://127.0.0.1:8081}
ADMIN=${AG_MKG_ADMIN_URL:-http://127.0.0.1:8083}
KC=${AG_KC_URL:-http://127.0.0.1:8180}
WAIT=0
if [ "${1:-}" = "--wait" ]; then WAIT=${2:?--wait <giây>}; fi

for c in curl jq; do command -v "$c" > /dev/null || { echo "cần $c trong PATH" >&2; exit 1; }; done
if [ -z "${KEYCLOAK_ADMIN_PASSWORD:-}" ] && [ -f deploy/compose/.env ]; then
  KEYCLOAK_ADMIN_PASSWORD=$(sed -n 's/^KEYCLOAK_ADMIN_PASSWORD=//p' deploy/compose/.env | tr -d '\r"')
fi
: "${KEYCLOAK_ADMIN_PASSWORD:?chạy scripts/gen-dev-secrets trước (deploy/compose/.env)}"
export MSYS_NO_PATHCONV=1   # Git Bash: không đổi /opt/... thành đường dẫn Windows

kc() { $COMPOSE exec -T keycloak /opt/keycloak/bin/kcadm.sh "$@"; }
kc config credentials --server http://localhost:8080 --realm master --user admin --password "$KEYCLOAK_ADMIN_PASSWORD" > /dev/null 2>&1

# token <clientId> <vai> → in access token (tạo client + mapper nếu chưa có; đổi secret mỗi lần).
token() {
  cid=$(kc get clients -r agrigreen -q clientId="$1" --fields id --format csv --noquotes | tr -d '\r')
  if [ -z "$cid" ]; then
    kc create clients -r agrigreen -s clientId="$1" -s enabled=true -s publicClient=false \
      -s serviceAccountsEnabled=true -s standardFlowEnabled=false -s directAccessGrantsEnabled=false \
      -s 'attributes."access.token.lifespan"=900' > /dev/null
    cid=$(kc get clients -r agrigreen -q clientId="$1" --fields id --format csv --noquotes | tr -d '\r')
    kc create "clients/$cid/protocol-mappers/models" -r agrigreen -s protocol=openid-connect -s name=aud \
      -s protocolMapper=oidc-audience-mapper -s 'config."included.client.audience"=agrigreen-api' \
      -s 'config."access.token.claim"=true' > /dev/null
    kc create "clients/$cid/protocol-mappers/models" -r agrigreen -s protocol=openid-connect -s name=tenant_id \
      -s protocolMapper=oidc-hardcoded-claim-mapper -s 'config."claim.name"=tenant_id' \
      -s "config.\"claim.value\"=$TENANT" -s 'config."jsonType.label"=String' -s 'config."access.token.claim"=true' > /dev/null
    kc create "clients/$cid/protocol-mappers/models" -r agrigreen -s protocol=openid-connect -s name=amr-dev \
      -s protocolMapper=oidc-hardcoded-claim-mapper -s 'config."claim.name"=amr' \
      -s 'config."claim.value"="[\"pwd\",\"otp\"]"' -s 'config."jsonType.label"=JSON' -s 'config."access.token.claim"=true' > /dev/null
    kc add-roles -r agrigreen --uusername "service-account-$1" --rolename "$2" > /dev/null
  fi
  secret=$(head -c 24 /dev/urandom | od -An -v -tx1 | tr -d ' \n')
  kc update "clients/$cid" -r agrigreen -s "secret=$secret" > /dev/null
  curl -fsS --retry 10 --retry-delay 2 --retry-all-errors -X POST "$KC/realms/agrigreen/protocol/openid-connect/token" \
    -d grant_type=client_credentials -d client_id="$1" --data-urlencode "client_secret=$secret" | jq -r .access_token
}
TP=$(token agrigreen-dev-edge-proposer scale_supervisor)
TA=$(token agrigreen-dev-edge-approver device_approver)

# Thông tin kỳ đăng ký của agent (không in khóa riêng).
info=""
for i in $(seq 1 30); do
  info=$($COMPOSE exec -T "$SERVICE" /app info -config /etc/edge-agent/run.json 2> /dev/null) && \
    echo "$info" | jq -e '.epoch and .public_key_base64 and .cert_sha256' > /dev/null && break
  info=""
  sleep 2
done
[ -n "$info" ] || { echo "không đọc được \`edge-agent info\` từ $SERVICE (đã enroll chưa? có chứng thư?)" >&2; exit 1; }
DEVICE=$(echo "$info" | jq -r .device_id)
EPOCH=$(echo "$info" | jq -r .epoch)
KEYID=$(echo "$info" | jq -r .key_id)
PUB=$(echo "$info" | jq -r .public_key_base64)
CERT=$(echo "$info" | jq -r .cert_sha256)
echo "thiết bị $DEVICE kỳ $EPOCH key_id $KEYID cert_sha256 $CERT"

# api <token> <METHOD> <url> [body] [idem] → in thân phản hồi, trả mã HTTP qua biến $code.
api() {
  out=$(mktemp)
  if [ -n "${4:-}" ]; then
    code=$(curl -sS -o "$out" -w '%{http_code}' -X "$2" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' \
      -H "Idempotency-Key: $5" --data "$4" "$3")
  else
    code=$(curl -sS -o "$out" -w '%{http_code}' -X "$2" -H "Authorization: Bearer $1" "$3")
  fi
  body=$(cat "$out"); rm -f "$out"
}

epoch_status() {
  api "$TP" GET "$APP/v1/measurement/devices/$DEVICE"
  if [ "$code" = 404 ]; then echo none; return; fi
  [ "$code" = 200 ] || { echo "GET thiết bị: HTTP $code $body" >&2; exit 1; }
  echo "$body" | jq -r --argjson e "$EPOCH" --arg k "$KEYID" --arg c "$CERT" \
    '[.epochs[] | select(.epoch == $e)][0] // empty | if .key_id == $k and .cert_sha256 == $c then .status else "mismatch:" + .status end' \
    | grep . || echo none
}

st=$(epoch_status)
case "$st" in
  none)
    body=$(jq -n --arg d "$DEVICE" --argjson e "$EPOCH" --arg f "$FACILITY" --arg p "$POINT" --arg k "$PUB" --arg c "$CERT" \
      '{device_id: $d, epoch: $e, facility_ref: $f, point: $p, public_key: $k, cert_sha256: $c, scale_model: "simulator (dev — mẫu cân chờ C-6)"}')
    api "$TP" POST "$ADMIN/v1/measurement/devices" "$body" "dev-edge-register.$DEVICE.e$EPOCH.$KEYID"
    case "$code" in 200|201) echo "đã đề xuất kỳ $EPOCH (scale_supervisor)";; *) echo "đề xuất: HTTP $code $body" >&2; exit 1;; esac
    st=pending ;;
esac
case "$st" in
  pending)
    api "$TA" POST "$ADMIN/v1/measurement/devices/$DEVICE/epochs/$EPOCH/decision" '{"decision":"approved"}' "dev-edge-approve.$DEVICE.e$EPOCH.$KEYID"
    case "$code" in 200|201) echo "đã duyệt kỳ $EPOCH (device_approver, khác người đề xuất)";; *) echo "duyệt: HTTP $code $body" >&2; exit 1;; esac ;;
  active) echo "kỳ $EPOCH đã active — giữ nguyên" ;;
  *)
    echo "sổ thiết bị có kỳ $EPOCH ở trạng thái '$st' không khớp khóa/chứng thư của agent." >&2
    echo "Thường do volume edge_sim_data hoặc chứng thư dev bị sinh lại (mất đĩa → kỳ mới, ADR-0019):" >&2
    echo "  docker compose … run --rm edge-sim enroll -config /etc/edge-agent/enroll.json -rotate -abandon-buffer -epoch <N+1>" >&2
    echo "  rồi chạy lại script này (xem docs/runbooks/ingest.md mục 5)." >&2
    exit 1 ;;
esac
[ "$(epoch_status)" = active ] || { echo "kỳ $EPOCH chưa active sau khi duyệt" >&2; exit 1; }

if [ "$WAIT" -gt 0 ]; then
  q="SELECT count(*) FROM ts_weigh.reading_batch WHERE device_id = '$DEVICE' AND epoch = $EPOCH"
  n=0
  for i in $(seq 1 "$WAIT"); do
    n=$($COMPOSE exec -T postgres psql -U postgres -d ag_ts -tAc "$q" 2> /dev/null | tr -d '\r ' || echo 0)
    [ "${n:-0}" -gt 0 ] && break
    sleep 1
  done
  [ "${n:-0}" -gt 0 ] || { echo "sau ${WAIT}s chưa có lô nào của $DEVICE trong ts_weigh.reading_batch" >&2; exit 1; }
  echo "ts_weigh.reading_batch: $n lô của $DEVICE kỳ $EPOCH"
fi
