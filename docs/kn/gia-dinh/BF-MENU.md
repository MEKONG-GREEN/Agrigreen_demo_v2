## BF-MENU — Menu theo Update prototype và hình khách gửi (#98)

Issue #98, xếp chồng trên #76 (`knsplit2-menu-kn`). Nguồn: PRD `kn-bugfix-brd-alignment` mục F (UP-01, UP-02, UP-03, UP-10), CN-13, R-A6; hình menu trong Update prototype v1 (TT xã, CNTS xã), v2 (TT tỉnh) và bản chép hình CNTS tỉnh 23/10 trong #98. Menu phiên bản `kn.menu/v6`.

| Mục | Quyết định | Bằng chứng |
|---|---|---|
| Ba nhóm | Mọi phòng ban, mọi cấp: Điều hành / Báo cáo / Dữ liệu (+ Dùng chung; Admin giữ Điều hành / Phòng ban / Quản trị hệ thống / Dùng chung). Không còn nhóm "Nghiệp vụ" | `kn-menu.contract.test.ts` ca "mọi phòng ban" |
| Cơ chế | Bảng mới `deptMenus` (khuôn × đối tượng → cây đủ nhóm). Có thì thay khung chung + "Nghiệp vụ"; không có thì giữ cách ghép cũ. `deptBusiness` còn là bảng rỗng (đường dự phòng cho khuôn mới). Trồng trọt vẫn dùng `trongTrotMenus` | `internal/kn/extension/menus/base.json`, `internal/kn/hrm/menus/base.json` |
| Sửa ngoài làn KN | Chỉ mức tối thiểu để cây compatibility (đang chạy thật tới #53) dựng được `deptMenus`: `party/domain/menu_departments.go` (biến `deptMenus` + 4 dòng trong `withBusiness`), `menu.go` (`MenuVersion` v6). Còn lại ở party là sinh lại (`menu_departments.go` vùng GENERATED, `kn_test.go` `implementedMenu`) và cập nhật kỳ vọng do đổi JSON (`kn_test.go`, `menu_w1107_test.go`, `app/kn_menu_test.go`, `handler/kn_integration_test.go`). Builder KN (#76) đổi cùng chỗ | parity 18 ca vàng + ảnh chụp compatibility sinh lại |
| Nguồn gộp | `cnts-model.json` bỏ (mục đã vào cây CNTS trong `base.json`); `khtc-report.json` chỉ còn mục Admin; HCTH chuyển sang `deptMenus` trong module hrm | `node scripts/gen-module-menus.mjs --check` |
| Bộ sinh | `deptMenus` là bảng hợp lệ; khuôn phải có `deptBusiness` hoặc `deptMenus`; mục sẵn sàng trong `deptMenus` phải có trang web; đối tượng nhóm (có `Items`) không tính là mục; xuất `DEPT_MENUS` cho bản giả lập | `scripts/module-registries.test.mjs` |
| Đường dẫn | Dùng lại route của màn có sẵn. Route menu không cho `?` (`partyport.validMenuPath`) nên không gắn mã biểu bằng query; nhiều mục trỏ cùng một màn (vd T01–T07). Khi màn nhận mã biểu qua đường dẫn thì đổi route từng mục | bảng dưới |
| Sắp có | Mục chưa có màn dùng `ComingSoon` (hiện "Sắp có", không bấm được), không có liên kết chết | bảng dưới |
| 3 cấp | Dữ liệu TT tỉnh có 3 cấp (Báo cáo tổng hợp các xã › Báo cáo tuần › T01…). Backend (`partyport.Menus`, độ sâu ≤ 12), `menuAdapter` (đệ quy) nhận. Sidebar và `menuLinks` trên nhánh nền chỉ vẽ 2 cấp: cấp 3 hiện mờ, Báo cáo tháng / năm vẫn bấm được. Chạy đủ khi #97 (`bf-ui-kit`) gộp | `shell.test.tsx` |
| Trang đích | CNTS xã không còn "Công việc cần làm" của Trồng trọt → thêm `contextDefaultRoutes.chan_nuoi_thuy_san.kn_xa = /kn/cnts/bao-cao-ky` (Việc cần làm) trong `navigation.json`. CNTS tỉnh giữ chính sách KHTC-DEFAULT-NAV (`/kn/bao-cao-khtc`). Bộ dựng thử của `kn_integration_test.go` không nối NavigationPolicy nên vẫn trả trang thô `/kn/nhiem-vu/cong-viec`; assembly thật có nối | `builder_test.go`, `kn-menu.json` |

### Bảng nhãn yêu cầu → route → trạng thái

"Sắp có" = `ComingSoon`. Mục "Báo cáo đột xuất" (chỉ hiện khi phòng có mẫu đột xuất), "Báo cáo cho KHTC" (cuối nhóm Báo cáo của Cấp Tỉnh phòng chuyên môn, W11.11) và nhóm "Dùng chung" giữ như trước, không có trong hình.

| Vai | Nhóm | Nhãn | Route | Trạng thái |
|---|---|---|---|---|
| TT xã (UP-01) | Điều hành | Nhiệm vụ › Công việc cần làm | `/kn/nhiem-vu/cong-viec` (badge `report_tasks.todo`) | Có màn |
| TT xã | Điều hành | Nhiệm vụ › Lịch công tác | `/kn/nhiem-vu/lich-cong-tac` | Có màn |
| TT xã | Báo cáo | Báo cáo tuần › T01 · Tiến độ XG … T07 · Thu hoạch (7 mục) | `/kn/bao-cao/tuan` | Có màn (chọn biểu trong màn) |
| TT xã | Báo cáo | Báo cáo tháng | `/kn/bao-cao/thang` | Có màn |
| TT xã | Báo cáo | Báo cáo hàng vụ › V01 · 3G3T … V06 · Mô hình (6 mục) | `/kn/bao-cao/vu` | Có màn |
| TT xã | Báo cáo | Báo cáo của xã | `/kn/bao-cao/cua-xa` | Có màn |
| TT xã | Dữ liệu | Thu thập dữ liệu › Danh sách HTX, Máy móc HTX, Vẽ thửa ruộng, Vật tư | `/kn/du-lieu/htx`, `/may-moc`, `/ve-thua`, `/vat-tu` | Có màn |
| TT tỉnh (UP-02) | Điều hành | Dashboard | `/kn/dashboard` | Có màn |
| TT tỉnh | Điều hành | Nhiệm vụ › Phát hành biểu mẫu, Trạng thái nộp, Rà soát & chốt kỳ, Lịch công tác | `/kn/nhiem-vu/phat-hanh`, `/trang-thai-nop`, `/ra-soat`, `/lich-cong-tac` (badge giữ nguyên) | Có màn |
| TT tỉnh | Báo cáo | Báo cáo tổng hợp các xã › Báo cáo tuần › T01, T03–T07 | `/kn/bao-cao/tong-hop` | Có màn (tổng hợp, chọn đợt/biểu trong màn) |
| TT tỉnh | Báo cáo | Báo cáo tổng hợp các xã › Báo cáo tháng; Báo cáo hàng vụ › V01–V06; Báo cáo năm | `/kn/bao-cao/tong-hop` | Có màn |
| TT tỉnh | Báo cáo | Báo cáo Chi cục | `/kn/bao-cao/chi-cuc` | Có màn |
| TT tỉnh | Dữ liệu | Thu thập dữ liệu › Danh sách HTX, Máy móc HTX, Thửa ruộng, Vật tư | `/kn/du-lieu/htx`, `/may-moc`, `/ve-thua`, `/vat-tu` | Có màn |
| CNTS xã (UP-03) | Điều hành | Việc cần làm | `/kn/cnts/bao-cao-ky` | Có màn |
| CNTS xã | Điều hành | Lịch trực KSGM của tôi | `/kn/cnts/lich-truc` | Có màn |
| CNTS xã | Điều hành | Lịch công tác | `/kn/nhiem-vu/lich-cong-tac` | Có màn |
| CNTS xã | Báo cáo | KSGM hằng ngày, Nhập liệu tuần chăn nuôi, Khai thác thủy sản tuần, Báo cáo công việc tuần | `/kn/cnts/bao-cao-ky` (lọc biểu BM-01/04/17/05 trong màn) | Có màn |
| CNTS xã | Báo cáo | Báo cáo dịch bệnh | `/kn/cnts/dich-benh` | Có màn |
| CNTS xã | Dữ liệu | Hộ mô hình | `/kn/cnts/ho-mo-hinh` | Có màn |
| CNTS xã | Dữ liệu | Nhật ký sản xuất › Thủy sản, Chăn nuôi | `/kn/cnts/nhat-ky` (cả hai mục, chọn lứa/biểu BM-08/09/19 trong màn) | Có màn (W11.48) |
| CNTS xã | Dữ liệu | Lịch tiêm phòng | `/kn/cnts/lich-tiem` | Có màn |
| CNTS xã | Dữ liệu | Bản đồ vùng dịch | `/kn/cnts/ban-do-dich` | Có màn (W11.52, màn bản đồ vùng nuôi, vùng dịch; nhãn giữ theo hình) |
| CNTS xã | Dữ liệu | Điểm trình diễn | `/kn/cnts/ho-mo-hinh` (màn hộ mô hình, điểm trình diễn của xã) | Có màn |
| CNTS xã | Dữ liệu | Quy trình, văn bản | `/kn/van-ban` (kho văn bản phòng, chỉ văn bản có đối tượng xã) | Có màn (W11.54) |
| CNTS tỉnh (UP-10) | Điều hành | Trang chủ | `/kn/dashboard` | Có màn |
| CNTS tỉnh | Điều hành | Cảnh báo | `/kn/cnts/canh-bao` | Có màn |
| CNTS tỉnh | Điều hành | Biểu mẫu › Thiết kế biểu mẫu | `/kn/cnts/thiet-ke-bieu-mau` | Sắp có |
| CNTS tỉnh | Điều hành | Biểu mẫu › Phát hành, theo dõi nộp | `/kn/cnts/bieu-mau` | Có màn |
| CNTS tỉnh | Điều hành | Biểu mẫu › Cấu hình nhắc nhở | `/kn/cnts/canh-bao` (tab Cấu hình nhắc nhở) | Có màn |
| CNTS tỉnh | Điều hành | Biểu mẫu › Rà soát nhật ký | `/kn/cnts/ra-soat-nhat-ky` | Có màn (W11.48) |
| CNTS tỉnh | Điều hành | Biểu mẫu › Cấp quyền | `/kn/cnts/cap-quyen` | Có màn |
| CNTS tỉnh | Điều hành | Lịch › Lịch trực KSGM; Lịch tiêm phòng | `/kn/cnts/lich-truc`; `/kn/cnts/lich-tiem` | Có màn |
| CNTS tỉnh | Điều hành | Lịch › Lịch công tác cán bộ xã | `/kn/nhiem-vu/lich-cong-tac` (badge `work_schedules.pending`) | Có màn |
| CNTS tỉnh | Điều hành | Lịch › Lịch công tác phòng | `/kn/nhiem-vu/lich-cong-tac` (tab Lịch của tôi) | Có màn |
| CNTS tỉnh | Điều hành | Chiến dịch điều tra › Quản lý chiến dịch; Tạo chiến dịch | `/kn/cnts/chien-dich`; `/kn/cnts/chien-dich/tao` | Sắp có |
| CNTS tỉnh | Báo cáo | Báo cáo định kỳ | `/kn/cnts/bao-cao-dinh-ky` | Sắp có |
| CNTS tỉnh | Báo cáo | KSGM tuần, tháng; Thủy sản (Phụ lục 2); Báo cáo dịch bệnh | `/kn/cnts/ksgm`; `/kn/cnts/phu-luc-2`; `/kn/cnts/dich-benh` | Có màn |
| CNTS tỉnh | Dữ liệu | Số liệu cấp xã › Nhập liệu tuần; Khai thác thủy sản; Nhật ký sản xuất | `/kn/cnts/so-lieu-xa/nhap-lieu-tuan`, `/khai-thac-thuy-san`, `/nhat-ky` | Sắp có |
| CNTS tỉnh | Dữ liệu | Số liệu cấp xã › Lịch tiêm (thực tế) | `/kn/cnts/lich-tiem` (cột ngày tiêm thực tế) | Có màn |
| CNTS tỉnh | Dữ liệu | Bản đồ vùng dịch | `/kn/cnts/ban-do-dich` | Có màn (W11.52, màn bản đồ vùng nuôi, vùng dịch; nhãn giữ theo hình) |
| CNTS tỉnh | Dữ liệu | Dự án KNQG › Dự án, điểm trình diễn | `/kn/cnts/du-an` | Có màn |
| CNTS tỉnh | Dữ liệu | Dự án KNQG › Mẫu biểu dự án | `/kn/cnts/du-an-mau-bieu` | Sắp có (W11.53) |
| CNTS tỉnh | Dữ liệu | Quy trình, văn bản | `/kn/van-ban` (kho văn bản phòng) | Có màn (W11.54) |
| CNTS TW | Dữ liệu | Dự án KNQG (khung TW chung giữ nguyên) | `/kn/cnts/du-an` | Có màn |
| KHTC tỉnh | Điều hành | khung chung + Kỳ báo cáo & phiếu các phòng; Kế hoạch năm | `/kn/khtc/ky-bao-cao`; `/kn/khtc/ke-hoach-nam` | Có màn |
| KHTC tỉnh | Báo cáo | khung chung + Soạn báo cáo (Mẫu 1–5); Tiến độ các chương trình; Báo cáo đã xuất | `/kn/khtc/bao-cao`; `/tien-do`; `/bao-cao-da-xuat` | Có màn |
| KHTC tỉnh | Báo cáo | Mẫu báo cáo KHTC | `/kn/khtc/mau-bao-cao` | Sắp có |
| KHTC tỉnh | Dữ liệu | Số liệu nguồn ngoài | `/kn/khtc/so-lieu-ngoai` | Sắp có |
| KHTC TW | Báo cáo | khung TW + Báo cáo Tỉnh (KHTC) | `/kn/khtc/bao-cao-da-xuat` | Sắp có |
| Báo giá tỉnh | Điều hành | khung chung + Tiến độ nhập giá; Kiểm duyệt & công bố; Cấu hình báo giá (QTNV) | `/kn/bao-gia/tien-do`; `/kiem-duyet`; `/cau-hinh` | Có màn |
| Báo giá tỉnh | Báo cáo | khung chung + Bảng giá tổng hợp; Báo cáo biến động giá; Dashboard theo vùng; Thống kê phiếu báo giá | `/kn/bao-gia/bang-gia`; `/bien-dong`; `/dashboard-vung`; `/thong-ke` | Có màn |
| Báo giá tỉnh | Dữ liệu | Điều tra giá thành; Mặt hàng | `/kn/bao-gia/dieu-tra`; `/kn/bao-gia/mat-hang` | Sắp có (giữ như trước) |
| Báo giá TW | Báo cáo | khung TW + Bảng giá tổng hợp; Dashboard theo vùng; Báo cáo biến động giá; Giá thành sản xuất (Sắp có) | `/kn/bao-gia/…` | như trước |
| Báo giá xã | Điều hành / Báo cáo / Dữ liệu | khung chung + Nhập báo giá, Yêu cầu xác minh / Bảng giá tổng hợp, Báo cáo biến động giá / Lịch sử báo giá | `/kn/bao-gia/nhap`, `/xac-minh` / `/bang-gia`, `/bien-dong` / `/lich-su` | Có màn |
| HCTH tỉnh | Điều hành | khung chung + Dashboard nhân sự; Danh sách nâng bậc; Quyết định nâng bậc; Đánh giá (Sắp có) | `/kn/nhan-su/dashboard`, `/nang-bac`, `/quyet-dinh-nang-bac`, `/danh-gia` | như trước |
| HCTH tỉnh | Báo cáo | khung chung + Biểu mẫu báo cáo Sở | `/kn/nhan-su/bao-cao-so` | Có màn |
| HCTH tỉnh | Dữ liệu | Hồ sơ nhân sự; Hồ sơ dữ liệu; Danh mục nhân sự | `/kn/nhan-su/ho-so`, `/kho-tai-lieu`, `/danh-muc` | Có màn |
| HCTH xã | Điều hành | khung chung + Đánh giá của tôi | `/kn/nhan-su/danh-gia-cua-toi` | Sắp có |
| Mọi phòng, Cấp Tỉnh (W11.54) | Điều hành | Trang chủ phòng (đầu nhóm) | `/kn/trang-chu` | Có màn |
| Trồng trọt, KHTC, Báo giá, HCTH — Cấp Tỉnh; Trồng trọt, Báo giá, HCTH — Cấp Xã (W11.54) | Dữ liệu | Quy trình, văn bản (cuối nhóm; HCTH xã thêm nhóm Dữ liệu) | `/kn/van-ban` | Có màn |

### Lệch so với hình, cần khách xác nhận

- W11.54 (gộp sau #98): thêm "Trang chủ phòng" đầu Điều hành của Cấp Tỉnh mọi phòng (CNTS tỉnh vẫn giữ "Trang chủ" → Dashboard theo hình, nên có hai mục trang chủ — để mọi màn còn mở được; chờ PO chọn gộp) và "Quy trình, văn bản" cuối nhóm Dữ liệu (W11.54 trước #98 đặt ở Điều hành cho Trồng trọt tỉnh; gộp đưa về Dữ liệu như CNTS). Hình chưa có hai mục này.
- TT tỉnh: hình file v2 không có T02 trong "Báo cáo tuần" (TT xã có T02 · Lúa mùa). Làm đúng hình.
- TT tỉnh: PRD UP-02 ghi "Dữ liệu: Thửa ruộng"; hình có đủ Thu thập dữ liệu (HTX, Máy móc, Thửa ruộng, Vật tư). Làm theo hình.
- Mục cũ không có trong hình nên bỏ khỏi menu (màn vẫn còn, mở qua Admin hoặc đường dẫn): CNTS tỉnh "Danh mục CNTS" (`/kn/cnts/danh-muc`; Admin còn `Danh mục CNTS` theo phòng ban), "Hộ mô hình" (`/kn/cnts/ho-mo-hinh`), "Duyệt báo cáo" (Sắp có); CNTS xã "Cảnh báo" (`/kn/cnts/canh-bao`, xã xử lý cảnh báo được giao), "Quyền của tôi" (`/kn/cnts/cap-quyen`), và khung báo cáo theo kỳ của Trồng trọt mà CNTS xã nhận nhầm trước đây.
- Nhãn T/V theo hình ("T03 · Phân trà", "T04 · Rau màu"…), dấu "·" như bộ UI #97; tên đầy đủ của biểu nằm trong màn.

### Còn mở

- Mỗi mục T0x/V0x/BM chưa mở thẳng đúng biểu: cần màn nhận mã biểu qua đoạn đường dẫn (không dùng query). Khi có thì đổi route từng mục trong JSON và tăng `kn.menu`.
- Hiển thị 3 cấp của TT tỉnh cần #97.
