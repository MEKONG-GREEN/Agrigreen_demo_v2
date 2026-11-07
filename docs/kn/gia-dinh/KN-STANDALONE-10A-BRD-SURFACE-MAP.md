## KN-STANDALONE-10A-BRD-SURFACE-MAP — Bản đồ bề mặt ở chế độ báo cáo

- `docs/kn/brd-surface.yaml` là danh sách phân loại được duy trì bằng tay. Namespace `tt`, `cnts`, `hcth`, `khtc`, `price` phân biệt mã FN trùng nhau giữa năm module. Catalogue lần lượt dùng phiên bản 7.7, 4.0, 4.0, 2.3, 2.0; chứa 113 mã FN hiện hành.
- Mỗi mục có `kind`, `keys`, `status`. `FN` cần `fn`; `HẠ_TẦNG` cần `needs` trỏ FN sử dụng nó; `GỠ` cần `package`; `HOÃN` cần `package` và `fn`. Tham chiếu phải tồn tại trong catalogue. Không thêm nội dung BRD hoặc dữ liệu người dùng vào tệp này.
- Menu dùng `<phòng>:<khóa>`, hoặc `admin/<phòng>:<khóa>` với mục Admin trỏ route của phòng. Nút nhóm và mục tắt/điều kiện cũng được kiểm kê. Cùng khóa Trồng trọt có thể là `GỠ` ở phòng khác. Menu không có wildcard.
- Web liệt kê chính xác `page.tsx` và route handler KN `route.ts`, giữ nguyên đoạn động như `[id]`. Trang ngoài `/kn` được kiểm kê để theo dõi thu hẹp phiên/bản build ở các gói sau. Màn mobile lấy tên lớp công khai `*Screen` trong field-app và thư viện mobile dùng chung.
- API dùng gốc tài nguyên với hậu tố `/**`: khớp chính gốc và các đoạn con; `/plots/**` không khớp `/plots-extra`. Go đăng ký thật Application/Admin API của bốn module KN bằng gin, không gửi request. API hạ tầng/nguồn ngoài được liệt kê riêng cùng định danh feed `price_board_daily`; chúng thuộc phạm vi thủ công của bản đồ và có thể không xuất hiện trong registry bốn module KN. Không dùng wildcard bao trùm `/v1/**`.
- Go kiểm menu, registry API, web, mobile; TS kiểm cây web, fixture menu và mobile, cùng đọc/kiểm schema của một tệp YAML. Hai bộ ghi `missing`, `unused` (không thấy trong collector đang chạy), `ambiguous`. `unused` của Go có thể là API phụ trợ do module khác đăng ký hoặc feed; không suy ra đã bỏ dùng. Chênh lệch không làm test đỏ. YAML sai cú pháp, sai schema, trùng khóa hoặc tham chiếu FN không tồn tại vẫn làm test đỏ.
- Gói này không thay đổi route, menu, quyền, dữ liệu hoặc trạng thái kết nối. Nhãn FN chỉ phân loại phạm vi, không chứng nhận FN đã triển khai/đạt nghiệm thu. Các gói gỡ, thay nguồn, hoãn và #272 xử lý hành vi/chế độ chặn. Mỗi gói sau cần cập nhật các dòng liên quan.
- Không dùng ngày hiện tại trong test; đồng hồ constructor đăng ký route cố định. Không cần migration, sinh API hoặc module registry.

Chạy lại từ gốc repo sau khi đặt toolchain theo `go.mod` và biến môi trường tạm theo hướng dẫn phát triển:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./tests/knsurface ./internal/kn/extension/menu/...
cd apps/web-ops
npm ci --no-audit --no-fund
npx vitest run src/kn/brd-surface.test.ts --reporter verbose
```

Kiểm thử có ca sai YAML/schema/FN, mục mới chưa ánh xạ, mục không còn thấy, hai ánh xạ chồng nhau, ranh giới tiền tố API và khóa menu giống nhau ở hai phòng. Thử sửa bộ so khớp để `/plots-extra` khớp `/plots/**` phải làm ca ranh giới đỏ; khôi phục rồi chạy lại phải xanh.
