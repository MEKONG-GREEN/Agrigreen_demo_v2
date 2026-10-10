# Mã phòng ban KN và khuôn menu `kn.menu/v3` (W11.06)

Tài liệu tham chiếu cho các gói KN sau (W11.07 phân quyền trong vai, W11.2x HCTH, W11.4x CNTS, W11.6x KHTC,
W11.7x Báo giá). Mã phòng ban là **định danh ổn định**: mọi bản ghi nghiệp vụ KN mang `department_code` là
một trong các mã dưới đây. Tên hiển thị có thể đổi (Admin sửa hoặc khách chốt Q-051), mã thì không đổi.

Nguồn trong mã:

- Hằng số mã: `internal/platform/party/port/kn.go` (`port.Dept*`, `port.MenuProfile*`).
- Dữ liệu nền: `internal/platform/party/seeddata/departments_an_giang.json`. Nạp bằng
  `POST /v1/orgs/{ref}/departments/seed` (Admin), chạy lại an toàn.
- Khuôn menu: `internal/platform/party/domain/menu.go` (Trồng trọt, khung chung) và
  `internal/platform/party/domain/menu_departments.go` (khuôn riêng, nhóm "Dùng chung").

## 1. Năm phòng ban của TTKN An Giang

| Thứ tự | Mã (`department_code`) | Tên hiển thị (seed) | Trạng thái seed | `menu_profile` | BRD |
|---|---|---|---|---|---|
| 10 | `trong_trot` | Phòng Trồng trọt | Đang áp dụng (`active`) | `trong_trot` | Trồng trọt Final (v7.7) |
| 20 | `chan_nuoi_thuy_san` | Phòng Chăn nuôi - Thủy sản | Chưa mở (`not_open`) | `chan_nuoi_thuy_san` | CNTS v3.7 |
| 30 | `ke_hoach_tai_chinh` | Phòng Kế hoạch - Tài chính | Chưa mở | `ke_hoach_tai_chinh` | KHTC v2.2 |
| 40 | `thong_tin_truyen_thong` | Phòng Thông tin Tuyên truyền | Chưa mở | `thong_tin_truyen_thong` | Báo giá nông sản v1.3 |
| 50 | `hanh_chinh_tong_hop` | Phòng Hành chính - Tổng hợp | Chưa mở | `hanh_chinh_tong_hop` | HCTH v3.0 |

- Seed chỉ đặt trạng thái ban đầu. Admin mở phòng ban (FN-03 AC-01: "Đang áp dụng" → hiện ngay ở bước 2
  đăng nhập) khi module của phòng sẵn sàng, không cần phát hành lại.
- `thong_tin_truyen_thong`: mã giữ từ seed v7.7 để không gãy tham chiếu cũ. Tên đổi theo BRD Báo giá/KHTC
  ("Thông tin Tuyên truyền"); chờ khách xác nhận (Q-051). Đơn vị đã nạp seed cũ giữ tên cũ cho tới khi
  Admin sửa.

## 2. Mã tạm cũ (Q-02 → Q-051)

| Mã tạm | Kế nhiệm | Xử lý |
|---|---|---|
| `phong_ban_5` | `hanh_chinh_tong_hop` | Bí danh: tra mã cũ khi dòng cũ đã xóa (hoặc chưa từng có) trả phòng ban HCTH |
| `phong_ban_6` | (không có) | Bỏ: BRD chỉ nêu tên 5 phòng ban. Tra mã cũ → không tìm thấy |

- `port.CanonicalDepartmentCode(code)` trả mã kế nhiệm. Tra cứu dùng **mã gốc trước**: đơn vị chưa nạp lại
  seed vẫn có dòng `phong_ban_5`, khi đó dòng đó được dùng.
- Nơi áp dụng bí danh: `party` `KN.DepartmentByCode` (cổng `port.Departments`, `accounts` dùng khi dựng ngữ
  cảnh phiên và kiểm phòng ban của gán vai), `GET /v1/menu?department=`, `GET /v1/departments/{code}/modules`.
- Nạp lại seed trên đơn vị đã có `phong_ban_5/6`: dòng mã tạm **chưa áp dụng, không có tài khoản/dữ liệu**
  (nguồn đếm đã nối) bị xóa bằng sự kiện `retired` (lý do "W11.06 (Q-051)…"); ngược lại giữ nguyên và trả
  trong `legacy_kept` để Admin xử lý. Không migration, không sửa dữ liệu bằng SQL.
- Bản ghi nghiệp vụ cũ mang `department_code = phong_ban_5` (nếu có) giữ nguyên mã. Gói đọc theo phòng ban
  nên chuẩn hóa qua `port.CanonicalDepartmentCode` khi lọc.

## 3. Khuôn menu `kn.menu/v3`

Thứ tự nhóm theo D3 C6: **Điều hành / Nghiệp vụ / Báo cáo / (Dữ liệu) / Dùng chung**.

| Khuôn | Nội dung |
|---|---|
| `trong_trot` | Như v2 (Điều hành / Báo cáo / Dữ liệu), thêm nhóm "Dùng chung" cuối |
| `generic` | Khung báo cáo theo kỳ (Phát hành / Trạng thái nộp / Rà soát / Báo cáo theo kỳ) + "Dùng chung" |
| `chan_nuoi_thuy_san`, `ke_hoach_tai_chinh`, `thong_tin_truyen_thong`, `hanh_chinh_tong_hop` | Khung `generic` + nhóm "Nghiệp vụ" (sau "Điều hành") theo bảng "Menu theo vai trò" §VI.3 của BRD từng phòng + "Dùng chung" |

- Phòng ban có `menu_profile = generic` mà mã nằm trong bảng trên dùng khuôn riêng
  (`domain.EffectiveMenuProfile`) — đơn vị nạp seed v7.7 nhận menu v3 không cần sửa dữ liệu.
- **Mục của module chưa có hiện "Sắp có"** (chọn thay cho "ẩn sau cờ"): `MenuItem.ComingSoon` → trên dây
  `enabled: false, status: "coming_soon", status_label: "Sắp có"`. Web-ops vẽ mờ, nhãn "Sắp có", không thành
  liên kết (cũng không vào được bằng URL vì `menuLinks` bỏ qua). App di động hiện mờ với chip "Sắp có".
  Lý do: người dùng thấy lộ trình; khi module tới, gói của module chỉ bỏ `ComingSoon` (và thêm ánh xạ
  `menuModules` nếu là module `per_tenant`), không cần cờ.
- Nhóm "Dùng chung" (`dung_chung`) cuối menu **mọi** đối tượng, kể cả Admin: hiện có `chung.contact`
  (HCTH FN-13, "Sắp có", đường dẫn `/kn/dung-chung/contact`).
- Admin › Phòng ban › <phòng>: mục chung (Theo dõi nộp, Tổng hợp, Dữ liệu báo cáo, Danh mục, Mẫu báo cáo)
  + mục "Sắp có" của phòng (Danh mục CNTS; Kế hoạch năm; Xã báo giá & mặt hàng; Phân quyền hồ sơ, Thư mục
  tài liệu).
- Tiền tố đường dẫn màn của từng phòng (dành cho gói module): `/kn/cnts/…`, `/kn/khtc/…`, `/kn/bao-gia/…`,
  `/kn/nhan-su/…`, `/kn/dung-chung/…`. Khóa mục: `<tw|tinh|xa>.<cnts|khtc|bao_gia|hcth>.<màn>`.
- Đổi bảng menu: sửa Go, rồi `AG_UPDATE_WEB_FIXTURES=1 go test ./internal/platform/party/handler -run
  TestWebMenuFixture` (ca vàng `apps/web-ops/src/api/mock/fixtures/kn-menu.json` có ca cho từng phòng ban ×
  đối tượng), và sửa bản giả lập `apps/web-ops/src/api/mock/kn.ts` cho khớp (Vitest `kn-menu.contract.test.ts`).

## 4. Ghi chú cho W11.07

- Phân quyền trong vai (cấp quản lý, nhóm Cấp Tỉnh, cờ QTNV báo giá) lọc **mục** trong khuôn trên; khóa mục
  ổn định ở §3 là điểm móc. Ví dụ "Cấu hình" Báo giá chỉ hiện với cán bộ QTNV; "Duyệt báo cáo" CNTS chỉ
  hiện với lãnh đạo phòng.
- Q-067 (đề xuất: menu "Nhân sự của phòng" hiện theo cấp quản lý ở mọi phòng ban) cũng thêm mục vào khuôn,
  không cần phòng ban mới.
- Q-068 (đơn vị nội bộ phòng/trạm/trại) — nếu chọn "mở rộng `department` với loại `tram`/`trai`", mã mới
  không được trùng 5 mã trên và không dùng lại `phong_ban_5/6`.
