# AgriGreen — Thiết kế kiến trúc hệ thống

**Phiên bản:** v4.1 · **Ngày:** 03/10/2026 · **Trạng thái:** khung phát triển, chờ MKG ký các mục đánh dấu `[KÝ]`
**Người soạn:** Senior Solution Architect
**Thay thế:** v4.0 cùng ngày, Thiết kế kiến trúc hệ thống v3.0, Đề xuất kiến trúc module hóa v1.0
**Hợp nhất ở v4.1:** Thiết kế chi tiết vùng nội bộ MKG v1.3 — tóm tắt ở mục 27.2, bản đầy đủ là tài liệu đi kèm; **Phần VI mới — Tích hợp liên hệ thống**
**Tài liệu đi kèm, còn hiệu lực:** SAD Lớp Định danh và Phân quyền v1.1 — đặc tả chi tiết module lõi `k/access`; BRD Lớp SSO và Vai trò v1.1

**Cách đọc:** Phần I là quyết định — không thảo luận lại khi viết mã. Phần II là hợp đồng module và quy ước. Phần III là thiết kế dữ liệu, gồm kết quả rà soát lại thiết kế database. Phần IV đặc tả từng module. Phần V là vận hành và an toàn. Quy tắc bắt buộc mang mã theo họ: **R-ARC** kiến trúc · **R-MOD** module · **R-CODE** mã · **R-API** giao diện · **R-EVT** sự kiện · **R-DB** dữ liệu · **R-SEC** an ninh · **R-OPS** vận hành. Bảng ánh xạ từ quy tắc v3.0 ở Phụ lục D.

---

## Thay đổi so với v3.0

| # | Thay đổi | Lý do |
|---|---|---|
| 1 | **Kiến trúc bốn tầng module**: lõi · năng lực dùng chung · nghiệp vụ · ứng dụng | v3.0 tổ chức theo miền, không có tầng dùng chung; ba ứng dụng UAT tự làm lại cùng chức năng và tự giữ cùng dữ liệu |
| 2 | **Ứng dụng là tổ hợp module** khai báo bằng tệp `app.yaml` | v3.0 không định nghĩa ứng dụng là gì |
| 3 | **Tệp khai báo module** `module.yaml`, phụ thuộc có phiên bản, bật theo đơn vị | Điều kiện để giữ ranh giới bằng CI thay vì bằng người review |
| 4 | **`dom/cooperative` là chủ duy nhất** của HTX, nông hộ, thửa | Sửa gốc CROSS-02: App Khuyến nông 0 nông hộ trong khi App HTX có 21 |
| 5 | **Chia sẻ giữa đơn vị trong vùng nền tảng: đọc tại chỗ có cấp phép**, không sao chép | Sửa gốc CROSS-03; thay SAD định danh mục 6.3 |
| 6 | **Rà soát lại thiết kế database** — 14 phát hiện, 2 nghiêm trọng; **toàn bộ DDL đã chạy thử trên PostgreSQL**, hai phát hiện chỉ lộ ra khi chạy | Phần III |
| 7 | Tích hợp SAD định danh v1.1 làm module lõi `k/access` | Một tài liệu kiến trúc, một nguồn sự thật |

---

## Thay đổi ở v4.1

| # | Thay đổi | Ở đâu |
|---|---|---|
| 1 | **Vùng nội bộ MKG chi tiết hóa:** ba thành phần — kế toán pháp định mua ngoài, hệ vận hành tự xây một khối, engine điều phối riêng ba chế độ; cả ba chế độ engine ở vùng nội bộ | 27.2 |
| 2 | **Phần VI mới — Tích hợp liên hệ thống.** Kết nối MKG ↔ AgriGreen là thí điểm cho việc kết nối nhiều bên: MKG kết nối qua **đúng cổng tích hợp mà mọi đối tác tương lai sẽ dùng** | 37–44 |
| 3 | **Bốn cửa thay ba cửa:** thêm **luồng song phương** — dữ liệu giữa hai bên của một giao dịch, chỉ hai bên đó thấy | 4, 38 |
| 4 | Gói lõi tối ưu dùng chung `lib/optcore` giữa `dom/hubsim` và engine nội bộ — mã dùng chung, dữ liệu riêng | 6, 27.2 |
| 5 | Tên trong repo: theo bản review 03/10, repo dùng `pf/*`, `mkg/*`; bảng ánh xạ ở Phụ lục D | D |

---

# PHẦN I — QUYẾT ĐỊNH

## 1. Phạm vi

Kiến trúc **toàn bộ hệ thống**: nền tảng AgriGreen, hệ nội bộ Mekong Green, kho định danh hộ, lớp định danh và phân quyền, các thành phần biên.

| Trong phạm vi | Ngoài phạm vi |
|---|---|
| Bảy ứng dụng: Khuyến nông (KN) · Hợp tác xã (HTX) · Bản đồ & Sàn Cơ giới hóa (CGH) · Bộ mô phỏng chọn hub (SIM) · Hệ nội bộ Mekong Green (MKG) · Kênh nông hộ (NH) · Quản trị nền tảng (PF) | Khối kế toán pháp định mua ngoài — chỉ thiết kế cầu nối |
| Toàn bộ module lõi, năng lực, nghiệp vụ | Hệ thống của khách hàng |
| Biên: agent cân IoT, app hiện trường, đồng bộ ngoại tuyến, telemetry | Quản lý tài khoản đặc quyền ở tầng hạ tầng — giải pháp riêng, **bắt buộc theo TT 12/2022, phải có chủ** |

## 2. Hai điểm thiết kế

| | ĐTK 1 — Bản thử nghiệm | ĐTK 2 — Hệ đích |
|---|---|---|
| Mốc | **15/12/2026** | Giao dịch bên thứ ba cuối 2028; Phase 1 giữa 2029 |
| Ứng dụng chạy thật | App Khuyến nông | Cả bảy |
| Đơn vị tham gia | Chương trình khuyến nông — một đơn vị, nhưng **mã đã chạy nhiều đơn vị** | Nhiều |
| Cắt phạm vi được? | Có | Không, ở nhóm cấu trúc |

**Phase K là trạng thái cấp phát của ĐTK 2, không phải điểm thiết kế riêng.**

> **R-ARC-01.** Mã chạy đúng ở cấu hình ĐTK 2 từ dòng đầu. Chuyển giai đoạn là thay cấu hình, số bản sao và tập module bật — không sửa mã.

## 3. Bảy ràng buộc cứng `[KÝ]`

Phương án vi phạm bị **loại**, không cân nhắc đánh đổi.

| Mã | Ràng buộc | Cưỡng chế bằng |
|---|---|---|
| **RB-1** | Chỉ ghi thêm. Không UPDATE, không DELETE dữ liệu nghiệp vụ; sửa sai bằng bản ghi bù; thời hạn lưu là chuyển tầng | Ba lớp ở DB — R-DB-06 |
| **RB-2** | Cách ly theo đơn vị tham gia ở tầng dữ liệu; `tenant_id` trong mọi bảng nghiệp vụ | Row-level security — R-DB-08 |
| **RB-3** | Dữ liệu cá nhân xóa được, tương thích RB-1 | Kho định danh + mã hóa theo khóa từng hộ — mục 20 |
| **RB-4** | Giấy phép cho phép dùng trong nền tảng thu phí, có đường thoát | Cổng giấy phép trong CI |
| **RB-5** | Mã chịu cấu hình ĐTK 2; chuyển giai đoạn không sửa mã | R-ARC-01, R-MOD-05 |
| **RB-6** | Mọi số đo ở biên giữa hai bên có đối soát chéo, ngưỡng dung sai thống nhất trước | `mkg/measurement` — mục 27 |
| **RB-7** | Dữ liệu cá nhân, sao lưu, ảnh hiện trường, nhật ký chứng cứ **không lưu ngoài lãnh thổ** | Danh mục nhà cung cấp được phép — mục 33 |

## 4. Bốn vùng chủ quyền và các cụm

| Vùng | Chủ dữ liệu | Cụm dữ liệu | Đơn vị triển khai ĐTK 2 |
|---|---|---|---|
| **Định danh** | Nền tảng | `idp` — Keycloak; Access và nguồn của cây tổ chức | `keycloak`, `access` |
| **Nền tảng AgriGreen** | Từng đơn vị tham gia | `pf` | `api-platform`, `worker-platform`, `sync` |
| **Nội bộ Mekong Green** | Mekong Green | `mkg`, `mkg-ts` | `api-internal`, `worker-internal`, `ingest`, `optimizer` |
| **Kho định danh hộ** | Hộ nông dân | `vault` — **cụm riêng từ ĐTK 1** | `identity-vault` |
| Biên | Theo nguồn | Thiết bị | `edge-agent`, app hiện trường |

**Hai dân số biên:** nhân viên MKG, agent cân, telemetry → vùng nội bộ. Cán bộ khuyến nông, HTX, nông hộ → **thẳng vùng nền tảng**.

**Bốn cửa giữa các vùng — sửa ở v4.1:**

| Cửa | Chiều | Cơ chế |
|---|---|---|
| Van công bố | Nội bộ MKG → nền tảng, **dữ liệu mạng lưới** | Hợp đồng khai báo, mặc định từ chối, CI hỏng build khi có trường ngoài hợp đồng |
| Tiêu thụ không đặc quyền | Nền tảng → nội bộ MKG | Hệ nội bộ đọc dữ liệu được cấp phép qua **cổng tích hợp** bằng chứng danh của đơn vị MKG. Không kết nối DB từ `mkg` tới `pf` |
| Van lọc dữ liệu công | Nền tảng → MKG, tuyến dữ liệu công | Chỉ dữ liệu tổng hợp qua ngưỡng gom nhóm **k ≥ `[KÝ]`** — R-DB-11 |
| **Luồng song phương — mới** | Giữa MKG và **một đối tác cụ thể** của một giao dịch — ví dụ số cuộn MKG ghi nhận cho đúng HTX đã bán | Chỉ bên đối tác đó thấy; không vào dữ liệu mạng lưới; cần ADR — mục 38.2 |

**Cả bốn cửa hiện thực bằng một cổng tích hợp — Phần VI — và MKG đi qua cổng đó như mọi đối tác (R-INT-01).**

**Trong cùng vùng nền tảng, giữa hai đơn vị:** **đọc tại chỗ có cấp phép** (`share_grant`) — mục 17.3. Không sao chép.

## 5. Mô hình bốn tầng module

```
ỨNG DỤNG     app/KN  app/HTX  app/CGH  app/SIM  app/MKG  app/NH  app/PF
             tệp khai báo tổ hợp module + cấu hình + giao diện
────────────────────────────────────────────────────────────────────────
NGHIỆP VỤ    dom/extension  dom/cooperative  dom/mechanization
             dom/hubsim  dom/waterway  dom/mrv  dom/metering
             mkg/fieldops  mkg/measurement  mkg/hub  mkg/logistics
             mkg/orders  mkg/costing  mkg/acctbridge
────────────────────────────────────────────────────────────────────────
NĂNG LỰC     cap/masterdata  cap/workflow  cap/forms  cap/export
DÙNG CHUNG   cap/analytics  cap/import  cap/task  cap/notify  cap/evidence
             cap/content  cap/price  cap/geo  cap/sync
────────────────────────────────────────────────────────────────────────
LÕI          k/access  k/orgtree  k/ref  k/ledger  k/jobs  k/audit  k/ui-shell
```

**Phụ thuộc chỉ đi xuống.** Tầng dưới không biết tầng trên tồn tại.

### 5.1 · Ma trận module × ứng dụng

● dùng trực tiếp · ○ đọc dữ liệu đơn vị khác qua cấp phép, hoặc qua API xuyên vùng · – không dùng

| Module | Chức năng | KN | HTX | CGH | SIM | MKG | NH |
|---|---|---|---|---|---|---|---|
| cap/masterdata | Danh mục, tham số có phiên bản và phê duyệt | ● | ● | ● | ● | ● | – |
| cap/workflow | Yêu cầu – duyệt; người lập khác người duyệt | ● | ● | ● | ● | ● | – |
| cap/forms | Biểu thu thập theo kỳ: phát hành, nhập, rà soát, chốt, đóng băng | ● | ● | ● | – | – | – |
| cap/export | Xuất Excel, PDF theo mẫu | ● | ● | ● | ● | ● | – |
| cap/analytics | Chỉ số, dashboard, định nghĩa chỉ số có phiên bản | ● | ● | ● | ● | ● | – |
| cap/import | Nhập Excel báo lỗi từng dòng | ● | ● | ● | ● | ● | – |
| cap/task | Nhiệm vụ, giao việc, SLA | ● | ● | ● | – | ● | – |
| cap/notify | Thông báo trong ứng dụng, email, SMS, Zalo | ● | ● | ● | ● | ● | ● |
| cap/evidence | Ảnh, tệp, GPS, mã hóa theo hộ | ● | ● | – | ● | ● | – |
| cap/content | Bài viết, tin, công bố có duyệt | ● | ● | – | – | – | ● |
| cap/price | Chuỗi giá, ngưỡng, công bố | ● | ○ | – | – | ● | ● |
| cap/geo | Hình học, lớp bản đồ, tile | ● | ● | ● | ● | ● | – |
| cap/sync | Đồng bộ ngoại tuyến | ● | ● | – | – | ● | – |
| dom/extension | Đầu mối, lịch công tác, đào tạo, danh bạ trực | ● | – | – | – | – | – |
| dom/cooperative | HTX, nông hộ, xã viên, thửa, vụ, nhật ký canh tác | ○ | ● | ○ | – | ○ | ● |
| dom/mechanization | Máy, chủ máy, định mức, cân đối, sàn dịch vụ | ○ | ● | ● | – | – | – |
| dom/hubsim, dom/waterway | Mô phỏng chọn hub, đồ thị đường thủy có phiên bản | – | – | – | ● | ○ | – |
| dom/mrv, dom/metering | Xuất package MRV; sổ đo lường sử dụng | – | – | – | – | ○ | – |
| mkg/* | Vận hành, cân, hub, vận tải, đơn hàng, giá thành, cầu nối kế toán | – | – | – | – | ● | – |

Mười ba module năng lực, mỗi module có ít nhất ba ứng dụng dùng — đạt quy tắc ba (R-MOD-09). Đào tạo và danh bạ trực **cố ý** ở lại `dom/extension` vì chỉ một ứng dụng dùng.

## 6. Stack

| Tầng | Chọn | Ghi chú |
|---|---|---|
| Ngôn ngữ dịch vụ | **Go 1.23+**, Gin | Một khối triển khai chia module; module gọi nhau trong tiến trình |
| Tối ưu hóa | **Python 3.12**, OR-Tools, HiGHS/Pyomo — dịch vụ riêng | Ngoại lệ bắt buộc |
| Agent biên | **Go**, nhị phân tĩnh | Ngoại lệ bắt buộc |
| Phần mở rộng Keycloak | **Java** — ba SPI: IdP Zalo, OTP SMS, đẩy sự kiện | Ngoại lệ bắt buộc, cô lập ở `keycloak-ext/` |
| Cơ sở dữ liệu | **PostgreSQL 17+**, PostGIS, pgRouting, pgcrypto, btree_gist; **TimescaleDB chỉ ở cụm `mkg-ts`** | Mục 21 về TimescaleDB |
| Gộp kết nối | PgBouncer transaction mode, **một pool mỗi đơn vị triển khai** | Mục 14 |
| Hàng đợi | RabbitMQ quorum, DLQ, publisher confirm | |
| Bộ đệm | Valkey — hai instance: nghiệp vụ, và **định danh `noeviction`** | SAD R-ID-31 |
| Định danh | Keycloak 26.x + dịch vụ Access | SAD định danh v1.1 |
| Kho đối tượng | Tương thích S3, trong lãnh thổ | RB-7 |
| Đồng bộ ngoại tuyến | Engine đồng bộ tự vận hành trong lãnh thổ | Chờ câu hỏi C-1 |
| Định tuyến đường bộ | OSRM tự vận hành | Khả năng tái hiện |
| Web | Next.js, TypeScript; mỗi ứng dụng một BFF | |
| App hiện trường | Flutter — chỉ app hiện trường | |
| Kênh nông hộ | Zalo Mini App | |
| Điều phối | Compose (ĐTK 1) → Kubernetes được quản trị trong lãnh thổ (từ G4) | |
| Lõi tối ưu dùng chung | `lib/optcore` — gói Python: mô hình mạng lưới, hàm chi phí, mô hình cung, bộ giải | Dùng bởi `dom/hubsim` (nền tảng) và engine nội bộ MKG; **thư viện, không phải module có dữ liệu** |
| Bộ giải | HiGHS (có); **OR-Tools — chờ ADR theo quy trình giấy phép** | Định tuyến ghe, lập lịch đội |
| Cổng tích hợp | Dịch vụ Go riêng `cmd/integration-gateway`; feed theo cursor, webhook mỏng, nạp theo lô, ảnh chụp lớn qua kho đối tượng | Phần VI |
| Quan sát | OpenTelemetry, Prometheus, Loki, Tempo, Grafana — trong lãnh thổ | |

---

# PHẦN II — HỢP ĐỒNG MODULE VÀ QUY ƯỚC

## 7. Hợp đồng module

### 7.1 · Tệp khai báo module

```yaml
# internal/cap/forms/module.yaml
module: cap/forms
version: 1.4.0
kind: capability                        # kernel | capability | domain
zones: [platform, internal]             # mã dùng chung; dữ liệu tách theo vùng — R-MOD-10
depends:
  k/access:     ">=1.2 <2"
  k/orgtree:    ">=1.0 <2"
  k/ledger:     ">=1.0 <2"
  cap/workflow: ">=1.0 <2"
  cap/export:   ">=1.0 <2"
provides:
  ports:  [FormsAPI/v1]
  events: [cap.forms.period.published/v1, cap.forms.submission.submitted/v1,
           cap.forms.period.closed/v1]
  publications: [cap_forms_pub.submission_v/v1]   # view cho cap/analytics — R-DB-10
consumes:
  events: [k.orgtree.node.moved/v1]
permissions:
  - {code: forms.template.write,   class: ADMIN}
  - {code: forms.period.publish,   class: PUBLISH}
  - {code: forms.submission.write, class: WRITE}
  - {code: forms.period.close,     class: CLOSE}
  - {code: forms.period.reopen,    class: CLOSE}
extension_points:
  - forms.field_types
  - forms.close.validators
shares: [submission]                    # bảng chấp nhận đọc chéo đơn vị qua share_grant
config_schema: config/forms.schema.json
owns_schema: cap_forms
enable: per_tenant
```

### 7.2 · Tệp khai báo ứng dụng

```yaml
# app/KN/app.yaml
app: KN
modules:
  - k/*
  - cap/masterdata
  - cap/workflow
  - cap/forms:     {hierarchy: [TW, TINH, XA], publish_by: KN_TINH, close_by: KN_TINH}
  - cap/export:    {templates: templates/bo-nnmt/*}
  - cap/analytics
  - cap/import
  - cap/task
  - cap/notify
  - cap/evidence
  - cap/content:   {publish_requires: KN_PUBLISHER}
  - cap/price
  - cap/geo
  - cap/sync
  - dom/extension
  - dom/cooperative: {access: shared-read, purpose: khuyen-nong}
roles: seed/roles/kn.yaml
landing: {KN_ADMIN: /tong-quan-he-thong, KN_TW: /dashboard, KN_TINH: /dashboard, KN_XA: /cong-viec-can-lam}
```

### 7.3 · Quy tắc module

> **R-MOD-01.** Module sở hữu dữ liệu của nó: một schema, một vai DB trong mỗi vùng. Không module nào đọc hay ghi bảng của module khác, **trừ quan hệ công bố của lõi** (R-DB-05).

> **R-MOD-02.** Gọi module khác chỉ qua port đã khai hoặc qua sự kiện. Cùng vùng: trong tiến trình. Khác vùng: API.

> **R-MOD-03.** Không kế thừa, không sửa model của module khác. Mở rộng chỉ qua điểm mở rộng đã khai.

> **R-MOD-04.** Module năng lực không biết tên ứng dụng. Cấm `if app == "KN"` trong `cap/*`; khác biệt nằm ở cấu hình theo `config_schema`.

> **R-MOD-05.** Phụ thuộc là đồ thị không chu trình, chỉ đi xuống: lõi ← năng lực ← nghiệp vụ ← ứng dụng. CI đọc mọi `module.yaml`, hỏng build khi có chu trình hoặc phụ thuộc đi lên.

> **R-MOD-06.** Port có phiên bản ngữ nghĩa. Thay đổi phá vỡ → port `v2` chạy song song tối thiểu một kỳ phát hành. Kiểm thử hợp đồng của mọi module tiêu thụ chạy trong CI của module cung cấp.

> **R-MOD-07.** Quyền do module khai (mã + lớp); vai trò do ứng dụng gom. Thuộc tính đặc quyền của vai trò tính ra từ lớp quyền — SAD R-ID-04.

> **R-MOD-08.** Bật, tắt theo đơn vị tham gia. Module chưa bật → port trả `module_not_enabled`, quyền liên quan không cấp được, menu không hiện.

> **R-MOD-09.** Quy tắc ba: một chức năng chỉ thành module năng lực khi có hai ứng dụng thật đang dùng và ứng dụng thứ ba đã có lịch.

> **R-MOD-10.** Mã dùng chung, dữ liệu không dùng chung giữa hai vùng chủ quyền. Một module chạy ở vùng nền tảng và vùng nội bộ là **hai thể hiện dữ liệu**, hai schema ở hai cụm.

### 7.4 · Hiện thực trong Go

- Module đăng ký vào registry **lúc biên dịch**; `cmd/*` ghép tập module theo `app.yaml` và `zones`. **Không dùng gói `plugin` của Go.**
- Bản lite bán cho khách hàng là **cấu hình build**, không phải nhánh mã.
- Ranh giới: `depguard` cấm import `domain/`, `app/`, `store/` của module khác; kiểm đồ thị từ `module.yaml`.
- **Ranh giới module chống nhầm lẫn, không chống mã độc.** Các module cùng tiến trình tin nhau ở mức mã máy; ranh giới ở DB (R-DB-03) là lớp chặn thứ hai cho lỗi lập trình, không phải hàng rào an ninh giữa các module.

## 8. Cấu trúc repo

```
agrigreen/
├── cmd/
│   ├── api-platform/   api-internal/   worker/   outbox-publisher/
│   ├── access/         identity-vault/ ingest/   sync/   edge-agent/
├── internal/
│   ├── k/              access/ orgtree/ ref/ ledger/ jobs/ audit/ uishell/
│   ├── cap/            masterdata/ workflow/ forms/ export/ analytics/ import/
│   │                   task/ notify/ evidence/ content/ price/ geo/ sync/
│   ├── dom/            extension/ cooperative/ mechanization/ hubsim/
│   │                   waterway/ mrv/ metering/
│   ├── mkg/            fieldops/ measurement/ hub/ logistics/ orders/
│   │                   costing/ acctbridge/
│   └── shared/         money/ mass/ clock/ problem/ telemetry/  ← thư viện, không nghiệp vụ
├── app/                KN/ HTX/ CGH/ SIM/ MKG/ NH/ PF/   ← app.yaml, seed vai trò, cấu hình
├── web/                ui-shell/ + giao diện theo ứng dụng
├── mobile/field-app/
├── keycloak-ext/       ← 3 SPI Java
├── optimizer/          ← Python
├── contracts/          openapi/ events/ publication/
├── deploy/             compose/ helm/ tofu/ policies/
└── docs/adr/
```

**Cấu trúc một module:**

```
internal/cap/forms/
├── module.yaml
├── port/            interface + DTO công khai — thứ duy nhất module khác import
├── domain/          thực thể, quy tắc; không phụ thuộc hạ tầng
├── app/             use case, giao dịch
├── store/           truy cập DB — chỉ qua vai DB của module này
├── handler/         HTTP, consumer
├── migrations/      lịch sử migration riêng của schema cap_forms
├── pub/             định nghĩa view công bố cho phân tích
├── config/          forms.schema.json
└── contract_test/   kiểm thử mà module tiêu thụ cung cấp
```

## 9. Quy ước mã

> **R-CODE-01.** Tiền, khối lượng dùng decimal, không bao giờ float — `shared/money`, `shared/mass`. Linter cấm `float32/float64` trong `mkg/*`, `dom/mechanization`, `dom/metering`, `cap/price`.

> **R-CODE-02.** Định danh là UUIDv7, sinh ở nơi tạo — kể cả trên thiết bị ngoại tuyến.

> **R-CODE-03.** Thời gian lưu `timestamptz` UTC; ngày nghiệp vụ theo `Asia/Ho_Chi_Minh` qua `shared/clock`. Cấm `time.Now()` trong `domain/`.

> **R-CODE-04.** Hai mốc thời gian: `event_time` — lúc xảy ra tại hiện trường; `recorded_at` — lúc hệ ghi nhận. Truy vấn nghiệp vụ dùng `event_time`; chỉ việc tính lại chỉ số quá khứ dùng `recorded_at`.

> **R-CODE-05.** Không ghi dữ liệu cá nhân vào log, trace, metric, thông báo lỗi. Linter cấm log trường có tag `pii:"true"`.

> **R-CODE-06.** Lỗi trả theo RFC 9457 `application/problem+json`; mã lỗi nghiệp vụ ổn định.

> **R-CODE-07.** Mọi truy cập DB đi qua `WithContextTx` (mục 14). Truy vấn ngoài giao dịch chạy với vai đăng nhập không có quyền gì — nên thất bại, đó là chủ ý.

## 10. Giao diện

| Bề mặt | Hostname | Xác thực | Ghi chú |
|---|---|---|---|
| Application | `api.<domain>` | OIDC + token ngữ cảnh của Access | Theo vai trò người dùng |
| Admin | `admin.<domain>` | OIDC LoA 2 + token biên | Dữ liệu chủ, vai trò, phê duyệt, nhật ký |
| Public (đối tác) | `partner.<domain>` | Khóa API do `k/access` quản lý | **Tắt ở ĐTK 1**; mọi gọi ghi vào `dom/metering` |
| Internal | trong cụm | mTLS | Worker, ingest, vault, access |
| Vận hành | cổng riêng | VPN + MFA | Grafana, Argo CD, bastion |

> **R-API-01.** Đường dẫn có phiên bản `/v1/...`; phá vỡ → `/v2/...` song song một kỳ.

> **R-API-02.** Mọi POST tạo tài nguyên bắt buộc `Idempotency-Key`; lưu 48 giờ theo `(tenant, key)`, trả lại đúng phản hồi lần đầu.

> **R-API-03.** Việc quá 2 giây → `202 Accepted` + `Location: /v1/jobs/{id}`.

> **R-API-04.** Phân trang bằng cursor.

> **R-API-05.** Tenant và ngữ cảnh lấy từ token, không bao giờ từ path, query, body.

> **R-API-06.** Backend tự xác minh mọi token theo SAD R-ID-30; phân quyền nghiệp vụ ở backend, không ở gateway.

> **R-API-07.** Đường dẫn API của một module nằm dưới tiền tố của module: `/v1/forms/...`, `/v1/coop/...`. Ứng dụng không có đường dẫn riêng cho chức năng của module — App Khuyến nông và App HTX gọi cùng `/v1/forms/...`, ngữ cảnh khác nhau.

## 11. Sự kiện và luồng bất đồng bộ

**Mẫu bắt buộc cho mọi việc dài** — mô phỏng, sinh báo cáo, xuất package, chốt kỳ, gọi dịch vụ ngoài:

| Bước | Lỗi nó chặn |
|---|---|
| Nhận yêu cầu kèm `Idempotency-Key` | Thử lại tạo hai việc |
| Ảnh chụp đầu vào + bản ghi việc + nhật ký + outbox **trong một giao dịch** | Việc có mà sự kiện không phát, hoặc ngược lại |
| Trả 202 kèm mã việc | Giữ kết nối hàng giờ |
| Publisher đọc `FOR UPDATE SKIP LOCKED` | Nhiều publisher tranh một sự kiện |
| **Confirm rồi mới đánh dấu đã phát** | Đánh dấu đã phát cho sự kiện chưa vào broker |
| Worker kiểm `inbox(event_id, consumer)` | Xử lý hai lần |
| Nạp ảnh chụp bất biến | Kết quả đổi vì dữ liệu nền đổi |
| Kết quả + inbox + nhật ký + outbox kết quả trong một giao dịch | Chuỗi sự kiện đứt |
| **ACK chỉ sau commit** | Mất việc khi worker chết |
| Trả `trace_id` | Không lần được sự cố |

**Phong bì:**

```json
{ "event_id": "0191f2c4-…", "event_type": "cap.forms.period.closed",
  "schema_version": 1, "module": "cap/forms", "zone": "platform",
  "tenant_id": "…", "occurred_at": "…", "recorded_at": "…",
  "trace_id": "…", "payload": { } }
```

> **R-EVT-01.** Tên sự kiện `<tầng>.<module>.<thực thể>.<động từ quá khứ>`. Schema trong `/contracts/events/`; CI kiểm tương thích ngược.

> **R-EVT-02.** Handler idempotent theo `event_id`. Kiểm thử: giao một sự kiện hai lần → kết quả như một lần.

> **R-EVT-03.** Hàng đợi có DLQ; sự kiện vào DLQ sinh cảnh báo, không tự xóa.

> **R-EVT-04.** **Hàng đợi không phải sổ.** Sự kiện nghiệp vụ được ghi sổ theo RB-1 ở bước nghiệp vụ; outbox là bảng vận hành.

> **R-EVT-05.** Sự kiện không bao giờ vượt ranh giới vùng chủ quyền qua broker. Xuyên vùng chỉ qua API hoặc van công bố.

---

# PHẦN III — THIẾT KẾ DỮ LIỆU

## 12. Rà soát lại thiết kế database

Đối tượng rà soát: mục 8–9 của kiến trúc v3.0, mục 6–7 của SAD định danh v1.1, và các hệ quả của mô hình module. **14 phát hiện; 2 nghiêm trọng, 7 cao.** Mười hai phát hiện từ đọc thiết kế; **hai phát hiện (DB-13, DB-14) chỉ lộ ra khi chạy DDL trên PostgreSQL thật** — mục 12.1. Tất cả đã sửa trong các mục 13–24.

| Mã | Mức | Phát hiện | Sửa ở |
|---|---|---|---|
| **DB-01** | **Nghiêm trọng** | **DDL mẫu bảng sổ không chạy được.** v3.0 mục 8.1 và SAD 7.1 khai `PRIMARY KEY (id)`, `UNIQUE (tenant_id, idem_key)`, `UNIQUE (stream_key, seq)` trên bảng `PARTITION BY RANGE (recorded_at)`. PostgreSQL bắt buộc mọi ràng buộc khóa chính và duy nhất trên bảng phân vùng **chứa cột phân vùng** — câu lệnh tạo bảng sẽ báo lỗi. Mọi bảng sổ của mọi module sinh từ mẫu này đều lỗi | Mục 15 |
| **DB-02** | **Nghiêm trọng** | **RLS không đọc được bảng ở cụm khác.** Chính sách phạm vi cần cây Nút (closure) và cấp phép chia sẻ; SAD đặt nguồn của chúng ở cụm định danh. Chính sách RLS ở cụm `pf` không thể truy vấn sang cụm `idp` | Mục 17.1 — `k/ref` |
| DB-03 | Cao | Hàm chuỗi băm tìm bản ghi cuối bằng `ORDER BY seq DESC LIMIT 1` trên bảng phân vùng **không có điều kiện thời gian** → quét chỉ mục ở **mọi** phân vùng; chậm tuyến tính theo số tháng vận hành | Mục 15.2 — `chain_head` |
| DB-04 | Cao | Một vai DB mỗi module × PgBouncer tạo pool theo cặp vai–DB. Với khoảng 25 module, hai vùng, pool 5 kết nối mỗi cặp → **khoảng 250 kết nối máy chủ**, vượt `max_connections` của một instance cỡ vừa | Mục 14 |
| DB-05 | Cao | Mâu thuẫn quy tắc: RLS của mọi module đọc bảng của `k/orgtree`, `k/access`, trong khi R-MOD-01 cấm đọc bảng module khác | Mục 17.1 — quan hệ công bố của lõi |
| DB-06 | Cao | **Đơn vị hành chính không có hiệu lực theo thời gian.** Sắp xếp đơn vị hành chính năm 2025 — bỏ cấp huyện, hợp nhất tỉnh — làm dữ liệu các vụ trước tham chiếu đơn vị không còn. Không có ràng buộc duy nhất theo mã chính thức, nên mới có **6 bản ghi "An Giang"** ở UAT (CROSS-01) | Mục 19 |
| DB-07 | Cao | Hợp nhất dữ liệu: App Khuyến nông, App HTX, CGH mỗi bên một bảng HTX, nông hộ, thửa | Mục 27 — `dom/cooperative` |
| DB-08 | Trung bình | Module năng lực tham chiếu thực thể nghiệp vụ không có quy ước — mỗi module tự làm, không kiểm được | Mục 18 |
| DB-09 | Trung bình | Phân tích đọc thẳng bảng nội bộ của module trên bản sao → phá ranh giới; đổi bảng là vỡ dashboard | Mục 22 |
| DB-10 | Trung bình | Tổng hợp toàn nền tảng — chỉ số tài sản dữ liệu, van lọc k — phải đọc vượt RLS; v3.0 không nêu cơ chế | Mục 22.2 — vai `r_agg` |
| DB-11 | Trung bình | Khóa idempotency duy nhất vĩnh viễn trên sổ → chỉ mục phình theo toàn bộ lịch sử, trong khi chỉ cần chống trùng trong vài ngày | Mục 15.3 |
| DB-12 | Thấp | Một kho migration chung, không có thứ tự theo phụ thuộc module | Mục 23 |
| **DB-13** | Cao | **"Thiếu biến ngữ cảnh → NULL → chính sách trả rỗng" sai trên kết nối tái sử dụng.** v3.0 R-31 và SAD định danh tuyên bố như vậy. Thực tế: sau khi biến đã từng được đặt trong phiên — luôn xảy ra khi PgBouncer tái sử dụng kết nối — `current_setting(k, true)` trả **chuỗi rỗng**, và phép ép `''::uuid` **ném lỗi**. Không lộ dữ liệu, nhưng hành vi khác thiết kế, và là chỗ dễ bị "sửa" sai | Mục 17.2 — `k_ref.ctx_uuid()` |
| **DB-14** | Cao | **Hàm phạm vi cây Nút không được inline trong nhánh chia sẻ.** Kế hoạch truy vấn cho thấy `k_ref.in_subtree()` bị gọi theo từng cặp (hàng × cấp phép) bên trong SubPlan tương quan — đúng trần hiệu năng đã cảnh báo, và tệ hơn dự kiến | Mục 17.3 — `k_ref.share_scope` |

### 12.1 · Kết quả chạy thử

Toàn bộ DDL của Phần III được chạy trên **PostgreSQL 16.15** — các tính năng dùng ở đây (bảng phân vùng có trigger hàng, RLS, `SET LOCAL ROLE`, `EXCLUDE` với btree_gist, pgcrypto) đều có trước bản 17. Riêng cơ chế failover slot của bản 17 (mục 36) không thuộc phép thử này.

| # | Phép thử | Kết quả |
|---|---|---|
| T0 | Chạy nguyên văn DDL bảng sổ của v3.0 | **Lỗi:** `unique constraint on partitioned table must include all partitioning columns` — xác nhận DB-01 |
| T1 | Ghi hai bản ghi cùng chuỗi qua vai module (`SET LOCAL ROLE`) | `seq` = 1, 2; `prev_hash` của bản ghi 2 bằng `row_hash` của bản ghi 1; bản ghi 1 nối từ `\x00`; `chain_head.last_seq` = 2 — **đạt** |
| T2 | `UPDATE` bảng sổ bằng vai module | `permission denied` — **đạt** |
| T3 | `UPDATE`, `DELETE`, `TRUNCATE` bằng owner | Trigger chặn cả ba — **đạt** |
| T4 | Vai đăng nhập truy vấn ngoài `SET LOCAL ROLE` | `permission denied` — **đạt**, mặc định chặn |
| T5 | Vai module A đọc bảng của module B | `permission denied` — **đạt** |
| T6a | Đơn vị chủ đọc dữ liệu của mình | 3/3 — **đạt** |
| T6b | Đơn vị xem đọc qua cấp phép phạm vi một tỉnh | Đúng hai hộ trong tỉnh, không thấy hộ ngoài phạm vi — **đạt** |
| T6c | Đơn vị xem với mục đích không khớp cấp phép | 0 hàng — **đạt** |
| T6d | Kết nối đã từng đặt biến, nay thiếu ngữ cảnh | **Lỗi ép kiểu thay vì 0 hàng** → DB-13; sau sửa: 0 hàng, không lỗi — **đạt** |
| T6e | Đơn vị xem ghi vào dữ liệu đơn vị chủ qua cấp phép | `new row violates row-level security policy` — **đạt** |
| T7 | Hai phiên bản liên tiếp cùng mã đơn vị hành chính; rồi một bản ghi chồng thời gian hiệu lực | Hai phiên bản liên tiếp hợp lệ; bản ghi chồng bị `EXCLUDE` chặn — **đạt**, chặn đúng kịch bản CROSS-01 |
| T8 | Nhánh chia sẻ với `in_subtree()` — xem kế hoạch truy vấn | Hàm **không được inline**, gọi theo từng cặp → DB-14 |
| T9 | Nhánh chia sẻ với `share_scope`, 200.000 hộ | `hashed SubPlan`, chạy **một lần** (`loops=1`); đếm 200.000 hộ khoảng 290 ms trên máy thử |
| T10 | Tính đúng của `share_scope` với phạm vi một nửa số Nút | Qua RLS **100.499** hộ = đếm đối chứng **100.499** hộ — **đạt** |
| T11 | Phạm vi cây Nút trong đơn vị, điều kiện viết thẳng vào chính sách, 200.000 thửa | `hashed SubPlan` `loops=1`; **100.499** = đối chứng **100.499** — **đạt**. Thay hàm `in_subtree()` |
| T12 | Trích **mọi khối SQL trong chính tài liệu này** và chạy theo thứ tự trên cụm mới | **9/9 khối chạy thành công** trên cụm PostgreSQL mới, chỉ thêm phần dựng sẵn mà tài liệu mô tả bằng lời (extension, schema, vai). Lần chạy đầu bắt thêm hai lỗi của chính bản nháp — một khối chỉ là mảnh cột, và mâu thuẫn giữa khuyến nghị `in_subtree()` với R-DB-08 — cả hai đã sửa trước khi phát hành |

Con số thời gian ở T9 là trên máy thử, dữ liệu giả lập, không phải cam kết hiệu năng. Giá trị của nó là **dạng kế hoạch truy vấn** — một lần dựng bảng băm thay vì một lần gọi hàm mỗi hàng — và dạng đó giữ nguyên khi tăng khối lượng.

**Về DB-01:** lỗi này không lộ ra trong review tài liệu vì không ai chạy DDL mẫu. Nó sẽ lộ ra ở ngày đầu viết migration, và nếu đội "sửa" bằng cách bỏ phân vùng, thì lựa chọn đó có hệ quả hiệu năng dài hạn mà không ai quyết. **Kiểm thử bắt buộc mới:** mọi DDL mẫu trong tài liệu kiến trúc được chạy trên PostgreSQL thật trong CI của kho tài liệu (R-OPS-04).

## 13. Tổ chức cơ sở dữ liệu

| Cụm | Vùng | Database | Schema | ĐTK 1 |
|---|---|---|---|---|
| `idp` | Định danh | `keycloak` | Keycloak tự quản | Một instance nhỏ, HA từ G4 |
| | | `access` | `k_access`, `k_orgtree` — **nguồn sự thật** | cùng instance |
| `pf` | Nền tảng | `agrigreen` | `k_ref`, `k_ledger`, `k_jobs`, `cap_*`, `dom_*` | Một instance |
| `mkg` | Nội bộ | `mkg` | `k_ref`, `k_ledger`, `k_jobs`, `cap_*` (thể hiện riêng), `mkg_*` | Chưa cần |
| `mkg-ts` | Nội bộ | `telemetry` | `mkg_measurement_ts` (hypertable) | Chưa cần |
| `vault` | Định danh hộ | `vault` | `vault` | **Instance riêng** |

> **R-DB-01.** Tên schema `<tầng>_<module>`: `k_access`, `cap_forms`, `dom_coop`, `mkg_measurement`. Mỗi module một schema **trong mỗi vùng nó chạy**. Schema `<tầng>_<module>_pub` chứa view công bố cho phân tích.

**Vì sao `vault` riêng từ ĐTK 1:** đây là kho duy nhất xóa được và chứa định danh hộ — nó cần chính sách sao lưu, khóa và quyền truy cập khác mọi cụm khác. Chi phí một instance nhỏ thấp hơn chi phí tách sau khi đã có dữ liệu thật.

## 14. Vai DB và kết nối

**Ba vai mỗi module mỗi vùng:**

| Vai | Loại | Quyền |
|---|---|---|
| `own_<module>` | NOLOGIN | Sở hữu schema; chỉ pipeline migration dùng |
| `r_<module>` | NOLOGIN | Quyền chạy: `SELECT, INSERT` trên bảng sổ; `SELECT, INSERT, UPDATE` trên bảng chiếu và bảng vận hành; `SELECT` trên quan hệ công bố của lõi; `EXECUTE` trên hàm `k_ledger` |
| `r_<module>_pub` | NOLOGIN | `SELECT` trên schema `_pub` — cấp cho `r_cap_analytics` |

**Một vai đăng nhập mỗi đơn vị triển khai**, không phải mỗi module:

```sql
CREATE ROLE app_platform LOGIN NOINHERIT;     -- tự nó không có quyền gì
GRANT r_cap_forms, r_dom_coop, r_cap_workflow /* … */ TO app_platform;
```

Mỗi giao dịch, `WithContextTx` đặt vai của module đang gọi:

```go
func WithContextTx(ctx context.Context, pool *pgxpool.Pool, m ModuleID, c *Context,
    fn func(pgx.Tx) error) error {
    return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
        if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{m.DBRole()}.Sanitize()); err != nil {
            return err
        }
        // set_config(..., true) = LOCAL: tenant, node, dept, xa_ids, purpose,
        // actor_ref, on_behalf_of_ref — theo SAD 6.1
        return fn(tx)
    })
}
```

> **R-DB-02.** Vai đăng nhập là `NOINHERIT`; mọi quyền có được chỉ sau `SET LOCAL ROLE` trong giao dịch. `SET LOCAL` hết hiệu lực khi giao dịch kết thúc, nên tương thích PgBouncer transaction mode.

> **R-DB-03.** Vai của module do registry gán lúc biên dịch; mã nghiệp vụ không truyền tên vai. Linter cấm chuỗi `SET ROLE` ngoài `k/jobs` và `shared`.

**Số kết nối, ĐTK 2:** cụm `pf` có bốn vai đăng nhập — `app_platform`, `app_worker`, `app_sync`, `app_migrator` — pool tối đa 20 mỗi vai → **tối đa 80 kết nối máy chủ**, so với khoảng 250 nếu mỗi module một pool (DB-04).

## 15. Bảng sổ — mẫu đã sửa

### 15.1 · DDL

Mẫu cho mọi bảng sổ. Ví dụ là số cuộn — dữ liệu làm căn cứ thanh toán HTX.

```sql
CREATE TABLE mkg_fieldops.roll_count (
  id                uuid        NOT NULL,                 -- UUIDv7
  recorded_at       timestamptz NOT NULL DEFAULT now(),   -- cột phân vùng
  tenant_id         uuid        NOT NULL,
  stream_key        text        NOT NULL,                 -- 'lot:<uuid>'
  seq               bigint      NOT NULL,                 -- gán bởi trigger
  plot_ref          uuid        NOT NULL,
  htx_ref           uuid        NOT NULL,
  rolls             integer     NOT NULL CHECK (rolls >= 0),
  htx_confirmation_ref uuid     NULL,                     -- xác nhận của đại diện HTX
  event_time        timestamptz NOT NULL,
  supersedes_id     uuid        NULL,
  correction_reason text        NULL,
  actor_ref         uuid        NOT NULL,
  on_behalf_of_ref  uuid        NOT NULL,
  device_id         text        NULL,
  idem_key          text        NOT NULL,
  rule_version      text        NOT NULL,
  prev_hash         bytea       NOT NULL,
  row_hash          bytea       NOT NULL,
  PRIMARY KEY (id, recorded_at),                          -- DB-01: có cột phân vùng
  CHECK ((supersedes_id IS NULL) = (correction_reason IS NULL))
) PARTITION BY RANGE (recorded_at);

CREATE INDEX ON mkg_fieldops.roll_count (tenant_id, stream_key, seq);
CREATE INDEX ON mkg_fieldops.roll_count (tenant_id, plot_ref, event_time);
```

**Hệ quả của khóa chính `(id, recorded_at)`:** tra một bản ghi theo `id` mà không biết `recorded_at` thì quét mọi phân vùng. Chấp nhận được vì truy vấn nghiệp vụ đi theo `stream_key`, `plot_ref`, `event_time` — có chỉ mục. Tra theo `id` đơn lẻ dùng bảng chiếu (mục 16).

**Tính duy nhất của `id`** dựa vào UUIDv7 (xác suất trùng không đáng kể) cộng bảng idempotency (15.3). **Tính liên tục của `seq`** dựa vào khóa hàng của `chain_head` (15.2) và được `verify_chain` kiểm hằng ngày — phát hiện cả trùng lẫn thiếu.

### 15.2 · Chuỗi băm với `chain_head`

```sql
CREATE TABLE mkg_fieldops.chain_head (          -- bảng vận hành, được UPDATE
  tenant_id  uuid   NOT NULL,
  stream_key text   NOT NULL,
  last_seq   bigint NOT NULL,
  last_hash  bytea  NOT NULL,
  PRIMARY KEY (tenant_id, stream_key)
);

CREATE FUNCTION k_ledger.fn_chain() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s bigint; h bytea; head text := format('%I.chain_head', TG_TABLE_SCHEMA);
BEGIN
  EXECUTE format('INSERT INTO %s (tenant_id, stream_key, last_seq, last_hash)
                  VALUES ($1, $2, 0, ''\x00'') ON CONFLICT DO NOTHING', head)
    USING NEW.tenant_id, NEW.stream_key;
  EXECUTE format('SELECT last_seq, last_hash FROM %s
                  WHERE tenant_id = $1 AND stream_key = $2 FOR UPDATE', head)
    INTO s, h USING NEW.tenant_id, NEW.stream_key;
  NEW.seq       := s + 1;
  NEW.prev_hash := h;
  NEW.row_hash  := digest(h || k_ledger.canonical(TG_TABLE_SCHEMA, TG_TABLE_NAME, to_jsonb(NEW)), 'sha256');
  EXECUTE format('UPDATE %s SET last_seq = $3, last_hash = $4
                  WHERE tenant_id = $1 AND stream_key = $2', head)
    USING NEW.tenant_id, NEW.stream_key, NEW.seq, NEW.row_hash;
  RETURN NEW;
END $$;
```

Một lần đọc theo khóa chính thay cho quét mọi phân vùng (DB-03). `FOR UPDATE` tuần tự hóa các bản ghi **của cùng một chuỗi** — các chuỗi khác nhau ghi song song. `k_ledger.canonical()` tuần tự hóa các cột nghiệp vụ theo thứ tự khai báo, loại `recorded_at`, `seq`, `prev_hash`, `row_hash`.

### 15.3 · Idempotency

```sql
CREATE TABLE mkg_fieldops.idempotency (          -- bảng vận hành, có thời hạn
  tenant_id   uuid        NOT NULL,
  idem_key    text        NOT NULL,
  event_id    uuid        NOT NULL,
  first_seen  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, idem_key)
);
```

Thời hạn lưu **30 ngày** — gấp mười lần cửa sổ ngoại tuyến 72 giờ. Xóa bảng này **không vi phạm RB-1**: nó là chỉ mục chống trùng, không chứa dữ liệu nghiệp vụ duy nhất; khóa vẫn còn trong cột `idem_key` của bảng sổ.

### 15.4 · Ba lớp bất biến

Giữ nguyên v3.0: (1) vai `r_<module>` không có `UPDATE`, `DELETE`, `TRUNCATE` trên bảng sổ; (2) trigger `k_ledger.fn_immutable()` chặn cả owner; (3) chuỗi băm + neo hằng ngày ra kho bất biến trong lãnh thổ.

> **R-DB-06.** Bảng sổ theo đúng mẫu 15.1–15.4: khóa chính chứa `recorded_at`, phân vùng theo tháng, `chain_head`, bảng idempotency riêng, ba lớp bất biến. View `k_ledger.v_missing_immutability` liệt kê bảng sổ thiếu trigger; CI hỏng nếu không rỗng.

## 16. Bảng chiếu và mức event sourcing

- Bảng chiếu `*_current` cập nhật **trong cùng giao dịch** với ghi sổ; được UPDATE; dựng lại được từ sổ bằng `make rebuild-projection`.
- **Mức event sourcing B:** sổ là nguồn **chỉ ở `mkg/measurement` và `mkg/costing`**. Module khác: bảng nghiệp vụ là nguồn, sổ đóng vai nhật ký thay đổi có chuỗi băm.
- Diễn tập dựng lại bảng chiếu ở G4 trên bản sao dữ liệu thật.

## 17. Cách ly theo đơn vị và chia sẻ

### 17.1 · `k/ref` — bản chiếu chỉ đọc của dữ liệu lõi trong mỗi cụm

Giải DB-02 và DB-05. Nguồn sự thật của cây tổ chức, đơn vị hành chính, cấp phép chia sẻ nằm ở cụm `idp`. **Mỗi cụm dữ liệu có schema `k_ref` là bản chiếu chỉ đọc**, do worker của `k/ref` cập nhật từ sự kiện `k.orgtree.*`, `k.access.share_grant.*`:

| Quan hệ trong `k_ref` | Nội dung |
|---|---|
| `org` | Đơn vị tham gia: mã, loại, trạng thái |
| `node_closure` | Cặp (tổ tiên, hậu duệ) của cây Nút |
| `admin_unit` | Đơn vị hành chính có hiệu lực theo thời gian — mục 19 |
| `share_grant_active` | Cấp phép chia sẻ đang hiệu lực |
| `share_scope` | **Phạm vi chia sẻ đã mở rộng**: (đơn vị xem, mục đích, bảng, đơn vị chủ, Nút) — worker `k/ref` dựng từ cấp phép × cây Nút. Chính sách RLS tra trực tiếp bảng này — DB-14 |
| Hàm `ctx_uuid`, `ctx_text` | Đọc biến ngữ cảnh, chuẩn hóa chuỗi rỗng thành NULL — DB-13 |

> **R-DB-05.** **Quan hệ công bố của lõi** là ngoại lệ duy nhất của R-MOD-01: chỉ module `k/*` được công bố quan hệ; danh sách nằm trong `module.yaml` của lõi; mọi vai `r_*` chỉ có `SELECT`; chỉ worker `k/ref` được ghi.

**Độ trễ có giới hạn, và phải nói thẳng:** thu hồi một cấp phép chia sẻ ở cụm `idp` tới `k_ref` của cụm `pf` sau một khoảng trễ đồng bộ. Mục tiêu **≤ 5 giây ở p99**, giám sát bằng chỉ số `k_ref_lag_seconds` có cảnh báo. Trong khoảng đó, đơn vị vừa bị thu hồi cấp phép còn đọc được. Rút ngắn thêm: thu hồi cấp phép **đồng thời tăng epoch ngữ cảnh** của mọi tài khoản thuộc đơn vị xem (SAD R-ID-07), nên phiên của họ phải làm mới ngữ cảnh ở yêu cầu kế tiếp, và việc làm mới chờ `k_ref` bắt kịp.

### 17.2 · Chính sách phạm vi

**Đọc biến ngữ cảnh — bắt buộc qua hàm chuẩn hóa (DB-13):**

```sql
CREATE FUNCTION k_ref.ctx_uuid(k text) RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$ SELECT NULLIF(current_setting(k, true), '')::uuid $$;
CREATE FUNCTION k_ref.ctx_text(k text) RETURNS text
LANGUAGE sql STABLE PARALLEL SAFE AS $$ SELECT NULLIF(current_setting(k, true), '') $$;
```

Kế hoạch truy vấn xác nhận hai hàm này **được inline** thành biểu thức `NULLIF(current_setting(...))`, không tốn lời gọi hàm.

**Phạm vi cây Nút cho dữ liệu của chính đơn vị** (Cấp Tỉnh xem các xã trong tỉnh):

```sql
CREATE POLICY p_scope ON dom_coop.plot
  USING (
        tenant_id = k_ref.ctx_uuid('app.tenant_id')
    AND EXISTS (SELECT 1 FROM k_ref.node_closure c
                WHERE c.ancestor   = k_ref.ctx_uuid('app.node_id')
                  AND c.descendant = plot.node_id)
  )
  WITH CHECK (tenant_id = k_ref.ctx_uuid('app.tenant_id'));
```

**Viết thẳng điều kiện vào chính sách, không bọc trong hàm.** Bản nháp đầu dùng hàm `in_subtree()`; kế hoạch truy vấn cho thấy hàm có truy vấn con không được inline trong chính sách (DB-14). Viết thẳng thì planner dựng một bảng băm các Nút trong phạm vi **một lần** cho cả truy vấn — phép thử T11: 200.000 thửa, `hashed SubPlan` với `loops=1`, đúng 100.499 = đối chứng 100.499.

**Vì sao vai module được `SELECT` trực tiếp `k_ref.node_closure`** thay vì giấu sau một hàm `SECURITY DEFINER`: hàm như vậy không được planner inline, bị gọi theo từng hàng. Bảng closure chỉ chứa cấu trúc cây, không chứa dữ liệu nghiệp vụ, nên cho đọc trực tiếp là đánh đổi chấp nhận được.

### 17.3 · Đọc tại chỗ có cấp phép

```sql
CREATE TABLE k_ref.share_scope (                -- worker k/ref dựng từ cấp phép × cây Nút
  viewer_tenant uuid NOT NULL, purpose text NOT NULL, module_table text NOT NULL,
  owner_tenant  uuid NOT NULL, node_id uuid NOT NULL,
  PRIMARY KEY (viewer_tenant, purpose, module_table, owner_tenant, node_id)
);

CREATE POLICY p_scope ON dom_coop.household
  USING (
        tenant_id = k_ref.ctx_uuid('app.tenant_id')
    OR  EXISTS (
          SELECT 1 FROM k_ref.share_scope s
          WHERE s.viewer_tenant = k_ref.ctx_uuid('app.tenant_id')
            AND s.purpose       = k_ref.ctx_text('app.purpose')
            AND s.module_table  = 'dom_coop.household'
            AND s.owner_tenant  = household.tenant_id
            AND s.node_id       = household.node_id)
  )
  WITH CHECK (tenant_id = k_ref.ctx_uuid('app.tenant_id'));
```

**Vì sao `share_scope` thay vì gọi `in_subtree()` trong chính sách (DB-14):** bản nháp đầu dùng `in_subtree()`; kế hoạch truy vấn cho thấy hàm đó bị gọi theo từng cặp hàng × cấp phép trong SubPlan tương quan. Với `share_scope`, điều kiện chỉ còn so sánh cột, planner dựng **một bảng băm một lần** cho cả truy vấn (phép thử T9, T10).

**Kích thước:** mỗi cấp phép sinh số dòng bằng số Nút trong phạm vi — một cấp phép phạm vi tỉnh khoảng vài trăm dòng. Vài nghìn cấp phép là vài trăm nghìn dòng, nhỏ so với bảng nghiệp vụ. **Cây Nút di chuyển hoặc cấp phép thay đổi → worker dựng lại các dòng liên quan trong một giao dịch**, nằm trong cùng giới hạn trễ ≤ 5 giây của mục 17.1.

> **R-DB-08.** `FORCE ROW LEVEL SECURITY` trên mọi bảng có `tenant_id`. Một chính sách mỗi bảng. **Đọc biến ngữ cảnh chỉ qua `k_ref.ctx_uuid()`, `k_ref.ctx_text()`** — cấm `current_setting(...)::uuid` trực tiếp (DB-13). **Chính sách không gọi hàm có truy vấn con** như `in_subtree()` — phạm vi chia sẻ tra qua `k_ref.share_scope` (DB-14). Nhánh `share_grant` **chỉ có** trên bảng mà module khai trong `shares` của `module.yaml`. `WITH CHECK` **không bao giờ** có nhánh chia sẻ: cấp phép chia sẻ là quyền đọc, không phải quyền ghi.

**Mọi lần đọc qua nhánh chia sẻ ghi nhật ký** ở tầng ứng dụng: port của module trả về cờ `via_share_grant` cho từng bản ghi, middleware ghi một sự kiện gộp mỗi yêu cầu.

**Trần hiệu năng còn lại:** phép thử T9 là 200.000 hộ trên máy thử. Bài đo bắt buộc trong hạng mục kiểm chứng vẫn giữ — dashboard Cấp Tỉnh ở khối lượng ĐTK 2, có và không có nhánh chia sẻ — nhưng nay là để **đặt ngưỡng vận hành**, không còn để quyết kiến trúc.

## 18. Tham chiếu chéo module

Module năng lực không biết thực thể nghiệp vụ là gì — `cap/evidence` gắn ảnh vào thửa, vào nhật ký, vào phiếu cân; `cap/workflow` duyệt tham số, nhật ký, định mức.

```sql
CREATE TABLE cap_evidence.attachment (
  id             uuid        NOT NULL,
  recorded_at    timestamptz NOT NULL DEFAULT now(),
  tenant_id      uuid        NOT NULL,
  subject_module text        NOT NULL,          -- 'dom/cooperative'
  subject_type   text        NOT NULL,          -- 'plot'
  subject_id     uuid        NOT NULL,
  object_key     text        NOT NULL,          -- khóa trong kho đối tượng
  wrapped_dek    bytea       NOT NULL,          -- khóa dữ liệu bọc bằng khóa công khai của hộ
  household_ref  uuid        NULL,
  gps            geography(Point, 4326) NULL,
  sha256         bytea       NOT NULL,
  device_id      text        NULL,
  event_time     timestamptz NOT NULL,
  actor_ref      uuid        NOT NULL,
  PRIMARY KEY (id, recorded_at)
) PARTITION BY RANGE (recorded_at);
CREATE INDEX ON cap_evidence.attachment (tenant_id, subject_module, subject_type, subject_id);
```

> **R-DB-07.** Không khóa ngoại xuyên schema. Tham chiếu chéo module dùng bộ ba `(subject_module, subject_type, subject_id)`. Khi ghi, module năng lực gọi port `Resolve(subject)` của module chủ: kiểm tồn tại, kiểm **thực thể cùng đơn vị với ngữ cảnh** hoặc có cấp phép. Module chủ khai `subject_type` nó cho phép tham chiếu trong `module.yaml`.

## 19. Đơn vị hành chính có hiệu lực theo thời gian

```sql
CREATE TABLE k_orgtree.admin_unit (                       -- nguồn ở cụm idp
  id            uuid PRIMARY KEY,
  official_code text NOT NULL,                            -- mã đơn vị hành chính chính thức
  level         text NOT NULL CHECK (level IN ('TINH','XA')),
  name          text NOT NULL,
  parent_id     uuid NULL,
  valid_from    date NOT NULL,
  valid_to      date NULL,
  EXCLUDE USING gist (official_code WITH =,
                      daterange(valid_from, valid_to, '[)') WITH &&)  -- btree_gist
);

CREATE TABLE k_orgtree.admin_unit_succession (            -- đơn vị cũ → đơn vị mới
  predecessor_id uuid NOT NULL,
  successor_id   uuid NOT NULL,
  effective_on   date NOT NULL,
  area_share     numeric(9,6) NULL,                       -- khi tách một phần
  legal_basis    text NOT NULL,                           -- văn bản quy định
  PRIMARY KEY (predecessor_id, successor_id, effective_on)
);
```

> **R-DB-09.** Danh mục có ý nghĩa pháp lý — đơn vị hành chính, định mức, giá công bố, tham số — **có hiệu lực theo thời gian** với ràng buộc loại trừ chồng lấn. Bản ghi nghiệp vụ trỏ tới **phiên bản có hiệu lực tại `event_time`**. Báo cáo kỳ cũ nhóm theo đơn vị tại thời điểm của kỳ, không theo danh mục hiện tại.

Ràng buộc `EXCLUDE` chặn đúng lỗi CROSS-01: không thể có hai bản ghi cùng mã chính thức với thời gian hiệu lực chồng nhau. **Câu hỏi C-9:** nguồn dữ liệu chính thức cho danh mục và bảng kế thừa sau sắp xếp năm 2025 — cần chốt trước khi nạp.

## 20. Dữ liệu cá nhân

Giữ nguyên SAD định danh v1.1 và v3.0, tóm tắt:

| Loại | Ở đâu | Xóa thế nào |
|---|---|---|
| Sự kiện, số đo, khối lượng | Sổ của module — chỉ `*_ref` | Không xóa |
| Họ tên, SĐT, giấy tờ, địa chỉ **hộ** | `vault` | Xóa bình thường |
| Ảnh và tệp có dữ liệu cá nhân | Kho đối tượng, mã hóa phong bì theo cặp khóa X25519 của từng hộ | Hủy khóa bí mật của hộ |
| Họ tên, email, SĐT **người dùng hệ thống** | `k_access.account_pii` — mã hóa mức trường, chỉ mục HMAC | Xóa bình thường; sổ giữ mã người thực hiện |

**Toạ độ thửa là dữ liệu cá nhân gián tiếp** — thửa chỉ một hộ canh tác thì toạ độ định danh hộ đó. Hình học thửa ở `dom_coop` được chính sách RLS bảo vệ như dữ liệu cá nhân, và **không bao giờ vào view công bố** ở mức thửa.

## 21. Chuỗi thời gian

- TimescaleDB **chỉ ở cụm `mkg-ts`** — luồng số đo thô của agent cân và telemetry phương tiện.
- Cụm đó thuộc vùng nội bộ, **một đơn vị duy nhất**, nên không cần RLS trên hypertable — tránh được câu hỏi chưa kiểm chứng về tương thích giữa RLS và nén của TimescaleDB.
- Số đo thô **không xóa**; sau cửa sổ kiểm toán `[KÝ]` chuyển tầng lạnh có nén.
- Phiếu cân là **dữ liệu dẫn xuất** do `mkg/measurement` tính và ghi vào sổ ở cụm `mkg`, kèm khoảng số đo thô đã dùng. Không truy vấn nào join cụm `mkg-ts` với cụm `mkg`.
- Vùng nền tảng **không có hypertable ở ĐTK 1**.

## 22. Phân tích

### 22.1 · View công bố

> **R-DB-10.** `cap/analytics` chỉ đọc view trong schema `<module>_pub`, khai trong `provides.publications`. Cấm đọc bảng nội bộ. Đổi cấu trúc bảng nội bộ không được làm vỡ view công bố — view là hợp đồng có phiên bản như port.

Phân tích chạy trên **bản sao chỉ đọc**. Truy vấn theo ngữ cảnh người dùng vẫn đi qua `WithContextTx`, nên RLS vẫn áp — dashboard Cấp Tỉnh chỉ tính trên dữ liệu tỉnh mình.

### 22.2 · Tổng hợp vượt phạm vi

Chỉ số tài sản dữ liệu (độ phủ, độ tươi, sai số dự báo) và van lọc dữ liệu công cần tổng hợp **trên mọi đơn vị**. Đây là chỗ duy nhất được vượt RLS:

> **R-DB-11.** Vai `r_agg` có `BYPASSRLS`, **chỉ dùng bởi job tổng hợp đã khai báo và đã duyệt**, chạy trên bản sao. Đầu ra chỉ ở mức tổng hợp với **ngưỡng gom nhóm k**; nhóm dưới k bị gộp hoặc ẩn. Mỗi lần chạy ghi nhật ký: job, phiên bản định nghĩa, số bản ghi đọc, số nhóm bị ẩn. Thêm job mới là thay đổi đặc quyền, đi qua `access_request`.

## 23. Migration

> **R-DB-12.** Mỗi schema một lịch sử migration riêng (bảng `schema_migrations` trong chính schema). Pipeline áp theo **thứ tự topo của đồ thị phụ thuộc module**: `k/*` trước, rồi `cap/*`, rồi `dom/*`, `mkg/*`. Migration chạy bằng vai `own_<module>` của đúng module — **không thể chạm schema khác**. Viết tay; tương thích ngược một phiên bản; không UPDATE, DELETE trên bảng sổ; không công cụ chuyển đổi tự động.

## 24. Kiểu dữ liệu chuẩn

| Loại | Kiểu | Ghi chú |
|---|---|---|
| Định danh | `uuid` (UUIDv7) | |
| Tiền | `numeric(18,2)` | Quy tắc làm tròn thuộc `rule_version` của phép tính |
| Đơn giá | `numeric(18,4)` | |
| Khối lượng kg | `numeric(14,3)` | |
| Tỷ lệ | `numeric(9,6)` | |
| Diện tích ha | `numeric(12,4)` | Tính từ hình học, lưu kèm phiên bản thuật toán |
| Thời gian | `timestamptz` | UTC; ngày nghiệp vụ tính theo giờ Việt Nam |
| Hình học | `geometry(…, 4326)` | Kiểm hợp lệ và tối thiểu 4 đỉnh khi ghi |
| Liệt kê | `text` + `CHECK`, hoặc bảng danh mục khi người dùng thêm được | Không để giá trị tự do |

> **R-DB-13.** Bảng vận hành — `outbox`, `inbox`, `idempotency`, `chain_head`, `context`, bảng job — **được UPDATE và có thời hạn lưu khai báo**, và không chứa dữ liệu nghiệp vụ duy nhất. Bảng nào không xếp được vào "sổ", "chiếu" hay "vận hành" thì thiết kế chưa xong.

---

# PHẦN IV — ĐẶC TẢ MODULE

## 25. Lõi

| Module | Sở hữu | Port chính | Ghi chú |
|---|---|---|---|
| **k/access** | Tài khoản, ứng dụng, vai trò có phiên bản, gán quyền, phê duyệt, ủy quyền, hỗ trợ, rà soát, thiết bị, khóa API, cấp phép chia sẻ | `Authorize`, `ResolveContext`, `ShareGrants` | **Đặc tả đầy đủ: SAD định danh v1.1.** Chạy ở cụm `idp` |
| **k/orgtree** | Đơn vị tham gia, cây Nút, đơn vị hành chính có hiệu lực | `Node`, `Subtree`, `AdminUnitAt(code, date)` | Nguồn ở cụm `idp`; chiếu ra `k_ref` |
| **k/ref** | Schema `k_ref` trong mỗi cụm dữ liệu | — (chỉ quan hệ công bố) | Worker đồng bộ; chỉ số `k_ref_lag_seconds` |
| **k/ledger** | Hàm `fn_immutable`, `fn_chain`, `canonical`, `verify_chain`, neo hằng ngày | — (hàm SQL) | Cài vào mỗi cụm |
| **k/jobs** | Mẫu outbox, inbox, idempotency, việc dài, publisher | `Enqueue`, `Job` | Mục 11 |
| **k/audit** | Sổ nhật ký an ninh; đẩy sang hệ thống giám sát tập trung | `Query` | Đọc nhật ký cũng ghi nhật ký — SAD R-ID-46 |
| **k/ui-shell** | Khung giao diện, trình khởi chạy, menu từ quyền, hệ thiết kế | — | Module giao diện đăng ký route và mục menu qua `module.yaml` |

**Thay đổi so với SAD v1.1 mục 6.3:** chia sẻ trong vùng nền tảng bằng đọc tại chỗ có cấp phép (mục 17.3); `k/access` thêm thực thể `share_grant` — đơn vị chủ, đơn vị xem, mục đích, bảng được khai, Nút phạm vi, hiệu lực, căn cứ (thỏa thuận chia sẻ). **Cấp phép chia sẻ là quyết định của đơn vị chủ dữ liệu**, nên luồng tạo nằm ở quản trị viên của đơn vị chủ, có nhật ký, và thu hồi có hiệu lực theo mục 17.1.

## 26. Năng lực dùng chung

| Module | Sở hữu (bảng chính) | Port | Sự kiện phát | Điểm mở rộng | Ghi chú bắt buộc |
|---|---|---|---|---|---|
| **cap/masterdata** | Danh mục, tham số — **có hiệu lực theo thời gian, có phiên bản** | `Lookup(at)`, `Propose`, `Publish` | `param.version_published` | Kiểu danh mục | Đổi tham số đi qua `cap/workflow` |
| **cap/workflow** | `request` (sổ), `decision` | `Submit`, `Decide` | `request.decided` | Validator theo loại | `CHECK` người duyệt khác người lập, khác người được hưởng — ở DB |
| **cap/forms** | `template` (phiên bản), `period`, `submission` (sổ), `close_snapshot` | `Publish`, `Submit`, `Review`, `Close`, `Reopen` | `period.closed` | Kiểu trường; validator trước chốt | Chốt kỳ = **ảnh chụp đóng băng** kèm băm và `rule_version`; mở lại kỳ là bản ghi mới, không sửa ảnh chụp |
| **cap/export** | Mẫu văn bản có phiên bản, lần xuất | `Render` (việc dài) | `export.ready` | Bộ định dạng | Mỗi lần xuất lưu băm tệp |
| **cap/analytics** | Định nghĩa chỉ số có phiên bản | `Metric(at)` | — | — | Chỉ đọc view `_pub`; tổng hợp vượt phạm vi qua `r_agg` |
| **cap/import** | Lô nhập, dòng lỗi | `Validate`, `Commit` | `import.committed` | Bộ ánh xạ cột theo module chủ | Ô trống chỉ loại dòng đó; chấp nhận .xlsx chuẩn bất kỳ |
| **cap/task** | Nhiệm vụ, giao việc, SLA | `Create`, `Assign`, `Close(result)` | `task.breached` | Loại nhiệm vụ | Đóng nhiệm vụ **bắt buộc** ghi kết quả |
| **cap/notify** | Kênh, mẫu, lần gửi | `Send` | — | Kênh | Trần chi phí SMS mỗi ngày |
| **cap/evidence** | Đối tượng bằng chứng: khóa đối tượng, `wrapped_dek`, `household_ref`, GPS, băm, thiết bị | `Attach`, `Open(purpose)` | `evidence.attached` | — | Mã hóa ngay khi thu; mở có mục đích, có nhật ký |
| **cap/content** | Bài viết, phiên bản, công bố | `Draft`, `Publish` | `content.published` | Loại nội dung | Công bố là quyền lớp PUBLISH riêng |
| **cap/price** | Chuỗi giá có hiệu lực, ngưỡng | `Series`, `Publish` | `price.published` | Nguồn giá | Công bố là quyền riêng |
| **cap/geo** | Lớp bản đồ, gói tile vùng | `Layer`, `TilePackage(area)` | — | — | Hình học thực thể thuộc module chủ; `cap/geo` chỉ cung cấp phép tính và hiển thị |
| **cap/sync** | Quy tắc đồng bộ, thiết bị, vé ngoại tuyến | — | — | Tập dữ liệu theo module | Sáu điều kiện failover slot — mục 36 |

**Bằng chứng tái sử dụng cho `cap/forms`:** cơ chế đóng băng của CGH qua UAT 23/09 — lưu cân đối, thêm máy, báo cáo giữ nguyên số đã lưu — được chuyển nguyên vào `cap/forms` làm cơ chế chốt kỳ cho App Khuyến nông.

## 27. Nghiệp vụ

### 27.1 · Vùng nền tảng

| Module | Sở hữu | Chia sẻ (`shares`) | Ghi chú |
|---|---|---|---|
| **dom/cooperative** | Hồ sơ HTX, nông hộ (`household_ref` → vault), xã viên, **thửa** (hình học), vụ, kế hoạch, nhật ký canh tác (sổ) | `household`, `plot`, `season`, `diary_entry` | **Chủ duy nhất** của các thực thể này. Chức năng "Cấp Xã quản lý HTX, thửa" của BRD Khuyến nông là thao tác trên module này theo quyền vai trò |
| **dom/extension** | Đầu mối khuyến nông, lịch công tác, khóa đào tạo, danh bạ trực | — | Đào tạo, danh bạ nâng lên năng lực khi có ứng dụng thứ hai |
| **dom/mechanization** | Máy, chủ máy (`owner_ref`), định mức có hiệu lực, cân đối, yêu cầu dịch vụ | `machine` | Định mức bắt buộc dẫn văn bản; vụ cũ tính theo định mức có hiệu lực tại thời điểm vụ |
| **dom/waterway** | Đồ thị node/edge có phiên bản | — | Tuyến nháp không vào đồ thị; truy vấn đồ thị trên bản sao |
| **dom/hubsim** | Dự án, kịch bản, ảnh chụp đầu vào, kết quả | — | Ảnh chụp gồm phiên bản tham số, phiên bản đồ thị, **kết quả định tuyến đường bộ đã dùng**, `algorithm_version` |
| **dom/mrv** | Danh mục trường bằng chứng, package đã xuất | — | Lưu chính package đã xuất kèm băm. Danh mục trường chờ C-2 |
| **dom/metering** | Sổ đo lường sử dụng | — | Là chứng từ, theo mẫu sổ |

### 27.2 · Vùng nội bộ Mekong Green — v4.1

**Tài liệu chi tiết:** Thiết kế chi tiết vùng nội bộ MKG v1.3. Mục này là tóm tắt có hiệu lực ràng buộc; chi tiết nằm ở tài liệu đi kèm.

**Ba thành phần:**

| # | Thành phần | Gồm | Cách làm |
|---|---|---|---|
| A | Kế toán pháp định | Sổ cái, thuế, hóa đơn, tài sản cố định — gồm 500+ máy cuộn, tiền lương, bảo hiểm; kho phụ tùng và nhiên liệu | **Mua ngoài**; nhận bút toán tổng hợp theo kỳ |
| B | Hệ vận hành MKG | Mọi giao dịch quanh lô rơm: mua từ HTX, đội hiện trường 1.500+ người, máy, thu gom, số cuộn, ghe thuê, điểm gom, hub, ba — bốn điểm cân, bán cho VFT, giá thành | **Tự xây, một hệ thống**, một nguồn sự thật cho lô |
| C | Engine điều phối | **Một lõi `lib/optcore`, ba chế độ:** mô phỏng (chọn hub, điểm gom, tồn đệm), kế hoạch cuộn 7–14 ngày, điều phối có lập lại kế hoạch | **Dịch vụ Python riêng**, không giữ dữ liệu nghiệp vụ; **cả ba chế độ ở vùng nội bộ** |

**Module của hệ vận hành** — theo nguyên tắc "không tạo module mới khi mở rộng được module có sẵn": `mkg/fieldops` (mở rộng: đội, máy cuộn, hỏng hóc) · **`mkg/workforce` (mới — tách vì dữ liệu cá nhân 1.500+ người, kho định danh nhân sự riêng trong cụm `mkg`)** · `mkg/orders` (mở rộng chiều mua từ HTX) · `mkg/logistics` (mở rộng: ghe thuê, chặng ghép ghe) · `mkg/measurement` (thêm điểm cân `CONSOLIDATION`, đối soát riêng cho lô đi thẳng) · `mkg/hub` (gia phả lô **dùng chung** cho cả mẻ hub lẫn ghép ghe trên sông) · `mkg/costing` · `mkg/acctbridge`.

**Engine điều phối — ba tầng:** T1 dòng chảy toàn mạng (quy hoạch nguyên, HiGHS) · T2a ghe nhỏ bến → điểm gom và T2b ghe lớn điểm gom → nhà máy, nối bằng tồn hoặc cửa sổ thời gian tùy sức đệm điểm gom · T3 phân công đội. Vùng đóng băng; mọi kế hoạch là đề xuất; tự duyệt chỉ trong năm điều kiện đồng thời; mục tiêu thời gian giải chốt theo kết quả làm thử ở cỡ Phase 1.

**Đầu vào engine:** hai loại — **quan sát trạng thái ngoài** (kho quan sát mới: `kind`, `source`, `as_of`, `max_age`, `uncertainty`) và **tham số chính sách** (sổ tham số của từng module). Quan sát từ nền tảng đến qua **feed của cổng tích hợp** — mục 41.4.

**Dòng rơm:** ngoài vụ chỉ hub → nhà máy; trong vụ đi thẳng nhà máy, phần vượt nhu cầu ngày vào hub — trung bình 45–55% đi thẳng; ghe thuê 6–200 tấn, ghép ở nhánh sông lớn.

## 28. Ứng dụng

| App | Module | Vai trò khởi tạo | Mốc |
|---|---|---|---|
| **KN** | Mục 7.2 | Quản trị KN · Cấp Quốc gia · Cấp Tỉnh · Cấp Xã · Biên tập nội dung · Duyệt công bố | **G1** |
| **HTX** | k/* · masterdata · workflow · forms · export · analytics · import · task · notify · evidence · content · geo · sync · **dom/cooperative** · dom/mechanization (đặt dịch vụ) | Quản trị App HTX · Quản trị HTX · Cán bộ HTX · Xã viên · Khuyến nông xem HTX | G2 |
| **CGH** | k/* · masterdata · workflow · forms · export · analytics · import · task · notify · geo · **dom/mechanization** · dom/cooperative (đọc chia sẻ) | Quản trị CGH · Cục KTHT · Cấp Tỉnh CGH · Chủ máy · Bên đặt dịch vụ | G2 |
| **SIM** | k/* · masterdata · workflow · export · analytics · import · notify · evidence · geo · **dom/hubsim** · **dom/waterway** | Quản trị mô phỏng · Người đặt tham số · Người phê duyệt tham số · Người lập kịch bản · Người xem · Biên tập đồ thị · Xác nhận tuyến | G2 |
| **MKG** | k/* · các `cap/*` thể hiện nội bộ · **mkg/*** | Theo BRD định danh IV.C.5 | G3 |
| **NH** | k/* · notify · content · price · dom/cooperative (hộ mình) | Nông hộ | G3 |
| **PF** | k/* | Quản trị nền tảng · Quản trị an ninh · Kiểm toán viên | G1 |

Danh mục vai trò chi tiết và ma trận quyền: BRD định danh v1.1 Mục IV.C, backlog User Story lớp định danh v1.0 Epic K.

---

# PHẦN V — VẬN HÀNH VÀ AN TOÀN

## 29. Phân vùng mạng — cấp độ 3

| Vùng mạng | Chứa | Được đi tới |
|---|---|---|
| Biên | WAF, chống DDoS, TLS | DMZ |
| DMZ | Ingress, gateway, **trang đăng nhập Keycloak** | Vùng ứng dụng (cổng API); Keycloak |
| Ứng dụng | `api-*`, `worker`, `outbox-publisher`, `ingest`, `sync`, `optimizer`, `access` | Vùng dữ liệu (DB, MQ, cache); vùng định danh (Access, vault — mTLS) |
| Dữ liệu | Cụm `pf`, `mkg`, `mkg-ts`, PgBouncer, RabbitMQ, Valkey | Không đi ra |
| Định danh | Cụm `idp` (Keycloak DB, Access DB), cụm `vault`, `identity-vault` | Không đi ra, trừ KMS; API quản trị Keycloak chỉ từ pod Access |
| Quản trị | Bastion, CI runner, Argo CD, Grafana | Mọi vùng qua VPN + MFA, có ghi phiên |
| Biên OT | Agent cân tại hub | Chỉ tới `ingest` qua VPN + mTLS |

> **R-SEC-01.** Máy chủ tiếp xúc Internet không kết nối trực tiếp vùng dữ liệu. Vùng ứng dụng tách vùng dữ liệu. Vùng định danh riêng. Trong Kubernetes: mỗi vùng một namespace, NetworkPolicy mặc định từ chối cả vào lẫn ra.

> **R-SEC-02.** Triển khai đám mây: nhà cung cấp phải **cung cấp bằng chứng** tách biệt logic với khách hàng khác, giữa các vùng mạng, và phân vùng lưu trữ — điều kiện mua sắm.

## 30. Kiểm soát an ninh

Lớp định danh, phiên, thu hồi, MFA, truy cập hỗ trợ, khẩn cấp: **SAD định danh v1.1** — R-ID-01 … R-ID-46, không lặp lại ở đây.

| Nhóm TCVN 11930 | Kiểm soát | Ở đâu |
|---|---|---|
| Mạng | Phân vùng; IDS/IPS giữa các vùng; dự phòng kết nối Internet | Mục 29 |
| Máy chủ | Quản trị qua bastion, VPN + MFA; chống mã độc quản lý tập trung; image tối giản, non-root, hệ tệp gốc chỉ đọc | Mục 32 |
| Ứng dụng | Xác thực, MFA cho đặc quyền, quản lý tài khoản đặc quyền ở tầng ứng dụng; SAST, quét dependency, SBOM, ký image | SAD; mục 32 |
| Dữ liệu | Bất biến ba lớp + chuỗi băm + neo; mã hóa đường truyền và lưu trữ; mã hóa theo hộ; **chống thất thoát**: mọi đường xuất (van công bố, Public API, `cap/export`, `dom/mrv`, `r_agg`) qua điểm kiểm soát có nhật ký, hạn mức, cảnh báo khối lượng bất thường | Mục 15, 20, 22.2 |
| Nhật ký | Tối thiểu 3 tháng nhật ký thiết bị và hệ thống; nhật ký định danh 13 tháng nóng; đồng bộ NTP; trong lãnh thổ | Mục 31 |

**Thêm ở v4.0 do mô hình module:**

> **R-SEC-03.** Vai `r_agg` (BYPASSRLS) và mọi job dùng nó là tài sản đặc quyền: danh sách job có phiên bản, thêm job qua phê duyệt bốn mắt, mỗi lần chạy có nhật ký.

> **R-SEC-04.** Cấp phép chia sẻ chỉ do quản trị viên của **đơn vị chủ dữ liệu** tạo; có căn cứ thỏa thuận; có thời hạn; xuất hiện trong rà soát quyền định kỳ của đơn vị chủ.

## 31. Quan sát

OpenTelemetry cho trace, metric, log; `trace_id` xuyên API → hàng đợi → worker → dịch vụ ngoài. Trường log chuẩn: `ts`, `level`, `service`, `module`, `zone`, `trace_id`, `tenant_id`, `actor_ref`, `event`, `outcome`. Không dữ liệu cá nhân, không bí mật, không URL có chữ ký.

**Chỉ số nghiệp vụ bắt buộc có cảnh báo:** độ trễ standby (đồng bộ ngoại tuyến) · **`k_ref_lag_seconds`** · độ trễ bảng chiếu so với sổ · số sự kiện trong DLQ · tỷ lệ phiếu cân vượt dung sai · độ đầy đệm agent biên · bản ghi xung đột ngoại tuyến quá SLA · **số lần đọc qua cấp phép chia sẻ theo đơn vị** · số lần chạy job `r_agg`.

## 32. CI/CD và cổng chất lượng

**Pipeline:** lint → kiểm thử → build → SBOM → quét lỗ hổng → ký image → push → Argo CD. Migration là job riêng, theo thứ tự topo module (R-DB-12).

| Cổng | Kiểm | Quy tắc |
|---|---|---|
| Đồ thị module | Không chu trình, chỉ đi xuống, phụ thuộc có phiên bản thỏa | R-MOD-05 |
| Ranh giới mã | depguard: chỉ import `port/` | R-MOD-02 |
| Không nhánh theo ứng dụng | Linter cấm so sánh tên ứng dụng trong `cap/*` | R-MOD-04 |
| Hợp đồng port | Kiểm thử của mọi module tiêu thụ chạy ở module cung cấp | R-MOD-06 |
| Hợp đồng sự kiện, view công bố | Tương thích ngược | R-EVT-01, R-DB-10 |
| Quyền DB | Không vai `r_*` nào có quyền trên schema của module khác, trừ `k_ref` và `_pub` được khai | R-MOD-01, R-DB-05 |
| Bất biến | Sáu phép thử ghi đè thất bại; `v_missing_immutability` rỗng | R-DB-06 |
| Chuỗi băm | `verify_chain` trên dữ liệu kiểm thử, gồm ghi song song nhiều chuỗi | R-DB-06 |
| Rò rỉ đơn vị | N luồng song song qua PgBouncer transaction mode, **gồm nhánh cấp phép chia sẻ, thu hồi cấp phép, và kết nối tái sử dụng thiếu ngữ cảnh** (T6d) | R-DB-08 |
| Kế hoạch truy vấn chính sách | `EXPLAIN` mọi bảng có RLS: cấm SubPlan gọi hàm theo từng hàng | R-DB-08, DB-14 |
| Vai DB | Truy vấn ngoài `WithContextTx` thất bại; module A không `SET ROLE` sang vai module B | R-DB-02, R-DB-03 |
| Ma trận quyền | Mỗi dòng ma trận BRD của từng ứng dụng | SAD R-ID-15 |
| Bảo mật lớp định danh | R-ID-15 … R-ID-46 | SAD |
| Kiểu số | Linter cấm float trong module tiền và khối lượng | R-CODE-01 |
| PII trong log | Linter tag `pii` | R-CODE-05 |
| Giấy phép, bảo mật | Danh sách giấy phép; SAST, quét bí mật, quét image | RB-4 |
| Ký | Admission policy từ chối image chưa ký | |

**Kiểm thử định kỳ:** diễn tập phục hồi hàng quý · failover cơ sở dữ liệu giữa lúc đồng bộ · dựng lại bảng chiếu từ sổ · hiệu năng RLS ở khối lượng ĐTK 2 **gồm nhánh chia sẻ** · tải đăng nhập 2.000 đồng thời · đánh giá an toàn thông tin định kỳ.

> **R-OPS-04.** Mọi DDL, đoạn SQL mẫu trong tài liệu kiến trúc được trích ra và **chạy trên PostgreSQL thật** trong CI của kho tài liệu. Tài liệu có DDL không chạy được thì không phát hành — bài học DB-01.

## 33. Môi trường và dữ liệu

| Môi trường | Dữ liệu | Hạ tầng |
|---|---|---|
| Local | Tổng hợp | Compose |
| Dev | Tổng hợp | Cụm dùng chung |
| Staging | Ẩn danh hóa không đảo ngược được, hoặc tổng hợp | Giống sản xuất về cấu hình |
| Production | Thật | Trong lãnh thổ |

> **R-OPS-01.** Dữ liệu cá nhân thật không ra khỏi production. Bộ sinh dữ liệu tổng hợp mô phỏng phân phối thực: số cuộn, sản lượng quy đổi, độ ẩm, toạ độ thửa ĐBSCL, cây đơn vị hành chính **sau sắp xếp 2025 kèm bảng kế thừa**.

**Danh mục nhà cung cấp được phép (RB-7):** dịch vụ lưu dữ liệu cá nhân, sao lưu, ảnh, nhật ký chứng cứ, nhật ký có định danh — chỉ nhà cung cấp có hạ tầng và pháp nhân lưu trữ trong lãnh thổ. Ngoại lệ phải có ADR và chứng minh dữ liệu không chứa định danh.

## 34. Sao lưu và khắc phục thảm họa

| Đối tượng | Cơ chế | RPO / RTO |
|---|---|---|
| `idp` (Keycloak, Access) | Full tuần, differential ngày, WAL liên tục | ≤ 1 giờ / ≤ 2 giờ — chặt hơn vì mọi ứng dụng phụ thuộc |
| `pf`, `mkg` | Như trên | ≤ 1 giờ / ≤ 4 giờ |
| `mkg-ts` | Như trên + chuyển tầng | ≤ 4 giờ / ≤ 8 giờ |
| `vault` | Sao lưu riêng, khóa riêng | ≤ 1 giờ / ≤ 4 giờ |
| Kho đối tượng | Sao chép gia tăng theo phiên bản, checksum, sang vùng thứ hai trong lãnh thổ | |
| Neo chuỗi băm | Bản ký số hằng ngày vào kho bất biến | |

**Quy tắc:** credential ghi sao lưu tách khỏi credential phục hồi · khóa đối tượng bất biến · **diễn tập hàng quý trên môi trường sạch**, kiểm: số dòng, băm bộ tham số, `verify_chain`, chạy lại một mô phỏng mẫu, checksum kho đối tượng, **`k_ref` dựng lại khớp nguồn** · sẵn sàng cao không thay sao lưu · xóa hộ áp vào cả bản sao lưu bằng hủy khóa.

**Thứ tự phục hồi:** `idp` → `vault` → `pf`, `mkg` → `k_ref` dựng lại từ `idp` → `mkg-ts`. Phục hồi `pf` trước `idp` thì `k_ref` không có nguồn và RLS trả rỗng — hệ chạy nhưng không ai thấy dữ liệu. Đó là hành vi đúng (mặc định chặn), nhưng phải được biết trước.

## 35. Ứng cứu sự cố

| Mức | Loại |
|---|---|
| **P1** | Ngừng toàn phần · **rò rỉ, mất, truy cập trái phép dữ liệu cá nhân** · đứt chuỗi băm · sai lệch số liệu tài chính · **đọc chéo đơn vị không có cấp phép** |
| P2 | Ngừng một module dùng chung (ảnh hưởng nhiều ứng dụng) · đồng bộ ngoại tuyến đứt diện rộng · `k_ref` trễ quá ngưỡng · agent cân mất kết nối quá ngưỡng |
| P3 | Suy giảm hiệu năng · lỗi chức năng có phương án tạm |
| P4 | Lỗi hiển thị |

**Hai nghĩa vụ báo cáo, một quy trình:** sự cố an toàn thông tin theo cấp độ, và sự cố dữ liệu cá nhân — hai đầu mối, hai thời hạn `[KÝ — pháp chế]`. Sổ tay lộ khóa ký, tài khoản bị chiếm, truy cập khẩn cấp: SAD định danh 12.4, 12.5.

## 36. Chế độ hỏng

| Thành phần | Chế độ hỏng | Hệ quả | Thiết kế để chịu |
|---|---|---|---|
| **Module năng lực dùng chung** | Lỗi phát hành | **Ảnh hưởng mọi ứng dụng dùng nó** — bài học DEF-SYS-02 | Kiểm thử hợp đồng của người tiêu thụ (R-MOD-06); bật module theo đơn vị cho phép phát hành dần |
| `k_ref` | Trễ đồng bộ | Cấp phép vừa thu hồi còn hiệu lực vài giây; Nút vừa di chuyển chưa áp | Mục tiêu ≤ 5 giây p99, cảnh báo; thu hồi cấp phép tăng epoch ngữ cảnh |
| `k_ref` | Trống sau phục hồi | RLS trả rỗng mọi nơi | Thứ tự phục hồi mục 34 |
| Cụm `idp` | Ngừng | Không đăng nhập mới, không làm mới ngữ cảnh → sau 5 phút mất quyền | Hai node trở lên; vé ngoại tuyến 72 giờ; tài khoản khẩn cấp |
| Valkey định danh | Đầy bộ nhớ | Ghi epoch mới bị từ chối | `noeviction`; vắng khóa → hỏi Access; cảnh báo ở 70% |
| Đồng bộ ngoại tuyến | Slot mất do failover; standby chậm | Replicate lại; chiều xuống đứng | Sáu điều kiện PostgreSQL 17 (SAD định danh 5.7 và v3.0 mục 15); độ trễ standby là chỉ số nghiệp vụ |
| `chain_head` | Tranh chấp khóa hàng | Ghi cùng một chuỗi bị tuần tự | Thiết kế `stream_key` theo lô, không theo đơn vị — chuỗi nhỏ, tranh chấp thấp |
| Phân vùng sổ | Thiếu phân vùng tháng tới | Ghi thất bại | Job tạo trước 3 tháng; cảnh báo khi còn dưới 1 tháng |
| Số liệu cân của khách | Không về | Không xuất hóa đơn | Đường nhập tay có bằng chứng |

---

# PHẦN VI — TÍCH HỢP LIÊN HỆ THỐNG

## 37. Nguyên tắc: MKG là đối tác đầu tiên, không phải ngoại lệ

Kết nối MKG ↔ AgriGreen là **thí điểm** cho việc AgriGreen kết nối với hệ thống của nhiều bên: doanh nghiệp thu mua, nhà máy, hệ thống của HTX lớn, hệ thống cơ quan nhà nước, nhà cung cấp thiết bị, sổ đăng ký tín chỉ carbon.

> **R-INT-01.** **MKG kết nối với nền tảng qua đúng cổng tích hợp, đúng hợp đồng dữ liệu, đúng hạn mức, đúng cách xác thực mà mọi đối tác tương lai sẽ dùng.** Không kết nối DB, không dùng chung hàng đợi, không endpoint riêng. Nếu MKG cần một khả năng cổng chưa có, cổng được bổ sung khả năng đó **cho mọi đối tác** — theo quyền — không mở lối riêng.

Ba lý do, theo mức độ:

- **Nguyên tắc đã chốt:** Mekong Green dùng nền tảng như một đơn vị bất kỳ, không có đặc quyền.
- **Mọi đường tắt dành cho MKG hôm nay là nợ kỹ thuật ngay khi có đối tác thứ hai.** Một kết nối DB trực tiếp chạy tốt với một đối tác và không mở rộng được với đối tác nào khác.
- **Thí điểm chỉ có giá trị nếu nó là thật.** MKG là đối tác có tải lớn nhất và yêu cầu khắt khe nhất mà nền tảng sẽ gặp trong vài năm tới. Cổng tích hợp chạy được cho MKG thì chạy được cho người đến sau.

## 38. Bốn loại luồng

### 38.1 · Phân loại

| Loại | Chiều | Ví dụ | Ai thấy | Cơ chế |
|---|---|---|---|---|
| **Dữ liệu được cấp phép** | Nền tảng → đối tác | Tiến độ gặt tổng hợp theo xã; lịch mùa vụ; đồ thị đường thủy | Đối tác được cấp phép | **Feed theo cursor** + ảnh chụp lớn |
| **Dữ liệu công bố** | Đối tác → nền tảng | Dữ liệu mạng lưới qua van công bố | Theo hợp đồng công bố | **Nạp theo lô, bất đồng bộ** |
| **Luồng song phương** | Hai chiều giữa hai bên của một giao dịch | MKG gửi số cuộn ghi nhận cho đúng HTX bán; HTX xác nhận | **Chỉ hai bên** | Nạp theo lô + feed riêng của cặp |
| **Tham chiếu dùng chung** | Nền tảng → mọi đối tác | Đơn vị hành chính có hiệu lực, cây tổ chức công khai, danh mục chuẩn | Mọi đối tác | Feed tham chiếu, ảnh chụp theo phiên bản |

### 38.2 · Luồng song phương — loại mới, cần ADR

Ràng buộc RB-6 bắt buộc **đối soát chéo** mọi số đo giữa hai bên — số cuộn MKG ghi nhận phải được HTX xác nhận. Theo bản review 03/10, `docs/design/mkg.md` §3.1 cấm giá, giá thành, dữ liệu lô đi qua van công bố. Hai quy định này **không mâu thuẫn**, nếu phân biệt:

- **Dữ liệu mạng lưới** — đi vào nền tảng để nhiều bên dùng. §3.1 đúng khi cấm giá và dữ liệu lô của MKG đi vào đây.
- **Dữ liệu song phương** — bản ghi của **một giao dịch giữa MKG và một HTX**, mà HTX là một bên của giao dịch đó. Nền tảng là nơi trung lập để hai bên cùng nhìn một bản ghi, **không bên thứ ba nào thấy**.

Đây cũng là vai trò lâu dài của nền tảng khi có giao dịch giữa các bên thứ ba cuối 2028. **Cần ADR** bổ sung loại luồng song phương vào ADR-0020, với ba ràng buộc: chỉ hai bên của giao dịch; không tham gia tổng hợp, chỉ số hay van lọc dữ liệu công; lưu theo quy tắc chỉ ghi thêm.

**Mâu thuẫn cần giải cùng ADR này:** BRD Lớp SSO v1.1 mục IV.C.6 cho Kênh nông hộ xem **"khoản thanh toán của hộ"**. Số tiền MKG trả cho hộ là giá — theo §3.1 không được qua van. Luồng song phương là cách hợp lệ duy nhất để hộ thấy khoản của chính mình trên nền tảng; nếu ADR không chấp nhận, mục IV.C.6 phải bỏ khoản thanh toán.

### 38.3 · Các luồng của thí điểm MKG

| Luồng | Chiều | Loại | Khối lượng — Phase 1 | Độ tươi cần |
|---|---|---|---|---|
| Tiến độ gặt, diện tích theo xã | Nền tảng → MKG | Cấp phép, tổng hợp, ngưỡng k | Vài nghìn dòng/ngày | ≤ 15 phút trong vụ |
| Lịch mùa vụ `season_harvest_dates` | Nền tảng → MKG | Cấp phép | Nhỏ | ≤ 1 ngày |
| Đồ thị đường thủy `waterway_routes` | Nền tảng → MKG | Tham chiếu, theo phiên bản | Vài phiên bản/năm, cỡ chục MB | ≤ 1 giờ sau phiên bản mới |
| Lịch gom gửi HTX | MKG → HTX | Song phương | Hàng trăm – nghìn/ngày | ≤ 15 phút |
| Số cuộn ghi nhận theo lệnh gom | MKG → HTX | Song phương | Khoảng 10.000–13.000 bản ghi/ngày | ≤ 15 phút |
| Xác nhận số cuộn của HTX | HTX → MKG | Song phương | Như trên | ≤ 1 giờ |

**Tồn kho và kế hoạch tiêu thụ của VFT không đi qua nền tảng.** VFT là khách hàng của MKG, dữ liệu đó thuộc vùng nội bộ, đi thẳng VFT → hệ vận hành MKG. Cổng nhận ở vùng nội bộ **dùng chung mã** với cổng tích hợp của nền tảng, triển khai và dữ liệu riêng — R-MOD-10.

## 39. Hiệu năng — rủi ro thật nằm ở đâu

**Băng thông không phải vấn đề.** Ước lượng với 1.500 xã cập nhật 4 lần/ngày: dữ liệu được cấp phép tính một lần là 6.000 dòng/ngày; phục vụ 200 đối tác là khoảng 240 MB/ngày và 3,3 yêu cầu/giây nếu mỗi đối tác hỏi feed mỗi phút. Chiều vào từ MKG ở Phase 1, gửi theo lô 500 bản ghi, là khoảng 21–26 yêu cầu/ngày. Đều cách xa giới hạn của một nút.

**Năm rủi ro thật**, và quy tắc chặn từng rủi ro:

| # | Rủi ro | Hệ quả nếu không chặn | Chặn bằng |
|---|---|---|---|
| 1 | **Gọi đồng bộ xuyên hệ thống trong đường găng** | Nền tảng chậm hoặc ngừng → điều phối của MKG đứng; độ trễ cộng dồn qua từng hệ | R-INT-02 |
| 2 | **Tính lại cho từng đối tác** | Chi phí tăng theo số đối tác × số sự kiện | R-INT-05 |
| 3 | **Một đối tác chậm kéo cả hệ** — hàng đợi chung, webhook treo | Đối tác thứ 50 làm hỏng đối tác thứ nhất | R-INT-04, R-INT-09 |
| 4 | **Sóng tải dồn** — phiên bản dữ liệu lớn mới, cuối kỳ báo cáo | 50 MB × 200 đối tác trong 5 phút là khoảng 33 MB/giây nếu phục vụ qua API | R-INT-06, ảnh chụp qua kho đối tượng |
| 5 | **Lọc từng hàng khi nhiều bên cùng đọc** | Chi phí RLS trên bảng giao dịch lớn tăng theo số người đọc | R-INT-05: dữ liệu cấp phép là tập dữ liệu dựng sẵn, cấp quyền ở mức tập |

## 40. Quy tắc tích hợp

> **R-INT-02. Không gọi đồng bộ xuyên hệ thống trong đường găng.** Engine và hệ vận hành MKG không bao giờ gọi nền tảng trong lúc giải bài toán hay trong một giao dịch người dùng — chúng đọc **bản sao cục bộ** được feed cập nhật. Nền tảng không bao giờ gọi hệ thống đối tác trong lúc phục vụ yêu cầu người dùng.

> **R-INT-03. Bất đồng bộ mặc định.** Ghi từ đối tác trả `202 Accepted` kèm mã lô nạp; kết quả từng bản ghi tra được sau. Dữ liệu ra qua feed.

> **R-INT-04. Vách ngăn theo đối tác.** Mỗi đối tác có hạn mức tần suất, hạn mức khối lượng, hàng đợi xử lý và DLQ riêng. Vượt hạn mức trả `429` kèm `Retry-After`. Tồn đọng của một đối tác không làm chậm đối tác khác.

> **R-INT-05. Tính một lần, phát nhiều lần.** Dữ liệu cấp phép là **tập dữ liệu dựng sẵn theo phiên bản** — ví dụ tiến độ gặt theo xã đã qua ngưỡng k — do job tổng hợp tạo một lần; quyền kiểm ở mức **tập dữ liệu và phạm vi**, không lọc từng hàng cho từng đối tác. Lọc từng hàng chỉ dùng cho luồng song phương, vốn tự nhiên theo từng cặp.

> **R-INT-06. Feed theo cursor, ảnh chụp lớn qua kho đối tượng.** Đối tác đọc "thay đổi kể từ cursor C"; giao ít nhất một lần, có thứ tự theo khóa thực thể; bên nhận idempotent theo `event_id`. Feed giữ **30 ngày**; mất cursor quá hạn thì đồng bộ lại từ ảnh chụp. Ảnh chụp lớn phục vụ bằng **URL ký có hạn** từ kho đối tượng trong lãnh thổ, không qua API.

> **R-INT-07. Webhook mỏng.** Webhook chỉ báo "feed X có dữ liệu mới tới cursor C", có chữ ký HMAC; đối tác tự kéo dữ liệu. Không đẩy dữ liệu qua webhook — tránh bão giao hàng, tránh mất thứ tự, tránh dữ liệu nằm trong nhật ký lỗi của bên nhận.

> **R-INT-08. Hợp đồng có phiên bản.** Mỗi feed, mỗi hợp đồng nạp có lược đồ trong `/contracts/integration/`; thay đổi phá vỡ → phiên bản mới chạy song song tối thiểu một kỳ phát hành; kiểm thử hợp đồng của đối tác chạy trong CI của nền tảng. Mỗi đối tác có môi trường thử với dữ liệu tổng hợp.

> **R-INT-09. Ngắt mạch.** Webhook tới một đối tác thất bại N lần liên tiếp → tạm dừng, báo đối tác, không mất dữ liệu — đối tác tiếp tục từ cursor khi khôi phục.

> **R-INT-10. Đối soát định kỳ.** Mỗi phiên bản tập dữ liệu và mỗi ngày của luồng song phương có **tổng kiểm** — số bản ghi và băm — để hai bên phát hiện lệch im lặng. Bài học CROSS-02: hai bản dữ liệu lệch nhau mà không ai biết.

> **R-INT-11. Đối tác không có đội IT vẫn tham gia được.** Mọi hợp đồng nạp và feed có **kênh tải tệp và tải xuống theo cùng lược đồ** — cho HTX nhỏ, cơ quan chỉ gửi được Excel. Cùng hợp đồng, khác kênh.

## 41. Kiến trúc cổng tích hợp

### 41.1 · Thành phần

```
  Đối tác (API)   Đối tác (tệp)   MKG (như mọi đối tác)
        │               │                 │
        ▼               ▼                 ▼
 ┌──────────────────────────────────────────────────────────────┐
 │ CỔNG TÍCH HỢP  cmd/integration-gateway  (vùng DMZ → ứng dụng) │
 │  xác thực máy (k/access) · hạn mức theo đối tác · đo lường    │
 │  ┌─────────────┐ ┌────────────────┐ ┌──────────────────────┐ │
 │  │ Feed API    │ │ Nạp theo lô    │ │ Ảnh chụp: URL ký     │ │
 │  │ cursor      │ │ 202 + mã lô    │ │ từ kho đối tượng     │ │
 │  └──────┬──────┘ └───────┬────────┘ └──────────┬───────────┘ │
 └─────────┼────────────────┼─────────────────────┼─────────────┘
           │                │ hàng đợi riêng      │
           ▼                ▼ từng đối tác        ▼
   kho feed (chỉ      inbox → kiểm hợp đồng → port của module
   ghi thêm, bản      công bố hoặc song phương
   sao đọc)                ▲
           ▲               │
   job dựng tập dữ liệu ───┘ webhook mỏng (ngắt mạch theo đối tác)
```

### 41.2 · Xác thực và quyền

- Định danh máy do `k/access` quản lý — SAD định danh mục 5.9: khóa API lưu HMAC; **mTLS bắt buộc** với đối tác ghi khối lượng lớn, trong đó có MKG.
- **Phạm vi của khóa = các loại cấp phép và hợp đồng nạp mà đối tác được phép.** Cấp phép do đơn vị chủ dữ liệu tạo — SAD định danh mục 6.3 sửa ở v4.0 mục 17.3.
- Mọi lời gọi ghi một dòng vào `dom/metering`.

### 41.3 · Ghi từ đối tác

Nhận lô → kiểm lược đồ đồng bộ (rẻ) → trả `202` + mã lô → hàng đợi riêng của đối tác → **inbox** → kiểm hợp đồng công bố hoặc song phương (mặc định từ chối) → gọi port của module chủ → kết quả từng bản ghi ghi vào lô nạp. Bản ghi lỗi không làm hỏng cả lô. Idempotent theo `Idempotency-Key` và khóa nghiệp vụ trong bản ghi.

### 41.4 · Phía MKG: feed thành quan sát

Ở vùng nội bộ, một **bộ điều hợp feed** kéo từ cổng tích hợp của nền tảng, ghi vào **kho quan sát** của engine (tài liệu vùng nội bộ v1.3 mục 7) với `source = api`, `as_of` lấy từ nền tảng. Hệ quả quan trọng nhất cho hiệu năng và độ bền:

**Nền tảng ngừng thì điều phối của MKG không ngừng.** Quan sát cũ dần; quy tắc `max_age` và bộ ước tính tự tăng tồn đệm; kế hoạch mang cờ dữ liệu cũ. Khi nền tảng trở lại, bộ điều hợp đọc tiếp từ cursor. Không có lời gọi đồng bộ nào để mà treo.

## 42. Mục tiêu dịch vụ của cổng

**Mục tiêu thiết kế, phải đo trong kiểm thử tải trước G4:**

| Chỉ số | Mục tiêu |
|---|---|
| Đọc feed, p95 | ≤ 300 ms |
| Nhận lô ghi, p95 đến lúc trả `202` | ≤ 200 ms |
| Độ tươi tập dữ liệu cấp phép | ≤ chu kỳ job dựng tập dữ liệu; tiến độ gặt trong vụ: 15 phút |
| Webhook báo dữ liệu mới, p95 | ≤ 30 giây sau khi có dữ liệu |
| Ảnh chụp sẵn sàng sau phiên bản mới | ≤ 15 phút |
| Xử lý lô song phương, p95 | ≤ 15 phút |
| Sẵn sàng của cổng | ≥ 99,9%/tháng |
| Một đối tác vượt hạn mức | Không làm chậm p95 của đối tác khác quá 10% |

Dòng cuối là phép thử của R-INT-04: kiểm thử tải phải có kịch bản một đối tác gửi gấp mười lần hạn mức.

**Núm mở rộng, theo thứ tự dùng:** thêm bản sao cổng (không trạng thái) · thêm bản sao đọc cho kho feed · tăng worker theo hàng đợi đối tác · ảnh chụp qua kho đối tượng có bộ đệm phân phối trong lãnh thổ.

## 43. Chế độ hỏng

| Hỏng | Hệ quả | Thiết kế để chịu |
|---|---|---|
| Nền tảng ngừng | Đối tác không kéo được feed | Đối tác dùng bản sao cục bộ; MKG tiếp tục điều phối với quan sát cũ dần — 41.4 |
| Hệ thống đối tác ngừng | Webhook thất bại | Ngắt mạch; feed giữ 30 ngày; đối tác tiếp tục từ cursor |
| Đối tác gửi dồn | Tồn đọng | Hạn mức, `429`, hàng đợi riêng — không lan sang đối tác khác |
| Mất cursor quá 30 ngày | Không đọc tiếp được | Đồng bộ lại từ ảnh chụp, rồi đọc tiếp feed |
| Lược đồ không khớp | Bản ghi bị từ chối | Từ chối rõ ràng từng bản ghi, cảnh báo; không chấp nhận một phần trường |
| Hai bên lệch im lặng | Số liệu khác nhau không ai biết | Đối soát tổng kiểm — R-INT-10 |

## 44. Mở rộng sang các bên khác

| Loại bên | Ví dụ | Kênh chính | Ghi chú |
|---|---|---|---|
| Doanh nghiệp thu mua khác | Bên mua rơm, phụ phẩm | API — như MKG | Thí điểm MKG là mẫu trực tiếp |
| Nhà máy, khách hàng của một đơn vị | VFT với MKG | **Không qua nền tảng** — thuộc vùng nội bộ của đơn vị đó | Cùng mã cổng, triển khai riêng |
| HTX có hệ thống riêng | Phần mềm quản lý HTX khác | API hoặc tải tệp | R-INT-11 |
| HTX không có IT | Hầu hết HTX | Tải tệp, nhập trên App HTX | R-INT-11 |
| Cơ quan nhà nước | TTKNQG, Cục KTHT, sở | Bộ điều hợp theo chuẩn kết nối của cơ quan — ví dụ nền tảng tích hợp, chia sẻ dữ liệu cấp quốc gia hoặc cấp tỉnh — **đánh giá khi có yêu cầu cụ thể** | Thường đòi chuẩn và thủ tục riêng; không thiết kế trước khi biết yêu cầu |
| Nhà cung cấp thiết bị, IoT | Cân, máy cuộn có telemetry | Thu nhận vào vùng nội bộ của đơn vị sở hữu thiết bị | Như agent cân |
| Sổ đăng ký tín chỉ carbon | Bên kiểm định MRV | Xuất package `dom/mrv` | Một chiều, theo phiên bản |

# PHỤ LỤC

## A. Lộ trình theo cổng

| Cổng | Mốc | Giao |
|---|---|---|
| **G0** | 30/09/2026 — **cần cập nhật trạng thái** | Khung module, registry, kiểm đồ thị; `k/ledger` với mẫu DDL đã sửa (DB-01, DB-03); `k/access` tối thiểu; `k/ref`; vai đăng nhập theo đơn vị triển khai; CI chạy DDL tài liệu (R-OPS-04) |
| **G1** | **15/12/2026** | App KN chạy thật trên: `cap/import`, `cap/workflow`, `cap/forms` (dùng cơ chế đóng băng của CGH), `dom/cooperative` làm chủ duy nhất — **hợp nhất dữ liệu ba hệ UAT trước khi nhận dữ liệu thật**; `cap/analytics`, `cap/price`, `cap/geo`, `cap/content`, `cap/notify`, `cap/evidence`, `cap/sync` ở dạng tối thiểu cho KN; `dom/extension`; lớp định danh theo SAD v1.1 mục 13 |
| **G1+** | Song song G1 | **Cổng tích hợp tối thiểu:** feed theo cursor, nạp theo lô, xác thực máy, hạn mức theo đối tác — cho luồng MKG đầu tiên. ADR luồng song phương và loại cấp phép tổng hợp theo xã |
| **G2** | 31/03/2027 | App HTX, CGH, SIM chuyển lên module dùng chung; `share_grant` chạy thật; tổng quát `cap/forms`, `cap/task`; Danh mục Ràng buộc Nền tảng được ký |
| **G3** | Q3/2027 | Vùng nội bộ MKG: `mkg/*`, cụm `mkg`, `mkg-ts`; App NH; chỉ số tài sản dữ liệu qua `r_agg` |
| **G4** | Q2/2028 | Kubernetes được quản trị; diễn tập tải ĐTK 2, tách đơn vị triển khai bằng cấu hình, dựng lại bảng chiếu, phục hồi đủ thứ tự |
| **G5** | Q4/2028 | Phase K; Public API và khóa API với bên thứ ba đầu tiên; bật module theo đơn vị |
| **G6** | Q1/2029 | Chuyển cấu hình Phase 1 không sửa mã; cấu hình build bản lite |

**Ước lượng bổ sung cho G1 do module hóa** — thô, theo tuần-người, phải đối chiếu với ICS: khung module và kiểm CI 2–3 · `cap/import` 1–2 · `cap/workflow` tối thiểu 2–3 · làm `cap/forms` thành module thêm 20–30% trên công sức vốn có · hợp nhất `dom/cooperative` 3–5 tùy chất lượng dữ liệu · `k/ref` và vai DB theo đơn vị triển khai 1–2.

## B. ADR phải viết trước G1

1. Kiến trúc bốn tầng module, tệp khai báo, ứng dụng là tổ hợp
2. Module gọi nhau trong tiến trình; không microservice theo module
3. Ngôn ngữ Go; bốn ngoại lệ (Python, Go biên, Java SPI)
4. RabbitMQ thay hàng đợi trong PostgreSQL
5. Mức event sourcing B
6. Mẫu bảng sổ: khóa chính có cột phân vùng, `chain_head`, idempotency riêng
7. Một vai đăng nhập mỗi đơn vị triển khai, `SET LOCAL ROLE` theo module
8. `k/ref`: bản chiếu dữ liệu lõi trong mỗi cụm, độ trễ có giới hạn
9. Đọc tại chỗ có cấp phép thay sao chép trong vùng nền tảng
10. Vai `r_agg` BYPASSRLS có kiểm soát
11. Đơn vị hành chính có hiệu lực theo thời gian và bảng kế thừa
12. `vault` cụm riêng từ ĐTK 1
13. TimescaleDB chỉ ở vùng nội bộ một đơn vị
14. Tách xác thực và phân quyền; không impersonation — SAD định danh
15. **v4.1** — MKG kết nối qua cổng tích hợp như mọi đối tác (R-INT-01)
16. **v4.1** — Luồng song phương: loại luồng thứ tư, sửa ADR-0020
17. **v4.1** — Loại cấp phép tổng hợp theo xã có ngưỡng k
18. **v4.1** — Feed theo cursor + webhook mỏng; không đẩy dữ liệu qua webhook
19. **v4.1** — Cả ba chế độ engine ở vùng nội bộ; `lib/optcore` dùng chung với `dom/hubsim`
20. **v4.1** — OR-Tools theo quy trình giấy phép

## C. Câu hỏi còn mở

| # | Câu | Ai | Hạn | Chặn |
|---|---|---|---|---|
| C-1 | Engine đồng bộ có tạo slot với `failover = true` không | Hỏi nhà cung cấp | Trước kiểm chứng đồng bộ | `cap/sync` |
| C-2 | Danh mục trường bằng chứng MRV | Thương mại | Trước khi thu dữ liệu ở ĐTK 1 | `dom/mrv` |
| C-3 | Cấp độ an toàn thông tin; khởi động hồ sơ đề xuất cấp độ | Pháp chế + ATTT | Ngay | Vận hành dữ liệu thật |
| C-4 | Ngưỡng gom nhóm k | MKG | G2 | R-DB-11 |
| C-5 | Bảng dung sai cân, trạng thái độ ẩm khi cân | Thương mại, chốt với khách | G2 | `mkg/measurement` |
| C-6 | Đường nhận số liệu cân của khách | Thương mại | G3 | `mkg/measurement` |
| C-7 | Mẫu cân và giao thức — **có thời gian dẫn phần cứng** | Vận hành | Q1/2027 | Agent biên |
| C-8 | Hai người giữ tài khoản khẩn cấp; hai người Quản trị nền tảng, Quản trị an ninh | MKG | **Trước G0** | Khởi tạo lớp định danh |
| C-9 | **Nguồn chính thức cho danh mục đơn vị hành chính sau sắp xếp 2025 và bảng kế thừa** | MKG + TTKNQG | Trước G1 | `k/orgtree`, mọi báo cáo theo địa bàn |
| C-10 | **Thỏa thuận chia sẻ dữ liệu mẫu** giữa HTX và cơ quan khuyến nông — căn cứ của `share_grant` | MKG + pháp chế | G1 cho chương trình khuyến nông; G2 cho HTX | Mục 17.3 |
| C-12 | **v4.1** — Luồng song phương có được chấp nhận không; nếu không, Kênh nông hộ bỏ "khoản thanh toán của hộ" | Product owner + pháp chế | G2 | Mục 38.2; BRD SSO IV.C.6 |
| C-13 | **v4.1** — Danh sách đối tác dự kiến 2027–2029 và loại kênh của từng bên | Thương mại | G3 | Kích thước kiểm thử tải cổng — mục 42 |
| C-11 | Người sở hữu từng module — đề xuất: một PO cho `dom/extension` + `cap/forms`; một PO cho `dom/cooperative` + `dom/mechanization`; một PO cho các module năng lực còn lại | MKG | G1 | Module không chủ |

## D. Ánh xạ quy tắc từ tài liệu cũ

| Nguồn | Quy tắc cũ | v4.0 |
|---|---|---|
| v3.0 | R-01 | R-ARC-01 |
| v3.0 | R-02 … R-07 | R-MOD-01, R-MOD-02, R-DB-07; R-07 (không join chuỗi thời gian) → mục 21 |
| v3.0 | R-10 … R-16 | R-CODE-01 … R-CODE-06, R-API-05 |
| v3.0 | R-20 … R-25 | R-DB-06, R-DB-12 — **mẫu DDL sửa theo DB-01, DB-03** |
| v3.0 | R-30 … R-33 | R-DB-08; `WithTenantTx` → `WithContextTx` có `SET LOCAL ROLE` (R-DB-02) |
| v3.0 | R-40 … R-42 | Mục 20; SAD định danh |
| v3.0 | R-50 … R-56 | R-API-01 … R-API-06; R-56 phân tách nhiệm vụ → `cap/workflow` + SAD |
| v3.0 | R-60 … R-64 | R-EVT-01 … R-EVT-04 |
| v3.0 | R-70, R-71 | Mục 27.2 `mkg/measurement`, mục 21 |
| v3.0 | R-80 … R-82 | R-SEC-01, R-SEC-02 |
| v3.0 | R-90 … R-92, R-95 | Mục 31, R-OPS-01 |
| SAD định danh v1.1 | R-ID-01 … R-ID-46 | **Giữ nguyên**, là đặc tả của `k/access` |
| SAD định danh v1.1 | Mục 6.3 — chia sẻ bằng sao chép | **Thay** bằng mục 17.3 |
| SAD định danh v1.1 | Mục 7.1 — DDL `assignment_event` | **Sửa** theo mẫu 15.1 (DB-01) |
| v3.0 R-31 và SAD định danh 6.2 | "Thiếu biến → NULL → chính sách trả rỗng" | **Sai trên kết nối tái sử dụng** — thay bằng `k_ref.ctx_uuid()` (DB-13) |
| SAD định danh 6.2 | Chính sách dùng `pf_access.in_node_scope()` | **Thay** bằng bảng chiếu `k_ref.node_closure` cho phạm vi trong đơn vị và `k_ref.share_scope` cho chia sẻ (DB-14) |
| Đề xuất module hóa v1.0 | R-MOD-01 … R-MOD-10 | R-MOD-01 … R-MOD-10; R-MOD-01 thêm ngoại lệ quan hệ công bố của lõi |
| v4.0 | `dom/hubsim`, `dom/waterway` | Trong repo, theo bản review 03/10: `pf/simulation`, `waterway_routes` (pf 000046) |
| v4.0 | `cap/*`, `dom/*`, R-MOD-10 | Chưa có trong repo — repo dùng `pf/*`, `mkg/*` theo v3.0. Đưa v4.x vào `docs/` qua ADR |
| Vùng nội bộ MKG v1.3 | Toàn bộ | Tóm tắt ở 27.2; cổng nhận dữ liệu VFT dùng chung mã cổng tích hợp — 38.3 |
