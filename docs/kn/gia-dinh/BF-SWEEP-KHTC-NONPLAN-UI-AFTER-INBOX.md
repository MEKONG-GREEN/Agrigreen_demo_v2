## BF-SWEEP-KHTC-NONPLAN-UI-AFTER-INBOX — editor phiếu

### Phạm vi và căn cứ

- Lát đầu trọn vẹn của #229, sau #206 (`52189196`) và #226 (`5ac399ec`) thực sự vào dev. Phần còn lại chính thức là #241: rà soát, report/snapshot, kỳ nhúng/Adhoc/confirmation còn lại và public history/audit contract. Không tuyên bố full FN/UAT/G1.
- BRD KHTC v2.3 (08/10/2026), FN-PN-03.02 P1511–1555; ma trận bugfix A1–A4. Giữ mẫu, tab, dòng được giao, cột cố định/công thức/module, phép tính máy chủ và nội dung gửi toàn phiếu.
- Chỉ entry `/kn/khtc/phieu/{periodId}`, thân lưới dùng chung, optional retry key trên API Lưu/Gửi hiện hữu; không sửa reviewer, backend, SQL, schema OpenAPI, menu, scanner/ignore/baseline hoặc gói có chủ.

### Hành vi

- Dùng DetailPage/breadcrumb/quay lại, NumberField và lỗi tiếng Việt liên kết từng ô. Số kiểm độ dài và grammar trước parse: integer tối đa 9 chữ số, decimal 15 + 6, không số mũ; theo cờ non_negative. Điều chỉnh được âm, khác 0 và cần hạng mục/kỳ đã khóa/lý do. Máy chủ vẫn kiểm quyền, phiên bản, công thức và nguồn.
- Phiếu thường chỉ cho phòng Cấp Tỉnh được giao, không read_only, với editable của máy chủ và kỳ chưa terminal. Mỗi API client, actor/tenant/profile/MFA/context/role/phòng/kỳ/query mới tạo lifetime mới; GET/ACK sai tài nguyên bị chặn, late ACK và A→B→A không ghi vào lifetime mới.
- Đang gửi hoặc chưa xác định kết quả: khóa nội dung và cả hai nút bình thường, chỉ thử lại đúng thao tác, nội dung, base_attempt và Idempotency-Key. Network/408/425/429/5xx/idempotency_in_progress giữ lượt; lỗi nghiệp vụ dứt khoát nhả lượt, 401/403/409 yêu cầu tải lại. Chỉ ACK đúng tài nguyên xóa dirty; beforeunload cảnh báo khi dirty/unknown.
- Gửi phiếu thường cần xác nhận đúng phòng/kỳ/lần/max từ API; đổi nội dung hủy xác nhận. Cảnh báo vượt kế hoạch không tự chặn. Lưu nháp giữ hành vi cho nội dung chưa hoàn chỉnh để nhận lỗi máy chủ. Luồng câu trả lời Adhoc hiện hữu không thêm confirmation ở lát này.

### Giới hạn có chủ

- Khóa retry sống trong lifetime editor, không lưu dữ liệu nhạy cảm hay khóa vào localStorage. Không có hợp đồng receipt/audit nháp bền vững để chứng minh reconciliation qua hard reload; không tuyên bố hỗ trợ việc đó. Rời route/context tạo lifetime sạch, không giữ bản sửa của quyền cũ. Cảnh báo beforeunload không phải bộ chặn mọi điều hướng SPA; phần confirmation/navigation còn lại nằm #241.
- GET sheet có lịch sử gửi/decision thật nhưng chưa có nội dung bất biến từng lần, actor hay draft audit; giữ lịch sử hiện hữu, không dựng LogPanel giả. #241 phải có public contract được review trước mở rộng.

### Kiểm chứng

- Checkpoint RED `e944460e1143e65ba3bc8dd7b157d36871d856d0`: hai test nghiệp vụ FAIL trên producer dev58c (vẫn sửa được trong pending; actor cũ còn hiển thị).
- Control và kiểm thử kết thúc được ghi trên bình luận Ready đúng SHA; bao gồm số mũ/chuỗi dài ở F/J/K và điều chỉnh, authority/resource/late GET/ACK/ABA, retry key qua HTTP client thật, confirmation, dirty và readonly.
- Cổng bắt buộc: Go/static/lint-new0, migration ALL và PG thật 0SKIP, web đủ và E2E/browser, controlled FAIL→restore/control PASS, exact-SHA CI xanh rồi Claude review. Kết quả chưa hoàn tất không được coi PASS từ tài liệu này.
