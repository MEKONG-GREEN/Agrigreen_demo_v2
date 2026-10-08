# Sổ tay vận hành

Các job vận hành dùng chung cho mọi cụm (`ag_pf`, `ag_mkg`, `ag_vault`). Mã nguồn ở `internal/shared/opsjobs`, `internal/shared/leader`, `internal/shared/schedule`, `cmd/worker`, `cmd/migrate`.

| Việc | Khi nào | Chạy bằng | Vai DB | Runbook |
|---|---|---|---|---|
| Neo chuỗi + manifest ký số | 00:30 giờ VN hằng ngày | `cmd/worker` (leader) — Kubernetes G4: chỉ cụm pf (`AG_CLUSTERS=pf`); cụm mkg/ts/vault bằng CronJob `anchor-mkg`, `anchor-vault` (`migrate anchor`) | `ag_ops_jobs` | [anchor.md](anchor.md), [k8s-deploy.md](k8s-deploy.md) mục 5 |
| Ghi sổ chỉ số + ảnh chụp lớp GIS (pf/metrics) | 00:45 giờ VN hằng ngày (sau neo); backfill khi cần | `cmd/worker` (leader), `worker -metrics-backfill` | `metrics_app` + vai nguồn; tenant qua `ag_ops_jobs` | [metrics.md](metrics.md) |
| Dọn Idempotency-Key hết hạn | mỗi giờ | `cmd/worker` (leader); Kubernetes G4 cụm mkg/ts/vault: CronJob `purge-idempotency-<vùng>` (`migrate purge-idempotency`) | `ag_ops_jobs` | mục dưới |
| Tạo trước phân vùng tháng | hằng tháng, sau mỗi `migrate up` | `migrate maintain` | `ag_migrator` | [partitions.md](partitions.md) |
| Xác minh chuỗi băm | hằng tuần, sau mỗi lần phục hồi | `migrate verify-chains` | `ag_migrator` | [verify-chains.md](verify-chains.md) |
| Sao lưu PostgreSQL (pgBackRest) + kho đối tượng (`objsync`) | full tuần, diff ngày, WAL liên tục; objsync hằng ngày | pgBackRest, `cmd/objsync` | — | [backup.md](backup.md) |
| Diễn tập phục hồi | CI hằng tuần; thủ công hằng quý trên môi trường sạch | `cmd/drill` | quản trị cụm, chỉ đọc | [restore-drill.md](restore-drill.md) |
| Kiểm đích trong lãnh thổ (RB-7) | mỗi lần khởi động ở production | `internal/shared/residency` | — | [residency.md](residency.md) |
| Deploy một VPS (demo/thử nghiệm) | khi dựng hoặc cập nhật | `docker-compose.vps.yml`, `deploy/vps/prepare.sh` | — | [vps-deploy.md](vps-deploy.md) |
| Triển khai Kubernetes trong lãnh thổ (G4): cài theo vùng, nâng cấp, lùi lại, bí mật, tách vùng, NetworkPolicy ADR-0008 | khi dựng cụm; mỗi phát hành | chart `deploy/k8s/charts/agrigreen`, `deploy/k8s/check.ps1`, OpenTofu `deploy/iac/opentofu` | theo vùng | [k8s-deploy.md](k8s-deploy.md) |
| Bật/tắt Phase K (G5): Public API cho bên thứ ba `api-public` (0 → 2 bản sao, host `partner.<domain>`, pepper khóa API, `AG_EGRESS_MODE=enforce`), cấp bên thứ ba, xoay pepper, rollback = 0 bản sao | khi khách chốt bên thứ ba đầu tiên (diễn tập trên staging trước); xoay pepper ≤ 365 ngày | `values-phasek.yaml` chồng lên `values-prod.yaml`, OpenTofu `phase_k`, `docker compose --profile phasek` (dev) | `public_api`, `apiclient_app`, `metering_app` + vai module nguồn | [phase-k.md](phase-k.md) |
| Cổng tích hợp `integration-gateway` (W5.2, ADR-0033): bật, mTLS đối tác ghi lớn (gắn chứng chỉ ↔ khách hàng API, xoay chứng chỉ), chuyển host `partner` khỏi bí danh `api-public` | khi có đối tác ghi lớn đầu tiên (MKG, W5.4/W5.5); bản phát hành kế tiếp (gỡ bí danh) | `apps.integration-gateway`, Secret `pf-gateway-mtls`, `docker compose --profile gateway` (dev) | `app_public` | [integration-gateway.md](integration-gateway.md) |
| PKI production: CA dịch vụ (cert-manager, mTLS) + CA thiết bị (step-ca + HSM), xoay vòng CA | dựng một lần; lá dịch vụ tự gia hạn 90 ngày; kiểm hằng quý | `deploy/k8s/charts/agrigreen/templates/pki.yaml` | — | [pki.md](pki.md) |
| Diễn tập tải ĐTK 2 (sync push/pull, ingest, đọc) + chuyển đổi dự phòng DB giữa lúc đồng bộ | G4, rồi mỗi thay đổi lớn về khối lượng | `tools/loadgen` (staging, dữ liệu tổng hợp) | — | [load-drill.md](load-drill.md) |
| Khai báo trước hàng đợi consumer RabbitMQ | mỗi lần `outbox-publisher` khởi động | `cmd/outbox-publisher` (`assembly.Queues`) | — | [rabbitmq-queues.md](rabbitmq-queues.md) |
| Giám sát đồng bộ tích hợp, DLQ, cảnh báo Admin | liên tục; cảnh báo khi DLQ > 0 | màn `/quan-tri/giam-sat-dong-bo`, Prometheus + Alertmanager | `sync_app` | [integration-alerts.md](integration-alerts.md) |
| Kiểm độ tươi dữ liệu App HTX (CGH FN-07) | mỗi giờ | `cmd/worker` (leader, job `mechanization_area_feed_check`) | `ag_ops_jobs` (tìm tenant), `mechanization_app` | [integration-alerts.md](integration-alerts.md) |
| Công bố sức chứa kho/Hub theo ngày (GIS US-WH-01) | 01:15 giờ VN hằng ngày, chạy bù khi khởi động | `cmd/api-internal` (leader cụm mkg, job `hub_capacity_daily`; cần `AG_MKG_DB_DIRECT_URL`) | `ag_ops_jobs` (tìm tenant), `hub_app` | mục dưới |
| Bản đồ chuẩn hóa cho TMS, đo độ trễ lớp | khi đổi truy vấn lớp; UAT | kiểm thử `TestStandardMapForTMSLatency_…` | `geo_app` | [gis-performance.md](gis-performance.md) |
| Dựng lại bảng chiếu | diễn tập G4, khi nghi lệch | theo module | vai của module | [rebuild-projection.md](rebuild-projection.md) |
| Agent biên đọc cân: cài, đăng ký, thay khóa, cập nhật | khi lắp/đổi cân, mất đĩa, phát hành | `cmd/edge-agent` | — | [edge-agent.md](edge-agent.md) |
| Nhận số đo cân (ingest), duyệt thiết bị hai người, thay khóa, mất đĩa, vhost `mkg.internal`, cảnh báo, tầng lạnh `ts` | khi lắp/đổi cân, sự cố, hết cửa sổ C-7 | `cmd/ingest`, Admin API mkg, `scripts/dev-edge-bootstrap.sh` (dev) | `ingest_ts`, `ts_reader`, `ingest_mkg` | [ingest.md](ingest.md) |
| PKI thiết bị: CA thiết bị riêng trong lãnh thổ, cấp/thu hồi chứng thư, thay CA | dựng một lần; mỗi kỳ thiết bị; kiểm hằng quý | CA ngoại tuyến + CA trung gian (HSM), `scripts/gen-dev-certs` (dev) | — | [device-pki.md](device-pki.md) |
| Giám sát SQL xuyên tenant (sql_exporter) | liên tục (60 s) | `deploy/observability/sql-exporter/*.collector.yml` | `mkg_monitor`, `ts_monitor` (chỉ EXECUTE hàm gộp) | [ingest.md](ingest.md) mục 7 |
| Leo thang hồ sơ MRV chờ duyệt quá hạn (KN US-MRV-06) | mỗi 15 phút | `cmd/worker` (leader, job `mrv_review_escalation`) | `ag_ops_jobs` (tìm tenant), `mrv_app` | mục dưới |
| Ghi `expired` cho khóa Public API quá hạn / quá hạn chồng lấn (ADR-0022 mục 4, G5) | mỗi giờ, chạy khi khởi động | `cmd/worker` (leader, job `apiclient_key_expiry`; chỉ khi có `AG_PUBLIC_OPERATOR_TENANT`) | `apiclient_app` | [public-api-onboarding.md](public-api-onboarding.md) mục 7 |
| Chuyển vai DB sang vai đăng nhập đơn vị `app_*` (W2.6, ADR-0028): bí mật → bootstrap → migrate → ứng dụng, lùi bản | một lần khi lên bản W2.6 | `migrate bootstrap`, `migrate up`, chart `data` + vùng ứng dụng | `app_*` (module chạy `r_<module>`); vai cũ còn một kỳ | [db-roles-w26.md](db-roles-w26.md) |

Từ W2.6, cột "Vai DB" ghi tên vai cũ hoặc vai module. Đọc tên đó là `r_<module>`, chạy qua vai đăng nhập của đơn vị (`app_worker`, `app_internal`, …); xem [db-roles-w26.md](db-roles-w26.md).

## Leo thang hồ sơ MRV quá hạn (pf/mrv)

Job `mrv_review_escalation` (mỗi 15 phút, chạy bù khi khởi động) tìm tenant có hồ sơ chờ duyệt bằng `ag_ops_jobs`. Vai này chỉ đọc hai cột `tenant_id` và `review_status` của `pf_mrv.evidence_current` (pf 000045). Job ghi bằng vai `mrv_app`, theo hai mức:

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

Cùng job (và `migrate purge-idempotency`) dọn luôn `shared.ledger_idem` — sổ đăng ký `idem_key` của bảng sổ, hạn 30 ngày (W2.1, DB-11, ADR-0012) — bằng `shared.purge_expired_ledger_idem(batch)` (migration `shared/000006`). Log: `ledger_idem_deleted=<số>` (worker) hoặc dòng `xóa … idem_key sổ quá 30 ngày` (CronJob). Bản ghi sổ không bị đụng tới. Sau 30 ngày, ghi lại cùng `idem_key` được nhận như bản ghi mới (kể cả khi job chưa chạy) — job trễ chỉ làm bảng phình, không đổi hành vi chống trùng.

## Chỉ số và cảnh báo (mục 19)

Có khi đặt `AG_OTLP_ENDPOINT`. Ngưỡng dưới đây là đề xuất, chốt khi dựng Alertmanager.

| Metric | Nguồn | Cảnh báo đề xuất |
|---|---|---|
| `ag.rabbitmq.dlq.depth{queue}` | worker (cần `AG_RABBITMQ_MGMT_URL`) | > 0 trong 5 phút (R-62) — luật Prometheus cụ thể và cách xử lý: [integration-alerts.md](integration-alerts.md) |
| `ag.outbox.backlog{cluster}` | outbox-publisher | > 1000 trong 10 phút |
| `ag.outbox.oldest_age{cluster}` | outbox-publisher | > 300 giây |
| `ag.outbox.parked{cluster}` | outbox-publisher | > 0 — sự kiện bị broker từ chối 5 lần đã bị gác (không phát, không chặn sự kiện sau). Xử lý: [rabbitmq-queues.md](rabbitmq-queues.md) "Sự kiện bị gác" |
| `ag.projection.lag{projection}` | module có bảng chiếu, qua `telemetry.RegisterProjectionLag` | > 60 giây (chưa có nguồn ở G1) |
| `ag.job.runs{job,outcome="error"}` | worker | tăng bất kỳ; job `anchor` lỗi là P1 (mục 23) |
| `kube_deployment_status_replicas_available{deployment="api-public"}`, `traefik_service_requests_total` (host `partner`) | kube-state-metrics, Traefik | Phase K: `AgPublicAPIDown` (0 bản sao sẵn sàng 5 phút, P2), 5xx > 5 %, 429 > 20 % — `agrigreen-public-api.rules.yml`, [phase-k.md](phase-k.md) |
| không có `ag.job.runs{job="anchor",outcome="ok"}` trong 26 giờ | worker | P1: mất neo hằng ngày |
| `http.server.request.duration` | api-* | p95 > 2 giây (R-52) |
| `ag.sync.conflict.overdue{tenant_id}` | worker khi có lịch (`registerGauges`; tenant qua `ag_ops_jobs`, migration pf 000043; đếm bằng `sync_app`) | > 0 trong 1 giờ — xung đột đồng bộ quá SLA C-8 (tạm 72 giờ, tham số `sync.conflict_sla_hours`, ADR-0006). Xử lý ở Admin `GET /v1/sync/conflicts?overdue=true` (vai `sync_conflict_resolver`). Luật Prometheus đề xuất: `max by (tenant_id) (ag_sync_conflict_overdue) > 0` for 1h |
| `ag.db.standby_replication_lag` (giây, `cluster`) | worker khi đặt `AG_DB_STANDBY_URL` (DSN tới bản sao chờ; vai thường, không cần quyền giám sát) | > 60 giây trong 5 phút (cảnh báo), > 300 giây (P1) — ADR-0006. Luật đề xuất: `max(ag_db_standby_replication_lag_seconds) > 60` for 5m; thiếu số đo khi đã cấu hình standby (`absent(...)` 15m) = cấu hình sai hoặc DSN trỏ vào primary. Tệp luật ở `deploy/observability/prometheus/` do người tích hợp thêm |
| `ag.k_ref.lag` (histogram giây, `relation`) — Prometheus `ag_k_ref_lag_seconds` | worker, consumer `pf.kref` (W2.5): từ `recorded_at` của sự kiện nguồn tới lúc `k_ref` phản ánh | p99 > 5 giây trong 5 phút → `AgKRefLagHigh` (P2, ADR-0029 §4, `agrigreen-platform.rules.yml`). **k_ref trễ:** xem hàng đợi `pf.kref` và DLQ `pf.kref.dlq`, log `kref:`; sự kiện vào DLQ vì `Nút chưa có trong k_ref` → chạy `worker -kref-rebuild` (dựng lại mọi tenant từ nguồn), rồi đẩy lại DLQ. Sau phục hồi `k_ref` trống/cũ → `worker -kref-rebuild` trước khi mở dịch vụ |
| `ag.db.ledger_default_rows{cluster,table}` (Prometheus `ag_db_ledger_default_rows`) | worker khi có lịch (cụm của lịch, vai `ag_ops_jobs`); sql_exporter target `mkg`, `ts` (`shared.ledger_default_rows()`, shared 000010) | > 0 trong 15 phút (`AgLedgerDefaultPartitionRows`) — từng thiếu phân vùng tháng: chạy `migrate maintain` (tự chuyển dòng), xem [partitions.md](partitions.md) |
| `ag.extension.support_task.latency{assignment_rule}` (giây) | api-platform | p99 > 60 giây trong 5 phút — KN US-TASK-01 (nhiệm vụ tự sinh ≤ 1 phút từ lúc nhận yêu cầu hỗ trợ). Kiểm pool `extension_app`, khóa trên `task_current`. Thêm cảnh báo nghiệp vụ: số yêu cầu `assignment_rule="admin_queue"` > 0 trong 1 giờ → danh bạ trực của xã/tỉnh trống |
