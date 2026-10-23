# AUDIT-QUERY-PORT (#70)

Con của #64 AUDIT-LOG. Capability này truy vấn các sổ có chủ, không sao chép sự kiện nguồn thành bảng nhật ký song song. Chưa hoàn tất màn Admin, reader KN, lưu nhật ký đọc, retention ba tháng hoặc xuất Excel.

## Hợp đồng HTTP

`GET /v1/accounts/audit` trên Admin API. `from` và `to` bắt buộc, RFC3339 với độ chính xác nano; lọc theo recorded_at trong [from,to), tối đa 93 ngày. Các khóa tùy chọn: `actor_ref` UUID khác zero; `department`, `module`, `action` là mã; `limit` mặc định 50, miền 1–200; `cursor`. Mỗi khóa xuất hiện một lần, khóa khác kể cả tenant bị từ chối. Mọi trang phải gửi lại cùng bộ lọc và limit.

Kết quả: `items`, `sources` (các nguồn thực sự tham gia), `as_of`, `next_cursor`. Item gồm `source`, `module`, `event_ref`, `entity_ref`, `actor_ref`, `occurred_at`, `recorded_at`, `department` tùy chọn, `action`, `outcome`, `changes`. Nguồn cố định source/module khi đăng ký; không nhận tên schema/bảng từ HTTP.

Cursor là base64url JSON v1 ràng buộc hash của tenant, người đọc, bộ lọc, limit và các source key; giữ as_of và vị trí (recorded_at,source,event_ref) giảm dần. Cursor không phải bằng chứng quyền: mọi trang đều xác thực lại. Mỗi nguồn đọc tối đa limit+1; các dòng phải theo đúng thứ tự/miền lọc/allowlist. Lỗi bất kỳ nguồn nào không trả trang một phần. as_of là watermark thời gian, không phải snapshot MVCC kéo dài qua nhiều request; sự kiện commit muộn có recorded_at trước watermark vẫn phụ thuộc ngữ nghĩa ghi sổ của owner.

## Đăng ký máy chủ

`port.NewAuditRegistry(sources...)` sao chép các đăng ký/allowlist theo instance. `accounts.Module.NativeAuditSource()` cung cấp reader metadata của account_event. Sau khi tạo module, gốc lắp ráp gọi `UseAudit(registry, port.AuditAccess{Authorize, RecordRead})` ở startup. Callback không serialize, không có global registration và không sửa cấu hình khi xử lý request.

Authorize phải kiểm phiên Admin thật của ứng dụng; lỗi là kết quả cuối, không fallback realm hay dữ liệu phiên KN cũ. Capability luôn yêu cầu Principal có actor, tenant và MFA; tenant context hiện có phải khớp. Không tạo Principal giả hoặc đổi tenant.

RecordRead bắt buộc phải lưu bền sự kiện xem trước khi trả dữ liệu, kể cả trang rỗng. Receipt chỉ gồm request_ref, tenant, actor, read_at, filter_hash, danh sách source và vị trí các dòng trả; không sao chép nội dung nhật ký nguồn. Thiếu callback hoặc callback lỗi thì không trả trang. Gói này không cung cấp sink giả; callback kiểm thử chỉ là test double, không phải bằng chứng lưu bền/retention trong sản phẩm. Claude sở hữu authorizer, readers KN, recorder thật và OpenAPI/generated/web/docsKN ở child tiếp theo. Capability chưa cấu hình trả accounts.dependency_unavailable (503).

## Metadata an toàn

Changes chỉ có boolean, UUID ref và mảng mã, với field/kind allowlist của nguồn. Không có reason, details, raw JSON, nhãn hiển thị, nội dung hỗ trợ hay tên người. Chủ nguồn chịu trách nhiệm chọn field/code không chứa PII; cú pháp không thay thế đánh giá ngữ nghĩa.

Native reader dùng giao dịch chỉ đọc r_accounts/RLS và SQL LIMIT trên account_event hiện có. Chỉ lấy UUID, thời điểm, action và snapshot enabled/roles/org_ref/lock_cause; chỉ có After khi sổ không chứa Before, không suy đoán trạng thái trước. Department không được lưu trong sổ nên để trống và không khớp department filter. Outcome để unknown vì sổ không lưu outcome; không suy từ action. Reader không đọc reason/details hoặc join assignment hiện hành để suy department lịch sử.

## Phần chưa giao trong #70

Không có Excel endpoint hoặc bypass DLP. SourceBuilder reports hiện chưa giữ được custom audit filters trong frozen job params; cần child shared riêng trước khi nối xuất. Không sửa KN, migration, schema OpenAPI, file API gom hoặc web. Không xác nhận retention khi owner recorder/chính sách dọn chưa có bằng chứng.

Review 23/10: From/To được canonical UTC/microsecond trước validation, filter hash, SQL và kiểm miền sau đọc; khoảng nhỏ hơn một microsecond co về rỗng bị từ chối. Receipt giữ From/To/AsOf, FilterActorRef (người bị tra; ActorRef là người đọc), Department/Module/Action/Limit và PurposeCode. Purpose hiện chỉ có allowlist `audit_view`, do server chọn; HTTP không nhận purpose tự do. Recorder #73 phải lưu các trường này cùng hash/returned refs để tra ngược được ai đã xem gì.

Ghi nhận các lần bị từ chối (thiếu MFA, tenant lệch, owner Authorize từ chối) và index account_event(tenant_id, recorded_at) vẫn là phần việc owner #73; gói này chưa xác nhận các chức năng đó. Không dùng receipt thành công để giả vờ đã ghi nhận denied reads.