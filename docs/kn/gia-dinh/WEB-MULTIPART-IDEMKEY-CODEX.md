## WEB-MULTIPART-IDEMKEY-CODEX — retry tệp HCTH/Báo giá

Issue #183, phần tiếp theo của #172. Codex viết, Claude review. Không sửa ba client của Claude hoặc shared form.

### Phạm vi và hành vi

- Kho HCTH `doc-api.upload`, tệp quyết định `promotion-api.attachFile`, xem trước/ghi nhận điều tra `surveys/api`: dùng `stableMultipart`, Content-Type có boundary tất định. File được đọc/kiểm với giới hạn hiện hữu trước dựng thân; MIME chuẩn theo chữ ký/định dạng, lastModified chuẩn 0 để chọn lại cùng nội dung/tên giữ đúng từng byte.
- Middleware idempotency hiện hữu băm toàn bộ byte request, gồm trường/đường dẫn/context. Hai tệp cùng metadata nhưng khác byte nhận khóa khác; không dùng metadata làm dấu nội dung. Không đổi API/backend/migration hay quyền.
- Đọc file có await: bắt `OperationRun` trước await rồi nhập lại cùng run khi gọi client. Không complete trong API con; nơi mở thao tác (`useAction`) chỉ complete sau cả lượt.
- `DecisionScreens.saveDecision`: kiểm tệp trước ghi metadata; tạo quyết định, gửi xác nhận cảnh báo và attach sau await đều nhập lại cùng run. Retry bước sau phát lại metadata bằng khóa trước, tránh ghi thêm quyết định/tệp. Đây là thay caller tối thiểu đã điều phối trên #183 trước #179/#182, không chuẩn hóa UI trong gói này.
- Giới hạn HCTH theo `domain.MaxDocBytes = 15 MiB - 1024`, bảng chữ ký `domain.DocContentType`; điều tra theo `surveyLimits.MaxBytes = 5 MiB`, UTF-8/ZIP theo `tabular.ReadBytes`. Máy chủ vẫn kiểm đầy đủ định dạng/nội dung/quyền. Không thay giới hạn, M1–M7, số liệu hay PII.

### Kiểm chứng và giới hạn bằng chứng

- Regression web dùng client thật/serializer/middleware thật, lớp mạng mô phỏng máy chủ đã commit rồi mất phản hồi. Phủ cả bốn đường multipart, khác byte/cùng metadata, chọn lại cùng byte, context khác, đọc lỗi/quá dung lượng, mất phản hồi ở metadata hoặc attach và nhánh xác nhận cảnh báo.
- Regression Go `TestMultipartRetry183_HRMHTTP` và `TestMultipartRetry183_SurveyHTTP` dùng PostgreSQL và middleware idempotency thật: cùng thân/khóa phát lại đúng response; đổi thân hoặc actor cùng khóa bị từ chối; số tài liệu/Attach/lần nhập/phiếu không tăng. Fixture danh tính và kho mã hóa giả được khai báo rõ, không chứng nhận OIDC hoặc kho đối tượng triển khai.
- Playwright `multipart183.spec.ts` kiểm tải PDF qua input trình duyệt: sai chữ ký giữ form, đúng tệp tạo một tài liệu. Browser dùng mock API theo cấu hình E2E hiện hữu; không tính là bằng chứng DB.
- Mutation bỏ capture run khỏi upload HCTH làm regression retry FAIL do khóa mới, khôi phục nguồn và chạy control. Các kết quả gate/DB/web/CI đúng SHA ghi ở bình luận ready của issue, không suy PASS từ việc biên dịch hoặc test SKIP.

Không ô số mới; không thêm cổng ghi vào HTX, I/O trong transaction, migration hoặc baseline. Không câu hỏi PO mới.
