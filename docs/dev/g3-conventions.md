# Quy ước phát triển G3

Đọc cùng [g1-conventions.md](g1-conventions.md) và [g2-conventions.md](g2-conventions.md). Mọi quy ước G1, G2 vẫn áp dụng: cấu trúc module, RLS, sổ chỉ ghi thêm, PII chỉ ở vault, kiểm thử bắt buộc, tiền tố mã story, lệnh kiểm tra trước khi báo xong. Tài liệu này chỉ ghi phần **thêm** cho G3.

Cổng G3 (tài liệu kiến trúc Phụ lục A, Q3/2027) gồm ba phần:
- `pf/metrics` có lịch sử;
- `mkg/measurement` có đường nhận số liệu cân của khách;
- agent biên theo mẫu cân đã chốt.

G3-0 (30/09/2026) dựng nền và **đóng băng hợp đồng** để các đợt G3-1/G3-2 làm song song. Kế hoạch đợt ở [roadmap](../roadmap.md#g3-q32027).

## 1. Quyết định đã chốt (30/09/2026)

| Chủ đề | Quyết định | ADR |
|---|---|---|
| Kho chuỗi thời gian | Cụm mới `ts`, database `ag_ts`. PostgreSQL 17 thuần, phân vùng tháng theo `received_at`, bố cục tương thích Timescale. **Không dùng TimescaleDB TSL** (nén, continuous aggregate) vì RB-4. Schema `ts_weigh` dùng từ G3, `ts_env` để dành. Tầng lạnh: tách phân vùng → xuất nén có ký số vào kho đối tượng trong lãnh thổ (R-71; cửa sổ kiểm toán C-7 chờ khách) | [0017](../adr/0017-kho-chuoi-thoi-gian.md) |
| Sự kiện nội bộ MKG (Q-MKG-01) | Vhost RabbitMQ riêng `mkg.internal`, tài khoản nền tảng không bind được. Cột `shared.outbox.kind` (`platform`/`internal`). Chú thích `//ag:internal-event <tên> v1`, **chỉ** trong `internal/mkg/**`, schema ở `contracts/events-internal/`. `contractcheck` bắt payload nội bộ không bao giờ đi exchange nền tảng. Consumer ở `cmd/worker-internal` | [0018](../adr/0018-su-kien-noi-bo-mkg.md) |
| Định danh thiết bị và đồng hồ | Mỗi thiết bị có chứng thư mTLS client (CA nội bộ; dev: `scripts/gen-dev-certs`) và một khóa ký nguồn Ed25519, đăng ký qua sổ thiết bị có duyệt. Lô là một dải bộ đếm đơn điệu; idempotency theo `(device_id, epoch, counter)`. Thời gian thiết bị và độ lệch NTP được ghi nhưng **không** dùng làm thứ tự. Mất đĩa → kỳ đăng ký mới, không dùng lại bộ đếm. Đệm ≥ 72 giờ; đầy thì ngừng và báo động | [0019](../adr/0019-dinh-danh-thiet-bi-va-dong-ho.md) |
| Phiếu cân (R-70) | Chỉ `cmd/ingest` tích phân luồng thô thành phiếu cân. Phiếu ghi `rule_version` và các `reading_batch` đã dùng. Cửa sổ lô tính theo **bộ đếm** | — |

### 1.1 Đặc tả rút từ prototype `mekong-green` (mặc định chờ khách)

Không có BRD hay backlog nào nói về cân, cuộn, độ ẩm quy mô lớn, dung sai, IoT/biên hay giá thành. Vì vậy mọi quy tắc đo lường hoặc là **quyết định của khách**, hoặc chép quy tắc (không chép mã) từ prototype. Các số dưới đây là **tham số có phiên bản** (người đặt ≠ người duyệt), mặc định lấy theo prototype, và **chờ C-4**:

| Quy tắc | Mặc định (prototype) | Nơi dùng |
|---|---|---|
| Chênh khối lượng cân so với ước lượng từ số cuộn | cờ > 5 %, nghiêm trọng > 15 % | `mkg/measurement` đối soát |
| Phiếu nhận hàng so với đơn mua | cảnh báo > 3 % (chỉ cảnh báo) | `mkg/measurement` / `orders` (G4) |
| kg/cuộn học theo HTX | Σ kg cân / Σ cuộn của 30 lần xếp hàng đã cân gần nhất → hệ thống → cấu hình → 20 | `mkg/fieldops` (= `yield_kg_per_roll`, **không bao giờ qua van**) |
| Công đoạn đếm cuộn | cuộn → tập kết → xếp ghe/xe; số cuộn mỗi công đoạn phải bằng công đoạn trước (FM-04). Bắt buộc có số cuộn; chỉ nhập tấn khi không có cuộn | `mkg/fieldops` |
| Phiếu cân nhà máy | chỉ ghi thêm; tịnh = tổng − bì > 0; ghi số cuộn nhà máy đếm lại | `mkg/measurement` (CUSTOMER_IN) |
| Rủi ro kho | độ ẩm 18/22 %, nhiệt độ 40/55 °C (khớp `hub.risk.v1`) | `mkg/hub` G3 (`hub.risk.v2` khi có cảm biến `ts_env`) |

- **Hợp đồng tính giá theo tấn nhà máy cân HOẶC theo cuộn** (cả hai có trong prototype). Với hợp đồng **theo tấn**, đối chiếu số cuộn ↔ khối lượng cân là **kiểm chéo hai bên theo RB-6**: `mkg.fieldops.roll_count.recorded` → measurement. Chưa biết hợp đồng thật dùng cách nào (câu hỏi khách).
- Độ ẩm: hợp đồng prototype có trần 18 % nhưng không cưỡng chế, không khấu trừ. Trạng thái ẩm khi cân vẫn là C-4.
- **CO2**: hệ số đốt lộ thiên 1460 kgCO2e/t (nhãn IPCC EFDB), vận chuyển đường bộ 0,062 và đường thủy 0,021 kgCO2e/t·km. Chỉ đưa vào như **tham số có phiên bản** trong `pf/masterdata`, kèm `rule_version`, gắn cờ "chờ khách ký phương pháp". Chưa có net, baseline, leakage, chất khô. `pf/mrv` G3 **không** công bố tCO2e trước khi C-1 và phương pháp được ký.
- Phần chia doanh thu carbon (45 %; phần còn lại 70/15/15) thuộc `mkg/costing` G4.
- Dữ liệu mẫu tất định cho R-95 và kiểm thử golden: một hợp đồng theo tấn và một theo cuộn; một lần xếp hàng 5800/5700 cuộn, cân 96,8 t, bị cờ +10 %.
- **GIS snapshot/replay** (GIS FN-18..21) thuộc phạm vi `pf/metrics`:
  - nhật ký thay đổi lớp chỉ ghi thêm, có trước/sau và nguồn (GIS US-EVL-01, Must);
  - ảnh chụp cuối ngày kèm watermark `last_event_id`;
  - `replay(D)` = ảnh chụp mới nhất ≤ D + sự kiện sau watermark;
  - lưu trữ/nén là chuyển tầng, không xóa (RB-1). Số ngày lưu chờ khách.

  Xem `metrics/port.LayerLog`.

## 2. Số migration

Chỉ dùng số đã cấp. Số trống có thể còn lại giữa các khoảng.

| Cụm | Số | Nội dung | Schema | Vai DB |
|---|---|---|---|---|
| shared | 000003 | `shared.apply_append_only`; `v_missing_immutability` bắt mọi bảng `ts_*` (G3-0, đã có) | `shared` | `ag_migrator` |
| shared | 000004 | `shared.outbox.kind` + CHECK `internal ⇒ mkg.*` (G3-0, đã có) | `shared` | `ag_migrator` |
| ts | **000001** | `ts_weigh.reading_batch`, `ts_weigh.raw_reading` (G3-0, đã có) | `ts_weigh` | `ingest_ts`, `ts_reader` |
| ts | 000002 | **dự phòng** `ts_env`: cảm biến kho, telemetry phương tiện | `ts_env` | xin khi làm |
| mkg | 000006 | fieldops G3: lệnh gom, công đoạn đếm, xác nhận HTX, sổ van `valve_export` | `mkg_fieldops` | `fieldops_app` |
| mkg | 000007–000008 | measurement: sổ thiết bị, phiếu cân, bảng chiếu `lot_mass_current`, dung sai/định mức có phiên bản, đối soát, tranh chấp, sổ van | `mkg_measurement` | `measurement_app`, `ingest_mkg` |
| mkg | 000009 | hub G3: khối lượng lô từ phiếu cân (Q-MKG-04), kiểm kê (Q-MKG-06), `hub.risk.v2` | `mkg_hub` | `hub_app` |
| pf | 000036–000037 | metrics: định nghĩa, giá trị theo ngày, ảnh chụp + watermark, nhật ký lớp GIS | `pf_metrics` | `metrics_app` |
| pf | 000038 | mrv G3: liên kết bằng chứng thu gom (van `mrv_collection_evidence`), đối chiếu nhật ký HTX ↔ KN (KN US-MRV-02) | `pf_mrv` | `mrv_app` |
| pf | 000039–000042 | **dự phòng** G3 — xin người tích hợp | | |
| mkg | 000010–000012 | **dự phòng** G3 — xin người tích hợp | | |

- `pf` 000030–000035 và `mkg` 000004–000005 để dành cho phần còn lại của G2; G3 không dùng.
- Module G4 (`logistics`, `orders`, `costing`, `acctbridge`) sẽ được cấp số từ 000013 cụm mkg ([mkg.md §5.8](../design/mkg.md#58-bảng-tổng-hợp-cụm--vai--migration)).
- Migration cụm `ts` **không** được tham chiếu schema `pf_*`, `mkg_*`, `vault` (R-07) và **không** dùng `timescaledb`. `migratecheck` cưỡng chế cả hai điều này, và cũng đòi mọi thư mục cụm có trong `//go:embed` của `migrations/embed.go`.

## 3. Vai

Vai DB đã khai trong `internal/shared/dbadmin/dbadmin.go` và trong userlist PgBouncer (CI + `deploy/compose/pgbouncer`). Agent đợt G3-1 **không** sửa các tệp đó; cần vai mới thì ghi vào báo cáo.

| Vai DB | Cụm | Của | Quyền |
|---|---|---|---|
| `ingest_ts` | ts | `cmd/ingest` | `SELECT, INSERT` trên `ts_weigh.*` (ts/000001); không UPDATE/DELETE/TRUNCATE |
| `ts_reader` | ts | tích phân lại, bằng chứng tranh chấp, xuất tầng lạnh | chỉ `SELECT` trên `ts_weigh.*` |
| `measurement_app` | mkg | `mkg/measurement` | schema `mkg_measurement` (mkg 000007–000008 cấp) |
| `ingest_mkg` | mkg | `cmd/ingest` | đọc sổ thiết bị đã duyệt, ghi phiếu cân + outbox (mkg 000007–000008 cấp) |
| `worker_internal` | mkg | `cmd/worker-internal` | `shared.inbox` qua `ag_shared_user`; consumer ghi nghiệp vụ bằng vai module |
| `metrics_app` | pf | `pf/metrics` | schema `pf_metrics` (pf 000036–000037 cấp) |

Vai nghiệp vụ đã có hằng trong `port/` và có trong realm Keycloak:
- `mkg/measurement`: `scale_supervisor`, `device_approver`, `reconciliation_officer`, `dispute_officer`, `tolerance_approver`;
- `mkg/fieldops`: `field_collector`, `field_supervisor`;
- `pf/metrics`: `metrics_viewer`.

Phê duyệt ở vùng mkg dùng vai riêng của MKG, không dùng `param_setter`/`param_approver` (lý do như `hub_approver`, mkg.md §4.4). R-56 giữ nguyên: kiểm ở `app/` và bằng CHECK DB.

RabbitMQ:
- `agrigreen` (nền tảng): vhost `/`;
- `mkg_internal`: **chỉ** vhost `mkg.internal` (Compose `rabbitmq-init`, bí mật `AMQP_INTERNAL_PASSWORD`);
- `outbox-publisher` giữ cả hai;
- `worker-internal` chỉ giữ `mkg_internal`.

## 4. Sự kiện G3

| Sự kiện | Loại | Phát | Nhận |
|---|---|---|---|
| `mkg.measurement.weigh_ticket.recorded` v1 | **nội bộ** (`contracts/events-internal/`) | measurement (ingest) | hub (khối lượng lô), costing (G4) |
| `mkg.measurement.reconciliation.completed` v1 | **nội bộ** | measurement | hub (cảnh báo), costing (G4) |
| `mkg.fieldops.roll_count.recorded` v1 | **nội bộ** | fieldops | measurement (cuộn ↔ khối lượng, RB-6), costing (G4) |
| `mkg.fieldops.collection_schedule.published` v1 | van `collection_schedule` (**dự thảo**) | fieldops | pf/cropcycle, pf/notify |
| `mkg.fieldops.htx_collection_statement.published` v1 | van `htx_collection_statement` (**dự thảo**) | fieldops | pf/cropcycle (HTX xác nhận → quay về MKG qua API công khai) |
| `mkg.measurement.mrv_collection_evidence.published` v1 | van `mrv_collection_evidence` (**dự thảo**, C-1) | measurement | pf/mrv |
| `pf.metrics.*`, `pf.mrv.*` G3 | nền tảng | — | đợt G3-1 đặt tên theo `pf.<module>.<thực thể>.<động từ>` |

Cách phát:
- **Sự kiện nội bộ:** `outbox.Event{Kind: outbox.KindInternal, Payload: port.XxxV1{…}}`, trong đó struct mang `//ag:internal-event <tên> v1`.
- **Van:** như G2 — `Kind` để trống hoặc `KindPlatform`, payload là struct `//ag:publication` + `//ag:event`, kèm sổ `valve_export` + `Contract.Validate`.

Ba hợp đồng van dự thảo đã có struct Go ở `fieldops/port` và `measurement/port`. Người đề xuất/phê duyệt đang là **vai giữ chỗ** (Q-MKG-08); MKG điền người thật trước khi bật ở môi trường có dữ liệu thật.

## 5. Vùng và cửa

| Chiều | Cơ chế | G3 |
|---|---|---|
| MKG → nền tảng | **Chỉ van công bố** (`contracts/publication/`, mặc định từ chối, sổ `valve_export`) | 3 hợp đồng dự thảo ở mục 4 |
| Nền tảng → MKG | **Chỉ Application API công khai**, gọi bằng **tài khoản dịch vụ thuộc tenant MKG** (OIDC client credentials). Không có kết nối DB từ `mkg` sang `pf`, không import (R-03) | fieldops đọc thửa/HTX; logistics (G4) đọc tuyến |
| Trong MKG | `port/` đồng bộ trong cùng tiến trình (mặc định) hoặc sự kiện nội bộ trên vhost `mkg.internal` | measurement ↔ hub ↔ fieldops |
| Biên → MKG | edge-agent → `cmd/ingest` qua mTLS (vùng biên OT, VPN), chỉ đi ra | lô `agrigreen.edge.batch.v1` |

**Câu hỏi RB-2 — cấp quyền chia sẻ.** Thửa, HTX và nông hộ thuộc tenant chương trình (ADR-0009). Token của tenant MKG gọi Application API sẽ thấy **rỗng** vì RLS. Muốn MKG đọc được thì cần một **cấp quyền chia sẻ** tường minh: bên cấp là tenant, bên nhận là tenant, kèm phạm vi, mục đích và hạn dùng, kiểm ở `app/` và có nhật ký. Cách khác là cấp cho tài khoản dịch vụ MKG một vai chỉ đọc bên trong tenant chương trình. Chờ khách và người tích hợp chốt. Tới lúc đó fieldops G3 dùng dữ liệu tổng hợp (R-95) sau một interface nguồn thửa/HTX.

**Không bao giờ qua van**, kể cả ở G3:
- số đo cân thô, phiếu cân, khối lượng chính xác của lô;
- `yield_kg_per_roll`;
- bảng dung sai, định mức hao hụt, hao hụt tính được;
- hồ sơ tranh chấp;
- giá mua/bán, giá thành, biên lợi nhuận;
- danh tính và điều khoản khách hàng;
- dữ liệu cá nhân;
- tồn theo lô và nguồn gốc lô tới thửa/HTX gắn với khối lượng.

MRV chỉ nhận khối lượng **theo bậc**.

Gốc lắp ráp (archlint):
- `cmd/api-platform` không import `mkg/*`;
- `cmd/api-internal`, `cmd/ingest`, `cmd/edge-agent`, `cmd/worker-internal` không import `platform/*`;
- `cmd/edge-agent` chỉ import thư viện chuẩn, `internal/shared/{clock,ids,mass}` và `edgewire`;
- `edgewire` chỉ dùng thư viện chuẩn.

## 6. Hợp đồng đóng băng ở G3-0

Đổi chữ ký hoặc ngữ nghĩa của các hợp đồng dưới đây phải qua người tích hợp. Thêm phương thức thì đặt vào interface **mới**.

| Hợp đồng | Nơi | Nội dung |
|---|---|---|
| Lô số đo edge → ingest | `contracts/edge/batch.v1.json`, `contracts/edge/batch.v1.vectors.json`, `internal/mkg/measurement/port/edgewire` | `Batch`/`Reading`, `Decode` (khóa lạ và thiếu khóa đều bị từ chối), `Validate`, `Canonical` (văn bản ASCII/LF), `PayloadSHA256`, `KeyID`, `Sign`, `Verify`, mã lỗi `edge.*` |
| Cổng measurement | `internal/mkg/measurement/port` | `Integrator` (tất định, cửa sổ theo bộ đếm, thiếu → `ErrWindowIncomplete`), `TicketWriter` (R-70, idempotent, phiếu bù), `LotMass` (khối lượng chuẩn theo điểm cân), `DeviceRegistry.Resolve(device_id, epoch, cert_sha256)`; `WeighPoint`, `Route`, `TicketSource`, `Window`, lỗi `Err*` |
| Chỉ số | `internal/platform/metrics/port` | `Source.AsOf(ctx, key, businessDate, scope)`: chỉ bản ghi có `recorded_at < Cutoff(D)` (đầu ngày D+1 giờ VN), tất định; `Definition` có phiên bản; `LayerLog` (nhật ký lớp GIS + `ReplayLayer` = ảnh chụp + sự kiện sau watermark) |
| Sự kiện nội bộ | `contracts/events-internal/*.v1.json` + struct `//ag:internal-event` | ba sự kiện ở mục 4 |
| Van dự thảo | `contracts/publication/{collection_schedule,htx_collection_statement,mrv_collection_evidence}.json` + schema sự kiện | ba hợp đồng ở mục 4 |
| Bảng ts | `migrations/ts/000001_ts_weigh.up.sql` | cột của `reading_batch`/`raw_reading` tái dựng được dạng chuẩn byte-cho-byte |

## 7. Kiểm thử — máy dev và CI

- **Máy dev (Windows, PostgreSQL 17 cục bộ, không Docker, không RabbitMQ):**
  - `pgtest` tự dựng thêm `ag_ts_<hậu tố>` và chạy migration cụm ts. Pool vai ứng dụng dùng chế độ thực thi của PgBouncer.
  - Định tuyến outbox theo `kind` được kiểm bằng broker giả (`internal/shared/inbox/kind_integration_test.go`).
  - Chạy đủ bộ lệnh ở g1-conventions §5.
  - Cụm ts thêm khoảng 26 phân vùng cho mỗi database kiểm thử. Khi chạy song song mọi gói, bảng khóa dùng chung của PostgreSQL có thể cạn (`out of shared memory`, SQLSTATE 53200, thường gặp ở `cmd/drill` khi đếm mọi bảng). CI và Compose đã đặt `max_locks_per_transaction=256`. Máy dev nên đặt cùng giá trị trong `postgresql.conf` rồi khởi động lại; nếu chưa đặt được thì chạy lại gói bị lỗi riêng.
- **CI:**
  - job `test` đi qua PgBouncer với userlist đã có vai G3;
  - job `images` build thêm `worker-internal`;
  - `compose-smoke` dựng `rabbitmq-init`, kiểm tài khoản nền tảng **không** có quyền trên `mkg.internal`, và kiểm `worker-internal` healthy.

  Máy dev không chạy được các phần này. Ghi trong báo cáo nếu thay đổi của bạn phụ thuộc chúng.
- **Kiểm thử bắt buộc thêm cho G3:**
  - sáu phép thử UPDATE/DELETE/TRUNCATE (vai ứng dụng + owner) trên mọi bảng `ts_*` mới;
  - xác minh chữ ký của vector `batch.v1.vectors.json` ở mọi hiện thực nhận lô;
  - tích phân tất định (cùng cửa sổ + cùng `rule_version` → cùng kết quả);
  - `AsOf` không đổi sau khi có bản ghi bù ghi nhận sau Cutoff;
  - replay = dựng thẳng từ nhật ký;
  - sự kiện nội bộ giao lặp → kết quả như giao một lần (R-61).
- Mã story ghi tiền tố: `GIS US-EVL-01`, `KN US-MRV-02`, `KN US-DASH-01`, `CGH FN-10`. Quy tắc lấy từ prototype ghi `proto FM-04`, `proto plant-weighing`.
- Dữ liệu tổng hợp (R-95) cho cân, cuộn, độ ẩm theo phân phối thực tế: `cmd/seed-synthetic` G3.

## 8. Giả định tạm (ghi trong báo cáo nếu bám vào)

- ĐTK 1: `ag_ts` chung instance PostgreSQL; RabbitMQ một cụm, hai vhost.
- Một thiết bị cân gắn với **một** điểm cân (HUB_IN hoặc HUB_OUT) của **một** cơ sở mỗi kỳ đăng ký.
- CUSTOMER_IN ở G3 là **nhập tay kèm bằng chứng**; tích hợp hệ cân của khách khi có C-5.
- Khối lượng lô chuẩn = phiếu cân hiệu lực. Nhập tay ở hub chỉ khi cân hỏng, có lý do (Q-MKG-04).
- Thuật toán tích phân `weigh.integrate.v1` là tạm cho đến khi có mẫu cân C-6: tịnh = trung vị các số đo `stable` của cửa sổ − bì.
