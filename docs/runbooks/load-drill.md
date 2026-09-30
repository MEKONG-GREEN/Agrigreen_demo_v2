# Diễn tập tải ĐTK 2 (G4)

Phụ lục A, G4: "diễn tập tải ĐTK 2"; [mkg.md §7](../design/mkg.md#7-trình-tự-g2--g4) hàng G4. Mục 20 đặt hai việc vào nhóm "kiểm thử định kỳ, không chạy mỗi PR": kiểm thử hiệu năng RLS ở khối lượng ĐTK 2, và diễn tập chuyển đổi dự phòng cơ sở dữ liệu giữa lúc đang đồng bộ. Công cụ: `tools/loadgen` (Go). Không dùng k6 vì k6 là AGPL-3.0, ngoài `deploy/policies/allowed-licenses.txt` (RB-4, [ADR-0021](../adr/0021-ha-tang-kubernetes-g4.md)).

**Chỉ chạy trên staging** (hoặc dev dùng chung), dữ liệu tổng hợp hoặc ẩn danh không đảo ngược được (mục 21, R-95). `loadgen` từ chối URL không chứa `staging`/`dev`/`localhost` trừ khi có cờ `-i-know-this-is-not-production`. Staging phải "giống sản xuất về cấu hình" (mục 21): cùng chart, `values-staging.yaml`, cùng số bản.

## 1. Ngưỡng mục tiêu

### 1.1 Ngưỡng có trong tài liệu kiến trúc và BRD (cứng)

| # | Ngưỡng | Nguồn |
|---|---|---|
| N1 | Mọi lời gọi API đồng bộ **p95 < 2 s**. Việc > 2 s phải trả `202` + job | Tài liệu kiến trúc v3.0 mục 11 **R-52**; cảnh báo `http.server.request.duration` p95 > 2 s ([README](README.md) "Chỉ số và cảnh báo") |
| N2 | `GET /v1/geo/standard-map?zoom=13` **p95 < 500 ms** | BRD GIS US-INT-02; [gis-performance.md](gis-performance.md) |
| N3 | `GET /v1/geo/layers?layers=plots&zoom=15` (1.000 polygon trong khung) **p95 ≤ 1,5 s** | BRD GIS US-BASE-01 AC-2; [gis-performance.md](gis-performance.md) |
| N4 | Yêu cầu hỗ trợ → nhiệm vụ tự sinh **≤ 1 phút** (p99 `ag.extension.support_task.latency` < 60 s) | BRD KN US-TASK-01; [README](README.md) |
| N5 | Lô đồng bộ lên ≤ 500 bản ghi; trang kéo xuống ≤ 1000. Không mất bản ghi, không trùng: gửi lặp → `duplicate` | Mục 15 ("chống trùng và không mất bản ghi là yêu cầu mức tài chính"); [sync-contract.md](../dev/sync-contract.md) |
| N6 | Cửa sổ ngoại tuyến **24–48 giờ**. Sau cửa sổ đó thiết bị đẩy dồn toàn bộ hàng đợi | Mục 15 |
| N7 | Agent biên đệm **≥ 72 giờ** ở tốc độ đọc tối đa. Khi kết nối lại, ingest phải nuốt được phần dồn mà không đầy đệm lần nữa | Mục 14; ADR-0019 mục 6; cảnh báo `AgrigreenEdgeBuffer*` |
| N8 | Không sự kiện nào vào DLQ vì quá tải; `ag.outbox.oldest_age` < 300 s; `ag.outbox.backlog` < 1000 | Mục 12 R-62; [README](README.md) "Chỉ số và cảnh báo" |
| N9 | Độ trễ standby < 60 s trong lúc tải | Mục 15 (`standby_replication_lag_seconds`); ADR-0006 |
| N10 | RLS vẫn đạt N1 ở khối lượng ĐTK 2. Vượt → chuyển phân mảnh theo bên tham gia (chỉ đổi cơ chế) | Mục 9 "Ngưỡng theo dõi"; mục 20 |

### 1.2 Khối lượng ĐTK 2 — **tạm, chờ khách**

Tài liệu kiến trúc không cho số người dùng hay tốc độ giao dịch của ĐTK 2. Mục 9 ghi "đo hiệu năng RLS ở khối lượng ĐTK 2"; mục 2 chỉ nêu "nhiều tenant". Hồ sơ tải dưới đây là **mặc định tạm** để diễn tập được. MKG chốt số thật (đề xuất ghi vào câu hỏi mở của G4), sau đó chỉ cần đổi cờ `-rate`.

| Tham số | Tạm | Cách suy ra tốc độ |
|---|---|---|
| Thiết bị hiện trường hoạt động (cán bộ KN, HTX, nhân viên MKG) | 3.000 | — |
| Chu kỳ đồng bộ nền mỗi thiết bị | 15 phút | pull = push = 3.000 / 900 s ≈ **3,3 lượt/s** |
| Hệ số giờ cao điểm (đầu ca, sau mất mạng diện rộng) | × 8 | pull ≈ push ≈ **27 lượt/s** |
| Bản ghi mỗi lượt push | 20 | ≈ 530 bản ghi/s lúc cao điểm |
| Người dùng web đồng thời (web-ops, web-internal, App HTX) | 300, mỗi người một lượt đọc / 10 s | read ≈ **30 lượt/s** |
| Cân tại hub (MKG, HUB_IN/HUB_OUT) | 20 hub × 2 cân | — |
| Tốc độ đọc tối đa của cân | 2 số đo/s (chờ C-6) | 40 cân × 2 = 80 số đo/s; lô 100 số đo → **0,8 lô/s** thường xuyên |
| Phục hồi sau mất kết nối 72 h (N7), ¼ số cân cùng lúc | 10 cân × 72 h × 3.600 × 2 = 5,2 triệu số đo | xả trong ≤ 6 h → 240 số đo/s ≈ **2,4 lô/s** (lô 100) hoặc 0,24 lô/s (lô 1000) |

## 2. Chuẩn bị

1. **Staging đủ cấu hình** (k8s-deploy.md mục 3). Seed dữ liệu tổng hợp mức ĐTK 2:
   - `cmd/seed-synthetic` (`AG_SEED_SCALE=medium`, chạy nhiều tenant);
   - thêm sổ lớn bằng cách chạy lặp.

   Ghi lại số dòng các bảng chính để đối chiếu RLS (N10).
2. **Tài khoản tổng hợp** `accounts.tsv`, mỗi dòng `device_id<TAB>access_token`:
   - `device_id` là UUIDv7 của thiết bị;
   - token của người dùng tổng hợp trong realm staging (vai `field_officer`/`coop_manager`…, phạm vi xã đã seed).

   Người vận hành sinh token bằng client dịch vụ của realm staging và ghi đè tệp định kỳ; `loadgen` đọc lại mỗi `-token-reload` (mặc định 1 phút). Tệp chứa token nên để ngoài repo, quyền 0600, xóa sau diễn tập.
3. **Thiết bị cân tổng hợp** `devices.json`:
   ```json
   [{"device_id":"scale-lt-001","epoch":1,"source_key":"lt-001/source.key","cert":"lt-001/tls/device.crt","key":"lt-001/tls/device.key"}]
   ```
   - mỗi thiết bị: `edge-agent enroll -epoch 1` với `data_dir` riêng;
   - CA thiết bị **staging** ký CSR ([pki.md](pki.md) mục 3);
   - đề xuất + duyệt kỳ hai người ([ingest.md](ingest.md) mục 3).

   Bộ đếm mỗi lần chạy bắt đầu sau `Unix ms × 1000` nên luôn tăng. Khoảng trống bộ đếm giữa các lần sinh cảnh báo `AgrigreenIngestCounterGap` ở staging: đó là dự kiến, ghi vào biên bản. Nâng `AG_INGEST_RATE_PER_MIN`/`AG_INGEST_RATE_BURST` của staging nếu tốc độ mỗi thiết bị vượt giới hạn.
4. **Máy phát** đặt trong mạng của staging:
   - kịch bản API đi qua Ingress công khai của staging, đúng đường người dùng thật (Biên → DMZ);
   - kịch bản ingest đi qua VPN OT (F40).

   Máy phát cần đủ CPU; `dropped` > 0 nghĩa là máy phát bão hòa, không phải hệ chậm. Tăng `-max-inflight` hoặc thêm máy.

## 3. Chạy

```powershell
# A. Nền ổn định 30 phút — hồ sơ thường (N1, N5, N8)
go run ./tools/loadgen -api https://api.staging.agrigreen.example.vn -accounts accounts.tsv -register `
  -rate sync_pull=3.3,sync_push=3.3,read=30 -duration 30m -json a-baseline.json

# B. Cao điểm 15 phút (× 8) + ngưỡng GIS riêng (N2) — chỉ đọc bản đồ chuẩn
go run ./tools/loadgen -api https://api.staging.agrigreen.example.vn -accounts accounts.tsv `
  -rate sync_pull=27,sync_push=27,read=30 -read-paths "/v1/geo/standard-map?zoom=13" `
  -max-p95 "sync_pull=2s,sync_push=2s,read=500ms" -duration 15m -json b-peak.json

# C. Nhận số đo thường xuyên + xả phần dồn 72 h (N7), song song với B
go run ./tools/loadgen -ingest https://ingest.ot.staging.agrigreen.example.vn:8443/v1/ingest/batches `
  -devices devices.json -ingest-ca service-ca.crt -batch-size 1000 -rate ingest_batch=3 -duration 60m -json c-ingest.json

# D. Chuyển đổi dự phòng DB giữa lúc đang đồng bộ (mục 20): trong lúc B chạy
kubectl cnpg promote pg-pf <bản sao> -n agrigreen-data     # hoặc xóa Pod primary
```

`loadgen` in bảng p50/p95/p99/max, tỷ lệ lỗi (gồm lượt bị bỏ) và rps theo kịch bản. Nó cũng in bộ đếm phụ:
- `sync_push.record.accepted|duplicate|conflict|rejected`;
- `sync_pull.changes`;
- `ingest.readings`.

Thoát mã 1 khi vượt `-max-p95` (mặc định 2 s cho mọi kịch bản — N1) hoặc `-max-error-rate` (mặc định 1 %).

Trong lúc chạy, theo dõi Grafana/Prometheus:
- `http.server.request.duration` theo route;
- `ag_outbox_backlog`, `ag_outbox_oldest_age`;
- DLQ (`ag_rabbitmq_dlq_depth`, `rabbitmq_detailed_queue_messages`);
- `ag_db_standby_replication_lag_seconds`;
- CPU/bộ nhớ Pod, HPA `api-platform`/`ingest`;
- PgBouncer `cnpg_pgbouncer_pools_cl_waiting`;
- PostgreSQL `pg_stat_statements`: truy vấn RLS chậm nhất.

## 4. Tiêu chí đạt

- [ ] A và B: loadgen thoát 0. Chỉ số N1 (p95 < 2 s) và N2 (bản đồ chuẩn p95 < 500 ms) đạt ở phía máy chủ.
- [ ] `sync_push.record.rejected` = 0 ngoài lỗi có chủ đích. Chạy lại một tệp kết quả với cùng Idempotency-Key trả `duplicate`, không tạo bản ghi mới: đếm `pf_sync.inbound` trước/sau (N5).
- [ ] C: `ingest_batch` p95 < 2 s, lỗi < 1 %. `AgrigreenIngestBatchesRejected` không kêu. Phần dồn 5,2 triệu số đo xả trong ≤ 6 h (N7).
- [ ] N8: DLQ = 0, `ag.outbox.oldest_age` < 300 s suốt diễn tập. N9: standby lag < 60 s.
- [ ] D: sau chuyển đổi dự phòng, loadgen hồi phục trong ≤ 60 s.
  - Không mất bản ghi: số `accepted` của loadgen = số dòng mới của `pf_sync.inbound`, `status = accepted`.
  - Không trùng. `migrate verify-chains` sạch.
- [ ] N10: p95 các route đọc nặng (`/v1/geo/layers`, `/v1/sync/pull`) không tăng quá 20 % so với staging dữ liệu nhỏ. Nếu tăng: ghi kế hoạch phân mảnh theo bên tham gia (mục 9).
- [ ] Biên bản: phiên bản image, `values-staging.yaml`, tệp JSON của loadgen, ảnh chụp dashboard. Lưu vào kho tài liệu dự án, không vào repo nếu có token.

## 5. Sau diễn tập

- Thu hồi token và tài khoản tổng hợp. Thu hồi kỳ thiết bị tổng hợp trong sổ thiết bị staging. Xóa `accounts.tsv`.
- Mở việc cho mọi ngưỡng không đạt: chỉ mục DB, pool PgBouncer, số bản/HPA, `max_connections`. Đổi cấu hình, **không đổi mã** nếu có thể (R-01).
- Cập nhật mục 1.2 khi MKG chốt khối lượng thật.
