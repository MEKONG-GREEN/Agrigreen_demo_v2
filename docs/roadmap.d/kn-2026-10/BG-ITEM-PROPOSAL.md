# BG-ITEM-PROPOSAL — đường vào và nghiệm thu đề xuất mặt hàng (#37)

- [x] Đối chiếu BRD Báo giá v1.3 UR-XA-06, FN-XA-03.01, FN-AD-04.03 AC05 và prototype A. W11.73/W11.74 đã ở dev; tái sử dụng sổ/API/lịch hiện hành.
- [x] Nguồn menu Price bật danh mục Admin/Tỉnh, thêm Đề xuất riêng Tỉnh/Xã; route mới mở tab đề xuất qua seam tối thiểu. Không refactor form thuộc #182.
- [x] Menu test tái hiện lỗi 2/2 trước sửa và đạt 2/2 sau sửa; route/quyền Vitest 3/3 và hồi quy ItemsScreen 6/6 đạt.
- [x] Native HTTP/PostgreSQL/race (2 subcase đạt, không SKIP, cụm tạm đã dọn): own history, tenant/point/QTNV, quyết định lặp, từ chối có lý do, ngày báo giá kế tiếp qua cuối tuần/lễ/bổ sung và lịch sử trước hiệu lực.
- [ ] Browser E2E qua menu Tỉnh/Xã → gửi → duyệt/từ chối → lịch sử riêng, controlled mutation và toàn bộ cổng.
- [ ] Sau #33 → #31 → #19 đã gộp: lấy dev mới, merge --no-ff, chốt phiên bản menu lớn hơn dev thực tế, kiểm toàn nguồn kết hợp rồi READY cho Claude. Không tự gộp.

Giả định/phạm vi: [BG-ITEM-PROPOSAL](../../kn/gia-dinh/BG-ITEM-PROPOSAL.md). Refactor Dialog → DetailPage/Log giữ ở #182; không thêm notification, ledger, migration hay ghi HTX.
