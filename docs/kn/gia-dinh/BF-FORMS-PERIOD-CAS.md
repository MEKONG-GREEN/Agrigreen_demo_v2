## BF-FORMS-PERIOD-CAS — CAS của kỳ Forms native

Issue #159, phần shared của lỗi #156. Codex viết, Claude review. Chỉ sửa Forms/port và kiểm thử; chưa nối consumer CNTS của Claude. Không tuyên bố lỗi end-to-end hay G1 đã hết khi consumer chưa chuyển sang port mới.

### Hợp đồng

- `Module.ConditionalControl()` trả `port.ConditionalPeriodControl`. Legacy `PeriodControl` giữ tương thích và vẫn vô điều kiện; các lần ghi legacy cũng làm revision cũ hết hiệu lực. Consumer phải chuyển mọi nhánh ghi sang CAS, không fallback.
- `ControlSnapshot` đọc tiến độ và revision trong một TX đang giữ khóa kỳ native. Revision gắn tenant/profile/period và head seq thực tế của hai sổ append-only `period_event`, `period_due`. Không dùng thời điểm, due/status hay generation lease giả; ABA đổi rồi quay về vẫn bị phát hiện.
- Bốn lệnh ChangeDue/Close/Cancel/Reopen CAS so cả hai head dưới cùng khóa kỳ, sau kiểm quyền và trước ghi. Guard, ledger, snapshot và outbox nằm cùng TX; callback nghiệp vụ vẫn gọi ngoài TX.
- Thành công trả revision đọc sau ghi trong chính TX đó. Chuỗi Reopen → ChangeDue dùng revision của commit trước, không đọc snapshot mới của successor. Bất kỳ lỗi nào đều trả revision zero, gồm commit không rõ kết quả; không mặc nhận rollback khi mất ACK.
- Token là điều kiện cập nhật, không phải quyền. Chính sách role, owner, template, target, tenant, NoReopen, máy trạng thái và ảnh chụp đóng băng được giữ nguyên. Không mở endpoint/ô số mới.
- `forms.period_conflict` trả 409. Caller phải ghi revision vào intent bền vững trước I/O và chỉ đối soát khi còn sở hữu intent. Không refresh token sau mất lease rồi retry vô điều kiện.

### Phạm vi và giới hạn

Không migration: dùng head của sổ hiện hữu, không thay dữ liệu lịch sử hoặc baseline. Port mới chưa tự bảo vệ các lệnh legacy đang gọi từ KN. Consumer còn thiếu được tách thành [#201](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/201), lane KN, sau khi #159 gộp: snapshot ngoài TX → xác minh/lưu vào intent trong TX KN → native CAS ngoài TX. Không sửa cntscampaign trong gói này.

### Kiểm chứng

- Baseline PG tái hiện old callback bị giữ ngoài TX, successor đã commit hạn mới, old vẫn ghi đè và trả nil; test FAIL đúng khi chưa dùng CAS. Không cần old caller đánh dấu/đối soát để quan sát lỗi.
- Regression PG cho callback/pool1, due/status ABA, cạnh tranh hai writer, quyền/tenant/identity/bounds, ảnh chụp/outbox bất biến và commit mất ACK. Kết quả chạy cuối, mutation và các cổng/CI đúng SHA ghi tại bình luận sẵn sàng.
- Revision là số bigint có kiểu từ DB, không parse số mũ/BigInt từ API/Excel. Trường revision zero/âm/MaxInt64 hoặc sai tenant/profile/period bị từ chối; số liệu Forms hiện có tiếp tục được kiểm ở domain/server và bộ regression đầy đủ.

### Câu hỏi PO

Không thay nghiệp vụ. Phải hoàn tất ticket consumer trước xác nhận lỗi mất lease của CNTS đã hết end-to-end.
