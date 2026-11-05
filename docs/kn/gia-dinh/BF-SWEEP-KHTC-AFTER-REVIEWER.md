# BF-SWEEP-KHTC-AFTER-REVIEWER (#242) — lịch sử nội dung đã gửi

Căn cứ BRD KHTC v2.3 Product, SHA-256 `15859351f90574bf667190b1a12160465ba03f8ca9e408b94c90d285a3502e25`, P886–P918; ma trận A1–A5. Parent #241 đã vào dev trước lát này. Chưa có UAT khách; không đánh dấu toàn FN-TI-02.04, KHTC hoặc G1 đạt.

## Lát hoàn chỉnh

- Từ rà soát mở danh sách lần gửi có STT; mỗi lần có URL chi tiết chỉ đọc và có thể đối chiếu hai lần gửi của cùng kỳ/phòng. Các ô đổi có nền vàng và nhãn chữ; số giữ nguyên chuỗi đã lưu, không parse hoặc tính lại.
- Hai GET mới dưới `/v1/extension/khtc/periods/{id}/forms/{dept}/submissions`: danh sách metadata native và chi tiết `/{submissionID}`. Đường dẫn UUID được giới hạn trước parse; phòng phải thuộc danh sách hiện hành, query lạ bị từ chối. GET không thực hiện thao tác ghi.
- Dùng cổng công khai `Forms.Entry.Submissions`; không đọc SQL riêng của Forms. Chỉ đọc ledger `khtc_form_sheet` của extension sau khi tìm đúng submission native. Kiểm kỳ KHTC/native, phòng, bảng sealed, base attempt, người gửi và SHA-256 nội dung; bảng mồ côi/định danh ghép không chứng minh lần gửi.
- Nhãn hạng mục dùng `PlanVersionRef` qua `KHTCPlanVersions.PlanVersion`, kiểm lại ID/năm; không dùng `PlanAt` hiện hành thay lịch sử. Định nghĩa mẫu phải khớp code/version đã lưu. Thiếu capability hoặc sai hash/metadata phụ thuộc thì fail closed.
- Người gửi/rà soát, ghi chú và thời điểm lấy từ bản ghi native. Log chỉ gồm sự kiện gửi và kết quả rà soát quan sát được; không giả audit lưu nháp hoặc tên người chưa có hợp đồng tra cứu.
- Giữ quyền tenant/profile/phòng và phạm vi từ phiên/cổng native; Tỉnh phòng khác không đọc phiếu không thuộc mình, cấp Xã/Quốc gia không đọc lịch sử phiếu. Thành viên không đọc phiếu kỳ đã hủy. Phát sinh giữ chặn readonly và MFA Admin hiện hữu. Không KN ghi dữ liệu HTX, không nới policy nguồn.
- Cổng native/plan được gọi ngoài transaction extension. Đổi API, tenant, phiên, phòng, vai, MFA hay tài nguyên tạo lifetime mới; GET muộn của A→B→A không phục hồi nội dung cũ. Mở lịch sử từ reviewer phải xác nhận bỏ ghi chú và bị chặn khi thao tác chưa có kết quả.

## Giới hạn có chủ rõ ràng

Nội dung ô nhập/lời văn/điều chỉnh được giữ bất biến; metadata rà soát là kết quả đã ghi nhận khi đọc. Đây không phải operation receipt, bằng chứng thao tác sau hard reload, hay audit đầy đủ. Ledger cũ không lưu đủ đầu vào/kết quả để dựng lại lũy kế, % KH và ba quy tắc tại thời điểm gửi; màn nêu rõ giới hạn, không hiển thị kết luận kiểm tra hiện hành thay lịch sử. Điều chỉnh đối chiếu theo thứ tự đã gửi; không tự gán định danh nghiệp vụ cho dòng chưa có ID.

Phần còn lại chính thức: [#244](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/244) — audit/receipt, Excel gốc, cảnh báo báo cáo đã xuất, cột tự tính lịch sử, P901/P904 so với Q-081/native flags/#217/#239; report editor/list/detail/form/Bản cuối và nguồn; adhoc/kỳ nhúng/CAS. Biên tập toàn văn chờ PO #240. Không thay max-submissions, Submit/Review, chính sách 3/3, nguồn số liệu, baseline hoặc assertion/budget cũ. Không có migration mới.

Giả lập giữ actor/time và payload khi thao tác gửi/rà soát thật xảy ra trong mock. Seed cũ thiếu các dữ kiện này trả lỗi phụ thuộc thay vì giả actor/log. Fingerprint mock là giá trị tổng hợp ổn định theo bảng, không phải chứng thực mật mã và không hiển thị như bằng chứng SHA; kiểm SHA-256 thực thuộc test PostgreSQL/native.

## Kiểm chứng

`TestKHTCHistory242_*`: nội dung lần 1 giữ nguyên sau lần 2/nháp mới; actor thật; quyền/tenant/target; native hoặc sealed tham chiếu sai; nhãn kế hoạch cũ sau khi kế hoạch hiệu lực mới thực sự được chọn. `submission-history.test.ts`, `KhtcFormHistory242.test.tsx` và assertion bổ sung ở `KhtcFormReview241.test.tsx`: GET/định danh, chuỗi chính xác, thêm/xóa/sửa, route, quyền và ABA/ghi chú. Browser `kn-khtc-history242.spec.ts` tạo phân công, mở kỳ, gửi hai lần và rà soát qua UI công khai, không ghi Auth/STORE để bỏ qua quyền. Chỉ báo cổng đạt trong packet đúng SHA sau khi có log thực; không suy từ tên test.
