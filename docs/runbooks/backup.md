# Sao lưu PostgreSQL và kho đối tượng

Mục 22 của tài liệu kiến trúc: PostgreSQL full hằng tuần, differential hằng ngày, WAL liên tục (pgBackRest); kho đối tượng kiểm kê + sao chép gia tăng theo checksum sang vùng thứ hai trong lãnh thổ; neo chuỗi ký số hằng ngày; **credential ghi sao lưu tách khỏi credential phục hồi**. Diễn tập phục hồi: [restore-drill.md](restore-drill.md).

## Thành phần

| Thành phần | Ở đâu | Ghi chú |
|---|---|---|
| pgBackRest trong container `postgres` | [deploy/compose/backup/](../../deploy/compose/backup) (image `agrigreen/postgres-pgbackrest`), [docker-compose.backup.yml](../../deploy/compose/docker-compose.backup.yml) | `archive_mode=on`, `archive_command=pgbackrest --stanza=agrigreen archive-push %p`, `archive_timeout=60`. Kho `repo1` trên volume **external** `agrigreen-backup-pgbackrest`, mã hóa AES-256-CBC (`PGBACKREST_CIPHER_PASS`) |
| Kho S3 thứ hai `backup-s3` | cùng tệp, profile `backup` | Bucket `agrigreen-backup`, volume external `agrigreen-backup-objects`, cổng `127.0.0.1:9100` |
| `cmd/objsync` | [cmd/objsync](../../cmd/objsync), [internal/shared/objsync](../../internal/shared/objsync) | Liệt kê + chép + SHA-256 tính lại từ nội dung; manifest `manifests/objects/<UTC>.json` |
| `migrate anchor` | [cmd/migrate/anchor.go](../../cmd/migrate/anchor.go) | Neo một lần ngoài lịch (neo bù, hoặc ngay trước sao lưu khi diễn tập) |

## Danh tính và quyền (tách ghi/đọc)

| Danh tính | Dùng bởi | Quyền |
|---|---|---|
| tiến trình `postgres` (archive-push, backup) | pgBackRest trong container DB | ghi volume kho pgBackRest |
| `pgbackrest-restore` | chỉ khi phục hồi | volume kho gắn **`:ro`** + mật khẩu mã hóa — không ghi được bản sao lưu |
| `backup-writer` (S3 thứ hai) | `objsync backup` | Read/Write/List bucket `agrigreen-backup` |
| `restore-reader` (S3 thứ hai) | `objsync restore`, `drill check` | Read/List — **không ghi** |
| `backup-admin` (S3 thứ hai) | chỉ `backup-s3-init` tạo bucket | quản trị |
| `agrigreen-app` (kho chính) | `objsync` đọc nguồn / ghi khi phục hồi | như ứng dụng |

Giới hạn Local/Dev: hành động `Write` của SeaweedFS gồm cả xóa, nên "backup-writer không xóa được" chỉ kiểm chứng được trên kho thật (production: không cấp `s3:DeleteObject`, bật object lock COMPLIANCE). Nguồn của `objsync backup` đang dùng danh tính ứng dụng `agrigreen-app` vì định nghĩa `seaweedfs` gốc không được sửa ở đợt này; production dùng một danh tính **chỉ đọc** riêng trên kho chính.

## Chạy ở Local/Dev (Docker)

```bash
sh scripts/gen-dev-secrets.sh          # .env cũ thiếu biến sao lưu → --force rồi down -v
docker volume create agrigreen-backup-pgbackrest
docker volume create agrigreen-backup-objects
C="docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.backup.yml"
$C --profile backup up -d --build --wait
$C exec -T -u postgres postgres pgbackrest --stanza=agrigreen stanza-create   # một lần
$C exec -T -u postgres postgres pgbackrest --stanza=agrigreen check
$C exec -T -u postgres postgres pgbackrest --stanza=agrigreen --type=full backup   # hằng tuần
$C exec -T -u postgres postgres pgbackrest --stanza=agrigreen --type=diff backup   # hằng ngày
$C exec -T -u postgres postgres pgbackrest --stanza=agrigreen info
```

Kho đối tượng (từ máy có Go; biến lấy từ `deploy/compose/.env`):

```bash
AG_ENV=local \
AG_S3_ENDPOINT=127.0.0.1:9000 AG_S3_BUCKET=agrigreen AG_S3_ACCESS_KEY=agrigreen-app AG_S3_SECRET_KEY=… AG_S3_USE_TLS=false \
AG_BACKUP_S3_ENDPOINT=127.0.0.1:9100 AG_BACKUP_S3_BUCKET=agrigreen-backup AG_BACKUP_S3_ACCESS_KEY=backup-writer AG_BACKUP_S3_SECRET_KEY=… AG_BACKUP_S3_USE_TLS=false \
  go run ./cmd/objsync backup -label pf
```

Kết quả là JSON: `copied`, `skipped` (đã có cùng băm — gia tăng), `manifest_key`. Chạy lại an toàn. `-dst-prefix backups/` khi buộc phải dùng chung bucket (khóa `backups/…` và `manifests/…` không bị sao lưu lại).

## Lịch production (đề xuất CronJob)

| Việc | Lịch (giờ VN) |
|---|---|
| WAL | liên tục (`archive_command`, `archive_timeout=60`) |
| `pgbackrest --type=full backup` | Chủ nhật 01:00 |
| `pgbackrest --type=diff backup` | thứ Hai–thứ Bảy 01:00 |
| `objsync backup` | hằng ngày 01:30 (sau neo 00:30, để manifest neo có trong bản sao) |
| `pgbackrest check`, `info` | sau mỗi lần sao lưu; cảnh báo P1 nếu thất bại |

Repo production là S3 trong lãnh thổ (TLS) có object lock; `repo1-retention-full-type=time`, `repo1-retention-full=30`, `repo1-retention-diff=7`.

### Thời gian giữ: tối thiểu 30 ngày (KN v7.7 NFR)

- **Theo ngày, không theo số bản.** Trước đây `repo1-retention-full=4` (đếm bản) với full hằng tuần chỉ giữ ~28 ngày — dưới yêu cầu ≥ 30 ngày. Nay `repo1-retention-full-type=time` + `repo1-retention-full=30`: pgBackRest chỉ cho một full hết hạn khi đã có full khác cũ hơn 30 ngày, nên luôn phục hồi (PITR) được về bất kỳ thời điểm nào trong 30 ngày gần nhất; thực tế giữ 5–6 full tuần (tới ~37 ngày). WAL hết hạn theo full (mặc định `repo1-retention-archive-type=full`), diff giữ 7 bản gần nhất (không ảnh hưởng PITR vì WAL còn).
- Compose/Local: [deploy/compose/backup/pgbackrest.conf](../../deploy/compose/backup/pgbackrest.conf). K8s (CloudNativePG + plugin pgBackRest): `postgres.backup.retentionFullType: time`, `retentionFull: 30` trong [values.yaml](../../deploy/k8s/charts/agrigreen/values.yaml) → `Stanza.spec…retentionPolicy {full: 30, fullType: time}`; `deploy/k8s/tests/check_flows.py` (chạy bởi `deploy/k8s/check.ps1`) từ chối mọi Stanza dưới 30 ngày hoặc đếm theo bản.
- Object lock của bucket sao lưu (COMPLIANCE) đặt **≥ 38 ngày** (30 ngày + một chu kỳ full tuần + biên) để `expire` không vướng khóa với bản còn trong cửa sổ, và không cho xóa sớm hơn cửa sổ phục hồi.
- Dung lượng: kho cần ~6 full + diff + WAL của 37 ngày — kiểm `pgbackrest info` sau đợt chuyển; kho cũ (đếm 4 bản) không mất bản nào khi đổi, chỉ ngừng hết hạn tới khi đủ 30 ngày.
- Kiểm hằng tháng: `pgbackrest info` phải có full cũ nhất ≥ 30 ngày (sau 5 tuần vận hành); diễn tập phục hồi về mốc 29 ngày trước ít nhất mỗi quý ([restore-drill.md](restore-drill.md)).

## Khi lỗi

- `archive-push` lỗi (`pg_stat_archiver.failed_count` tăng): WAL tích tụ trong `pg_wal` — xử lý trước khi đầy đĩa. Thường do chưa `stanza-create` hoặc sai `PGBACKREST_CIPHER_PASS`.
- `objsync` báo `nội dung nguồn không khớp băm khai báo`: đối tượng ở kho chính hỏng. **Không** chép đè. Xử lý như sự cố P1, đối chiếu bản sao lưu cũ.
- `objsync` báo `ErrExists`: kho sao lưu đã có cùng khóa khác nội dung — dấu hiệu bị sửa. Không xóa; báo an ninh.
