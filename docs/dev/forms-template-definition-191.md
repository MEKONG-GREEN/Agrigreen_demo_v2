# Chính sách và phiên bản định nghĩa biểu dùng chung (#191)

Trạng thái triển khai: đã nối REST/Publisher/module sau khi #159 được gộp; đang hoàn tất cổng kiểm trước review. Không dùng tài liệu này để tuyên bố FN-07 của KN đã hoàn tất. Chủ nghiệp vụ và màn KN là #192.

`cap/forms` giữ các phiên bản biểu bất biến theo tenant/profile/code. Kỳ chọn một `template_id` cụ thể; thêm phiên bản mới không đổi schema hoặc giá trị bài nộp của kỳ cũ.

## Cổng chính sách của chủ nghiệp vụ

[TemplateDefinitionPolicy](../../internal/platform/forms/port/definition.go) nhận principal đã xác thực, session projection do chủ đăng ký, schema đề xuất và ảnh chụp phiên bản hiện hành. Quyết định mặc định từ chối; lỗi hoặc thiếu policy bắt buộc không rơi về vai `DefineBy`. Callback chạy ngoài giao dịch ghi của forms, không được dùng nó để thực hiện ghi dữ liệu nghiệp vụ.

Policy định nghĩa tách khỏi `ResolvedActor.AllowsTemplate`, vốn lọc đọc. Mẫu khóa cấu trúc vẫn có thể xuất hiện trong danh mục đọc. Chủ ứng dụng tự kiểm khóa mẫu, phòng ban và sở hữu chương trình từ registry tin cậy, đồng thời cấu hình vai/phạm vi đọc qua resolver. Shared không chứa danh sách mã KN hoặc truy vấn sổ của KN.

Cấu hình `Profile.RequireDefinitionPolicy=true` bắt buộc policy ngay cả khi chủ chưa đăng ký. Gốc lắp ráp truyền `forms.Deps.DefinitionPolicies` hoặc gọi `Module.UseDefinitionPolicy` trước khi phục vụ; tên profile lạ trả lỗi, callback nil từ chối ghi. `Module.Middleware()` chạy sau xác thực và trước shared idempotency cache theo `httpx.RequestMiddleware`; mọi router tự dựng cũng phải giữ thứ tự này.

Profile legacy chưa chuyển policy giữ quy tắc hiện hành. Việc chuyển KN phải do #192 cấu hình bắt buộc policy và đăng ký callback; không nới `DefineBy` để thay thế quyền định nghĩa theo mẫu. Tạo seed cho owner cũng cần hợp đồng quyền riêng được kiểm, không dùng tài khoản giả hay bypass khóa KNQG.

## Phiên bản và retry

Client gửi `base_version=0` để tạo mã mới, hoặc phiên bản đã đọc để thêm bản tiếp theo. Profile có policy bắt buộc gửi giá trị này. Trong cùng giao dịch, forms khóa khóa idempotency của tenant và stream profile/code, đọc replay bền vững, kiểm phiên bản hiện hành rồi append. Hai người cùng base chỉ một người thành công; người còn lại nhận `409 conflict` với trường `base_version`.

Retry cùng key/payload/actor trả ID phiên bản ban đầu, kể cả đã có phiên bản mới. Payload khác nhận `422 idempotency_key_reused`. Hậu tố idempotency `:template:<code>` được giữ để tương thích batch seed nhiều mã; không replay chéo profile hoặc actor. Lối REST cần tái kiểm quyền trước cache HTTP, đồng thời giữ nguyên scope binding của session ở middleware phía trước.

## Danh mục đọc

[TemplateCatalog](../../internal/platform/forms/port/definition_catalog.go) là cổng đọc tùy chọn, giữ nguyên interface Publisher bắt buộc cho các consumer hiện hành. Cổng có danh sách mới nhất, đọc bản chính xác và lịch sử giảm dần theo version. Lịch sử giới hạn 1–200 phần tử, mốc 0 là mới nhất; dùng version cuối trang làm mốc trang kế tiếp. Tất cả đường đọc giữ profile/code filter và tenant RLS.

REST tương ứng là `GET /v1/forms/template-versions?profile=...&code=...` và `GET /v1/forms/template-versions/{version}?profile=...&code=...`. Đường đọc bằng UUID và danh mục phiên bản mới nhất hiện hành vẫn giữ tương thích. Không lấy phiên bản mới nhất thay cho ID mà người dùng đã chọn khi phát hành kỳ.

## Bằng chứng cần hoàn tất

Kiểm PG thật bao gồm default-deny, policy error, role/phòng/owner/tenant, khóa qua REST và Publisher, retry/revocation trước cache, cạnh tranh base, schema/kỳ/bài nộp cũ và chọn phiên bản mới. Cần cổng static/lint, hợp đồng/types sinh lại, web đầy đủ, phép làm sai có kiểm soát và CI đúng SHA trước khi gửi Claude review. Bằng chứng và cổng cuối cùng được ghi trên issue #191; không có migration mới trong gói này.
