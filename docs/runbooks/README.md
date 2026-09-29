# Sổ tay vận hành

Các job vận hành dùng chung cho mọi cụm (`ag_pf`, `ag_mkg`, `ag_vault`). Mã nguồn ở `internal/shared/opsjobs`, `internal/shared/leader`, `internal/shared/schedule`, `cmd/worker`, `cmd/migrate`.

| Việc | Khi nào | Chạy bằng | Vai DB | Runbook |
|---|---|---|---|---|
| Neo chuỗi + manifest ký số | 00:30 giờ VN hằng ngày | `cmd/worker` (leader) | `ag_outbox_publisher` | [anchor.md](anchor.md) |
| Dọn Idempotency-Key hết hạn | mỗi giờ | `cmd/worker` (leader) | `ag_outbox_publisher` | mục dưới |
| Tạo trước phân vùng tháng | hằng tháng, sau mỗi `migrate up` | `migrate maintain` | `ag_migrator` | [partitions.md](partitions.md) |
| Xác minh chuỗi băm | hằng tuần, sau mỗi lần phục hồi | `migrate verify-chains` | `ag_migrator` | [verify-chains.md](verify-chains.md) |
| Sao lưu PostgreSQL (pgBackRest) + kho đối tượng (`objsync`) | full tuần, diff ngày, WAL liên tục; objsync hằng ngày | pgBackRest, `cmd/objsync` | — | [backup.md](backup.md) |
| Diễn tập phục hồi | CI hằng tuần; thủ công hằng quý trên môi trường sạch | `cmd/drill` | quản trị cụm, chỉ đọc | [restore-drill.md](restore-drill.md) |
| Kiểm đích trong lãnh thổ (RB-7) | mỗi lần khởi động ở production | `internal/shared/residency` | — | [residency.md](residency.md) |
| Deploy một VPS (demo/thử nghiệm) | khi dựng hoặc cập nhật | `docker-compose.vps.yml`, `deploy/vps/prepare.sh` | — | [vps-deploy.md](vps-deploy.md) |
| Khai báo trước hàng đợi consumer RabbitMQ | mỗi lần `outbox-publisher` khởi động | `cmd/outbox-publisher` (`assembly.Queues`) | — | [rabbitmq-queues.md](rabbitmq-queues.md) |
| Giám sát đồng bộ tích hợp, DLQ, cảnh báo Admin | liên tục; cảnh báo khi DLQ > 0 | màn `/quan-tri/giam-sat-dong-bo`, Prometheus + Alertmanager | `sync_app` | [integration-alerts.md](integration-alerts.md) |
| Kiểm độ tươi dữ liệu App HTX (CGH FN-07) | mỗi giờ | `cmd/worker` (leader, job `mechanization_area_feed_check`) | `ag_outbox_publisher` (tìm tenant), `mechanization_app` | [integration-alerts.md](integration-alerts.md) |
| Công bố sức chứa kho/Hub theo ngày (GIS US-WH-01) | 01:15 giờ VN hằng ngày, chạy bù khi khởi động | `cmd/api-internal` (leader cụm mkg, job `hub_capacity_daily`; cần `AG_MKG_DB_DIRECT_URL`) | `ag_outbox_publisher` (tìm tenant), `hub_app` | mục dưới |
| Bản đồ chuẩn hóa cho TMS, đo độ trễ lớp | khi đổi truy vấn lớp; UAT | kiểm thử `TestStandardMapForTMSLatency_…` | `geo_app` | [gis-performance.md](gis-performance.md) |
| Dựng lại bảng chiếu | diễn tập G4, khi nghi lệch | theo module | vai của module | [rebuild-projection.md](rebuild-projection.md) |
| Agent biên đọc cân: cài, đăng ký, thay khóa, cập nhật | khi lắp/đổi cân, mất đĩa, phát hành | `cmd/edge-agent` | — | [edge-agent.md](edge-agent.md) |

## Leader

Nhiều bản `worker` chạy song song được. Chỉ bản giữ khóa tư vấn `727101` (`leader.LockSchedules`) chạy lịch. Khóa là khóa mức phiên trên một kết nối riêng tới cụm `pf`, nên:

- `AG_DB_DIRECT_URL` (hoặc `AG_DB_URL`) phải nối **thẳng** PostgreSQL, không qua PgBouncer.
- Leader chết thì PostgreSQL tự nhả khóa. Bản khác lấy lại trong vòng 15 giây.
- Xem ai đang giữ: `SELECT pid, application_name, backend_start FROM pg_locks JOIN pg_stat_activity USING (pid) WHERE locktype = 'advisory' AND objid = 727101;`

Chạy worker không có lịch: `worker -jobs=none`. Chỉ chạy lịch, không consumer: `worker -modules=none`.

## Công bố sức chứa kho/Hub theo ngày (mkg)

`cmd/api-internal` chạy lịch của vùng mkg khi có `AG_MKG_DB_DIRECT_URL` (nối **thẳng** cụm mkg, không qua PgBouncer) hoặc `AG_MKG_JOBS=all|hub_capacity_daily`; `AG_MKG_JOBS=none` tắt. Chỉ bản giữ khóa tư vấn `727101` trên database `ag_mkg` chạy. Job `hub_capacity_daily` chụp sức chứa mọi cơ sở đang dùng và xuất qua van `hub_capacity` (sổ `mkg_hub.valve_export` + outbox cụm mkg); khóa chống trùng `capacity-daily:<hub>:<YYYY-MM-DD>` nên chạy lại cùng ngày không xuất thêm. pf/geo nhận qua consumer `pf.geo.hub_valve` (số nhập tay của Admin vẫn được giữ — BR-52). Log: `event=job.hub_capacity_daily`, `published`, `skipped`.

Compose Local/Dev chưa đặt `AG_MKG_DB_DIRECT_URL` cho `api-internal` (không sửa `deploy/**` trong đợt này) — thêm `AG_MKG_DB_DIRECT_URL: postgres://postgres:5432/?sslmode=disable` để bật.

## Dọn Idempotency-Key

Hàm `shared.purge_expired_idempotency(batch)` (migration `shared/000002`) xóa theo lô các khóa quá `expires_at` (48 giờ, R-51). Job gọi lặp tới khi lô trả ít hơn `batch`. Log: `event=job.idempotency`, `deleted=<số>`. Bảng phình to thì kiểm log `job.run outcome=error`.

## Chỉ số và cảnh báo (mục 19)

Có khi đặt `AG_OTLP_ENDPOINT`. Ngưỡng dưới đây là đề xuất, chốt khi dựng Alertmanager.

| Metric | Nguồn | Cảnh báo đề xuất |
|---|---|---|
| `ag.rabbitmq.dlq.depth{queue}` | worker (cần `AG_RABBITMQ_MGMT_URL`) | > 0 trong 5 phút (R-62) — luật Prometheus cụ thể và cách xử lý: [integration-alerts.md](integration-alerts.md) |
| `ag.outbox.backlog{cluster}` | outbox-publisher | > 1000 trong 10 phút |
| `ag.outbox.oldest_age{cluster}` | outbox-publisher | > 300 giây |
| `ag.projection.lag{projection}` | module có bảng chiếu, qua `telemetry.RegisterProjectionLag` | > 60 giây (chưa có nguồn ở G1) |
| `ag.job.runs{job,outcome="error"}` | worker | tăng bất kỳ; job `anchor` lỗi là P1 (mục 23) |
| không có `ag.job.runs{job="anchor",outcome="ok"}` trong 26 giờ | worker | P1: mất neo hằng ngày |
| `http.server.request.duration` | api-* | p95 > 2 giây (R-52) |
| `ag.extension.support_task.latency{assignment_rule}` (giây) | api-platform | p99 > 60 giây trong 5 phút — KN US-TASK-01 (nhiệm vụ tự sinh ≤ 1 phút từ lúc nhận yêu cầu hỗ trợ). Kiểm pool `extension_app`, khóa trên `task_current`. Thêm cảnh báo nghiệp vụ: số yêu cầu `assignment_rule="admin_queue"` > 0 trong 1 giờ → danh bạ trực của xã/tỉnh trống |
