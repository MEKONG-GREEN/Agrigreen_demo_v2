## BF-SWEEP-KHTC-YEAR-LIFECYCLE — Nháp máy chủ ban đầu của năm

Lát đầu của #225 cung cấp một vòng đời nháp **được lưu trên máy chủ** cho năm chưa có phiên bản kế hoạch và chưa có tham chiếu kỳ KHTC hoặc ý định mở kỳ phát sinh. BRD KHTC v2.3 ngày 08/10/2026, P340/P782–825, BR-DL-21 là căn cứ; bản DOCX đối chiếu có SHA256 `15859351f90574bf667190b1a12160465ba03f8ca9e408b94c90d285a3502e25`. Đây là phạm vi initial-only, không phải hoàn tất toàn bộ FN, vòng đời năm hay UAT.

### Dữ liệu và thao tác

API `/v1/kn/khtc/plans/{year}/draft` cung cấp đọc và ghi nháp; `/discard` hủy đúng thế hệ/lần ghi, `/history` phân trang các sự kiện bất biến, `/compare` so với nền rỗng của năm chưa có phiên bản. Lần ghi trả event ID, draft ID, revision, actor, thời gian và cây đã chốt. Không cộng revision bằng suy đoán ở client. History dùng `before_revision` và `next_before_revision`; tối đa 100 sự kiện mỗi trang.

Nháp là tài nguyên chung của những người hiện có quyền KHTC, không phải tài sản riêng của người tạo. Ghi/hủy phải khớp `draft_id` cùng `expected_revision`; hủy rồi tạo lại sinh thế hệ khác, nên một yêu cầu cũ không thể chạm nháp kế tiếp. Mỗi thao tác giữ nguyên idempotency key và thân qua lỗi transport hoặc `idempotency_in_progress`. Biên nhận bền gắn toàn bộ nội dung và quyền của người gọi; replay đọc chính sự kiện đã ghi, không lấy trạng thái mới nhất làm ACK cũ. Kiểm tra quyền hiện hành luôn đi trước HTTP cache và đọc biên nhận. Đổi ID/hạn của một phiên còn hợp lệ không làm đổi dấu quyền; actor, tenant, tổ chức, phân công, vai, scope, MFA, support binding và profile vẫn được giữ. Không bỏ qua hạn phiên hiện hành hay sửa Principal đang dùng.

Đọc nháp chỉ cho Admin/KHTC; KHTC chỉ xem được đọc nhưng không ghi. Các phòng tỉnh khác không được nhận thêm quyền đọc tài nguyên chưa công bố. Giữ chính sách MFA hiện có: Admin/support phải có MFA; không bắt buộc MFA mới cho cán bộ KHTC tỉnh thông thường. Router giữ nguyên thứ tự middleware cũ, thêm hook tùy chọn sau ngữ cảnh và trước idempotency cho riêng năm đường dẫn nháp, kể cả tham số năm không hợp lệ. Gated module chuyển tiếp hook; không đổi shared Auth hoặc policy toàn cục.

### Khóa, bất biến và điều kiện mở kỳ

Ghi nháp và SaveVersion dùng cùng khóa `plan:<year>`. Năm có nháp active chặn một lần công bố mới qua đường SaveVersion cũ, kể cả client cũ gọi trực tiếp. Replay lịch sử đã xác nhận chỉ đọc được giữ. Năm không có nháp vẫn giữ đường công bố legacy hiện có; không tự chuyển nháp mới sang đường đó. Published PlanAt, báo cáo, export và lịch sử phiên bản chỉ đọc dữ liệu đã công bố, không thấy nháp.

Cổng công khai do calendar sở hữu kiểm tra chính xác sự có mặt của metadata kỳ và mọi ý định mở phát sinh theo owner year, không dựa vào trang danh sách, trạng thái, hạn lease hoặc ClosedCutoff. Thiếu cổng, không phân loại được owner year hoặc lỗi đọc phải fail closed. Đọc cổng bên ngoài transaction; SQL guard và cùng khóa năm bảo vệ phần ghi sở hữu. Ledger header/entry chỉ append; entry khóa năm trước khi kiểm header, header được ghi sau toàn bộ entry, FK trì hoãn gắn đúng event/recorded-at/năm. Không gọi cổng module khác hoặc native I/O trong transaction này.

P804 được kiểm trước mọi phát hành native mới, gồm explicit departments, phát sinh và direct recovery: phải có PlanAt thật cho **owner Year/Cutoff của kế hoạch hoặc ý định đã lưu**. Không ép tất cả năm nguồn chéo năm có kế hoạch mới. Ý định cũ chưa có metadata kỳ vẫn giữ nguyên khi bị chặn, không tự hủy/tạo lại. Kỳ đã xác nhận tồn tại được replay chỉ đọc trước guard, không phát hành thêm. PlanAt rỗng nhưng có version là có kế hoạch, khác với not-found. Presence này không chứng minh đầy đủ quyết định Áp dụng theo BRD.

### Giao diện và ranh giới nhập

Route `ke-hoach-nam/nhap-may-chu` cùng các alias phòng ban dùng workspace hiện hữu. Thêm/sửa/xem CT/DA, nhóm, hạng mục dùng form và NumberField chung, lỗi từng ô, cây và Log từ dữ liệu đã chốt. Chọn CT/DA hoặc Lưu vào nháp ở form mới chỉ sửa cây cục bộ; phải bấm Lưu nháp máy chủ để nhận ACK. Hủy/Back của form không hủy nháp máy chủ. UI ghi rõ nháp máy chủ khác phiên bản áp dụng; nút Áp dụng hiện chưa khả dụng.

Giữ cây, head và ACK đã biết qua lỗi GET tải lại; có lỗi đọc riêng và nút thử đọc lại. Khi một ô form chưa đưa vào cây, reload do tạo master không được tự nhận revision của đồng nghiệp rồi ghép form cũ vào CAS mới. Bỏ thay đổi trên màn là thao tác rõ ràng để nhận trạng thái đọc mới. Owner theo API/quyền/năm và token route theo từng lần commit ngăn ACK muộn sau A→B→A điều hướng hoặc sửa owner kế tiếp; không dùng tên hiển thị/menu/badge làm quyền.

Server kiểm resource/decimal trước hash, JSON rendering và Decimal.String, kể cả gọi app trực tiếp. HTTP giữ giới hạn thân 1 MiB trước cache; stream tối đa 5.000 entry, tối đa 500 mã xã/dòng, kiểm mọi lần xuất hiện numeric gồm key case-fold. Lexical/giải mã không hợp lệ trả 400; giới hạn nghiệp vụ trả 422; thân quá lớn trả 413. Không chuyển tiền/khối lượng qua float. Các probe bao gồm `1e100000`, `1e-100000`, 100.000 chữ số, số thường quá lớn, precision, âm và một lần xuất hiện hostile bị ghi đè bằng giá trị hợp lệ.

### Mock và giới hạn triển khai

Mock giữ cùng family trong envelope sessionStorage có version, gồm catalog/program/published refs cần thiết và draft/events/biên nhận. Persist candidate thành công trước khi đổi RAM hoặc ACK. JSON hỏng, version lạ, bucket kế hoạch sai owner year, storage unavailable/quota trả 503; không tự reset nháp active để vượt fence. Mất key chỉ được coi là tab demo mới khi chưa giữ bản đã lưu trong RAM. Dữ kiện quyền trong biên nhận chỉ là các semantic audit facts để so sánh exact; không lưu JWT/Bearer/refresh/AuthProvider hoặc opaque session payload, và dữ kiện đã lưu không cấp quyền hiện hành. Current policy chạy lại trước replay.

Fingerprint 64-hex của mock chỉ phục vụ hiển thị demo, **không phải SHA-256 hoặc bằng chứng native**. Equality biên nhận dùng toàn bộ canonical request/authority text, không dựa fingerprint. Mock nhận JSON đã parse nên không chứng minh raw duplicate-token/decimal lexical behavior; phần đó được kiểm bằng native HTTP. Unit module-reset không thay cho Playwright F5 trên build mới. Mock trước phiên bản này và các thay đổi cũ còn chỉ nằm trong RAM không có migration lịch sử tự động; không khẳng định nhận lại mọi trạng thái legacy đã mất.

Không tuyên bố an toàn zero-native-effect nếu còn binary legacy đang chạy hoặc phát hành cũ chưa có intent/metadata đối chiếu; rollout phải dừng các writer cũ và đối soát unknown trước khi dựa vào invariant mới. SQL guard bảo vệ dữ liệu KN không phải cơ chế bù Forms đã phát hành bởi code cũ.

### Phần còn lại

Formal remainder #233 giữ draft điều chỉnh của năm đã có phiên bản, Apply và các hiệu ứng Forms/auto-adjust, autosave nếu cần, ba trạng thái năm/đóng năm, copy, quy tắc STT toàn bộ, Excel và xóa có dữ liệu. Giao thức close/recovery #211 và public creation/compensation #224 chưa được thay bằng receipt giả hoặc truy cập Forms private SQL. Gói này không sửa các nhánh đó và không đóng các AC tương ứng bằng state client. Kết quả thực thi, SHA, controlled fault/control, full gates/native/migrations/web/E2E và giới hạn provenance nằm trong packet review; chỉ READY sau CI đúng SHA và Claude review chéo theo quy trình.
