#!/bin/sh
# Sinh chứng thư mTLS cho bề mặt Internal ở Docker Compose Local/Dev (mục 11):
#   ca.crt / ca.key                 CA dev tự ký (ca.key không gắn vào container nào)
#   api-platform.crt / .key         máy chủ listener Internal (SAN api-platform, localhost, 127.0.0.1)
#   optimizer.crt / .key            client của optimizer (CN = SAN = optimizer, AG_INTERNAL_CLIENT_NAMES)
#   ingest.crt / .key               máy chủ nhận lô mTLS của cmd/ingest (SAN ingest, localhost, 127.0.0.1)
#   device-<device_id>.crt / .key   client mTLS của từng thiết bị biên dev (CN = SAN = device_id, ADR-0019);
#                                   danh sách thiết bị: DEV_EDGE_DEVICES (mặc định "edge-sim-01", cách nhau bởi dấu cách)
# vào deploy/compose/.certs/ (đã gitignore — không bao giờ commit).
# Dùng: sh scripts/gen-dev-certs.sh [--force]   (gen-dev-secrets.sh tự gọi)
# CHỈ cho Local/Dev. Production cấp chứng thư từ PKI nội bộ trong lãnh thổ (RB-7).
set -eu

devices=${DEV_EDGE_DEVICES:-edge-sim-01}
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
dir="$root/deploy/compose/.certs"
have=1
for f in ca.crt optimizer.key api-platform.key ingest.key; do [ -f "$dir/$f" ] || have=0; done
for d in $devices; do [ -f "$dir/device-$d.key" ] || have=0; done
if [ "$have" = 1 ] && [ "${1:-}" != "--force" ]; then
  echo "Đã có chứng thư dev trong $dir — giữ nguyên (dùng --force để sinh lại)."
  exit 0
fi
command -v openssl > /dev/null || { echo "cần openssl trong PATH" >&2; exit 1; }
# Git Bash (Windows): không đổi "/CN=…" thành đường dẫn Windows. Vì vậy mọi đường
# dẫn truyền cho openssl đều TƯƠNG ĐỐI trong $dir.
export MSYS_NO_PATHCONV=1

mkdir -p "$dir"
cd "$dir"
tmp=.tmp.$$
mkdir "$tmp"
trap 'rm -rf "$tmp"' EXIT
umask 077

openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "ca.key" 2> /dev/null
openssl req -x509 -new -key "ca.key" -sha256 -days 3650 -subj "/O=AgriGreen Local-Dev/CN=agrigreen-dev-internal-ca" \
  -addext "basicConstraints=critical,CA:TRUE,pathlen:0" -addext "keyUsage=critical,keyCertSign,cRLSign" \
  -out "ca.crt"

# leaf <tệp> <serverAuth|clientAuth> <subjectAltName> [CN — mặc định = tên tệp]
leaf() {
  cn=${4:-$1}
  openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$1.key" 2> /dev/null
  openssl req -new -key "$1.key" -subj "/O=AgriGreen Local-Dev/CN=$cn" -out "$tmp/$1.csr"
  printf 'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=%s\nsubjectAltName=%s\n' "$2" "$3" > "$tmp/$1.ext"
  openssl x509 -req -in "$tmp/$1.csr" -CA "ca.crt" -CAkey "ca.key" -CAcreateserial -CAserial "$tmp/ca.srl" \
    -days 825 -sha256 -extfile "$tmp/$1.ext" -out "$1.crt" 2> /dev/null
}
leaf api-platform serverAuth "DNS:api-platform,DNS:localhost,IP:127.0.0.1"
leaf optimizer clientAuth "DNS:optimizer"
# cmd/ingest: listener nhận lô mTLS (agent biên gọi https://ingest:8443 trong mạng compose).
leaf ingest serverAuth "DNS:ingest,DNS:localhost,IP:127.0.0.1"
# Thiết bị biên dev: CN = SAN DNS = device_id (ingest so với device_id của lô, sổ thiết bị
# giữ cert_sha256). Production: CA nội bộ ký CSR do `edge-agent enroll` sinh trên thiết bị.
for d in $devices; do
  case "$d" in *[!a-z0-9._-]*) echo "device_id không hợp lệ: $d" >&2; exit 1 ;; esac
  leaf "device-$d" clientAuth "DNS:$d" "$d"
done

# Container chạy bằng uid khác người dùng máy (distroless nonroot 65532, optimizer 10001)
# và chỉ gắn tệp ở chế độ đọc: khóa lá phải đọc được. Khóa CA giữ 600.
# Khóa thiết bị biên: edge-sim-init chép vào volume của agent với quyền 0600, chủ 65532
# (agent từ chối khóa đọc được bởi người khác).
chmod 755 .
chmod 644 ./*.crt ./*.key
chmod 600 "ca.key"
echo "Đã ghi chứng thư mTLS dev vào $dir (khóa không in ra)."
