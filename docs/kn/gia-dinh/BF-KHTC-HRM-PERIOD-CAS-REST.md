## BF-KHTC-HRM-PERIOD-CAS-REST — Đóng kỳ KHTC có receipt

Ngày: 04/11/2026. Phạm vi #211 là một họ thao tác hoàn chỉnh: Close KHTC. Phần còn lại thuộc #228; tài liệu này không xác nhận hoàn tất HRM, Cancel, Open/bù mở kỳ, UAT hoặc toàn bộ BRD.

### Lệnh và quyền

Giữ quy tắc BR-DL-15 của BRD KHTC v2.3: đóng bình thường khi đủ điều kiện, hoặc đóng ngoại lệ sau số ngày quá hạn cho phép với lý do hợp lệ. Bản cuối đã có không cấm Close. Lý do quyết định được giữ trong sổ KN; lệnh Close gửi native có Reason rỗng theo hợp đồng receipt #212.

Close yêu cầu phiên hiện tại hợp lệ và MFA. Intent ràng buộc tenant, actor, assignment, vai trò, phạm vi và request gốc. Khóa dùng lại với actor/nội dung/quyền khác bị từ chối trước supplier; không trả kết quả thành công từ cache cũ. Direct service không MFA trả403; middleware idempotency HTTP hiện hữu có thể trả409 khi cùng khóa đổi authority hash. Không thay đổi middleware dùng chung.

### Sở hữu và đối soát

Các đọc điều kiện Get/ControlSnapshot diễn ra trước PREPARE. Intent và pha dispatched được ghi trước Execute gây hiệu ứng native. Intent đang chờ dùng cùng khóa tài nguyên với deadline #209. Reservation phía báo cáo lấy cùng khóa snapshot với Cancel và không tự hết hạn khi kết quả chưa biết. Cancel thắng khoảng trống trước reservation thì Close kết thúc bằng bằng chứng chưa gọi native Execute/chưa gây hiệu ứng native; Close thắng thì chặn Cancel, Final mới và snapshot mới. Final trước reservation vẫn được phép.

Mỗi operation chỉ có một hiệu ứng native nhờ permanent key và CAS supplier212. Retry luôn Read receipt gốc trước. Chỉ khi dispatched/unresolved chưa lưu receipt và supplier trả đúng unknown không lỗi, gửi lại nguyên immutable command (OperationID, Expected và digest gốc); không suy unknown là rollback. Key đã có replay hoặc tiếp tục unknown khi receipt bị purge; key chưa có mới CAS Expected gốc. Receipt applied đã lưu, hydrated/completed chỉ Read; Read tìm được applied sau mất ACK không gọi Execute lại. Mất ACK không có nghĩa rollback, không cấp Expected mới, không undo mù, không đổi operation ID để vượt xung đột. Receipt chưa biết giữ reservation và chặn hành động cạnh tranh. Policy owner từ chối intent rejected, kể cả public Execute giữ command gốc; unresolved vẫn được Read để đối soát.

Receipt applied với SnapshotID/No/hash riêng được lưu trước hydration. Hydration dùng ControlSnapshot trước/sau public Progress; cả hai revision phải trùng own Applied và kỳ đã đóng. Submit đã nhận và Review bị native khóa sau Close. Bản offline giữ lại vẫn có thể đổi nhãn hiển thị thô; không coi toàn bộ Progress bất biến. Dữ liệu được dùng để chiếu kết quả là head bài đã nhận, phiên bản, quyết định, thời điểm và metadata; không biến bản offline giữ lại thành bài đã nộp. Thay đổi revision/successor trong hydration khiến thao tác chưa đối soát, không lấy Get mới làm kết quả của thao tác cũ.

LastSubmissionID bị loại khỏi JSON public được giữ bằng map UUID riêng trong payload intent, kiểm đúng đơn vị/head và khóa bất biến cùng progress. Với kỳ phát sinh, yêu cầu KN đang có được đọc ngay trong giao dịch LOCAL completion và chiếu bằng projectAdhocRequest hiện hữu: instruction, lifecycle, hạn yêu cầu, Late/NativeLate. Đây là ảnh metadata KN tại LOCAL completion, không phải lời hứa ảnh chụp KN tại thời điểm native Close.

LOCAL completion ghi cùng giao dịch dữ liệu extension: own event ID, outcome bất biến, completed phase và kết thúc reservation. Callback không gọi native/port/Get trong giao dịch KN. Mất ACK commit được đối soát bằng chính event/outcome này; không dùng latest Get hoặc một thao tác tiếp theo làm bằng chứng sở hữu.

### Lỗi và giới hạn

- `extension.khtc_report_closing` (409): reservation Close đang giữ snapshot; Final mới hoặc capture mới phải chờ đối soát thao tác gốc.
- Kết quả chưa xác định trả409 và giữ intent/reservation. Thiếu capability receipt/CAS/guard không rơi về đường Close legacy.
- Chưa thay caller Cancel legacy, chưa làm giao thức bù Open/HCTH, chưa xác nhận revocation qua OIDC production. Supplier #212 và session registry thật là hợp đồng runtime; fixture registry chỉ là composition test.
- Migration chỉ thêm ledger/trigger extension, không ghi dữ liệu HTX hoặc sửa Forms private schema. Không đổi hash lịch sử của #209.

### Kiểm chứng

Bằng chứng chạy test, source SHA, sai oracle fixture đã sửa, mutation/control và giới hạn thực thi được giữ riêng trong packet review #211. Không suy PASS từ compile hoặc từ một lần chạy ở SHA cũ. Native dùng pg-ephemeral riêng, Go1.26.10, count1/p1/timeout55m; CI exact SHA và review chéo Claude vẫn là điều kiện trước tích hợp.

### Sửa review Close211

Fingerprint close211/v2 loại riêng ID và ExpiresAt của selected session khỏi quyền bền, nhưng mỗi lần vẫn kiểm phiên trusted hiện hành, thời hạn, MFA, actor/tenant/org, assignment, vai, phạm vi và support binding. Đổi phiên cùng quyền có thể tiếp tục intent v2; đổi assignment/quyền hoặc phiên hết hạn bị chặn trước supplier. Không tự migrate/adopt ledger fingerprint v1 hay hash deadline209; không tuyên bố chuyển đổi intent v1 từ môi trường prototype khác. Bốn legacy callers còn ở #228 và Open phụ thuộc #224.
