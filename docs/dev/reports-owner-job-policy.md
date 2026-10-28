# Chính sách việc xuất của nguồn sở hữu

Ticket [REPORTS-OWNER-JOB-POLICY](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/82), con AUDIT-LOG #64. Nguồn KN và giao diện do owner #83 thực hiện. Gói này chưa hoàn thành Excel nhật ký, retention hoặc tách KN.

## Đăng ký

Dùng SourceBuilderRegistration.JobPolicy cùng builder hiện có; không thêm registry hay route xuất riêng. SourceJobPolicy bắt buộc Version dương, ScopeMode (SourceScopeGeographic hoặc SourceScopeTenant), Freeze và AuthorizeJob đầy đủ. Registry chụp bản sao từng policy khi dựng; cấu hình thiếu dừng startup. Nil giữ luồng native.

ExportInput.SourceArgs và trường HTTP source_args chỉ nhận JSON object tối đa 16 KiB, sâu tối đa 16; cấm khóa trùng, JSON nối tiếp, UTF-8 hỏng và khóa _reports_source. Nguồn không đăng ký policy từ chối SourceArgs thay vì bỏ qua. Owner Freeze phải giải lược đồ đóng, cấm trường identity/tenant/scope do client truyền, kiểm ngày/mã, chuẩn hóa mặc định theo RequestedAt. Freeze nhận JobID/TenantID/RequestedBy/RequestedAt/Source/TemplateKey/Format do server lập; không thể trả lại các trường này.

Snapshot trong Job.SourceArgs mang json:"-". Codec lưu phần riêng _reports_source trong cột params JSONB hiện có, không thêm migration. Snapshot ràng buộc source/version/job/tenant/requester/time/mẫu/phiên bản/định dạng. Native Params không có snapshot giữ byte marshal cũ. Cổng đọc chuẩn hóa lại JSON object sau PostgreSQL JSONB để replay không phụ thuộc thứ tự khóa của DB. Thời điểm truyền cho owner được chuẩn UTC kể cả khi PostgreSQL trả timestamptz theo múi giờ máy. Số JSON được chuẩn thành số thập phân chính xác, không float64; tính giới hạn kích thước bản in trước khi cấp phát để chặn số mũ gây phình bộ nhớ. Số nguyên 100/1000 vẫn là JSON integer cho bộ giải lược đồ đóng của owner. Các trường Params không biết vẫn theo hợp đồng native bị bỏ qua và không thể thay snapshot riêng. Không trả snapshot qua DTO, event, lỗi hoặc log.

SourceBuildQuery bổ sung JobID, RequestedBy, ArgsVersion và SourceArgs được sao chép từ việc xuất thực. Các slice/JSON đều sao chép qua ranh giới callback.

## Quyền hiện hành và replay

AuthorizeJob chạy ở request, replay (gồm nhánh tranh unique-key), list, detail, download và worker build. RequestedBy không cho phép bỏ qua owner authorization. SourceJobAccess mang operation, Now, actual ActorRef của HTTP, metadata bất biến và Scope/SourceArgs sao chép. Worker ActorRef là UUID Nil; owner phải tra quyền hiện tại của RequestedBy trong tenant từ nguồn đáng tin cậy. Không dựng Principal từ vai đã lưu. Owner phải kiểm MFA/session/Admin, thu hồi, hạn, readonly và phạm vi của toàn snapshot; lỗi dependency từ chối.

Replay dùng JobID/RequestedAt gốc chuẩn UTC để Freeze cùng as_of; vẫn kiểm quyền tại Now. Actor/mẫu/format/Params/phạm vi/canonical args thay đổi bị từ chối. Version snapshot không khớp registry, snapshot thiếu hoặc sai metadata đều dừng, không fallback. Bỏ policy khỏi registry cũng không làm snapshot cũ trở thành nguồn native.

reports.Module.Middleware() là RequestMiddleware đã được gốc lắp ráp đặt sau xác thực/middleware accounts và trước common idempotency cache. Nó kiểm quyền owner và nội dung replay trước khi cached 202 có thể được trả. Guard luôn được lắp kể cả khi không còn policy, để job còn snapshot riêng bị từ chối sau khi gỡ policy cuối cùng; job native không snapshot giữ luồng cache cũ khi không có policy. Khi lắp module thủ công phải đặt middleware này ở cùng vị trí. Cache scope hash không thay thế live authorization.

List từ chối toàn trang nếu bất kỳ việc xuất policy-enabled trên trang không còn được phép hoặc owner lỗi. Không trả trang một phần có thể che sự cố nguồn. Tenant scope chỉ cho nguồn đăng ký tường minh; lưu phạm vi địa lý rỗng thực, không giả Scope.All. Geographic scope vẫn phải bao trọn phạm vi đã đóng băng khi đọc.

Owner callback/Builder có private args phải không tiết lộ dữ liệu trong lỗi; shared luôn đổi lỗi Freeze/AuthorizeJob/Builder thành lỗi công khai an toàn đối với nguồn policy-enabled. Hành vi lỗi builder native giữ nguyên.

## Pipeline hiện hành

Request vẫn đi DLP và ghi job + outbox nguyên tử. Download kiểm lại quyền trước điểm DLP và trước URL ngắn hạn. Worker kiểm envelope khớp job thực, kiểm owner trước builder rồi dùng render/object store/completion ledger hiện có. Các sự kiện chỉ chứa metadata job, không có args.

Nguồn policy-enabled chưa được lập lịch: tạo mới, replay lịch cũ, fire lịch và delivery pending của nguồn mới bật policy đều từ chối. Delivery pending được ghi skipped bền vững trước presign/email, kể cả khi policy bị gỡ nhưng job còn snapshot riêng. Deactivate lịch cũ vẫn được phép theo quyền hiện hành. Cần hợp đồng freeze từng lượt và owner authority của scheduler trước khi bật.

## Kiểm thử

Các test domain kiểm byte native, private codec, metadata binding, unknown/duplicate/depth/size/UTF-8. Test app kiểm callback snapshot/clone, request/replay authority và lỗi không lộ filter. PostgreSQL kiểm roundtrip JSONB, replay as_of, worker dựng artifact, revoke/readonly/dependency ở mọi lối, tenant khác, DLP/outbox và lịch cũ. Test assembly HTTP kiểm cùng token/body/key sau khi thu hồi quyền không nhận cached 202.

Kết quả kiểm thực tế được ghi trên issue #82 sau khi runner kết thúc. Không được coi preflight hoặc DB SKIP là bằng chứng PostgreSQL đạt. 35 golden KHTC không thay đổi.

## Bằng chứng kiểm cuối

Production 19bd68f4; cây mã đóng băng suốt Go/test/lint. Lệnh Go 1.26.9 trên cụm PostgreSQL riêng: go test -v -count=1 -p 1 -timeout 55m ./internal/platform/reports/... ./tests/reports ./cmd/api-platform/assembly — actual exit 0, 95 test cấp đầu PASS, không FAIL/SKIP/INFRA.

- reports/app 95.962s; lifecycle PG mới 0.17s, exact numeric JSONB 0.00s, pending scheduled delivery 0.01s.
- tests/reports 76.175s: native export, scope, quota/anomaly, lịch/delivery, history đều PASS.
- assembly 105.071s; HTTP owner cache 0.19s, gồm revoke cùng token/key/body, source_args reserved injection, Params không ghi đè snapshot và gỡ policy cuối cùng.
- domain 0.704s, dispatch 0.753s, formula 0.947s, mail 0.743s, render 0.945s; typed int100/1000, UTC cùng instant +07, số rất lớn/nhỏ và guard phạm vi native đều PASS.
- build/vet/modgen/migration/archlint/contractcheck/docscheck/knboundary đều 0. Lint toàn repo 0 issues. Boundary 314. Gofmt chỉ còn hai tệp archlint testdata có sẵn.
- Cụm tạm 127.0.0.1:52734, thư mục 9fd439af75c5 đã dừng và dọn. tests/golden có 35 tệp, tree 06a780646532b38221d14cea61d636b1f78c5033 giữ nguyên byte; thư mục con KHTC cũng không thay.

Giữ nguyên lịch sử thất bại trước sửa: source đầu exit1 (UTC replay qua timestamptz làm khác as_of), lint đầu exit1 (test Close response chưa kiểm lỗi). Lượt revised source exit1 chỉ vì assertion của HTTP test nhầm tên trường problem.errors.field là rò payload. Các lỗi đã sửa, vòng full cuối ở trên đạt; không đổi thất bại cũ thành PASS.

Chờ Claude Review OK và tích hợp qua ticket.ps1. Owner #83 còn phải đăng ký nguồn thật, quyền Admin/MFA hiện hành, trusted reader + durable read receipt, schema/generated/mock/web và retention. Chưa hoàn thành AUDIT-LOG #64.

## Review 23/10: lịch sử và worker

MyExports quét từng lô 200 job theo cursor, kiểm quyền hiện hành rồi phân trang trên tập còn được xem. Job bị owner từ chối 4xx hoặc snapshot/policy không tương thích được bỏ khỏi lịch sử; lỗi owner 5xx vẫn chặn cả trang, không trả một phần. Snapshot cũ không được chuyển ngầm sang đường native. Phân loại nội bộ snapshot/policy không tương thích để ẩn khỏi lịch sử; worker ghi error_code reports.source_job_invalid và chốt failed trong transaction inbox thay vì retry/dead-letter, trước builder/kho đối tượng. Endpoint request/replay/detail/download giữ lỗi dependency 503 như hợp đồng cũ; owner tạm không hoạt động vẫn 503 để retry. Regression assembly giữ nguyên assertion 503 sau khi gỡ policy, không đổi test để hợp thức hóa 409.

Assembly giữ pf/accounts trước pf/reports để phiên hiện hành được giải trước kiểm replay; có regression bảo vệ thứ tự. Middleware giới hạn body 1MiB và JSON sai trả 400 như handler; mỗi POST có Idempotency-Key đọc job hiện có trong tenant transaction trước cache để không giữ quyền đã thu hồi. Replay lịch native trả lịch đã lưu khi template đã mất; template còn resolve được vẫn kiểm cấm custom schedule. Không cấp URL tải hay nới DLP khi lọc lịch sử.