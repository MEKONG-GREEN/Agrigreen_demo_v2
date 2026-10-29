## BF-SWEEP-HCTH-REST — phần đầu #163: mục danh mục nhân sự FN-19

Ngày 29/10/2026. Phần này hoàn tất luồng danh sách → thêm/sửa → chi tiết và lịch sử cho 16 loại mục FN-19, cả route chính `/kn/nhan-su/danh-muc` lẫn alias `/kn/phong-ban/[dept]/danh-muc-nhan-su`. Mục Thêm/Sửa/Lịch sử không mở popup; trang dùng DetailPage/LogPanel, bảng dùng DataTable có STT. Quyền thêm/sửa lấy từ `GET /v1/hrm/status.can_manage`; khi chưa rõ quyền hoặc chỉ xem thì không mở form ghi. API và kiểm quyền mỗi request tại máy chủ giữ nguyên.

Nguồn: BRD HCTH v3.0 (FN-19 P2058–2097; SHA-256 `1ed44aa50d2f797422ef63b32948f24c4f3856f0419814cf274cb64ac6f2db66`), hiện nằm trong thư mục `01. BRD/V1.0/`; PRD `kn-bugfix-brd-alignment` R-E1/R-A1–A5; [UI-KIT](../ui-kit.md); [ma trận BRD](../doi-chieu-brd-bugfix-2026-10.md). Không tự chuyển phạm vi sang BRD HCTH v4.0.

- Ba ô số dùng NumberField, validateForm và lỗi từng ô: thứ tự 0–100000, thứ hạng nguyên 1–100, hệ số phụ cấp tùy chọn >0 đến 10, tối đa hai chữ số thập phân, theo domain HRM hiện hành. Chặn cú pháp số mũ và chuỗi quá 32 ký tự trước chuẩn hóa/so sánh/chuyển Number. Không lọc mất dấu âm để biến dữ liệu sai thành số đúng.
- Ngoại lệ file chung được điều phối xác nhận: UI-KIT `components/form.tsx` thêm `maxInputLength` opt-in (chỉ ba ô FN-19 dùng 32) và chặn số mũ trước Decimal.parse; test shared form chứng minh parser không được gọi cho dữ liệu hostile. Không sửa client multipart của #172.
- Giữ mã bất biến khi sửa, trạng thái ngừng sử dụng, thuộc tính riêng/nhóm chuyên ngành NS05, nhãn Mẫu/Q-070 và phiên bản cũ. `useAction` giữ Idempotency-Key khi thử lại cùng thao tác; lỗi máy chủ theo trường được gắn vào ô.
- API history chỉ có phiên bản và thời điểm, chưa có tên người thực hiện. Log ghi rõ thiếu thông tin này, không suy đoán người thực hiện từ phiên hiện tại. Không thêm API hoặc PII mới.
- Không thay nghiệp vụ, mẫu in, seeded KHTC, M1–M7, menu, baseline, Go hay migration. Giữ các câu hỏi Q-005/Q-070 và giới hạn W11.21. Không có quyết định PO mới.

**Chưa hoàn tất toàn bộ #163/HCTH.** Bảng hệ số/quy tắc FN-19 và StaffScreens, PromotionScreens, PlanScreens, ReportScreens, DecisionScreens, DocScreens chuyển sang [#179 BF-SWEEP-HCTH-FORMS](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/179), phụ thuộc phần đầu này. Các popup và bảng còn lại của các màn đó chưa được tính đạt nhóm A. E2E Thông tin làm việc được sửa expectation cũ để theo route FN-08 đã gộp ở #117.
