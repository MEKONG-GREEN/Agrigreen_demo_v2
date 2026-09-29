#!/bin/sh
# Sinh chứng thư mTLS cho bề mặt Internal ở Docker Compose Local/Dev (mục 11):
#   ca.crt / ca.key                 CA dev tự ký (ca.key không gắn vào container nào)
#   api-platform.crt / .key         máy chủ listener Internal (SAN api-platform, localhost, 127.0.0.1)
#   optimizer.crt / .key            client của optimizer (CN = SAN = optimizer, AG_INTERNAL_CLIENT_NAMES)
# vào deploy/compose/.certs/ (đã gitignore — không bao giờ commit).
# Dùng: sh scripts/gen-dev-certs.sh [--force]   (gen-dev-secrets.sh tự gọi)
# CHỈ cho Local/Dev. Production cấp chứng thư từ PKI nội bộ trong lãnh thổ (RB-7).
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
dir="$root/deploy/compose/.certs"
if [ -f "$dir/ca.crt" ] && [ -f "$dir/optimizer.key" ] && [ -f "$dir/api-platform.key" ] &&[ "${1:-}" != "--force" ]; then
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

# leaf <tên> <serverAuth|clientAuth> <subjectAltName>
leaf() {
  openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$1.key" 2> /dev/null
  openssl req -new -key "$1.key" -subj "/O=AgriGreen Local-Dev/CN=$1" -out "$tmp/$1.csr"
  printf 'basicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=%s\nsubjectAltName=%s\n' "$2" "$3" > "$tmp/$1.ext"
  openssl x509 -req -in "$tmp/$1.csr" -CA "ca.crt" -CAkey "ca.key" -CAcreateserial -CAserial "$tmp/ca.srl" \
    -days 825 -sha256 -extfile "$tmp/$1.ext" -out "$1.crt" 2> /dev/null
}
leaf api-platform serverAuth "DNS:api-platform,DNS:localhost,IP:127.0.0.1"
leaf optimizer clientAuth "DNS:optimizer"

# Container chạy bằng uid khác người dùng máy (distroless nonroot 65532, optimizer 10001)
# và chỉ gắn tệp ở chế độ đọc: khóa lá phải đọc được. Khóa CA giữ 600.
chmod 755 .
chmod 644 *.crt "api-platform.key" "optimizer.key"
chmod 600 "ca.key"
echo "Đã ghi chứng thư mTLS dev vào $dir (khóa không in ra)."
