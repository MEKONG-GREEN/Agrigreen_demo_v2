## BF-SWEEP-KHTC-AFTER-EDITOR — rà soát đúng lần gửi

### Phạm vi và căn cứ

- Lát rà soát trọn vẹn đầu tiên của #241 sau khi #229 đã thực sự vào dev. Căn cứ BRD KHTC v2.3, FN-TI-02.04 (P886–918), ma trận bugfix A2–A4 và hợp đồng Forms public hiện hữu. Phần còn lại chính thức là #242; không tuyên bố full FN/UAT/G1.
- Chỉ reviewer `/kn/khtc/ra-soat/{periodId}`, lớp KHTC review/HTTP, nguồn OpenAPI cùng client/mock sinh và các kiểm thử tương ứng. Giữ nguyên thân FormHead, editor #229, native Forms, quyền module, mẫu/công thức, SQL/migration, menu, global CSS, scanner/ignore/baseline.

### Lỗi và hợp đồng

- Trước sửa: KHTC tự lấy lần gửi mới nhất lúc POST. Màn đang đọc lần 1 có thể quyết định nhầm lần 2 vừa được gửi. Test PostgreSQL trên producer cũ chứng minh lần 2 bị accepted với HTTP 200.
- POST review bắt buộc `expected_submission_id` và `base_attempt` của lần gửi đã đọc. Thiếu trường ở client cũ trả 422; UUID/JSON/số phiên bản sai định dạng hoặc body vượt 16 KiB trả 400. Không tự chuyển yêu cầu cũ sang phiên bản mới. Chỉ nhận quyết định returned/accepted; returned cần nội dung, tối đa 1000 ký tự Unicode sau trim, cùng giới hạn native Forms.
- Kiểm quyền/ngữ cảnh và tài nguyên hiện hành, so ID/attempt trước I/O native. Gọi Entry.Review đúng ID đã khóa trong ý định, ngoài giao dịch KN; native Forms kiểm lại phiên bản cuối dưới khóa kỳ để chặn thay thế giữa bước đọc và quyết định. Không sửa hay truy vấn riêng bảng của module khác.
- ACK thêm `reviewed_submission_id`, `reviewed_attempt`, `review_decision` lấy từ kết quả public native đã đối chiếu ID/version/unit/decision/note/thời điểm. Phiếu hiện hành đọc sau đó có thể đã thay đổi; không dùng nó để suy diễn ACK. Lỗi hoặc ACK lệch sau native có thể là thao tác đã ghi nhưng chưa đối chiếu, không phải bằng chứng thao tác chưa xảy ra.

### Hành vi giao diện

- DetailPage/breadcrumb/quay lại; nhóm nút bên phải, Yêu cầu bổ sung là nút chính; nguồn phòng chuyên môn chỉ đọc. Chọn phòng chỉ trong danh sách public của kỳ đã được đọc bằng quyền hiện tại. Kiểm UUID/query/phòng, dữ liệu GET và ACK đúng tài nguyên.
- Thay client, actor/tenant/profile/MFA/context/role/phòng/kỳ/query tạo lifetime sạch. Phản hồi muộn và A→B→A không đưa dữ liệu hay thông báo thành công của lifetime cũ sang lifetime mới.
- Hai quyết định đều cần xác nhận đúng phòng/kỳ/lần gửi và ghi chú đã trim. Xác nhận, đối chiếu và bỏ ghi chú nằm ngay dưới nhóm nút, trước bảng nguồn; focus chuyển vào xác nhận hoặc ô ghi chú sai để không bỏ sót ở phiếu dài. Ghi chú có nhãn, dấu bắt buộc và lỗi liên kết ô. Đổi ghi chú hủy xác nhận. Đổi phòng hoặc liên kết quay lại khi có ghi chú cần xác nhận bỏ; beforeunload cảnh báo dirty/unknown.
- Pending/unknown khóa ghi chú, chuyển phòng và các nút quyết định. Chỉ thử lại cùng payload, target/version và Idempotency-Key. Network/408/425/429/5xx/idempotency_in_progress giữ nguyên lượt; 401/403/409 buộc đọc lại. Chỉ ACK trùng đúng ý định mới báo thành công và xóa ghi chú. Không ghi nội dung/khóa vào localStorage.

### Phần còn lại có chủ

- #242: nội dung bất biến theo từng lần gửi, so sánh/actor/audit và receipt public để đối chiếu qua hard reload; report/snapshot/Bản cuối/Excel, cảnh báo xuất lại dựa nguồn public; kỳ nhúng/Adhoc/confirmation còn lại. Không dựng lịch sử hay LogPanel từ dữ liệu suy diễn. beforeunload và nút quay lại riêng không phải bộ chặn mọi điều hướng SPA.
- Giữ đúng flags native hiện hành: lần gửi tối đa không được returned, có thể accepted nếu máy chủ cho phép. Điểm khác BRD v2.3 P901/P904 (tự khóa lần cuối) được ghi rõ ở #242, phối hợp #217/#239; lát này không thay chính sách toàn hệ thống hoặc làm yếu kiểm thử cũ.
- Nếu native đã ghi nhưng bước đọc sau đó lỗi, retry cùng khóa có thể nhận 409 và yêu cầu kiểm tra lại trạng thái; chưa có receipt public bền vững để khẳng định kết quả của chính thao tác đó qua reload. Không thông báo thành công dựa vào phiếu đang confirmed.

### Kiểm chứng và bàn giao

- RED native checkpoint `cc31b2955136cb6361c8eb5cd2b365b8bdac2027`: `TestKHTCReview241_StaleRenderedSubmissionCannotReviewSuccessor` FAIL đúng tác động nghiệp vụ; cụm PG riêng được dừng/xóa. Ba RED web về xác nhận, ghi chú và authority cũng FAIL trên reviewer cũ rồi control đạt.
- Bộ mới kiểm target/attempt thiếu/sai/cực trị, ghi chú Unicode/body, ACK native và replay, native chặn superseded sau khi KHTC đọc; web kiểm late GET/ACK/ABA, quyền, sai tài nguyên, pending/retry qua HTTP client thật và E2E bằng phân công/giao diện công khai, thời gian giả lập.
- Kết quả cổng/static/lint, native PG 0SKIP và migration ALL, full web/browser, phép làm sai có kiểm soát rồi khôi phục, scanner không đổi cấu hình và CI toàn nhánh đúng SHA được ghi trong bình luận Ready. Tài liệu này không thay bằng chứng thực thi. Claude phải review chéo đúng SHA trước khi ticket merge.
