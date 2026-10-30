# BG-ITEM-PROPOSAL — đường vào và nghiệm thu đề xuất mặt hàng (#37)

- [x] Đối chiếu BRD Báo giá v1.3 UR-XA-06, FN-XA-03.01, FN-AD-04.03 AC05 và prototype A. W11.73/W11.74 đã ở dev; tái sử dụng sổ/API/lịch hiện hành.
- [x] Nguồn menu Price bật danh mục Admin/Tỉnh, thêm Đề xuất riêng Tỉnh/Xã; route mới mở tab đề xuất qua seam tối thiểu. Không refactor form thuộc #182.
- [x] Menu test tái hiện lỗi 2/2 trước sửa và đạt 2/2 sau sửa; route/quyền Vitest 3/3 và hồi quy ItemsScreen 6/6 đạt.
- [x] Native HTTP/PostgreSQL/race (2 subcase đạt, không SKIP, cụm tạm đã dọn): own history, tenant/point/QTNV, quyết định lặp, từ chối có lý do, ngày báo giá kế tiếp qua cuối tuần/lễ/bổ sung và lịch sử trước hiệu lực.
- [x] Browser production mock 1/1: fixture Tỉnh tạo qua Admin UI, đăng nhập thật, menu Tỉnh/Xã → gửi → duyệt/từ chối → lịch sử riêng. Server riêng đã dừng sau kiểm tra.
- [x] Controlled mutation nguồn menu Price đỏ khi đề xuất bị đánh dấu Sắp có; khôi phục đúng nội dung xanh 2/2. Fixture menu sinh lại từ Go/PostgreSQL/race đạt; hợp đồng menu web đạt 34/34, giữ kỳ vọng các phòng khác.
- [x] Cổng web checkpoint: full Vitest một worker 119 file/979 test, check:api, TypeScript, full ESLint, build:mock đạt; ESLint hai tệp test chỉnh sau cùng đạt. Chỉ lưu commit local, cổng toàn nguồn kết hợp vẫn chờ bên dưới.
- [ ] Sau #33 → #31 → #19 đã gộp: lấy dev mới, merge --no-ff, chốt phiên bản menu lớn hơn dev thực tế, kiểm toàn nguồn kết hợp rồi READY cho Claude. Không tự gộp.

Giả định/phạm vi: [BG-ITEM-PROPOSAL](../../kn/gia-dinh/BG-ITEM-PROPOSAL.md). Refactor Dialog → DetailPage/Log giữ ở #182; không thêm notification, ledger, migration hay ghi HTX.
