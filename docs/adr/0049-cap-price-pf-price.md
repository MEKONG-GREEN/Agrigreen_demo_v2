# ADR-0049: `cap/price` = module năng lực `pf/price` — chuỗi giá có hiệu lực, danh mục nền, công bố là quyền riêng

**Trạng thái:** Đề xuất — khung hiện thực ở W11.71 (10/10/2026); phạm vi G1 theo QĐ-5 mặc định (a), chờ chủ sản phẩm (Q-044) · **Ngày:** 10/10/2026 (gói W11.71) · **Yêu cầu:** kiến trúc v4.1 §26 (`cap/price`), §5.1 (ma trận KN ●, HTX ○, MKG ●, NH ●), R-CODE-01, R-MOD-04, R-MOD-09, R-DB-10, Phụ lục A (G1 cần `cap/price` tối thiểu) · **Nguồn:** BRD Báo giá nông sản v1.3 (07/10/2026), đối chiếu v4.3/MKG v1.4/BRD KN Phụ lục D mục 2.3, BG1 · **Thành phần:** `internal/platform/price`, `cmd/api-platform/assembly/price.go`, `migrations/pf/000138_pf_price.up.sql`, `tools/archlint/nofloat.go` · **Liên quan:** [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0031](0031-vai-tong-hop-r-agg.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), [ADR-0035](0035-cap-phep-tong-hop-theo-xa-nguong-k.md)

## Bối cảnh

- v4.1 §26 định nghĩa `cap/price`: sở hữu chuỗi giá có hiệu lực và ngưỡng; port `Series`, `Publish`; sự kiện `price.published`; điểm mở rộng "Nguồn giá"; công bố là quyền riêng. ADR-0027 bảng mục 2 ghi "Chưa có module".
- BRD Báo giá nông sản v1.3 (Phòng TTTT): 26 xã báo giá, 30–40 mặt hàng/xã, 5 nhóm hàng, 2 nhóm vùng; phiếu xã hằng ngày khóa 12:00, cảnh báo biến động, duyệt/công bố bảng giá ngày có phiên bản, điều tra giá thành theo vụ (phiếu có PII nông hộ). Không module domain nào khác của repo cần quy trình phiếu xã.
- Bên dùng: KN (ngay), MKG và NH (đọc bảng giá qua cổng tích hợp), HTX (○) — đạt quy tắc ba (R-MOD-09) cho một module năng lực.
- R-CODE-01 nêu đích danh `cap/price` trong danh sách cấm float.

## Quyết định

### 1. Một module năng lực `pf/price`

`pf/price` (`internal/platform/price`, `kind: capability`, `aka: [cap/price]`, `zones: [pf]`, `enable: per_tenant`) sở hữu schema `pf_price` (nội bộ) và `pf_price_pub` (view công bố). Vai nhóm `r_price` (ADR-0028; `app_platform` là thành viên), vai đọc view `r_price_pub` (thành viên: `r_metrics`, `r_agg`). Module khác chỉ import `price/port` (R-02).

Quy trình phiếu xã hằng ngày nằm TRONG `pf/price` như **nguồn giá nội tại** (không module domain nào khác cần nó). Điều tra giá thành là nguồn giá thứ hai, cũng trong `pf/price`; PII nông hộ đi `vault/identity` (chỉ giữ `household_ref`/băm SĐT).

### 2. Cấu hình theo bên dùng (Profile), không so tên ứng dụng

`port.Profile` (tên, phòng ban chủ, vai → cấp TW/TINH/XA, vai quản trị danh mục) khai ở gốc lắp ráp (`cmd/api-platform/assembly/price.go`, KN = `kn`), như `forms.Profile` (R-MOD-04). Các gói sau mở rộng Profile (cờ Quản trị nghiệp vụ, giờ khóa, hạn phản hồi) thay vì rẽ nhánh theo ứng dụng. Phòng TTTT là cấu hình phòng ban của `dom/extension` cho menu; dữ liệu giá do `pf/price` sở hữu.

### 3. Dữ liệu: sổ chỉ ghi thêm, hiệu lực theo ngày báo giá, decimal

- Mọi bảng nghiệp vụ là sổ chỉ ghi thêm (`shared.apply_ledger`), RLS `p_tenant` (`shared.apply_tenant_rls`). Danh mục có phiên bản: mỗi dòng là một phiên bản của một mã (`version = seq` của chuỗi `<loại>:<mã>`), hiệu lực theo `valid_from` (date, giờ VN) — BRD BR-02 "mọi thay đổi áp dụng từ ngày báo giá kế tiếp"; ngưng là phiên bản `retired`, không xóa (BR-04).
- Giá, %, ngưỡng là `numeric` ở DB và decimal ở Go. `internal/platform/price` nằm trong `floatFreeZones` của agnofloat (cấm cả trong kiểm thử).
- Một danh mục mặt hàng duy nhất ở `pf/price`; KHTC và module khác đọc qua `port.Catalog` (đề xuất Q-082).

### 4. Điểm mở rộng cho các gói sau

| Gói | Thêm vào | Ghi chú |
|---|---|---|
| W11.72 xã báo giá + phân công | bảng `pf_price.collection_point` (↔ mã ĐVHC có hiệu lực ADR-0032, nhóm vùng), phân công CTV/Tỉnh theo ngày; `Profile` thêm cờ QTNV | migration riêng, tiền tố `pf_price` |
| W11.73 mặt hàng theo thời kỳ | sổ mặt hàng tham chiếu `unit.code`, `commodity_group.code`; gán xã, bộ theo vụ, "thay thế cho" | dùng mẫu phiên bản + `valid_from` của pf 000138 |
| W11.74 lịch, giờ khóa, ngưỡng | lịch + ngoại lệ, `Profile` thêm giờ khóa/hạn phản hồi, ngưỡng decimal | job khóa ở `cmd/worker` |
| W11.75 phiếu báo giá + xác minh | sổ phiếu/dòng giá (min/max numeric), cảnh báo, hiệu chỉnh/xóa mềm | |
| W11.76 duyệt/công bố | bảng giá ngày có phiên bản, port `Series`, `Publish`, sự kiện `price.published`, view `pf_price_pub.v1_board_daily` (`//ag:pubview`) | công bố là quyền riêng |
| W11.81 website | tập dữ liệu `price_board_daily` qua cổng tích hợp (ADR-0033, feed cursor) | không DB chung |

### 5. Ngưỡng k

Bảng giá xã là giá thị trường, không phải dữ liệu hộ → không áp k. Giá thành điều tra (theo nông hộ) rời tenant phải là tập tổng hợp có k theo `household` (ADR-0035, mặc định `publicdata.k_min` = 10); trong tenant giữ nhãn "Mẫu nhỏ" của BRD (đề xuất Q-083).

## Phương án đã cân nhắc

- **Đặt Báo giá trong `pf/extension` (dom/extension) như một phòng ban.** Loại: v4.1 §26 đã tách `cap/price`; MKG, NH cũng cần chuỗi giá; R-CODE-01 cần ranh giới module để cấm float.
- **Danh mục mặt hàng ở `pf/masterdata`.** Loại ở khung: danh mục giá có quy tắc riêng (hiệu lực theo ngày báo giá, khóa ĐVT khi đã có giá, "thay thế cho") và chủ là Phòng TTTT; `pf/masterdata` giữ tham số chung. Xem lại nếu Q-082 chọn KHTC làm chủ.
- **Một module nguồn giá riêng (domain) cho phiếu xã.** Loại: chỉ một bên dùng, chưa đạt quy tắc ba.

## Hệ quả

- Thêm một schema, hai vai nhóm (`r_price`, `r_price_pub`) — `cmd/migrate bootstrap` phải chạy trước pf 000138.
- KN có route `/v1/price/*`; bật/tắt theo tenant qua cổng module (`enable: per_tenant`, mặc định bật).
- Các gói W11.72–W11.76 song song được trên cùng schema nhờ mỗi gói một migration và bảng riêng; xung đột chỉ ở `port.Profile` và `docs/errors.md`.

## Hiện thực

### W11.71 khung (10/10/2026, pf 000138)

- `internal/platform/price`: `module.yaml` (sinh: `ports: [Catalog]`, `owns_schema: [pf_price, pf_price_pub]`), `port` (Profile, Catalog, PubSchema, `price.profile_not_found`), `domain` (mã, `valid_from` theo ngày VN, 5 nhóm hàng BRD + 8 đơn vị tính mặc định), `store`, `app`, `handler`.
- pf 000138: schema `pf_price`, `pf_price_pub` (chưa view); sổ `pf_price.unit`, `pf_price.commodity_group`; quyền `r_price` SELECT/INSERT + `k_ref`; `r_price_pub` USAGE schema công bố. Xã báo giá KHÔNG có ở khung (W11.72: cần ĐVHC có hiệu lực, không đơn giản).
- HTTP (`contracts/openapi/application.yaml`, mục pf/price): `GET /v1/price/status`, `GET /v1/price/catalog?at=`, `POST /v1/price/catalog/defaults` (Admin KN; chỉ ghi mã chưa có).
- `app/KN/app.yaml` thêm `pf/price`; agnofloat `floatFreeZones` thêm `/internal/platform/price`; `dbadmin` kiểm thử: `laterGroups`, số vai nhóm, `r_price_pub`.
- Giả định (docs/kn/gia-dinh-brd-2026-10.md): QĐ-5 (a), Q-082 (một danh mục ở `cap/price`), mã phòng ban `thong_tin_truyen_thong`, danh sách đơn vị tính mặc định.
