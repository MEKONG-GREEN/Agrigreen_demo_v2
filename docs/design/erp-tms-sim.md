# ERP, TMS và bộ mô phỏng — tính năng, hướng phát triển, công nghệ và cổng kết nối

**Ngày:** 18/10/2026 · **Trạng thái:** đề xuất, chờ MKG duyệt · **Phạm vi thời gian:** từ nay tới Phase 1 (G6, Q1/2029)
**Căn cứ (kiến trúc đã thống nhất):**
- Kiến trúc hệ thống **v4.3** (06/10/2026): Phần III hệ nội bộ, Phần IV kết nối hai hệ, ADR-21…24 của tài liệu kiến trúc.
- Kiến trúc **v4.1** [Phần III (dữ liệu) và Phần VI (tích hợp)](../kien-truc/AgriGreen-Thiet-ke-kien-truc-he-thong-v4_1.md), còn hiệu lực theo v4.3.
- **Thiết kế vùng nội bộ MKG v1.4** (06/10/2026), ký hiệu "v1.4 §x" bên dưới.
- Thiết kế module trong repo: [mkg.md](mkg.md), [ADR-0020](../adr/0020-lien-thong-platform-mkg-khac-tenant.md), [ADR-0033](../adr/0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), [ADR-0034](../adr/0034-luong-song-phuong.md), [ADR-0035](../adr/0035-cap-phep-tong-hop-theo-xa-nguong-k.md), [ADR-0036](../adr/0036-engine-ba-che-do-vung-noi-bo-optcore.md), [kế hoạch v4.1](../kien-truc-v4.1-ke-hoach.md) (W8.x).
- Prototype `mekong-green/src/erp/*` là **đặc tả chạy được**. Không chuyển mã từ prototype sang repo ([tong-hop.md](../requirements/tong-hop.md#vai-trò-của-prototype-mekong-green)).

**Lưu ý nguồn:** bản v4.3 và v1.4 gốc nằm ở ổ chung (`Development/`) và **chưa được đưa vào repo**. Việc đó là D-9 / W10.0. Khi đưa vào, đánh số ADR-21…24 của v4.3 thành ADR-0039…0042 trong repo để tránh trùng số. Tài liệu này dẫn chiếu theo số của tài liệu kiến trúc và ghi số repo trong ngoặc.

---

## 0. Tóm tắt

1. **ERP** là **hệ vận hành MKG**, tự xây một khối bằng Go ở vùng nội bộ. Kế toán pháp định, hóa đơn điện tử, tiền lương, kho phụ tùng và nhiên liệu là phần **mua ngoài**, chỉ nối qua `mkg/acctbridge`. Hệ nội bộ không dùng Odoo (v4.3 ADR-21 = ADR-0039).
2. **TMS không phải hệ thống riêng** (v1.4 §3). TMS gồm phân hệ vận tải `mkg/logistics` để ghi giao dịch, chế độ *kế hoạch cuộn* và *điều phối* của engine để tính toán, và màn điều phối trên web nội bộ. Lý do: ghép ghe ở điểm gom là **trộn lô trên mặt nước**, nên phải ghi cùng chỗ với số cuộn, tồn hub và cân nhà máy. Nếu tách TMS ra, gia phả lô bị cắt đôi.
3. **Bộ mô phỏng có hai triển khai, chung một lõi `lib/optcore`, không chung dữ liệu** (v1.4 §4.5, ADR-0036):
   - **App SIM** (`pf/simulation` + `optimizer/`) ở vùng nền tảng là sản phẩm cho các đơn vị khác. Đã có từ G2.
   - **Mô phỏng MKG** chạy ở engine vùng nội bộ, trên dữ liệu thương mại của MKG. **Phải xây đầu tiên**, vì có giá trị trước khi có vận hành.
4. **Ngôn ngữ:**
   - Go 1.23+ cho mọi dịch vụ giao dịch.
   - Python 3.12 cho engine tính toán (HiGHS, OR-Tools sau khi có ADR).
   - Next.js/TypeScript cho web.
   - Flutter cho app hiện trường.
   - Go nhị phân tĩnh cho agent cân.
5. **Kết nối:** 24 cổng kết nối (mục 9): 9 đã có mã (có cổng còn chờ ký hợp đồng hoặc phải chuyển khỏi van RabbitMQ), 4 có một phần, 9 còn thiếu, 2 để sau hoặc chưa quyết. Mọi luồng giữa hai hệ thống đi qua **cổng tích hợp**: không kết nối DB, không hàng đợi dùng chung. Luồng thương mại (số cuộn, xác nhận HTX, VFT) phải có **chữ ký số**.
6. **Đường găng:**
   - (1) Kho quan sát v2 → ảnh chụp → dịch vụ engine nội bộ → mô phỏng MKG.
   - (2) Làm thử T2a/T2b ở cỡ Phase 1 (D-6), đây là rủi ro kỹ thuật lớn nhất.
   - (3) Logistics: ghe thuê và ghép ghe → gia phả dùng chung → điểm cân thứ tư.
   - (4) Kế hoạch đề xuất có tự duyệt → điều phối.
   - Mọi phần chạm dữ liệu nhân sự, vị trí người lái hay chữ ký số đều chờ pháp chế.

---

## 1. Định nghĩa và phạm vi

| Thuật ngữ | Nghĩa trong tài liệu này | Nằm ở đâu | Không phải là |
|---|---|---|---|
| **ERP** | Hệ vận hành MKG (thành phần **B**, v1.4 §3): mọi giao dịch quanh lô rơm, đội hiện trường, ghe thuê, hub, cân, mua, bán, giá thành | `internal/mkg/*`, `cmd/api-internal`, `cmd/worker-internal`, `apps/web-internal`, `apps/mkg-field-app`, cụm `ag_mkg` + `ag_ts` | Phần mềm kế toán pháp định (thành phần **A**, mua ngoài) |
| **TMS** | Phân hệ vận tải đường thủy và đường bộ của ERP: danh mục và thuê ghe, chuyến, chặng, ghép ghe, điểm gom, GPS ghe, cước, đối soát chi phí, cùng **điều phối** có lập lại kế hoạch | `mkg/logistics` (giao dịch) + engine chế độ kế hoạch cuộn/điều phối (tính toán) + `mkg/engine` (dữ liệu của engine) | Một hệ thống, một DB hay một app riêng |
| **Engine** | Thành phần **C** (v1.4 §3, §4): dịch vụ Python **không giữ dữ liệu nghiệp vụ**, một lõi mô hình, ba chế độ: mô phỏng · kế hoạch cuộn · điều phối | `lib/optcore` (lõi) + dịch vụ engine nội bộ (mới, W8.3c) | Nơi lưu kế hoạch. Kế hoạch đề xuất lưu ở `mkg/engine` |
| **Bộ mô phỏng (Simulator)** | (a) **Mô phỏng MKG**: chế độ mô phỏng của engine ở vùng nội bộ. (b) **App SIM**: sản phẩm nền tảng `pf/simulation` | (a) engine nội bộ + `apps/web-internal` · (b) `pf/simulation` + `optimizer/` + `apps/web-ops/src/app/mo-phong` | Một bản dùng chung dữ liệu giữa MKG và đơn vị khác |

**Ngoài phạm vi xây:**
- Sổ cái, công nợ, thuế, hóa đơn điện tử, tài sản cố định (gồm 500 máy cuộn), tiền lương, bảo hiểm, kho phụ tùng và nhiên liệu. Các phần này mua ngoài; ở đây chỉ thiết kế cầu nối.
- Hệ thống của khách hàng (VFT). Ở đây chỉ thiết kế cổng nhận.
- Các ứng dụng nền tảng (KN, HTX, CGH, GIS). Chỉ ghi những luồng dữ liệu mà ERP/TMS/SIM dùng.

---

## 2. Vị trí trong kiến trúc tổng thể

```
┌──────────────────── HỆ THỐNG THÔNG TIN 1: NỀN TẢNG AGRIGREEN (đề xuất cấp độ 3) ────────────────────┐
│ api-platform · worker · optimizer (App SIM) · web-ops (/mo-phong, GIS, thủy hệ)                      │
│ pf/simulation · pf/waterway · pf/geo · pf/cropcycle · pf/cooperative · pf/mrv · …   cụm ag_pf       │
│                         integration-gateway (feed · lô · tệp · webhook · song phương)                │
└───────────────────────────────────────────────┬──────────────────────────────────────────────────────┘
                                                │ chỉ qua cổng: khóa API + mTLS, cursor, hợp đồng có phiên bản,
                                                │ chữ ký số cho luồng thương mại (v4.3 ADR-22, ADR-24; ADR-0033/0034)
┌───────────────────────────────────────────────┴──────────────────────────────────────────────────────┐
│ HỆ THỐNG THÔNG TIN 2: HỆ NỘI BỘ MKG (đề xuất cấp độ 2)                                               │
│                                                                                                      │
│  ERP (B, Go)                                     ENGINE (C, Python, không giữ dữ liệu)               │
│  api-internal · worker-internal                  dịch vụ engine nội bộ ── lib/optcore                │
│  fieldops · workforce* · measurement · hub       chế độ: mô phỏng · kế hoạch cuộn · điều phối        │
│  logistics (TMS) · orders · costing · acctbridge         ▲ ảnh chụp (Internal API mTLS)              │
│  engine (kho quan sát, ảnh chụp, đề xuất) ───────────────┘   ▼ kết quả / đề xuất                     │
│  cụm ag_mkg · cụm ag_ts (cân thô, GPS ghe*) · RabbitMQ vhost mkg.internal                            │
│  web-internal (Next.js) · mkg-field-app (Flutter, ngoại tuyến)                                       │
│  cổng nhận nội bộ* (VFT) ── cùng mã integration-gateway, triển khai và dữ liệu riêng (R-MOD-10)      │
└───────┬──────────────────────────┬──────────────────────────────────────┬────────────────────────────┘
        │ mTLS, chỉ đi ra          │ bút toán tổng hợp tháng, một chiều   │ chữ ký số
 BIÊN OT: edge-agent tại hub/   KẾ TOÁN PHÁP ĐỊNH (A, mua ngoài)       VFT (nhà máy): tồn, kế hoạch tiêu
 điểm gom → ingest              + kho phụ tùng, nhiên liệu             thụ, nghiệm thu
                                                                        (* = chưa có, cần xây)
```

**Quy tắc ranh giới bắt buộc:**
- **R-03:** `platform/*` và `mkg/*` không import nhau, kể cả ở gốc lắp ráp (`tools/archlint`, `agboundary`).
- **R-05:** mỗi module một vai DB, chỉ CONNECT cụm của mình.
- **R-06:** không join hay khóa ngoại xuyên schema. Tham chiếu là UUID thuần.
- **ADR-0020:** MKG là **tenant riêng**. Dữ liệu nền tảng đến MKG chỉ qua cấp quyền chia sẻ tường minh, có mục đích.
- **ADR-0033:** MKG kết nối qua cổng tích hợp **như mọi đối tác**. Vùng mkg chỉ được đi ra cổng, không gọi `api-platform` (`deploy/k8s/tests/check_flows.py`).
- **v4.3 ADR-22 (= ADR-0040, đề xuất):** hai hệ thống thông tin. Không dùng chung tài khoản quản trị. Không dùng chung hàng đợi giữa hai hệ, nên phải gỡ van RabbitMQ còn lại (W10.2).
- **v4.3 §21.1:** HTX là bên kiểm soát dữ liệu hộ, xã viên và thửa. MKG chỉ dùng dữ liệu mức thửa cho mục đích mà cấp phép cho phép. Engine mặc định chỉ nhận **tổng hợp theo xã qua ngưỡng k** (W10.15).

---

## 3. Quy mô thiết kế (v1.4 §1)

| Giai đoạn | Tấn/ngày thu gom (TB trong vụ) | Tấn/ngày nhà máy | Máy cuộn cần (10 t/ngày) | Chuyến ghe/ngày (toàn 40 t · toàn 150 t) | Tồn hub cho mỗi 30 ngày nghỉ |
|---|---|---|---|---|---|
| Phase K | 917–1.100 | 500 | 92–110 (500 máy MKG đủ) | 23–28 · 6–7 | 15.000 t |
| Phase 1 | 12.389–18.667 | 6.758–8.485 | 1.239–1.867 (MKG đáp ứng 27–40 %) | 310–467 · 83–124 | 202.727–254.545 t |
| Phase 2 cao | 23.889–28.667 | 13.030 | 2.389–2.867 | 597–717 · 159–191 | 390.909 t |

**Hệ quả thiết kế:**
- Khoảng một nửa lượng rơm đi thẳng về nhà máy, một nửa vào hub. Hai dòng phải tối ưu cùng nhau.
- Sức chứa hub do **khoảng nghỉ dài nhất** quyết định. Vì vậy mô phỏng chạy trên lịch thu hoạch thật theo vùng.
- Mô hình cung có **hai nguồn với độ tin cậy khác nhau**: đội MKG và mua cuộn từ HTX.
- Số ghe khả dụng theo cỡ và theo ngày là **tham số đầu vào**.
- Ngoài ra còn 1.500+ nhân sự hiện trường. Họ là chủ thể dữ liệu cá nhân và dữ liệu vị trí.

---

## 4. Ngôn ngữ và công nghệ

| Thành phần | Ngôn ngữ / công nghệ | Ghi chú, căn cứ |
|---|---|---|
| Dịch vụ ERP, TMS: `api-internal`, `worker-internal`, `ingest`, cổng nhận nội bộ | **Go 1.23+**; `pgx`; `shared/{tenant,ledger,outbox,inbox,idempotency,money,mass,clock,problem,httpx}`; bộ sinh `tools/modgen` (`module.yaml`) | v3.0 §4, v4.3 ADR-21 (= ADR-0039). Tiền và khối lượng dùng decimal (`shopspring/decimal` qua `shared/money`/`mass`). `agnofloat` áp cho `costing, measurement, orders, acctbridge, logistics, hub` và áp cho cả `engine` khi ghi giá trị |
| Engine: mô phỏng, kế hoạch cuộn, điều phối | **Python 3.12**; `lib/optcore` (gói dùng chung, `py.typed`, mypy chặt); **HiGHS** (`highspy`, MIT) cho T1 và chọn hub; **OR-Tools** (Apache-2.0) gồm định tuyến cho T2a và **CP-SAT** cho T2b, T3; `pydantic` với `extra="forbid"`; `pika` | Ngoại lệ ngôn ngữ bắt buộc (v3.0 §4). OR-Tools **cần ADR phụ thuộc mới** theo quy trình [ADR-0016](../adr/0016-giay-phep-python.md) (D-6). Giấy phép numpy đi kèm highspy chờ pháp chế xác nhận |
| Agent cân tại hub và điểm gom | **Go**, nhị phân tĩnh, chỉ thư viện chuẩn + `shared/{clock,ids,mass}` + `edgewire` | v3.0 §14, [ADR-0019](../adr/0019-dinh-danh-thiet-bi-va-dong-ho.md). Bộ điều hợp modbus-tcp/serial-ascii đã có kèm bộ mô phỏng đầu cân. Mẫu cân thật chờ C-6 |
| Web nội bộ MKG | **Next.js + TypeScript** (`apps/web-internal`), mã client sinh từ `contracts/openapi/mkg-internal.yaml`, mock backend | Chỉ gọi `api-internal`. Màn điều phối và mô phỏng MKG là màn mới |
| App SIM (web) | **Next.js + TypeScript** (`apps/web-ops`, trang `/mo-phong`) | Theo v3.0 §4, bộ mô phỏng trên di động là **web responsive**, không làm app riêng |
| App hiện trường MKG: đếm cuộn, tài xế, tổ trưởng | **Flutter** (`apps/mkg-field-app` + `packages/agrigreen_mobile`), SQLCipher, UUIDv7 sinh trên máy | Ngoại tuyến 24–48 giờ (v3.0 §15). Khóa app sau 5 phút, mở bằng PIN; thiết bị đăng ký + PIN là hai yếu tố (v4.3 ADR-23 = ADR-0041, W10.4) |
| Cơ sở dữ liệu | **PostgreSQL 17** + PostGIS. **pgRouting chỉ ở cụm `pf`** (đồ thị thủy hệ). Cụm `ag_ts` dùng phân vùng tháng thuần, bố cục tương thích Timescale, không dùng TSL | [ADR-0017](../adr/0017-kho-chuoi-thoi-gian.md). Không join `mkg` ↔ `ts` (R-07) |
| Hàng đợi | **RabbitMQ**, quorum queue + DLQ. Vhost **`mkg.internal`** dành riêng cho sự kiện nội bộ và việc của engine | [ADR-0018](../adr/0018-su-kien-noi-bo-mkg.md). Không dùng vhost nền tảng cho luồng giữa hai hệ |
| Định tuyến đường bộ | **OSRM tự vận hành trong lãnh thổ**. Hiện tạm dùng Haversine × hệ số #10 | v3.0 §4, RB-7 |
| Định danh, phân quyền | **Keycloak (OIDC)**, MFA theo nhóm (ADR-0041); `shared/authz` | Tài khoản quản trị tách riêng cho từng hệ (v4.3 ADR-22) |
| Xuất báo cáo | Thư viện theo [ADR-0014](../adr/0014-thu-vien-xuat-bao-cao-va-phong-chu.md) (Excel, PDF) | Báo cáo mô phỏng cho CEO và đầu tư |
| Quan sát | OpenTelemetry → Prometheus, Loki, Tempo, Grafana trong lãnh thổ | R-90…R-92 |

**Không dùng:** Odoo (ADR-21); dịch vụ tính toán ngoài lãnh thổ cho dữ liệu có định danh (RB-7); float trong tiền và khối lượng (R-10).

---

## 5. ERP — hệ vận hành MKG

### 5.1 Bản đồ module

| Module | Trách nhiệm | Đã có (mốc) | Cần phát triển (gói, mục 10) |
|---|---|---|---|
| `mkg/fieldops` | Lệnh gom, đội, máy cuộn, đếm cuộn, nhật ký sản xuất, xác nhận HTX | G3, mkg 000006/000012/000033/000036/000038 | Máy cuộn và hỏng hóc (W8.5d); `worker_ref` (C-7); GPS người đếm theo D-12 (W8.7); T3 phân công gợi ý (W8.3g); ký số lô gửi HTX (W10.7) |
| `mkg/workforce` **(mới)** | 1.500+ nhân sự: định danh, ca, chấm công, sản lượng khoán | — | W8.4 (chờ pháp chế) |
| `mkg/measurement` | Bốn điểm cân, phiếu cân dẫn xuất, đối soát, tranh chấp | G3-A2 (ba điểm), mkg 000007–000008 | `CONSOLIDATION`, `weighing_mode`, đối soát riêng cho lô đi thẳng, cờ độ tin cậy (W8.5a) |
| `mkg/hub` | Cơ sở, lô có N nguồn, mẻ sấy/nén, xuất theo rủi ro, kiểm kê, chuyển kho | G2–G3, mkg 000002–000003/000009 | Điểm gom là cơ sở, nút trộn dùng chung (W8.5b); phạm vi người vận hành theo hub (Q-MKG-03) |
| `mkg/logistics` (**TMS**) | Ghe, chuyến, chặng, cước | G4, mkg 000015 | Mục 6 |
| `mkg/orders` | Bán (VFT và khách khác), mua (HTX), giao nhận | G4, mkg 000016 | Nguồn cung hai độ tin cậy (W8.5e); nghiệm thu VFT có chữ ký số (W10.8) |
| `mkg/costing` | Giá thành mức B, quy đổi cuộn → tấn, phân bổ đa chặng, đóng kỳ, quyết toán HTX | G4, mkg 000017–000018 | Chi phí trung chuyển và chờ ghe; hao hụt lô đi thẳng; cờ độ tin cậy; chi phí từ cam kết thuê ghe (W8.5a/c) |
| `mkg/acctbridge` | Bút toán tổng hợp theo kỳ → kế toán pháp định | G4, mkg 000019 | Bộ điều hợp nhà cung cấp thật (D-11); xuất bảng công (W8.4) |
| `mkg/engine` | Phía dữ liệu của engine: kho quan sát, danh mục, ảnh chụp, kế hoạch đề xuất | W8.2, mkg 000037 | W8.2b, W8.2c, W8.3a/b, W8.3f |
| `cap/workflow` ở cụm mkg | Duyệt bốn mắt cho ba chỗ có tiền (v4.3 §9) | Chỉ có ở `pf` | W10.17 |

### 5.2 Tính năng theo module

**`mkg/fieldops` — hiện trường**
- *Đã có:*
  - Lệnh gom theo HTX/xã/khung thời gian, người duyệt khác người lập.
  - Đếm cuộn theo công đoạn (cuộn → gom ra bờ → xuống ghe), làm ngoại tuyến được.
  - Xác nhận và phản đối của HTX qua luồng song phương (W5.6).
  - Đối soát chiều mua.
  - Sổ tổ gom có phiên bản. Phân công theo năng lực (FM-06/07/08).
  - Ngày gặt lấy từ feed. Hạn cuộn sau gặt (`baling_deadline_days`).
  - Học kg/cuộn theo HTX (`KgPerRollAsOf`).
  - Bản sao danh bạ HTX và thửa từ feed (W5.5).
- *Cần làm:*
  - **Máy cuộn:** danh mục 500+ máy có phiên bản; gán máy cho tổ; sự kiện hỏng, sửa, ngừng; năng suất thực tế theo máy. Phần này sinh khóa quan sát nội bộ "đội/máy/người khả dụng" (`max_age` 1 ngày) cho engine.
  - **Tham chiếu nhân sự:** chuyển `crew_event.member_refs` và `trip_event.driver_ref` (đang là subject tài khoản) sang `worker_ref` bút danh, bằng sổ phụ, không sửa dòng cũ (C-7).
  - **GPS người đếm** (`roll_count_location`): bật cờ tạm dừng ở production tới khi có D-12. Đích là lưu tọa độ chính xác ở kho có thời hạn lưu; sổ chỉ giữ `gps_status` và mã ô lưới thô (C-8).
  - **Phân công đội gợi ý (T3):** dùng `crew_assignment.mode = suggested` đã có. Tổ trưởng nhận hoặc từ chối trên app.
  - **Chữ ký số** trên lô `roll_count_statement` gửi HTX (bắt buộc) và `collection_schedule` (nên có).

**`mkg/workforce` — nhân sự hiện trường (module mới duy nhất theo v1.4 §5)**
- Định danh nhân sự ở **schema riêng trong cụm `mkg`**: mã hóa mức trường, chỉ mục HMAC để tìm, xóa bằng hủy khóa. Dùng lại mã `vault` theo R-MOD-10.
- Bảng vận hành chỉ giữ `worker_ref`. Mọi lần đọc định danh đều ghi nhật ký kèm mục đích.
- Phân ca, chấm công (tổ trưởng chấm trên app, có thể ngoại tuyến), sản lượng khoán theo tổ và máy.
- **Xuất bảng công** sang kế toán: chỉ gồm mã nhân viên bên kế toán, công và sản lượng. **Không** lưu số tài khoản ngân hàng, lương hay bảo hiểm.
- Thời hạn lưu sau nghỉ việc do pháp chế chốt. Hồ sơ đánh giá tác động riêng cho hệ nội bộ.

**`mkg/measurement` — cân và đối soát**
- *Đã có:*
  - Sổ thiết bị cân có duyệt. Phiên cân. Phiếu cân `HUB_IN`, `HUB_OUT`, `CUSTOMER_IN` **chỉ do ingest tạo** (R-70).
  - Dung sai và định mức có phiên bản, áp theo thời điểm cân.
  - Đối soát, hồ sơ tranh chấp, nhập số cân của khách kèm bằng chứng.
  - Van MRV. Kiểm chéo cuộn ↔ khối lượng cho hợp đồng theo tấn (RB-6).
- *Cần làm (D-4):*
  - **Điểm cân thứ tư `CONSOLIDATION`.** Đổi CHECK `point`/`route` bằng DROP/ADD constraint, không đụng chuỗi băm. Sự kiện `weigh_ticket.recorded` lên v2. Cập nhật OpenAPI và web nội bộ. Tác động tới `orders`, `costing` và `pf/mrv` (`mass_basis`).
  - **`weighing_mode` theo điểm gom:**
    - `scale`: phiếu cân `CONSOLIDATION`, tỷ lệ trộn tính theo khối lượng.
    - `sampling`: lưu cỡ mẫu và sai số.
    - `roll_estimate`: kg/cuộn **có phiên bản** theo HTX, bằng cách đóng băng ước lượng học được theo kỳ.
  - **Đối soát và định mức hao hụt riêng cho lô đi thẳng.** Dùng `reconciliation` v2, thêm stream `loss_norm:direct`. Không trộn với hao hụt qua hub.
  - **Cờ độ tin cậy** (`mass_confidence`) trên mỗi nút trộn và mỗi kết quả giá thành.
  - Tích hợp hệ cân nhà máy (C-5) và mẫu cân thật (C-6): xem cổng CK-12, CK-13.

**`mkg/hub` — kho và gia phả lô**
- *Đã có:*
  - Dữ liệu chủ cơ sở có phiên bản và duyệt (hub, kho, bãi).
  - Lô có N nguồn, Σ tỷ lệ = 1 chính xác. Mẻ sấy, nén, phân loại.
  - Xuất theo rủi ro xuống cấp (`hub.risk.v2`). Kiểm kê có duyệt. Chuyển kho có hao hụt đường.
  - Khối lượng lô lấy từ phiếu cân. Van `hub_facility`/`hub_capacity`.
- *Cần làm (D-4, W8.5b):*
  - **Điểm gom là một loại cơ sở** (`facility.kind = consolidation_point`) với `buffer_capacity`, `weighing_mode`, năng lực trung chuyển/ngày, chi phí/tấn. Ghép ghe tạo **lô mới có nguồn `kind = lot`** (nút trộn). `hub/port.LotOrigins` giữ nguyên. Đề xuất này chờ MKG xác nhận (Q-10 ở mục 11).
  - Nới quan hệ 1 lệnh gom ↔ 1 lô (`lot_collection_current`) thành N ↔ 1 qua nút trộn. MRV tuyến đi thẳng dùng tỷ lệ của nút trộn.
  - Phạm vi người vận hành theo hub (Q-MKG-03). Hủy chuyển kho. Nguồn cảm biến môi trường thật (`ts_env`) thay `NoEnvReadings`.

**`mkg/orders` — mua và bán**
- *Đã có:* hợp đồng mua rơm với HTX theo tấn hoặc theo cuộn (CT-01..03); đơn mua ↔ phiếu nhận (chênh > 3 % chỉ cảnh báo, cần biên bản); đơn bán, lịch giao, giao nhận gắn `CUSTOMER_IN`; duyệt giá và chiết khấu theo ngưỡng (R-56).
- *Cần làm:*
  - **Nguồn cung hai độ tin cậy** (đội MKG / mua cuộn HTX): tỷ lệ và năng suất máy là tham số chính sách cho engine (W8.5e).
  - **Hợp đồng VFT:** kế hoạch tiêu thụ, lịch dừng lò, mức phạt giao thiếu (tham số chính sách), **biên bản nghiệm thu có chữ ký số** nhận qua cổng nhận nội bộ (W10.8).
  - Hóa đơn điện tử do phần mềm kế toán phát hành. ERP chỉ gửi căn cứ (giao nhận đã nghiệm thu) qua bút toán tổng hợp.

**`mkg/costing` — giá thành và quyết toán (sổ là nguồn, ADR-0003)**
- *Đã có:* quy đổi cuộn → tấn; quy đổi ẩm độ; phân bổ chi phí đa chặng; quy kết hao hụt theo tỷ lệ nguồn của lô; đóng kỳ theo mẫu 202 + job; quyết toán HTX (người lập ≠ người duyệt); chia carbon (tạm); mỗi phép tính ghim `rule_version`.
- *Cần làm:*
  - Chi phí **trung chuyển ở điểm gom** và **chờ ghe**.
  - Chi phí theo **cam kết thuê ghe**: hợp đồng khung so với theo chuyến.
  - Hao hụt lô đi thẳng phân bổ riêng.
  - Kết quả mang cờ độ tin cậy theo `weighing_mode`.
  - Chi phí nhiên liệu và phụ tùng theo kỳ từ phần mềm mua ngoài (CK-24, chờ quyết định).

**`mkg/acctbridge` — cầu nối kế toán**
- *Đã có:* bút toán tổng hợp theo kỳ, **một chiều, tần suất tháng**; bảng ánh xạ tài khoản có phiên bản; duyệt (người dựng ≠ `chief_accountant`); đẩy idempotent qua `LedgerAdapter`; bộ xuất tệp CSV/XML tất định + bộ giả; thử lại + DLQ ([acctbridge-adapter.md](../dev/acctbridge-adapter.md)).
- *Cần làm:*
  - Bộ điều hợp cho phần mềm kế toán đã chọn (D-11): đáp ứng Thông tư 99/2025, hóa đơn điện tử, lương mùa vụ, kho phụ tùng; nhà cung cấp trong lãnh thổ.
  - Xuất bảng công từ `workforce`.
  - Không đẩy giao dịch chi tiết.

**Màn hình web nội bộ (`apps/web-internal`)**
- *Đã có:* `can`, `gom`, `hub`, `van-tai`, `don-hang`, `gia-thanh`, `ke-toan`.
- *Cần thêm:*
  - `dieu-phoi`: bảng điều phối và duyệt đề xuất (mục 6.4).
  - `ke-hoach`: kế hoạch cuộn 7–14 ngày.
  - `mo-phong`: mô phỏng MKG (mục 7.3).
  - `quan-sat`: nhập tay, tải tệp, xác nhận quan sát.
  - `may-cuon`, `nhan-su`.
  - `tham-so`: sổ tham số chính sách của các module, có duyệt hai người.

**App hiện trường MKG (`apps/mkg-field-app`)**
- *Đã có:* đếm cuộn theo công đoạn; mốc chuyến của tài xế, làm ngoại tuyến được.
- *Cần thêm:* báo hỏng máy; tổ trưởng chấm công và nhận phân công; ghi nhận ghép ghe ở điểm gom; GPS ghe **chỉ trong giờ làm việc**; khóa PIN 5 phút và khóa ký thiết bị trong Keystore (W10.4).

---

## 6. TMS — vận tải và điều phối

### 6.1 Ranh giới

TMS là **phân hệ của ERP**. Giao dịch nằm ở `mkg/logistics`, dữ liệu kế hoạch ở `mkg/engine`, tính toán ở engine (Python). Không có DB hay đơn vị triển khai riêng cho TMS. Gia phả lô khi ghép ghe ghi qua `hub/port`, không có gia phả riêng (v1.4 §6).

### 6.2 Giao dịch vận tải — `mkg/logistics`

| Tính năng | Trạng thái | Mô tả và quy tắc |
|---|---|---|
| Danh mục phương tiện (ghe, sà lan, xe) | **Đã có** (`vessel_event`: loại, tải rơm, chủ, cách tính cước) | Cần thêm: cỡ ghe theo lớp luồng; hạn giấy tờ (prototype `expiryStatus`); cảnh báo phương tiện cần xử lý |
| Tham số cước có duyệt | **Đã có** (`param_version`, `rate_vnd_per_t_km`, ghim vào chặng) | Cước theo cự ly và cỡ ghe; cước ngoài vụ và trong vụ là phiên bản riêng |
| Chuyến và chặng (thủy, bộ), trạng thái chỉ tiến | **Đã có** (`trip_event`, `leg_event`, `fn_trip_forward`) | Chặng lưu **ảnh chụp định tuyến + SHA-256** để tái hiện chi phí |
| Gắn phiếu xuất hub và phiếu cân; thông báo hàng đến (FM-05); app tài xế | **Đã có** | — |
| **Thuê ghe:** cam kết thuê (hợp đồng khung so với theo chuyến), đặt chuyến, chủ ghe xác nhận, hủy, phí chờ | **Thiếu** (W8.5c) | Cam kết thuê **là cam kết tiền**: đề xuất của engine có cam kết thuê mới thì **không được tự duyệt** (điều kiện 3, v1.4 §4.3) |
| **Ghe khả dụng theo cỡ và theo ngày** | **Thiếu** (W8.5c + W8.2b) | Khóa quan sát, nguồn là đội thuê ghe (nhập tay hoặc tệp mẫu MKG), `max_age` 1 ngày |
| **Bến xếp** (cỡ ghe tối đa) và **điểm gom** (sức đệm, năng lực trung chuyển, chi phí và thời gian mỗi tấn, chế độ cân) | **Thiếu** | Điểm gom là cơ sở ở `mkg/hub` (W8.5b). Bến xếp là dữ liệu chủ của logistics |
| **Chặng ghép ghe** (`transshipment`): N chuyến ghe nhỏ → 1 chuyến ghe lớn | **Thiếu** (W8.5c) | Ghi nút trộn qua `hub/port`; cân `CONSOLIDATION` nếu điểm gom ở chế độ `scale` |
| **GPS ghe và ETA** | **Thiếu** (CK-14) | Telemetry → `ingest` → cụm `ts` (`ts_fleet`). **Chỉ thu trong giờ làm việc** khi ghe hoặc máy gắn một người lái (D-12). Không join với bảng nghiệp vụ (R-07) |
| **Định tuyến** | **Một phần**: thủy qua `/v1/partner/waterway/route` (sẽ gỡ); bộ dùng Haversine × #10 | Đích: thủy hệ qua feed `waterway_routes` (CK-06), đồ thị dựng trong vùng mkg, có **cỡ ghe tối đa, tĩnh không cầu, ngưỡng mực nước** (W8.3h); đường bộ qua OSRM (CK-18) |
| **Biên bản giao nhận điện tử (ePOD)** | **Một phần** (giao nhận ở `orders`) | Với VFT: nghiệm thu có chữ ký số (CK-11) |
| **Đối soát chi phí vận tải kế hoạch – thực tế**, phát thải theo tấn·km | **Thiếu** (prototype `costVariance`) | Kết quả đi vào `costing`. Phát thải dùng tham số MRV tạm (0,062 / 0,021 kgCO2e/t·km) |
| **Lệnh chuyến từ đề xuất đã duyệt** | **Thiếu** (W8.3f) | Đề xuất được duyệt sinh chuyến hoặc chặng qua API nội bộ của logistics; không ghi thẳng vào DB |

### 6.3 Điều phối — engine (v1.4 §4.2, §4.3)

| Tầng | Bài toán | Công cụ | Đơn vị chia |
|---|---|---|---|
| **T1** dòng chảy mạng lưới | Tấn đi thẳng / vào từng hub / qua từng điểm gom theo ngày, chân trời cuộn 7–14 ngày, tồn đệm | MILP — **HiGHS** | Toàn mạng |
| **T2a** ghe nhỏ bến → điểm gom | Đón – trả có cửa sổ thời gian | **OR-Tools Routing** | Hành lang sông |
| **T2b** ghe lớn điểm gom → nhà máy | Lập lịch, nối với T2a | **CP-SAT** hoặc MILP | Hành lang sông |
| **T3** phân công đội | Đội cuộn nào về xã nào ngày nào | **CP-SAT** | Toàn vùng |

- **Nối T2a–T2b theo sức đệm điểm gom.** Điểm gom *có đệm* thì hai bài toán tách nhau bằng tồn. Điểm gom *không đệm* (ghép mạn) thì T2b phủ cửa sổ đến của các ghe nhỏ, và chi phí chờ đưa vào hàm mục tiêu.
- **Vùng đóng băng:** lệnh đã giao cho ghe đang chạy, hoặc ghe khởi hành trong vòng 2 giờ tới, thì engine không được đổi. Kiểm với `logistics/port.Trips`.
- **Kích hoạt lập lại kế hoạch:** tham số `[KÝ]`. Ví dụ: tồn nhà máy dự báo xuống dưới mức an toàn trong 48 giờ; một hành lang trễ quá 4 giờ; một đội ngừng quá nửa ngày.
  - *Sửa nhanh:* chỉ giải lại T2 của hành lang bị ảnh hưởng.
  - *Giải lại đầy đủ:* giải lại T1 rồi T2.
- **Mọi kế hoạch mới là đề xuất.** Đề xuất được tự duyệt khi thỏa **đồng thời năm điều kiện**:
  1. chỉ đổi trong một hành lang;
  2. không chạm vùng đóng băng;
  3. không thêm cam kết thuê ghe mới;
  4. chi phí tăng dưới X %;
  5. tồn an toàn dự báo không giảm dưới ngưỡng.
- Chính sách tự duyệt là **tham số đặc quyền**, đổi phải qua hai người. Có chỉ số **tỷ lệ tự duyệt bị con người đảo ngược**.
- **Sổ** (W8.3f, ở `mkg/engine`):
  - `plan_proposal`: phiên bản, phạm vi hành lang, delta chi phí, delta tồn an toàn, cam kết thuê mới.
  - `plan_decision`: `human`/`auto`; người duyệt ≠ người đề xuất; lý do khi chỉnh tay; `reverted_by`.
  - Bản chỉnh tay kèm lý do được dùng để hiệu chỉnh mô hình.
- **Mục tiêu thời gian giải:** sửa nhanh dưới 2 phút, giải lại dưới 15 phút. Đây là **mục tiêu thiết kế**. Chỉ cam kết sau khi làm thử D-6 trên dữ liệu tổng hợp cỡ Phase 1 (310–470 chuyến ghe nhỏ và 83–124 chuyến ghe lớn mỗi ngày), với tiêu chí đạt định trước. Không đạt thì hạ mục tiêu, hoặc chuyển sửa nhanh sang heuristic chèn – đổi chỗ.

### 6.4 Màn hình TMS

| Người dùng | Màn | Nội dung |
|---|---|---|
| Điều phối viên | `dieu-phoi` | Bảng theo hành lang: ghe, chuyến, vị trí và ETA, cảnh báo lệch kế hoạch; danh sách đề xuất kèm khác biệt so với kế hoạch đang chạy, nút duyệt/từ chối/chỉnh tay có lý do; xem lại theo lô các đề xuất đã tự duyệt cuối ca |
| Trưởng điều phối | `ke-hoach` | Kế hoạch cuộn 7–14 ngày: tấn theo cung đường và ngày, số ghe cần thuê theo cỡ, phân bổ đội; so sánh kế hoạch với thực hiện |
| Đội thuê ghe | `van-tai` (mở rộng) | Cam kết thuê, ghe khả dụng theo cỡ/ngày, hạn giấy tờ |
| Tài xế, chủ ghe | App hiện trường | Nhận lệnh chuyến, ghi mốc (ngoại tuyến), ghép ghe ở điểm gom |

---

## 7. Bộ mô phỏng

### 7.1 Hai triển khai, một lõi

| | **Mô phỏng MKG** (vùng nội bộ) | **App SIM** (vùng nền tảng) |
|---|---|---|
| Ai dùng | CEO, đầu tư, tối ưu hóa của MKG | Các đơn vị khác trên nền tảng; đội sản phẩm thử với dữ liệu công khai |
| Dữ liệu | Thương mại của MKG: cước, chi phí trung chuyển, nhu cầu và điều khoản VFT; quan sát từ nền tảng dạng tổng hợp theo xã qua k | Tham số của tenant đơn vị đó ở `pf/masterdata` (57 tham số); nguồn cung từ cropcycle/party/geo; đồ thị `pf/waterway` |
| Mã | Dịch vụ engine nội bộ + `lib/optcore` (mở rộng) + `mkg/engine` | `pf/simulation` (Go) + `optimizer/` (vỏ Python) + `lib/optcore` 1.x |
| Trạng thái | **Chưa có — xây đầu tiên** (W8.3c, W8.3d) | **Đã có từ G2** (A-6…A-11 ở [g2-acceptance](../uat/g2-acceptance.md)) |

Hai triển khai **chung mã, không chung dữ liệu**: không đơn vị nào thấy chi phí của MKG, và MKG không ghi gì vào `pf/simulation`. Hệ quả: App SIM không có MKG làm khách hàng dùng dữ liệu thật (v1.4 §4.5).

### 7.2 App SIM — đã có và cần làm

- *Đã có:*
  - Mẫu bất đồng bộ của mục 12 kiến trúc: `POST /v1/simulations` → 202 + job → optimizer qua RabbitMQ → Internal API mTLS → kết quả bất biến.
  - Ảnh chụp đầu vào bất biến, có băm, không định danh, kèm **kết quả định tuyến đã dùng** và `algorithm_version`.
  - Đánh giá theo chuỗi FN-04 → FN-12, đối chứng FN-17, luồng rơm theo ngày 2 năm, độ nhạy FN-18.
  - `select_hubs` (MILP HiGHS) ở chế độ **thử nghiệm**.
  - Bộ 49 + 8 tham số. "Chính thức" chỉ khi mọi tham số đã duyệt.
  - 5 ca golden tái lập prototype, golden byte cho cả lõi và phong bì.
  - Màn `/mo-phong`.
- *Cần làm:*
  - Giá trị và người duyệt cho bộ 57 tham số (chờ khách).
  - Chốt *đánh giá kịch bản* hay *MILP chọn hub* (Q2-18).
  - Khi lõi có thêm chế độ mới (mục 7.4), App SIM chỉ dùng khi có phiên bản `optcore` mới **và** golden byte cũ giữ nguyên.

### 7.3 Mô phỏng MKG — tính năng cần xây

| Nhóm | Tính năng |
|---|---|
| Mạng lưới | Điểm cung theo xã (lịch thu hoạch thật theo vùng, ba vụ); bến xếp; **điểm gom** (sức đệm, năng lực trung chuyển, có cân hay không); hub (sức chứa, sấy/nén); nhà máy (nhu cầu ngày, tồn an toàn); cung đường theo **cỡ ghe đi được**, phụ thuộc mực nước, thủy triều, tĩnh không |
| Cung | Hai nguồn: đội MKG (số máy × năng suất) và mua cuộn HTX (tỷ lệ); rơm mất giá trị theo thời gian chờ |
| Bất định | **Monte Carlo thời tiết có hạt giống**, nên cùng hạt giống cho cùng kết quả; nhiều mùa vụ giả lập; kịch bản thiếu ghe |
| Kịch bản mặc định (giá trị `assumed`, v1.4 §9.1) | Máy cuộn 10 t/ngày; tỷ lệ đội MKG 100 / 40 / 27 %; ghe đủ / thiếu 30 % / thiếu 50 %; 150 và 180 ngày vụ; tiêu thụ đều 330 ngày; mức phạt giao thiếu chạy nhiều mức |
| Đầu ra | Vị trí và công suất hub; vị trí điểm gom theo nhánh sông; cơ cấu cỡ ghe (khung so với chuyến); **mức tồn an toàn** sao cho xác suất nhà máy thiếu rơm dưới ngưỡng `[KÝ]` với chi phí thấp nhất; chi phí và rủi ro ở từng mức ghe |
| Nuôi điều phối | Kết quả được ghi thành **đề xuất tham số chính sách** (tồn an toàn, cơ cấu ghe) vào sổ tham số của module chủ, qua duyệt hai người. Không ghi thẳng |
| Hiệu chỉnh | Mỗi mùa vụ trả dữ liệu kế hoạch – thực hiện để hiệu chỉnh mô hình cung, hàm chi phí, thời gian chuyến. Từ đó có chỉ số **độ chính xác dự báo so với thực tế thu gom** |
| Dữ liệu | Quan sát `kind = scenario` **chỉ dùng ở chế độ mô phỏng**, bị loại ở kế hoạch và điều phối. Mọi lần chạy có ảnh chụp bất biến (W8.2c) |
| Màn hình | `web-internal/mo-phong`: lập kịch bản, chạy (202 + job), so sánh nhiều kịch bản, độ nhạy, xuất báo cáo Excel/PDF cho CEO |
| Thời gian giải | Tính bằng giờ là chấp nhận được |

### 7.4 Lõi `lib/optcore` — hướng mở rộng

| Phần | Hiện có (1.0.0) | Thêm |
|---|---|---|
| `network` | Hub, nguồn, ba chặng | Bến xếp, điểm gom (có đệm hoặc không), cung đường theo cỡ ghe, đồ thị theo phiên bản |
| `supply` | Lịch thu hoạch, luồng theo ngày | Hai nguồn có độ tin cậy khác nhau; hao giá trị theo thời gian chờ |
| `cost` | Chi phí FN-04…FN-12 | Trung chuyển, chờ ghe, lưu kho, tổn thất do chờ, phạt khi dưới tồn an toàn |
| `uncertainty` | — | Monte Carlo thời tiết có hạt giống; tối ưu tồn an toàn |
| `solvers` | `select_hubs` (HiGHS) | T1 MILP theo ngày (HiGHS); T2a (OR-Tools Routing); T2b, T3 (CP-SAT). OR-Tools là phụ thuộc tùy chọn `[routing]` |
| Hợp đồng | `contracts/optimizer/snapshot.v1`, `result.v1` | `contracts/engine/snapshot.v1` (có `catalog_version`; engine **từ chối** phiên bản cao hơn mức hỗ trợ), `result.v1`, `plan_proposal.v1` |

**Quy tắc:** semver cho `agrigreen-optcore`; mô-đun mới không sửa `evaluate`; golden byte của App SIM không đổi; số học tất định (`js_round`, `jsum`); kết quả không chứa thời điểm tính.

---

## 8. Đầu vào của engine — chung cho TMS và mô phỏng (v1.4 §7)

- **Hai loại đầu vào:**
  - *Quan sát trạng thái ngoài* nằm ở `mkg_engine.observation`.
  - *Tham số chính sách* nằm ở sổ `param_version`/`param_current` **của module sở hữu**: logistics, hub, fieldops, measurement, costing, và `engine.replan.*`, `engine.autoapprove.*` mới.
  - Không tạo kho tham số thứ hai.
- **Kho quan sát v2 (W8.2b, sửa C-1…C-4):**
  - Thêm loại nhận thức (`measured · declared · estimated · assumed · scenario`) và phạm vi thời gian `valid_from/valid_to`.
  - Thêm `source` `file`/`estimator`/`scenario`, cùng `source_ref` và `entered_by`.
  - Mở rộng `unit` theo danh mục.
  - Tách `max_age` của danh mục (dùng để chọn giá trị) khỏi `max_age` của feed (dùng cho cảnh báo vận hành).
- **Danh mục có phiên bản** `contracts/engine/catalog.v1.json` (D-2):

| Nhóm | Khóa | Loại | Bắt buộc | `max_age` gợi ý |
|---|---|---|---|---|
| Nhà máy | Tồn hiện tại | Quan sát, có bộ ước tính | Có | 7 ngày (số đo làm mốc) |
| Nhà máy | Kế hoạch tiêu thụ, lịch dừng lò | Quan sát | Có | 7 ngày |
| Nhà máy | Sức chứa bãi, năng lực bốc dỡ bến | Quan sát | Có | 90 ngày |
| Cung | Tiến độ gặt, diện tích theo xã | Quan sát | Có | 2 ngày trong vụ |
| Cung | Sản lượng rơm tấn/ha | Chính sách (hệ số) | Có | Theo vụ |
| Đội, máy | Đội/máy/người khả dụng | Quan sát nội bộ | Có | 1 ngày |
| Ghe | Ghe khả dụng theo cỡ/ngày | Quan sát | Có | 1 ngày |
| Mạng lưới | Đồ thị thủy, cỡ ghe tối đa, tĩnh không | Quan sát, theo phiên bản đồ thị | Có | Theo phiên bản |
| Mạng lưới | Thủy triều | Quan sát | Tùy chọn ở Phase K | 1 ngày |
| Thời tiết | Dự báo mưa | Quan sát | Tùy chọn | 12 giờ |

- **Quy tắc chọn giá trị (v1.4 §7.3):**
  - Khóa có bộ ước tính dùng đầu ra của bộ ước tính, neo vào số đo hợp lệ mới nhất.
  - Khóa không có bộ ước tính dùng giá trị mới nhất theo `as_of` trong `max_age`. Loại nhận thức chỉ dùng để phân xử khi cùng `as_of`.
  - Giá trị quá `max_age` bị loại.
  - Khi không còn giá trị hợp lệ:
    - khóa bắt buộc → engine **từ chối chạy** và nêu khóa thiếu;
    - khóa tùy chọn → dùng mặc định gắn nhãn `assumed` và ghi cờ vào kết quả.
- **Bộ ước tính tồn VFT (D-3):** `tồn(t) = tồn đo(t0) + Σ CUSTOMER_IN(t0..t) − Σ tiêu thụ kế hoạch(t0..t)`, chặn trong [0, sức chứa], chạm biên thì cảnh báo. Độ bất định cộng dồn, khoảng 750 t/ngày ở Phase 1, và được quy thành tồn đệm.
- **Cổng nạp:**
  - Thứ tự 1: nhập tay theo danh mục, bắt buộc `as_of` và đơn vị; **tệp theo mẫu chuẩn do MKG phát hành**, có người xác nhận; API kéo hoặc đẩy.
  - Thứ tự 2: email, có kiểm DKIM/SPF/DMARC.
- **Ảnh chụp (W8.2c):** sổ `input_snapshot` + `input_snapshot_key`. Mỗi khóa ghi: bản ghi đã dùng, `as_of`, **tuổi lúc chụp**, loại nhận thức, quy tắc chọn, cờ. Ghi thêm phiên bản của mọi sổ tham số đã đọc. Ảnh chụp lưu bất biến ở kho đối tượng trong lãnh thổ, kèm SHA-256.

---

## 9. Các cổng kết nối cần tạo

**Nguyên tắc chung cho mọi cổng giữa hai hệ thống:**
- Đi qua `cmd/integration-gateway`.
- Hợp đồng nằm ở `contracts/integration/`. Hợp đồng có trạng thái `proposed` → `approved`, người đề xuất khác người duyệt (R-56).
- Đọc dùng **cursor**, hỗ trợ đọc lại sau 410. Ghi idempotent theo `idem_key`.
- Xác thực bằng khóa API kèm **mTLS bắt buộc cho đối tác máy** (ADR-0041).
- Mọi luồng ra đều ghi `egress_event` (DLP, ADR-0024).
- Không kết nối DB xuyên cụm. Không hàng đợi chung giữa hai hệ.
- Luồng thương mại có **chữ ký số của tổ chức gửi** trên từng lô (v4.3 ADR-24 = ADR-0042). Chữ ký lưu ở **sổ phụ**, không thêm cột vào sổ đã có chuỗi băm.

| Mã | Kết nối | Chiều | Cơ chế, hợp đồng | Xác thực / ký | Trạng thái | Gói |
|---|---|---|---|---|---|---|
| **CK-01** | Danh bạ HTX | Nền tảng → ERP | Feed `cooperative_directory.v1` → bản sao `mkg_fieldops` + kho quan sát | Khóa API + mTLS | **Có** (W5.5, W8.2); hợp đồng `proposed`, chờ A7 | Chuyển `approved` |
| **CK-02** | Danh bạ thửa | Nền tảng → ERP | Feed `plot_directory.v1` | Như trên + cấp phép có `purpose_code` "lập lệnh gom" | **Có**; đưa vào engine **chỉ khi cấp phép cho mục đích đó** | W10.15 |
| **CK-03** | Tiến độ gặt, diện tích theo xã (ngưỡng k) | Nền tảng → ERP / engine | Feed `harvest_progress_by_commune.v1`, `grant_class = aggregate` (ADR-0035) | Khóa API + mTLS | **Mã có**; chờ ký ADR-0035 và giá trị k (C-3) | — |
| **CK-04** | Đơn vị hành chính | Nền tảng → ERP | Feed `admin_units.v1` | Như trên | **Có** | — |
| **CK-05** | Lịch mùa vụ, cửa sổ gặt | Nền tảng → ERP / engine | Feed `season_harvest_dates.v1` | Như trên | **Thiếu producer** ở cổng | W8.3h |
| **CK-06** | Đồ thị đường thủy (có phiên bản) + cỡ ghe tối đa, tĩnh không cầu, ngưỡng mực nước | Nền tảng → ERP (TMS, engine) | Feed `waterway_routes.v1` (siêu dữ liệu phiên bản + ảnh chụp qua URL ký); thay `/v1/partner/waterway/route` | Như trên | **Thiếu producer**; đoạn luồng mới có `waterway_class`, chưa có thuộc tính cỡ ghe | W8.3h, W5.10 |
| **CK-07** | Bản đồ chuẩn hóa GIS cho TMS (GIS US-INT-02) | Nền tảng → ERP | Hiện là `GET /v1/geo/standard-map` (vai `map_consumer`). Vùng mkg không gọi được Application API nên phải thành feed hoặc tệp của cổng | Như trên | **Một phần**; phạm vi TMS chờ khách (Q2-22) | Chỉ làm nếu TMS cần lớp ngoài CK-06 |
| **CK-08** | Cơ sở hub và sức chứa | ERP → nền tảng | Lô `hub_facility.v1`, `hub_capacity.v1` gửi `POST /v1/batches/{hợp đồng}`; sổ `valve_export` vẫn là bằng chứng | Khóa API + mTLS | **Có qua van RabbitMQ**; phải chuyển sang cổng | W10.2 |
| **CK-09** | Bằng chứng thu gom cho MRV (khối lượng làm tròn theo bậc) | ERP → nền tảng | Lô `mrv_collection_evidence.v1` | Như trên | **Có qua van**; danh mục trường MRV chờ C-1 | W10.2 |
| **CK-10** | Lịch gom, bảng kê số cuộn theo thửa, xác nhận số cuộn của HTX | ERP ↔ HTX (song phương, chỉ hai bên thấy) | `POST /v1/batches/roll_count_statement`, `collection_schedule`; `GET /v1/feeds/bilateral?pair=…` cho `htx_roll_confirmation`; tổng kiểm ngày `bilateral_day.v1` | Khóa API + mTLS + **chữ ký số bắt buộc** (lịch gom: nên có); HTX chưa có chứng thư thì nền tảng ký thay có ủy quyền | **Mã có** (W5.6, chạy song song với van), chờ C-12; **chưa có chữ ký số** | W10.7 (= W8.6) |
| **CK-11** | VFT: tồn nhà máy, kế hoạch tiêu thụ, lịch dừng lò, sức chứa bãi, biên bản nghiệm thu | VFT → ERP (**không qua nền tảng**) | **Cổng nhận nội bộ**: cùng mã `integration-gateway`, triển khai và dữ liệu riêng ở vùng mkg; API đẩy + tệp theo mẫu MKG; ghi vào kho quan sát (`declared`/`measured`) và `orders` | Khóa API + mTLS + **chữ ký số** cho dữ liệu làm căn cứ nghiệm thu | **Thiếu** | W8.3b, W10.8 |
| **CK-12** | Số cân của nhà máy khách (`CUSTOMER_IN`) | Khách → ERP | Hiện nhập tay kèm bằng chứng + nhập lô (kênh `api`). Đích là tích hợp hệ cân nhà máy (có thể gộp vào CK-11 với VFT) | Như CK-11 | **Một phần**; chờ C-5 | W8.5a |
| **CK-13** | Đầu cân tại hub và điểm gom | Biên OT → ERP | `edge-agent` → `ingest` theo `contracts/edge/batch.v1`, đệm đĩa ≥ 72 giờ, bộ đếm đơn điệu; tích phân thành phiếu cân (R-70) | mTLS chỉ đi ra, Ed25519 theo thiết bị, cập nhật có ký | **Có khung + bộ mô phỏng**; bộ điều hợp cho mẫu cân thật chờ C-6; thêm `CONSOLIDATION` | W8.5a |
| **CK-14** | GPS ghe và xe, ETA | Biên → ERP | Telemetry → `ingest` → cụm `ts` (`ts_fleet`, migration mới); chỉ thu trong giờ làm việc | mTLS / token thiết bị | **Thiếu**; chờ D-12 | W8.7 |
| **CK-15** | Phần mềm kế toán pháp định | ERP → kế toán | `acctbridge/port.LedgerAdapter`: bút toán tổng hợp tháng, một chiều; bảng công từ `workforce` | Theo nhà cung cấp, trong lãnh thổ | **Một phần**: bộ xuất CSV/XML + bộ giả; nhà cung cấp chưa chọn (D-11) | Bộ điều hợp thật |
| **CK-16** | Engine ↔ ERP nội bộ | Trong vùng mkg | Việc qua vhost `mkg.internal` (`mkg.engine.job.requested`/`completed`); engine `GET` ảnh chụp và `POST` kết quả/đề xuất qua **Internal API mTLS** của `api-internal`; kiểm SHA-256 | mTLS, CN `engine` | **Thiếu** | W8.3c |
| **CK-17** | App SIM optimizer ↔ api-platform | Trong vùng nền tảng | Queue `pf.simulation.optimizer` + Internal API `:8084` | mTLS, CN `optimizer` | **Có** (G2) | — |
| **CK-18** | Định tuyến đường bộ | ERP → OSRM | OSRM tự vận hành trong lãnh thổ; kết quả đã dùng lưu nguyên văn + SHA-256 vào chặng | Mạng nội bộ | **Thiếu** (đang dùng Haversine × #10) | Gói hạ tầng |
| **CK-19** | Thời tiết, thủy triều, mực nước | Nguồn ngoài → ERP (kho quan sát) | Truy vấn theo **ô lưới hoặc tâm xã** (không mang dữ liệu cá nhân); qua proxy nếu ADR-0007 bắt buộc | Theo nhà cung cấp | **Thiếu**; nguồn trong nước chưa chọn (Q2-10) | Sau khi chọn nguồn |
| **CK-20** | Tệp mẫu chuẩn của MKG cho quan sát (đội thuê ghe, nhà máy, vận hành) | Đối tác / đội nội bộ → ERP | Tải lên `api-internal`: `staging` → người xác nhận → ghi sổ; `source = file`, `source_ref = tệp#dòng`; dùng lại `shared/docfile` | Đăng nhập OIDC + MFA | **Thiếu** | W8.3a |
| **CK-21** | Email báo cáo của đối tác | Đối tác → ERP | Hộp thư trong lãnh thổ; kiểm DKIM/SPF/DMARC; nội dung là dữ liệu, không bao giờ là lệnh; người xác nhận | — | **Để sau** (Phase K, nếu vẫn cần) | — |
| **CK-22** | Chữ ký số: CA, HSM hoặc ký từ xa | ERP, cổng ↔ nhà cung cấp chứng thực trong nước | Ký CAdES/PKCS#7 tách rời trên băm chuẩn hóa của lô; kiểm OCSP/CRL; lưu bằng chứng kiểm | Chứng thư tổ chức | **Thiếu**; chờ pháp chế chọn CA và hình thức ủy quyền ký thay | W10.7 |
| **CK-23** | Định danh người dùng ERP | Người dùng → Keycloak → `api-internal` | OIDC; MFA cho mọi người dùng không phải nông hộ; app hiện trường dùng thiết bị đăng ký + PIN; tài khoản quản trị riêng của hệ nội bộ | ADR-0041 | **Một phần** (MFA chỉ ở vai `[MFA]`) | W10.3, W10.4 |
| **CK-24** | Kho phụ tùng, nhiên liệu (mua ngoài) → chi phí kỳ | Kế toán → ERP | Tệp hoặc API chi phí theo kỳ, tính theo máy/ghe/hub để `costing` phân bổ | Theo nhà cung cấp | **Chưa quyết** cần hay không | — |

**Thứ tự ưu tiên cổng:**
1. CK-16, CK-20, CK-05/CK-06: để mô phỏng MKG chạy được.
2. CK-11 (VFT), CK-08/CK-09 sang cổng.
3. CK-10 có chữ ký và CK-13 với mẫu cân thật: trước Phase K.
4. CK-14, CK-18, CK-19: trước điều phối Phase 1.
5. CK-15 khi đã chọn phần mềm kế toán.

---

## 10. Lộ trình và gói việc

Các gói **làm mịn W8.3–W8.5** của [kế hoạch v4.1](../kien-truc-v4.1-ke-hoach.md); điều kiện chặn "MKG v1.3" nay đổi thành v1.4. Migration đặt tên theo mốc thời gian (`go run ./tools/migratecheck -new mkg_<module>_<tên>`, WH-B, [parallel-packages](../dev/parallel-packages.md)). Số giữ chỗ cũ mkg 000027–000032 bỏ.

| Gói | Nội dung | Phụ thuộc | Làm ngay? | Cỡ | Mốc |
|---|---|---|---|---|---|
| **D-9** | Đưa v4.3 và v1.4 vào `docs/kien-truc/`, bảng ánh xạ ADR-21…24 ↔ ADR-0039…0042, sửa C-5/C-6 trong [mkg.md](mkg.md) | — | Có | S | Ngay |
| **W8.2b** | Kho quan sát v2 + danh mục `catalog.v1` + đường ghi `manual` + `Observations.Select` theo quy tắc chọn | W8.2 | Có (`max_age` tạm, gắn `provisional`) | M | G2 |
| **W8.2c** | Ảnh chụp đầu vào engine + hợp đồng `contracts/engine/snapshot.v1` | W8.2b | Có | M | G2 |
| **W8.3a** | Cổng tệp mẫu chuẩn MKG (CK-20) | W8.2b | Có | M | G2 |
| **W8.3b** | Bộ ước tính tồn VFT + khóa nhà máy; kênh VFT (CK-11) | W8.2b/c | Bộ ước tính: có. Kênh VFT: chặn (thỏa thuận VFT) | M | G2 / Phase K |
| **W8.3c** | Dịch vụ engine nội bộ: triển khai Python thứ hai, vhost `mkg.internal`, Internal API mTLS (CK-16), NetworkPolicy không có luồng sang nền tảng | W8.1, W8.2c | Có | M | G2 |
| **W8.3d** | **Mô phỏng MKG**: mở rộng `optcore` (mục 7.4), kịch bản `assumed`, màn `web-internal/mo-phong` | W8.3c; W8.3h để có đồ thị thật | Có, với dữ liệu giả định | L | **Giữa 2027** |
| **W8.3e** | **Làm thử T2a/T2b (D-6)** + ADR phụ thuộc OR-Tools; bộ sinh dữ liệu tổng hợp cỡ Phase 1; báo cáo đạt/không đạt | ADR mới | Có, **ưu tiên cao** | M–L | G2 |
| **W8.3h** | Producer `waterway_routes`, `season_harvest_dates` (CK-05/06); thuộc tính đoạn luồng: cỡ ghe, tĩnh không, mực nước | W5.3 | Có (phía nền tảng) | M | G2 |
| **W8.5c** | `logistics`: thuê ghe, ghe khả dụng, bến xếp, chặng ghép ghe | W8.2b | Có | M | G3 |
| **W8.5b** | Gia phả dùng chung: điểm gom là cơ sở, nút trộn N → 1 | W8.5c | Có (chờ trả lời Q-10) | M–L | G3 |
| **W8.5a** | `measurement`: `CONSOLIDATION`, `weighing_mode`, đối soát lô đi thẳng, cờ độ tin cậy; tác động `costing` | W8.5b | Lược đồ: có. Định mức: chờ C-4/C-5/C-6 | L | G3 |
| **W8.5d** | `fieldops`: máy cuộn, hỏng hóc, khả dụng đội/máy | W8.4 (để gắn người lái) | Danh mục và hỏng hóc: có | M | G3 |
| **W8.5e** | `orders`: nguồn cung hai độ tin cậy | — | Có | S | G3 |
| **W10.17** | `cap/workflow` ở cụm mkg cho ba chỗ có tiền | W6.1 | Có | M | G3 |
| **W10.2** | Gỡ van RabbitMQ `hub_valve`, `mrv` sang lô của cổng (CK-08/09) | W5.10, W10.1 | Sau | M | G3 |
| **W8.4** | `mkg/workforce` + kho định danh nhân sự; xuất bảng công; `worker_ref` (C-7) | W2.6 | **Chặn** (pháp chế). Làm trước được khung `worker_ref` và kho rỗng | L | Trước Phase K |
| **W8.3f** | Kế hoạch đề xuất, vùng đóng băng, tự duyệt năm điều kiện, chỉ số đảo ngược | W8.3c, W8.5c | Mã: có. Bật: chặn (X %, D-7) | L | Trước Phase K |
| **W8.3g** | Kế hoạch cuộn 7–14 ngày chạy hằng ngày + T3 bán tự động | W8.3d, W8.3f, W8.5d | Chặn bởi dữ liệu thật (A7, k) | L | **Phase K** |
| **W10.7 / W8.6** | Chữ ký số lô song phương và VFT (CK-10, CK-11, CK-22) | W5.6 | **Chặn** (pháp chế, CA) | L | Trước Phase K |
| **W10.8** | Cổng nhận nội bộ cho VFT (CK-11) | W10.7, W8.5 | Chặn (VFT) | L | Trước Phase K |
| **W8.7** | Dữ liệu vị trí (D-12): cờ tạm dừng ngay; kho có thời hạn lưu; GPS ghe (CK-14) | Pháp chế | Cờ: có | S–M | Trước Phase K |
| **Bộ điều hợp kế toán** | CK-15 cho phần mềm đã chọn | D-11 | Chặn | M | Trước Phase K |
| **Điều phối đầy đủ** | Sửa nhanh và giải lại theo **kết quả D-6**; OSRM (CK-18); thời tiết/thủy triều (CK-19); màn `dieu-phoi`; tăng bản sao | W8.3e…g | — | L | **Trước Phase 1** |

```
Ngay ─► D-9 ─► W8.2b ─► W8.2c ─► W8.3c ─► W8.3d (mô phỏng MKG, giữa 2027)
         │                          ▲
         ├─► W8.3e (làm thử D-6) ───┘ (kết quả quyết mục tiêu điều phối)
         ├─► W8.3h (producer feed, đồ thị) ──► W8.3d
         └─► W8.3a, W8.3b (bộ ước tính)
G3:  W8.5c ─► W8.5b ─► W8.5a · W8.5d · W8.5e · W10.17 · W10.2
Trước Phase K: W8.3f ─► W8.3g · W8.4 · W10.7 ─► W10.8 · W8.7 · bộ điều hợp kế toán
Phase K (mùa vụ đầu): kế hoạch cuộn chạy song song với điều phối viên; đo kế hoạch – thực hiện; hệ số tấn/ha tự đo lần đầu
Trước Phase 1: điều phối đầy đủ, tự duyệt theo chính sách đã ký
```

**Định nghĩa "xong" cho mỗi gói:**
- Sổ theo mẫu 8.1 + `shared.apply_ledger`, có kiểm thử UPDATE/DELETE/TRUNCATE (R-20).
- RLS một policy (R-33), kiểm thử rò tenant.
- POST có `Idempotency-Key`; việc dài hơn 2 giây trả 202 + job.
- Duyệt bốn mắt ở `app/` và CHECK DB (R-56).
- Sự kiện có schema và kiểm tương thích ngược (R-64).
- `agnofloat`, `archlint`/`agboundary`, `modgen -check`, `migratecheck`, `contractcheck` đều xanh.
- OpenAPI `mkg-internal.yaml` + mã sinh của web nội bộ.
- Kiểm thử DB chạy thật (`--- PASS`, không SKIP).
- Cập nhật [mkg.md](mkg.md) và ma trận nghiệm thu.

---

## 11. Quyết định chờ và câu hỏi mở

| # | Câu hỏi | Đề xuất | Ai | Chặn |
|---|---|---|---|---|
| D-2 | Danh mục quan sát v1: `max_age`, khóa bắt buộc/tùy chọn | Dùng bảng mục 8 làm giá trị tạm | MKG vận hành | W8.2b (bật thật) |
| D-5 | Đường dữ liệu nền tảng → MKG (k, ADR-0035, A7) | `grant_class = aggregate` của ADR-0035 tương đương ý v1.4 §7.7 | MKG + pháp chế | Cả ba chế độ engine |
| D-6 | Tiêu chí đạt của làm thử T2a/T2b | Định trước khi chạy: p95 sửa nhanh, thời gian giải lại, khoảng chênh chi phí | Kiến trúc + điều phối | Mốc điều phối |
| D-7 | X % và các ngưỡng tự duyệt, kích hoạt | Tham số đặc quyền, đổi qua hai người | MKG `[KÝ]` | Bật W8.3f |
| D-8, D-12 | Kho định danh nhân sự; căn cứ và thời hạn lưu vị trí người lái | Năm quy tắc của v1.4 §5.1 | Pháp chế | W8.4, W8.7, CK-14 |
| D-11 | Phần mềm kế toán | Tiêu chí: TT 99/2025, hóa đơn điện tử, lương mùa vụ, kho phụ tùng | MKG tài chính | CK-15 |
| Q-10 | Điểm gom là cơ sở (`mkg/hub`) hay chỉ là nút mạng của logistics? | Là cơ sở, để giữ một mô hình gia phả | MKG | W8.5b |
| Q-13 | 1.500 công nhân có tài khoản đăng nhập không, hay chỉ tổ trưởng? | Chỉ tổ trưởng; công nhân là `worker_ref` | MKG | W8.4, CK-23 |
| Q-14 | Kênh VFT: API, tệp mẫu hay cả hai? Ai cấp chứng thư cho VFT? Kế hoạch tiêu thụ theo ngày hay theo lò? | Cả hai, cùng một hợp đồng | Thương mại + VFT | CK-11 |
| Q-15 | Nguồn thời tiết, thủy triều, mực nước trong nước | — | Đội dữ liệu | CK-19 |
| Q-18 | Mô phỏng MKG chạy ngay trên đồ thị hiện có (chỉ `waterway_class`) hay chờ thuộc tính cỡ ghe/tĩnh không? | Chạy ngay với cỡ ghe suy từ `waterway_class` và gắn `assumed` | Kiến trúc | W8.3d |
| Q2-22 | Phạm vi dữ liệu TMS cần từ GIS ngoài đồ thị thủy (CK-07)? | Chỉ CK-06, trừ khi có yêu cầu cụ thể | Khách | CK-07 |
| CK-24 | Có nhận chi phí nhiên liệu và phụ tùng theo kỳ từ phần mềm mua ngoài vào `costing`? | Có, tổng hợp theo máy/ghe/hub, một chiều | MKG tài chính | Giá thành đầy đủ |
| ADR | OR-Tools (giấy phép, wheel nhị phân); ADR-0016 (numpy) | Viết ADR theo quy trình ADR-0016 | Kiến trúc + pháp chế | W8.3e |
| C-4…C-6 | Dung sai cân, đường nhận số cân khách, mẫu cân | — | Thương mại, vận hành | W8.5a, CK-12, CK-13 |
| C-12 | Luồng song phương | — | MKG + pháp chế | CK-10 |

---

## Phụ lục A — Ánh xạ prototype `mekong-green/src/erp` → module đích

| Prototype | Nội dung | Module đích |
|---|---|---|
| `field/service.ts` | Đội, thành viên, phương tiện hiện trường, việc thu gom, năng lực tấn/ngày | `mkg/fieldops` (+ `mkg/workforce` cho thành viên) |
| `straw/contracts.ts`, `straw/tickets.ts` | Hợp đồng thu mua với HTX (CT-01..), phiếu mua rơm, công nợ HTX | `mkg/orders` (chiều mua) + `mkg/costing` (quyết toán) |
| `procurement/service.ts` | PR → PO → GRN → đối soát ba chiều → đề nghị thanh toán | `mkg/orders`; thanh toán ở kế toán mua ngoài |
| `warehouse/service.ts` | Kho rơm FN-01…39: nhập, giám sát ẩm/nhiệt, xuất theo rủi ro, kiểm kê | `mkg/hub` |
| `sales/service.ts` | Hợp đồng khung → SO → lệnh giao → ePOD → hóa đơn | `mkg/orders`; hóa đơn ở kế toán mua ngoài |
| `tms/service.ts`, `tms/vessels.ts` | Định tuyến đa tiêu chí, chuyến, ePOD, chênh lệch chi phí, danh mục ghe và đơn giá thuê, hạn giấy tờ | `mkg/logistics` + engine (TMS, mục 6) |
| `finance/service.ts` | Bút toán, phí nền tảng, revenue-share, chia doanh thu carbon, P&L, ngân sách | `mkg/costing` + `mkg/acctbridge` + kế toán mua ngoài. Phí nền tảng và đo lường sử dụng thuộc `pf/metering`, không thuộc ERP |
| `simulation/*`, `params/catalog.ts` | Mô phỏng chọn hub, 49 tham số, Assumption Register | `pf/simulation` + `lib/optcore` (App SIM); engine nội bộ (mô phỏng MKG) |
| `reporting/service.ts` | Dashboard điều hành, tài chính, MRV; xuất CSV/HTML giữ nguyên số đang hiển thị | `cap/analytics`, `cap/export` của vùng tương ứng |

## Phụ lục B — Thuật ngữ

| Thuật ngữ | Nghĩa |
|---|---|
| Điểm gom | Nút trung chuyển ở nhánh sông lớn, nơi ghép ghe nhỏ thành ghe lớn |
| Hành lang | Một nhánh sông mà ghe đi trong đó; đơn vị chia bài toán T2 |
| Nút trộn | Bản ghi gia phả: lô ra có N nguồn kèm tỷ lệ (mẻ ở hub, ghép ghe ở điểm gom) |
| Vùng đóng băng | Lệnh đang chạy, hoặc khởi hành dưới 2 giờ tới, mà engine không được đổi |
| Quan sát / tham số chính sách | Trạng thái bên ngoài có độ cũ / quyết định của MKG có phiên bản và người duyệt |
| `assumed` | Giá trị tạm khi chưa có nguồn, luôn ghi cờ vào kết quả |
| Phase K / Phase 1 | Trạng thái cấp phát của hệ đích: G5 (Q4/2028) và G6 (Q1/2029) theo kiến trúc v3.0 Phụ lục A |
