## BF-FORMS-PUBLISH-OP-RECEIPT — Receipt phát hành native theo operation (pf 20261105022736)

Supplier additive từ phần còn lại #212; base đã có #212. `PublishOperations`/`UsePublishOperationPolicy` độc lập với `Publisher`, `DurablePublisher`, `ConditionalPeriodControl`, không chuyển caller KN/HRM/CNTS, không tích hợp Apply hay bù của #211/#233 và không tuyên bố UAT/G1/rollout.

### Hợp đồng

Consumer ghi durable intent/full `PublishOperationRequest` và trạng thái first-dispatch trước Execute. Command gồm explicit tenant, operation ID và toàn BatchInput/template/resources. Supplier kiểm tenant trùng principal, MFA, profile/actor/native publication permission/target/template và owner policy hiện hành ở cả Execute/Read trước receipt. Callback ngoài Forms TX; nil policy/capability/pool hoặc joined caller TX fail closed503. Binding của owner phải phản ánh assignment/scope/revocation hiện tại; không lấy digest/header của caller làm quyền.

Canonical mới bound366periods/2.000units, strings/arrays/typed MaxSubmissions0–20 trước hash; serialized command1MiB. UnitKind bắt buộc ghi rõ và khớp native spec: không dùng mặc định profile có thể đổi thứ tự khi khởi động lại. Legacy Publisher/DurablePublisher vẫn giữ mặc định cũ; không đổi allowed kinds hay grants. Thứ tự periods/units/parents được giữ; nil/empty parents đồng nghĩa, nil MaxSubmissions khác0. Clone callback input tránh callback sửa command. DueAtUTC microsecond, From/ToUTCdate, label/key/rule trim theo native; duplicate effective native keys bị từ chối. Đây là port Go typed, không thêm routeHTTP/rawnumericparser/web; integer cực trị bị chặn trước hash, không có decimal/money mới.

Native batch dùng chung private core với hook224, không legacy wrapper rồi đọc head. Period/spec/published event, receipt và permanent anchor cùng TX. Applied.EventSeq và EventRecordedAt từ chính native INSERT RETURNING; DueSeq0 là creation state của ID mới, không literal head1/snapshot sau đó. Ordered fullItems receipt bind exact original native event tuple `(id,recorded_at,seq)`, tenant/profile/template/actor/native idem của operation, ordinal/count/uniqueness/fullcommand. Single invalid/missing item làm toàn receipt opaque unknown. SQL INSERT proof kiểm fresh due0; Read/replay không kiểm currenthead/due/status, giữ original token sau successor/ABA.

Hai bảng riêng trong pf_forms: key permanent bất biến và receipt ledger append-only/RLS. Header-last bind exact partitioned receipt identity; deferred FK receipt→key ngăn orphan COMMIT, không FK giả lên native event(id). Request JSONB và receipt JSONB có cap2MiB cho khác biệt biểu diễn JSONB; Go canonical command cap1MiB. Receipt read join key/fullrequest chính xác và SQL proof trên toàn bytes trả về. Không thêm native publication outbox/event: primitive hiện chỉ có native/feed behavior; kiểm baselineoutbox không tăng do publication/replay.

### Unknown và retention

Chỉ trả Applied sau COMMIT ACK; lỗi muộn hoặc lostACK luôn zeroUnknown+error, không trả prefix/partial slice. Không có rejected/savepoint/partial-commit mode. Invalid input/policy trước dispatch là lỗi, không giả durable rejected outcome. Mixed-period acceptance là successor states khác nhau sau một batch atomic.

Read chỉ đọc; missing/purged/damaged receipt hoặc mismatch khi anchor còn →Unknown/no metadata/no reexecute. Anchor không TTL và không purge cùng receipt. Toàn TX rollback/key absent không để lại lịch sử firstdispatch ở supplier; consumer bắt buộc dispatched/unknown→Read-only, không suy rollback hoặc gọi Execute lại. Kiểm thiếu evidence dùng fault QueryRow boundary, không nhận đã chạy retention GC thật. Bù caller tương lai chỉ được dùng exact creation token; token không cấp quyền và CAS phải từ chối successor.

### Kiểm chứng và giới hạn

Pureport/factory/normalize; native batch2/many32/currentauthority/realCOMMIT-loss/rollback/missingorbaditem/latefailure/RLS/reusedpool1/concurrentpool2 actualadvisory waiter/SQLproof/immutability. Thêm missing-kind422/zeroeffects, explicitfacility receipt giữ qua restart cùng allowed-kind set đổi thứ tự, legacy default positive và SQL empty/missing-kind negative. ONE controlled fault refresh creation token từ currentheads phải FAIL sau ABA, exact restore/controlPASS. Cổng Go/MFA/lint/scanner/migration-base/ALL và nativePG0SKIP/cleanup phải đủ; trạng thái thực thi nằm trong packet ngoài repo, không coi compile-only là nativePASS. Windows local không có CGO compiler nên không claim local-race; wholeCI Linux-race exactSHA bắt buộc.

Không thêm permission, flag, baseline, privatecrossmodule reader hoặc HTTP/web contract. Sharedrepo scanner có findings lịch sử của nhánh chưa publish khác không được miễn bằng cấu hình gói; final official default scan trên independent full-origin clone+candidate được ghi provenance riêng, không gắn base scan thành final.

Chỉ READY sau đủ cổng và CI đúng SHA; Claude review chéo trước khi gộp theo quy trình. Caller rollout và lifecycle có gói riêng.
