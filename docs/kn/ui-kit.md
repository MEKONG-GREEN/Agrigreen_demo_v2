# UI-KIT cho màn KN (BF-UI-KIT, 23/10/2026)

Các component dùng chung để sửa các lỗi chung trong log bug của khách (PRD `kn-bugfix-brd-alignment`, nhóm A). Màn mới và các gói sweep (#100 TT, #101 CNTS, #117 HCTH/KHTC/Báo giá) **phải** dùng các component dưới đây, không tự viết component riêng.

Nguồn thiết kế: System Design `Design_Sample_Screens.pdf` (Product team/System Design).

| Yêu cầu | Dùng | Ở đâu |
|---|---|---|
| R-A1 không nhận số âm | `NumberField`, `parseNumberInput` | `apps/web-ops/src/components/form.tsx` |
| R-A4 trường bắt buộc `*` + lỗi theo trường | `TextField`, `SelectField`, `NumberField`, `validateForm` | `components/form.tsx` |
| R-A2 Thêm / Sửa / Xem trên trang riêng | `DetailPage` (+ `LogPanel`) | `components/patterns/detail.tsx` |
| R-A5 STT do hệ thống đánh | `DataTable` (bỏ `onPage` nếu không phân trang) | `components/patterns/list.tsx` |
| R-A6 sidebar 3 nhóm, nhiều cấp | `Sidebar` hiển thị menu từ `GET /v1/menu`, mục cha của mục đang chọn tự mở, hỗ trợ menu 3 cấp | `shell/Sidebar.tsx`, `shell/menu.ts` |
| R-A3 Noto Serif cho tiêu đề, Roboto cho phần còn lại | Đã có sẵn ở `app/globals.css` (h1–h3). Tiêu đề thẻ/khối nào không dùng h1–h3 thì đổi sang h2/h3 | `app/globals.css` |

## Quy tắc
- **Số:**
  - Mọi ô số dùng `NumberField`. Mặc định không nhận số âm.
  - Thêm `integer` cho số lượng và năm; `positive` khi phải > 0 (số lượng máy, diện tích vùng dịch); `max` cho năm sản xuất (`new Date().getFullYear()`).
  - `allowNegative` chỉ dùng khi nghiệp vụ có nghĩa âm, và phải ghi lý do trong comment.
  - Giá trị gửi máy chủ là chuỗi đã chuẩn hóa từ `validateForm(...).values`. **Máy chủ vẫn phải kiểm lại** (BF-SRV-GUARD #113 cho các module nền).
- **Lưu:** khi bấm Lưu, gọi `validateForm`. Nếu `valid === false` thì hiện `errors[k]` qua prop `error` của từng trường và **không gọi API**. Lỗi máy chủ trả về theo trường cũng đưa vào `error`.
- **Pop-up:** `Dialog` chỉ còn dùng cho xác nhận xóa, nhập lý do ngắn và thông báo. Thêm, sửa và xem chi tiết mở route riêng (`…/them-moi`, `…/[id]`, `…/[id]/sua`), dựng bằng `DetailPage`. Nút **Hủy** quay về danh sách, nút **Lưu** là nút chính.
- **Bảng:** dùng `DataTable` thay cho `<table className="dt">` tự viết. Bảng nhập kiểu Excel (`kn/grid/FormGrid`) giữ nguyên như hiện tại.
- **Menu:**
  - Menu nằm ở JSON trong module (`internal/kn/*/menus/*.json`), sinh lại theo `rebase-merge.ps1`.
  - Thứ tự nhóm: Điều hành / Báo cáo / Dữ liệu (+ Dùng chung).
  - Mục cha có thể lồng thêm một cấp, ví dụ menu TT tỉnh: Báo cáo tổng hợp các xã › Báo cáo tuần › T01.
