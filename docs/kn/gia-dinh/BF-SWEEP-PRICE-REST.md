## BF-SWEEP-PRICE-REST — lát danh mục mặt hàng và nhóm hàng (#198)

Nguồn: BRD Báo giá v1.3, 07/10/2026, MụcIV/FN-AD-04.03 (bản trích analysis/src/brd_baogia.txt:618–632); UI-KIT R-A2/A4/A5 và ma trận bugfix. Phụ thuộc #182 và #183 đã vào dev; không đổi payload/API/schema, menu, multipart, mẫu giấy hay sharedform.tsx. Guard quyền thay thế trong modulePrice được mở rộng hẹp sau khi tái hiện bypass ngưng mã cũ (bình luận scope issue198).

### Phạm vi đã viết

- Danh mục mặt hàng: thêm/sửa/xem/ngưng/thay thế trên route riêng dùng DetailPage. Lịch sử phiên bản vẫn từ ItemDetail; Log lấy event_time/actor_ref/nội dung cũ→mới thật từ ItemVersion, không tạo tên người hoặc mốc thời gian giả. Mã mặt hàng hệ thống sinh theo hợp đồng hiện tại MH#####; URL sai định dạng/thừa đoạn trả not-found.
- Nhóm hàng: thêm/sửa tên/xem trên route riêng; xóa có xác nhận ngắn và không cho bấm khi nhóm còn mặt hàng. Giữ thứ tự và cơ chế hiệu lực hiện có; không thêm ô nhập sort_order hoặc tự đặt giới hạn nghiệp vụ mới. Phiên bản/hiệu lực nhóm là snapshot, không bị chuyển thành Log.
- validateForm/field errors/required, giữ catalog choices và khóa giống/nhóm/ĐVT/quy cách/loại khi hàng đã có giá; route thay thế chỉ dành cho Quản trị nghiệp vụ/Admin, tạo mã mới, không đổi mã nguồn. Tỉnh thường giữ quyền thêm và sửa mặt hàng chưa có giá; form thêm của Tỉnh thường không mở trường thay thế. Máy chủ kiểm lại quyền, ngày báo giá, khóa trường, bản ghi tồn tại và danh mục tại ngày hiệu lực.
- Ngày/tab/bộ lọc danh sách đi cùng URL quay lại/Hủy. Quyền can_edit/can_steward từ phản hồi hiện hành; mở URL ghi bằng CấpTW chỉ xem, Xã không có đường ghi danh mục. Phản hồi của phiên cũ không điều hướng phiên mới; form và thao tác được tạo lại theo tenant/actor/context/route/bộ lọc.
- Giữ useAction/IdempotentOperation/client hiện có: mất phản hồi sau commit rồi thử lại cùng dữ liệu giữ Idempotency-Key, không tạo mặt hàng thứ hai. Không sửa client multipart của #183. Không có ô số mới: thứ tự, số lượng và phiên bản chỉ đọc; thao tác lên/xuống hiện có được giữ. Chuỗi mã/ngày rất dài/số mũ giả làm mã bị chặn theo cú pháp; giới hạn text theo schema hiện có.

### Bảo vệ thao tác thay thế (BR-06)

BRD FN-AD-04.03/BR-06 dòng627: Quản trị nghiệp vụ ngưng/sửa đầy đủ; Tỉnh thường thêm và sửa mặt hàng chưa có giá. ReplacesCode tự ngưng mã cũ nên phải có quyền Quản trị nghiệp vụ. HTTP/PostgreSQL trước guard: Tỉnh thường nhận201, số dòng item/assignment và mã lớn nhất từ[1,1,1] thành[3,2,2]. Sau guard phải403 và ba chứng cứ giữ nguyên.

CreateItem quyết định Steward trước giao dịch; createItemTx yêu cầu capability private đã xác minh, gắn người gọi trước mọi đọc/cấp mã/ghi. ApproveProposal giữ kiểm quyền riêng và truyền capability đó, không gọi hook I/O trong giao dịch. Test pool1 thực hiện SELECT1 trong hook để bắt giữ kết nối hoặc gọi lặp; test native session/tenant/view-only và retry cùng khóa kiểm sổ thật. Không sửa cơ chế phân quyền rộng hay hợp đồng API.

### Log, quyền và giới hạn

BRD MụcII.1.1 dòng134/142 quy định nhật ký nghiệp vụ cho CấpTỉnh, nhật ký truy cập do Admin; FN-AD-04.03 dòng625/627 yêu cầu giữ lịch sử danh mục/tên. Log actor/time mới chỉ hiển thị cho kn_tinh/admin; CấpTW giữ snapshot và bảng phiên bản như trước. Đây là giới hạn hiển thị mới, không phải chứng nhận đã hạn chế payload ở backend.

GetItem hiện hữu của app/item.go trả history cho bên được phép resolve; handler/versionOf đã có actor_ref/event_time trước #198. Nếu cần hạn chế cả payload metadata của CấpTW thì phải chốt/kiểm hợp đồng đọc và quyền tại backend; gap và cách tái hiện được ghi trên child #204. Không tuyên bố đã giải quyết toàn PII/Log API bằng cách ẩn UI.

PriceGroup chưa có history/actor_ref/event_time: màn nhóm nói rõ chưa có lịch sử thay đổi; không tạo bảng nhật ký phụ hay dùng valid_from làm event_time. Child #204 giữ hợp đồng Log nhóm/nhóm vùng nếu cần; nhập Excel danh mục cũng chưa được thực hiện trong lát này.

### Phần còn lại

#204 BF-SWEEP-PRICE-SCREENS-REST giữ Gán cho xã, Bộ theo vụ, đề xuất/duyệt/từ chối trên route riêng, tickets/reports/surveys, lịch sử nhóm/nhóm vùng và những gap nghiệp vụ/UAT. Các dialog đề xuất của #37 và multipart183 được giữ; không coi toàn FN-AD-04.03/Báo giá/G1 hoặc UAT của PO đã hoàn tất.

Kiểm kỹ thuật và CI đúng SHA được công bố tại issue198 khi đạt đủ; test web/mock không thay HTTP/PostgreSQL thật hoặc nghiệm thu staging. Chỉ gửi ReviewerClaude khi đủ cổng và CI xanh, không tự gộp.
