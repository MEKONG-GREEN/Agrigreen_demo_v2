#!/usr/bin/env bash
# Báo cáo tiến độ gộp prototype (Demo_KNS + HTML dev-temp) và mã lên dev — chạy 6 giờ/lần (kn-handoff/TWO-AGENT.md).
# Đăng một bình luận lên issue theo dõi (AG_TRACK_ISSUE, mặc định #65) và lưu bản sao ở $AG_HANDOFF_DIR/reports/.
# Chỉ đọc repo; không gộp, không sửa nhãn.
# Dùng: bash scripts/workflow/progress-report.sh [số_giờ=6] [--dry-run]
# Biến môi trường: AG_REPO_ROOT (mặc định: bản checkout chính chứa script), AG_HANDOFF_DIR (mặc định: <cha repo>/kn-handoff,
# nơi có prototype-coverage.tsv), AG_GH_REPO (mặc định: gh repo view), AG_TRACK_ISSUE.
set -u
HOURS=${1:-6}; DRY=${2:-}
SD=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
if [ -n "${AG_REPO_ROOT:-}" ]; then R=$AG_REPO_ROOT; else
  COMMON=$(git -C "$SD" rev-parse --path-format=absolute --git-common-dir) || { echo "không tìm thấy repo git từ $SD" >&2; exit 1; }
  if [ "$(basename "$COMMON")" = ".git" ]; then R=$(dirname "$COMMON"); else R=$(git -C "$SD" rev-parse --show-toplevel); fi
fi
H=${AG_HANDOFF_DIR:-$(dirname "$R")/kn-handoff}
REPO=${AG_GH_REPO:-$(cd "$R" && gh repo view --json nameWithOwner -q .nameWithOwner)}
TRACK=${AG_TRACK_ISSUE:-65}
[ -f "$H/prototype-coverage.tsv" ] || { echo "thiếu $H/prototype-coverage.tsv (đặt AG_HANDOFF_DIR)" >&2; exit 1; }
mkdir -p "$H/reports"
git -C "$R" fetch -q origin +refs/heads/dev:refs/remotes/origin/dev 2>/dev/null
NOW=$(date '+%d/%m/%Y %H:%M')
OUT="$H/reports/progress-$(date +%Y%m%d-%H%M).md"

# Mọi issue kn (mở + đóng) → tiêu đề [PKG] → trạng thái
ISSUES=$(gh issue list -R $REPO --state all --label kn --limit 300 --json number,title,state,labels \
  --jq '.[]|[(.title|capture("^\\[(?<p>[^\\]]+)\\]").p // ""),(.number|tostring),.state,([.labels[].name|select(startswith("st:"))]|join(",")),([.labels[].name|select(startswith("agent:"))]|join(",")|sub("agent:";""))]|@tsv')
status_of(){ # pkg → "trạng thái|#số|agent"
  [ "$1" = "dev" ] && { echo "có sẵn trên dev||"; return; }
  local l; l=$(printf '%s\n' "$ISSUES" | awk -F'\t' -v p="$1" '$1==p{print; exit}')
  [ -z "$l" ] && { echo "chưa có ticket||"; return; }
  local num st lab ag; num=$(cut -f2 <<<"$l"); st=$(cut -f3 <<<"$l"); lab=$(cut -f4 <<<"$l"); ag=$(cut -f5 <<<"$l")
  if [ "$st" = "CLOSED" ]; then echo "đã gộp vào dev|#$num|$ag"; return; fi
  case "$lab" in
    *st:merging*) echo "đang gộp|#$num|$ag";; *st:review*) echo "có mã, chờ review/gộp|#$num|$ag";;
    *st:claimed*) echo "đang làm|#$num|$ag";; *st:blocked*) echo "bị chặn|#$num|$ag";;
    *st:ready*) echo "chưa làm (sẵn sàng)|#$num|$ag";; *) echo "?|#$num|$ag";;
  esac
}

LOCK=$(git -C "$R" ls-remote origin refs/heads/lock/integration | cut -c1-8)
{
echo "## Báo cáo tiến độ G1-1031 — $NOW (kỳ $HOURS giờ)"
echo
if [ -n "$LOCK" ]; then LOCKTXT="đang có người giữ (\`$LOCK\`)"; else LOCKTXT="trống"; fi
echo "**dev:** \`$(git -C "$R" rev-parse --short origin/dev)\` · **Khóa gộp:** $LOCKTXT"
echo
echo "### Đã vào dev trong $HOURS giờ qua"
LOG=$(git -C "$R" log origin/dev --since="$HOURS hours ago" --format='%s' | sed -E 's/^([^:]+):.*/\1/' | sort | uniq -c | sort -rn)
if [ -z "$LOG" ]; then echo "- (không có commit mới)"; else echo "$LOG" | awk '{c=$1; $1=""; printf "- %s (%d commit)\n", substr($0,2), c}'; fi
echo
echo "### Độ phủ prototype"
tail -n +2 "$H/prototype-coverage.tsv" | while IFS=$'\t' read -r src key name dept pkg; do
  s=$(status_of "$pkg"); printf '%s\t%s\t%s\t%s\t%s\n' "$src" "$dept" "$name" "$pkg" "$s"
done > "$H/reports/.cov.tsv"
total=$(wc -l < "$H/reports/.cov.tsv")
for k in "có sẵn trên dev" "đã gộp vào dev" "đang gộp" "có mã, chờ review/gộp" "đang làm" "chưa làm (sẵn sàng)" "bị chặn" "chưa có ticket"; do
  n=$(awk -F'\t' -v k="$k" 'index($5,k)==1' "$H/reports/.cov.tsv" | wc -l); [ "$n" -gt 0 ] && echo "- **$k:** $n/$total"
done
echo
echo "| Nguồn | Phòng | Màn / tính năng | Gói | Trạng thái | Issue | Ai làm |"
echo "|---|---|---|---|---|---|---|"
awk -F'\t' '$5 !~ /^có sẵn trên dev/' "$H/reports/.cov.tsv" | sort -t$'\t' -k5,5 | while IFS=$'\t' read -r src dept name pkg s; do
  IFS='|' read -r st num ag <<<"$s"; echo "| $src | $dept | $name | $pkg | $st | $num | $ag |"; done
echo
echo "_Nguồn A = app Demo_KNS (zip), B = HTML trên dev-temp, X = phụ thuộc dữ liệu/pháp chế của khách. Các màn \"có sẵn trên dev\" không liệt kê lại._"
echo
echo "### Cần người quyết / đang chặn"
printf '%s\n' "$ISSUES" | awk -F'\t' '$3=="OPEN" && $4 ~ /st:blocked/ {printf "- [%s] #%s bị chặn\n", $1, $2}'
REV=$(printf '%s\n' "$ISSUES" | awk -F'\t' '$3=="OPEN" && $4 ~ /st:review/ {printf "[%s] #%s (%s), ", $1, $2, $5}')
[ -n "$REV" ] && echo "- Chờ review chéo / gộp: ${REV%, }"
} > "$OUT"

echo "$OUT"
if [ "$DRY" != "--dry-run" ]; then gh issue comment $TRACK -R $REPO --body-file "$OUT" >/dev/null && echo "posted to #$TRACK"; fi
