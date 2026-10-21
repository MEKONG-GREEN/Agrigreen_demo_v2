# Chính sách việc xuất của nguồn sở hữu

Ticket [REPORTS-OWNER-JOB-POLICY](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/82), con AUDIT-LOG #64. Nguồn KN và giao diện do owner #83 thực hiện. Gói này chưa hoàn thành Excel nhật ký, retention hoặc tách KN.

## Đăng ký

Dùng SourceBuilderRegistration.JobPolicy cùng builder hiện có; không thêm registry hay route xuất riêng. SourceJobPolicy bắt buộc Version dương, ScopeMode (SourceScopeGeographic hoặc SourceScopeTenant), Freeze và AuthorizeJob đầy đủ. Registry chụp bản sao từng policy khi dựng; cấu hình thiếu dừng startup. Nil giữ luồng native.

ExportInput.SourceArgs và trường HTTP source_args chỉ nhận JSON object tối đa 16 KiB, sâu tối đa 16; cấm khóa trùng, JSON nối tiếp, UTF-8 hỏng và khóa _reports_source. Nguồn không đăng ký policy từ chối SourceArgs thay vì bỏ qua. Owner Freeze phải giải lược đồ đóng, cấm trường identity/tenant/scope do client truyền, kiểm ngày/mã, chuẩn hóa mặc định theo RequestedAt. Freeze nhận JobID/TenantID/RequestedBy/RequestedAt/Source/TemplateKey/Format do server lập; không thể trả lại các trường này.

Snapshot trong Job.SourceArgs mang json:"-". Codec lưu phần riêng _reports_source trong cột params JSONB hiện có, không thêm migration. Snapshot ràng buộc source/version/job/tenant/requester/time/mẫu/phiên bản/định dạng. Native Params không có snapshot giữ byte marshal cũ. Cổng đọc chuẩn hóa lại JSON object sau PostgreSQL JSONB để replay không phụ thuộc thứ tự khóa của DB. Không trả snapshot qua DTO, event, lỗi hoặc log.

SourceBuildQuery bổ sung JobID, RequestedBy, ArgsVersion và SourceArgs được sao chép từ việc xuất thực. Các slice/JSON đều sao chép qua ranh giới callback.

## Quyền hiện hành và replay

AuthorizeJob chạy ở request, replay (gồm nhánh tranh unique-key), list, detail, download và worker build. RequestedBy không cho phép bỏ qua owner authorization. SourceJobAccess mang operation, Now, actual ActorRef của HTTP, metadata bất biến và Scope/SourceArgs sao chép. Worker ActorRef là UUID Nil; owner phải tra quyền hiện tại của RequestedBy trong tenant từ nguồn đáng tin cậy. Không dựng Principal từ vai đã lưu. Owner phải kiểm MFA/session/Admin, thu hồi, hạn, readonly và phạm vi của toàn snapshot; lỗi dependency từ chối.

Replay dùng JobID/RequestedAt gốc để Freeze cùng as_of; vẫn kiểm quyền tại Now. Actor/mẫu/format/Params/phạm vi/canonical args thay đổi bị từ chối. Version snapshot không khớp registry, snapshot thiếu hoặc sai metadata đều dừng, không fallback. Bỏ policy khỏi registry cũng không làm snapshot cũ trở thành nguồn native.

reports.Module.Middleware() là RequestMiddleware đã được gốc lắp ráp đặt sau xác thực/middleware accounts và trước common idempotency cache. Nó kiểm quyền owner và nội dung replay trước khi cached 202 có thể được trả. Khi lắp module thủ công phải đặt middleware này ở cùng vị trí. Cache scope hash không thay thế live authorization.

List từ chối toàn trang nếu bất kỳ việc xuất policy-enabled trên trang không còn được phép hoặc owner lỗi. Không trả trang một phần có thể che sự cố nguồn. Tenant scope chỉ cho nguồn đăng ký tường minh; lưu phạm vi địa lý rỗng thực, không giả Scope.All. Geographic scope vẫn phải bao trọn phạm vi đã đóng băng khi đọc.

Owner callback/Builder có private args phải không tiết lộ dữ liệu trong lỗi; shared luôn đổi lỗi Freeze/AuthorizeJob/Builder thành lỗi công khai an toàn đối với nguồn policy-enabled. Hành vi lỗi builder native giữ nguyên.

## Pipeline hiện hành

Request vẫn đi DLP và ghi job + outbox nguyên tử. Download kiểm lại quyền trước điểm DLP và trước URL ngắn hạn. Worker kiểm envelope khớp job thực, kiểm owner trước builder rồi dùng render/object store/completion ledger hiện có. Các sự kiện chỉ chứa metadata job, không có args.

Nguồn policy-enabled chưa được lập lịch: tạo mới, replay lịch cũ và fire lịch đều từ chối. Deactivate lịch cũ vẫn được phép theo quyền hiện hành. Cần hợp đồng freeze từng lượt và owner authority của scheduler trước khi bật.

## Kiểm thử

Các test domain kiểm byte native, private codec, metadata binding, unknown/duplicate/depth/size/UTF-8. Test app kiểm callback snapshot/clone, request/replay authority và lỗi không lộ filter. PostgreSQL kiểm roundtrip JSONB, replay as_of, worker dựng artifact, revoke/readonly/dependency ở mọi lối, tenant khác, DLP/outbox và lịch cũ. Test assembly HTTP kiểm cùng token/body/key sau khi thu hồi quyền không nhận cached 202.

Kết quả kiểm thực tế được ghi trên issue #82 sau khi runner kết thúc. Không được coi preflight hoặc DB SKIP là bằng chứng PostgreSQL đạt. 35 golden KHTC không thay đổi.
