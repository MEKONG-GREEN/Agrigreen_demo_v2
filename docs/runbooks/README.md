# Sổ tay vận hành

Các job vận hành dùng chung cho mọi cụm (`ag_pf`, `ag_mkg`, `ag_vault`). Mã nguồn ở `internal/shared/opsjobs`, `internal/shared/leader`, `internal/shared/schedule`, `cmd/worker`, `cmd/migrate`.

| Việc | Khi nào | Chạy bằng | Vai DB | Runbook |
|---|---|---|---|---|
| Neo chuỗi + manifest ký số | 00:30 giờ VN hằng ngày | `cmd/worker` (leader) — Kubernetes G4: chỉ cụm pf (`AG_CLUSTERS=pf`); cụm mkg/ts/vault bằng CronJob `anchor-mkg`, `anchor-vault` (`migrate anchor`) | `ag_outbox_publisher` | [anchor.md](anchor.md), [k8s-deploy.md](k8s-deploy.md) mục 5 |
| Ghi sổ chỉ số + ảnh chụp lớp GIS (pf/metrics) | 00:45 giờ VN hằng ngày (sau neo); backfill khi cần | `cmd/worker` (leader), `worker -metrics-backfill` | `metrics_app` + vai nguồn; tenant qua `ag_outbox_publisher` | [metrics.md](metrics.md) |
| Dọn Idempotency-Key hết hạn | mỗi giờ | `cmd/worker` (leader); Kubernetes G4 cụm mkg/ts/vault: CronJob `purge-idempotency-<vùng>` (`migrate purge-idempotency`) | `ag_outbox_publisher` | mục dưới |
| Tạo trước phân vùng tháng | hằng tháng, sau mỗi `migrate up` | `migrate maintain` | `ag_migrator` | [partitions.md](partitions.md) |
| Xác minh chuỗi băm | hằng tuần, sau mỗi lần phục hồi | `migrate verify-chains` | `ag_migrator` | [verify-chains.md](verify-chains.md) |
| Sao lưu PostgreSQL (pgBackRest) + kho đối tượng (`objsync`) | full tuần, diff ngày, WAL liên tục; objsync hằng ngày | pgBackRest, `cmd/objsync` | — | [backup.md](backup.md) |
| Diễn tập phục hồi | CI hằng tuần; thủ công hằng quý trên môi trường sạch | `cmd/drill` | quản trị cụm, chỉ đọc | [restore-drill.md](restore-drill.md) |
| Kiểm đích trong lãnh thổ (RB-7) | mỗi lần khởi động ở production | `internal/shared/residency` | — | [residency.md](residency.md) |
| Deploy một VPS (demo/thử nghiệm) | khi dựng hoặc cập nhật | `docker-compose.vps.yml`, `deploy/vps/prepare.sh` | — | [vps-deploy.md](vps-deploy.md) |
| Triển khai Kubernetes trong lãnh thổ (G4): cài theo vùng, nâng cấp, lùi lại, bí mật, tách vùng, NetworkPolicy ADR-0008 | khi dựng cụm; mỗi phát hành | chart `deploy/k8s/charts/agrigreen`, `deploy/k8s/check.ps1`, OpenTofu `deploy/iac/opentofu` | theo vùng | [k8s-deploy.md](k8s-deploy.md) |
| PKI production: CA dịch vụ (cert-manager, mTLS) + CA thiết bị (step-ca + HSM), xoay vòng CA | dựng một lần; lá dịch vụ tự gia hạn 90 ngày; kiểm hằng quý | `deploy/k8s/charts/agrigreen/templates/pki.yaml` | — | [pki.md](pki.md) |
| Diễn tập tải ĐTK 2 (sync push/pull, ingest, đọc) + chuyển đổi dự phòng DB giữa lúc đồng bộ | G4, rồi mỗi thay đổi lớn về khối lượng | `tools/loadgen` (staging, dữ liệu tổng hợp) | — | [load-drill.md](load-drill.md) |
| Khai báo trước hàng đợi consumer RabbitMQ | mỗi lần `outbox-publisher` khởi động | `cmd/outbox-publisher` (`assembly.Queues`) | — | [rabbitmq-queues.md](rabbitmq-queues.md) |
| Giám sát đồng bộ tích hợp, DLQ, cảnh báo Admin | liên tục; cảnh báo khi DLQ > 0 | màn `/quan-tri/giam-sat-dong-bo`, Prometheus + Alertmanager | `sync_app` | [integration-alerts.md](integration-alerts.md) |
| Kiểm độ tươi dữ liệu App HTX (CGH FN-07) | mỗi giờ | `cmd/worker` (leader, job `mechanization_area_feed_check`) | `ag_outbox_publisher` (tìm tenant), `mechanization_app` | [integration-alerts.md](integration-alerts.md) |
| Công bố sức chứa kho/Hub theo ngày (GIS US-WH-01) | 01:15 giờ VN hằng ngày, chạy bù khi khởi động | `cmd/api-internal` (leader cụm mkg, job `hub_capacity_daily`; cần `AG_MKG_DB_DIRECT_URL`) | `ag_outbox_publisher` (tìm tenant), `hub_app` | mục dưới |
| Bản đồ chuẩn hóa cho TMS, đo độ trễ lớp | khi đổi truy vấn lớp; UAT | kiểm thử `TestStandardMapForTMSLatency_…` | `geo_app` | [gis-performance.md](gis-performance.md) |
| Dựng lại bảng chiếu | diễn tập G4, khi nghi lệch | theo module | vai của module | [rebuild-projection.md](rebuild-projection.md) |
| Agent biên đọc cân: cài, đăng ký, thay khóa, cập nhật | khi lắp/đổi cân, mất đĩa, phát hành | `cmd/edge-agent` | — | [edge-agent.md](edge-agent.md) |
| Nhận số đo cân (ingest), duyệt thiết bị hai người, thay khóa, mất đĩa, vhost `mkg.internal`, cảnh báo, tầng lạnh `ts` | khi lắp/đổi cân, sự cố, hết cửa sổ C-7 | `cmd/ingest`, Admin API mkg, `scripts/dev-edge-bootstrap.sh` (dev) | `ingest_ts`, `ts_reader`, `ingest_mkg` | [ingest.md](ingest.md) |
| PKI thiết bị: CA thiết bị riêng trong lãnh thổ, cấp/thu hồi chứng thư, thay CA | dựng một lần; mỗi kỳ thiết bị; kiểm hằng quý | CA ngoại tuyến + CA trung gian (HSM), `scripts/gen-dev-certs` (dev) | — | [device-pki.md](device-pki.md) |
| Giám sát SQL xuyên tenant (sql_exporter) | liên tục (60 s) | `deploy/observability/sql-exporter/*.collector.yml` | `mkg_monitor`, `ts_monitor` (chỉ EXECUTE hàm gộp) | [ingest.md](ingest.md) mục 7 |
| Leo thang hồ sơ MRV chờ duyệt quá hạn (KN US-MRV-06) | mỗi 15 phút | `cmd/worker` (leader, job `mrv_review_escalation`) | `ag_outbox_publisher` (tìm tenant), `mrv_app` | mục dưới |

## Leo thang hồ sơ MRV quá hạn (pf/mrv)

Job `mrv_review_escalation` (mỗi 15 phút, chạy bù khi khởi động) tìm tenant có hồ sơ chờ duyệt bằng `ag_outbox_publisher`. Vai này chỉ đọc hai cột `tenant_id` và `review_status` của `pf_mrv.evidence_current` (pf 000045). Job ghi bằng vai `mrv_app`, theo hai mức:

- Hồ sơ vào hệ thống quá `mrv.review_sla_hours` (mặc định 72) → cảnh báo mức 1 cho người duyệt (`mrv_reviewer`).
- Quá `mrv.review_escalate_hours` (mặc định 120) → mức 2 cho quản trị TTKN tỉnh (`kn_province_admin`).

Hai mặc định trên là **tạm, chờ khách**; đổi bằng tham số masterdata có duyệt hai người.

Khóa chống trùng `overdue:<phiên bản hồ sơ>:<mức>`: chạy lại hoặc chạy chồng không báo hai lần. Sửa hồ sơ (bản ghi bù) tạo phiên bản mới với hạn mới. Log: `event=job.mrv_review_escalation`, `raised`. Xem cảnh báo: `GET /v1/mrv/alerts?kind=review_overdue`.

## Leader

Nhiều bản `worker` chạy song song được. Chỉ bản giữ khóa tư vấn `727101` (`leader.LockSchedules`) chạy lịch. Khóa là khóa mức phiên trên một kết nối riêng tới cụm `pf`, nên:

- `AG_DB_DIRECT_URL` (hoặc `AG_DB_URL`) phải nối **thẳng** PostgreSQL, không qua PgBouncer.
- Leader chết thì PostgreSQL tự nhả khóa. Bản khác lấy lại trong vòng 15 giây.
- Xem ai đang giữ: `SELECT pid, application_name, backend_start FROM pg_locks JOIN pg_stat_activity USING (pid) WHERE locktype = 'advisory' AND objid = 727101;`

Chạy worker không có lịch: `worker -jobs=none`. Chỉ chạy lịch, không consumer: `worker -modules=none`.

## Công bố sức chứa kho/Hub theo ngày (mkg)

`cmd/api-internal` chạy lịch của vùng mkg khi có `AG_MKG_DB_DIRECT_URL` (nối **thẳng** cụm mkg, không qua PgBouncer) hoặc `AG_MKG_JOBS=all|hub_capacity_daily`; `AG_MKG_JOBS=none` tắt. Chỉ bản giữ khóa tư vấn `727101` trên database `ag_mkg` chạy. Job `hub_capacity_daily` chụp sức chứa mọi cơ sở đang dùng và xuất qua van `hub_capacity` (sổ `mkg_hub.valve_export` + outbox cụm mkg); khóa chống trùng `capacity-daily:<hub>:<YYYY-MM-DD>` nên chạy lại cùng ngày không xuất thêm. pf/geo nhận qua consumer `pf.geo.hub_valve` (số nhập tay của Admin vẫn được giữ — BR-52). Log: `event=job.hub_capacity_daily`, `published`, `skipped`.

Compose Local/Dev chưa đặt `AG_MKG_DB_DIRECT_URL` cho `api-internal` (không sửa `deploy/**` trong đợt này) — thêm `AG_MKG_DB_DIRECT_URL: postgres://postgres:5432/?sslmode=disable` để bật.

## Cấp quyền chia sẻ nền tảng → MKG (ADR-0020)

MKG là tenant **riêng**. Muốn MKG đọc được HTX, thửa, ngày thu hoạch, phản hồi HTX, và muốn nền tảng nhận van MKG vào tenant chương trình, cần một cấp quyền chia sẻ.

Các bước ở tenant chương trình (Admin API, MFA):
1. Một `platform_admin` gọi `POST /v1/share-grants` với `grantee_tenant_id` = tenant MKG, `kinds`, `purpose`, `valid_to`.
2. Một `platform_admin` **khác** gọi `POST /v1/share-grants/{id}/approve`.
3. Thu hồi: `POST /v1/share-grants/{id}/revoke` kèm lý do.
4. Sổ đọc của MKG: `GET /v1/share-grants/{id}/reads`.

Cấu hình `api-internal`: `AG_PLATFORM_API_URL`, `AG_PLATFORM_TOKEN_URL`, `AG_MKG_SA_CLIENT_SECRET`, `AG_MKG_TENANT_ID`, `AG_PARTNER_GRANTOR_TENANT` = tenant chương trình (`deploy/compose/.env.example`).

DLQ `pf.geo.hub_valve`, `pf.cropcycle.htx_collection_valve`, `pf.mrv.collection_evidence` có lỗi `party.valve_share_grant_missing` nghĩa là tenant phong bì chưa có cấp quyền `valve_*` đang hiệu lực. Duyệt cấp quyền rồi chuyển lại thông điệp từ DLQ; xử lý là idempotent.

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
| `ag.sync.conflict.overdue{tenant_id}` | worker khi có lịch (`registerGauges`; tenant qua `ag_outbox_publisher`, migration pf 000043; đếm bằng `sync_app`) | > 0 trong 1 giờ — xung đột đồng bộ quá SLA C-8 (tạm 72 giờ, tham số `sync.conflict_sla_hours`, ADR-0006). Xử lý ở Admin `GET /v1/sync/conflicts?overdue=true` (vai `sync_conflict_resolver`). Luật Prometheus đề xuất: `max by (tenant_id) (ag_sync_conflict_overdue) > 0` for 1h |
| `ag.db.standby_replication_lag` (giây, `cluster`) | worker khi đặt `AG_DB_STANDBY_URL` (DSN tới bản sao chờ; vai thường, không cần quyền giám sát) | > 60 giây trong 5 phút (cảnh báo), > 300 giây (P1) — ADR-0006. Luật đề xuất: `max(ag_db_standby_replication_lag_seconds) > 60` for 5m; thiếu số đo khi đã cấu hình standby (`absent(...)` 15m) = cấu hình sai hoặc DSN trỏ vào primary. Tệp luật ở `deploy/observability/prometheus/` do người tích hợp thêm |
| `ag.extension.support_task.latency{assignment_rule}` (giây) | api-platform | p99 > 60 giây trong 5 phút — KN US-TASK-01 (nhiệm vụ tự sinh ≤ 1 phút từ lúc nhận yêu cầu hỗ trợ). Kiểm pool `extension_app`, khóa trên `task_current`. Thêm cảnh báo nghiệp vụ: số yêu cầu `assignment_rule="admin_queue"` > 0 trong 1 giờ → danh bạ trực của xã/tỉnh trống |
