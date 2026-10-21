# Đề xuất lõi chuyển tenant (#55)

Trạng thái: thiết kế để root/Claude review, chưa có Go/DDL/importer hoặc thao tác dữ liệu. Nền c6ce02b5. Đọc cùng ADR-0055, ADR-0020, ADR-0030, ADR-0052; quyết định D2 G1 chỉ hồ sơ KN trong ADR-0055 ưu tiên phần KN cũ của ADR-0052. G1 chưa đạt bởi thiết kế này.

## Chủ module và bất biến

Đặt hợp đồng generic ở party/port, điều phối ở party/app, lưu tại party/store và schema pf_party, wiring ở cmd/*/assembly. Party là kernel quản quan hệ bên tham gia/cấp phép; accounts đã phụ thuộc party nên không tạo cạnh vòng. Không tạo module/schema shared mới. Phạm vi issue55 ghi port/assembly; root cần xác nhận app/store cho phần thực thi generic trước code. Migration pf_party dùng tools/migratecheck -new, chỉ sau chốt thiết kế; modgen sinh lại.

Core chỉ giữ metadata được kiểm: cặp tenant/phạm vi, approvals, manifest, hash/provenance, ID map, batch receipts và trạng thái. Không raw payload/PII, SQL/table name từ request, toàn bộ dữ liệu HTX/MRV/MKG hoặc luật phòng ban KN. Assembly đăng ký allowlist rõ từng owner/namespace/version; owner typed exporters lọc đúng dữ liệu riêng KN (kể cả dữ liệu KN nằm trong forms/reports). Không chuyển HTX/Nút cooperative/Neo MST, quyền đồng sở hữu D2 hoặc MRV. Tenant digest recipient map hiện có chỉ tenant→mailbox, không phải map Trung tâm→tenant.

Mỗi binding giữ một SourceScopeRef (Trung tâm nguồn do owner xác nhận) và một TargetTenant/TargetScopeRef. Hai Trung tâm không dùng chung tenant đích; một nguồn lưu ký có thể có nhiều Trung tâm. ID map ghi rõ SourceTenant/namespace/source_ref→TargetTenant/namespace/target_ref, không sửa tenant_id hay hash của dòng nguồn. Mỗi phiên bản ledger mới ở đích có provenance gốc và chuỗi băm đích do shared.fn_chain tính lại.

## Hai ngữ cảnh xác thực thật

Không có một Principal giả dùng cho cả hai tenant. Phía nguồn Propose/Freeze/Seal/Export chạy dưới actual source context; phía đích Intent/Accept/Reserve/Apply/Resume chạy dưới actual target context. Mỗi mutation yêu cầu Principal đúng tenant của pha, actor có thật, platform_admin + MFA và callback owner kiểm quyền hiện hành trên đúng SourceScopeRef hoặc TargetScopeRef. Không callback/không đủ quyền/approval hết hạn hoặc bị thu hồi thì deny trước cache, replay, đọc dữ liệu hoặc gọi importer.

H4 share-grant đọc không phải quyền ghi chuyển tenant. Core không truyền tenant.Grant ra owner writers, không dựng Principal/SelectedSession/ScopeAll, không xóa Principal/WithoutNodeScope để vượt p_scope. Đặc biệt db.ContextVarsFrom(ctx) giữ Nút của phiên thực: importer phải dùng context đích thật và kiểm scope đích; không mang Nút nguồn sang target rồi xóa Nút cho qua. V1 chưa có job capability được duyệt thì worker/CLI không có actual target authority phải bị từ chối.

Handshake không cần key/token mới:

1. Actual target admin tạo TargetIntent, ghi append-only tại tenant đích: source tenant/scope dự định nhận, target scope thật, adapter allowlist, expiry, approval actor/ref/epoch. Trả reference để review, chưa cho ghi dữ liệu owner.
2. Actual source admin đọc đúng intent dành cho source tenant/scope của mình, kiểm authority của cả hai phía hiện hành, rồi tạo SourcePlan gắn IntentID, binding hash và adapter versions. Target không tự tạo nguồn/Trung tâm giả.
3. Sau source freeze/seal, actual target admin Accept SourcePlan đã chỉ đúng IntentID/target. Đích lưu bản metadata manifest đã xác minh trong tenant mình và cấp ID map. Chỉ lúc này mới có import batches.

Party chỉ cho phép cross-tenant đọc metadata intent/offer/receipt của đúng cặp: kiểm actual actor/quyền ở phía hiện hành và invitation/binding đã lưu, dùng private read-only transaction + H4 permit, SELECT có điều kiện source/target/plan/intent/hash. Không raw owner data và không expose permit. Cả sau Accept phải đọc lại approvals/fence trạng thái hiện hành, không coi offer cũ là quyền vĩnh viễn. Đây là loại permission metadata riêng của transfer, không tái sử dụng share_grant dữ liệu HTX hay cấp write grant. Root cần review riêng chốt này trước thực thi.

## API port dự kiến

Các struct dưới đây là hợp đồng review, không có implementation. Ref UUIDv7, Digest SHA-256 hex thường đúng64 ký tự, cursor bounded/opaque; không fields authority do HTTP body tự quyết. Copies sâu và registry immutable theo instance.

```go
type TransferScope struct {
    SourceTenant, SourceScopeRef uuid.UUID
    TargetTenant, TargetScopeRef uuid.UUID
    IntentID, PlanID uuid.UUID
    BindingHash Digest
}
type TransferKey struct { Namespace string; Ref uuid.UUID }
type TransferHeader struct {
    Key TransferKey
    SourceRowHash, LogicalHash Digest
    SourceSeq int64
    References []TransferReference
}
type TransferReference struct {
    Key TransferKey
    External bool // owner khai và chứng minh external license; không tự copy
    ExternalTenant uuid.UUID
    BasisRef uuid.UUID
}
type TransferApproval struct {
    Ref, ActorRef uuid.UUID
    Epoch string
    ValidUntil time.Time
}
type TransferFence struct {
    Ref uuid.UUID
    Epoch, HighWatermark string
    HeaderRoot Digest
}
type TransferArtifact struct {
    Ref uuid.UUID // owner-controlled immutable encrypted transport/evidence
    Hash Digest
    HeaderRoot Digest
}
type TransferReceipt struct {
    Ref, PlanID, BatchID uuid.UUID
    TargetTenant uuid.UUID
    BindingHash, ManifestHash, MapHash, ArtifactHash, LogicalRoot Digest
    Items []TransferAppliedRef
    OwnerVersion string
}
```

Registration gồm Module/Namespace/Version, namespace dependencies không vòng, maximum batch (≤500), Authority, FenceSource/FenceTarget/CheckFence, ScanHeaders, ExportBatch, ApplyBatch, ReadReceipt, VerifyBatch, CompensateBatch, ReadCompensationReceipt. Mỗi adapter sở hữu một module đã đăng ký; core không nhận arbitrary module/table lúc gọi. Authority/Fence/Scan/Apply/Receipt/Verify thiếu là deny, không fallback/no-op. Artifact authority và source data transport do owner, không có generic payload field.

Facade party.Module.Transfers() trả TenantTransfers: TargetIntent(ctx,in), Propose(ctx,intentRef), DryRun(ctx,planRef), Freeze(ctx,planRef), Seal(ctx,planRef), Accept(ctx,sourceOfferRef), ReserveBatch(ctx,planRef,limit), ExportBatch(ctxSource,batchRef), ApplyBatch(ctxTarget,batchRef), Verify(ctxTarget,planRef), PrepareCutover(ctx,planRef), Compensate(ctxTarget,batchRef,reasonRef), Status(ctx,planRef). Mỗi phương thức định rõ phase tenant; refs/receipts từ caller chỉ là khóa tra, không được tin kết quả trong body. Approval, fence và durable receipt luôn gọi current server lookup trước replay.

ApplyBatch nhận BatchID đã reserve, dùng artifact gắn batch đã xuất dưới actual source context. Owner đọc/giải mã artifact có cấp phép vận chuyển, không tự đọc DB nguồn với Principal đích. Nếu artifact thiếu hoặc không còn quyền thì deny/giữ checkpoint. Core không log artifact content, token hay lý do có PII. Chi tiết envelope/typed canonicalizer thuộc owner child trước triển khai.

## Schema/khóa dự kiến

Tất cả metadata được RLS FORCE, tenant_id là tenant thực của participant (source hoặc target), không nhánh share cho writes; r_party SELECT/INSERT, không UPDATE/DELETE trên bản ghi bất biến. Không BYPASSRLS/SECURITY DEFINER và không cấp quyền r_party ghi schema owner. Không đổi shared/db để né scope.

- transfer_binding: target intent + cặp tenant/scope/actor refs/adapter-set hash; unique target tenant cho một Trung tâm và unique source tenant/scope→target binding. Tạo dưới actual target, source acceptance append qua event; không sửa binding đã dùng. Binding mới cần phiên bản/approval mới, không viết đè.
- transfer_plan: immutable header, side source/target, IntentID/PlanID, BindingHash, adapter versions. PlanID nguồn được đích Accept cùng giá trị; tenant local khác nhau. Source sealed manifest được target copy đúng metadata/hash và kiểm lại.
- transfer_event: shared.apply_ledger, stream transfer:<plan>; created/fenced/page_recorded/sealed/accepted/batch_reserved/batch_verified/verified/cutover_prepared/compensation_requested/compensated/aborted. Transition kiểm seq kỳ vọng; authoritative status lấy từ ledger, projection nếu thêm chỉ là cache.
- transfer_item: immutable source header/provenance và hash, unique tenant/plan/namespace/source_ref. Header không chứa dữ liệu người. Source rows chỉ append, target copies giữ source row hash nguyên như evidence.
- transfer_idmap: target tenant, source tenant, namespace, source_ref, target_ref, binding ref; unique source tuple→một target_ref, unique target tenant/namespace/target_ref. UUIDv7 do core cấp dưới actual target và lưu trước Apply. Độc lập shared.ledger_idem TTL30ngày; reserve lại sau thời gian dài vẫn cùng ID, hash khác thì conflict.
- transfer_batch + transfer_batch_item: immutable member keys đã seal, BatchID, ManifestHash/MapHash/artifact ref+hash/fence epochs; unique mỗi item của plan chỉ thuộc một batch. Membership không được thay khi retry.
- transfer_receipt: immutable owner receipt refs/hash, counts/target IDs/logical root, unique plan/batch/operation (apply/verify/compensate). Full authoritative owner receipt nằm ở owner schema cùng target ledger commit; core không nhận receipt giả từ request.

Một transaction party local ghi reservation + idmap + batch + event. Advisory xact lock theo (local tenant, plan) và DB unique xử lý concurrent reserve; lock theo binding/target giữ cutover preparations không chạy song song hai plan. Ghi nhiều ledger streams luôn theo thứ tự ledger/stream byte order. Lock này không thay owner write fence.

## Fence, manifest và checksum

DryRun dùng source read-only, trả provisional count/root/exclusions/conflicts, không ghi/import/khóa hoặc xác nhận trạng thái cuối. Dữ liệu có thể đổi sau dry-run.

Trước seal phải có durable fences cho tất cả namespace nguồn và đích đã đăng ký. Owner Fence lấy khóa chung với mọi writer hiện hữu, đợi writer đang chạy kết thúc rồi append fence active; mọi HTTP/CLI/offline sync/job phải kiểm fence trong cùng giao dịch với ghi. Không thể chỉ giữ advisory lock trong coordinator hoặc chặn riêng route mới. Fence không tự mở khi coordinator crash/lease timeout. Thiếu bất kỳ owner producer integration nào thì Freeze/Seal/Apply/Cutover deny.

Scan source khi fences active, theo watermark recorded_at/ledger seq đã chốt (không chỉ event_time vì nhập muộn). Mỗi page có ordinal/cursor digest/hash và replay key; page khác dữ liệu dưới cùng key thì conflict. Manifest root hash canonical có version: binding, adapter set, source/target fence epochs, sorted namespace+source_ref, source row hash, logical hash và sorted references. Chỉ primitives/length-prefixed encoding hoặc canonical struct versioned, không map tùy ý/float. Seal chỉ ghi khi scan complete, counts/root/chain verification khớp và fences vẫn active/current. Adapter version/authority/fence thay đổi thì dừng, không âm thầm sửa manifest cũ.

Foreign ref nội bộ phải có trong manifest đúng owner/namespace và source tenant/Trung tâm; idmap không nhận key của Trung tâm khác. Dependency DAG import trước referrer. Chu kỳ giữa namespace chưa được owner chứng minh atomic import sẽ bị từ chối; aggregate có cycle nội bộ cần cùng batch và owner transaction/constraint proof. Ref HTX/MKG/vault ngoài transfer giữ ref gốc và license/basis thật do owner xác minh, không tự thêm vào idmap/copy dữ liệu đó.

LogicalHash là canonical nội dung nghiệp vụ do owner có version, bỏ tenant/ID/chain metadata và thay refs bằng source-key chuẩn để so trước/sau; SourceRowHash giữ riêng bằng chứng bất biến. Core đối chiếu per-item receipts lẫn aggregate roots, không chỉ count hoặc phép cộng checksum. KHTC frozen snapshot/Bản cuối giữ nghĩa/hash nghiệp vụ, 35 goldens không sửa; owner child phải chứng minh.

## Crash/resume và bù

Không tuyên bố atomic transaction xuyên module hoặc Keycloak. Core không truyền raw pgx.Tx cho owner:

1. Target reserve commit ID map/batch/event. Crash trước commit không có batch; sau commit retry dùng đúng IDs và hashes.
2. Source export immutable artifact gắn batch/map/manifest, ghi durable owner export receipt. Crash sau export thì lookup trả lại artifact cũ, không chụp dữ liệu khác dưới cùng batch.
3. Target owner Apply trong transaction r_owner thật, đúng target context, append target ledger + provenance + durable batch receipt/unique binding cùng commit. Owner outbox/side effects theo chuẩn module; không external side effect giả atomic.
4. Core ReadReceipt + VerifyBatch đọc kết quả đã commit, đối chiếu actual target IDs/count/hash/owner version/fence/binding rồi ghi core receipt/event. Nếu core crash sau bước3, retry owner phải đọc đúng durable receipt theo BatchID+hash, không import lần hai (kể cả ngoài TTL ledger_idem). Receipt thiếu/sai/count lệch thì core không tiến checkpoint.
5. Resume không tin cursor trong CLI: lấy batches/reservations/receipts từ metadata đã seal, recheck source/target approvals và fences hiện hành. Thu hồi ngay thì stop trước replay hoặc cấp artifact, không trả cached quyền cũ.

Compensate tạo operation ID/receipt riêng, xác minh Apply receipt thật, giữ idmap ban đầu. Owner append bản ghi bù/supersedes theo luật module và receipt bù trong cùng transaction; không DELETE target rows hoặc sửa ledger/source tenant/hash. Crash/retry dùng operation ID lâu dài. Core chỉ ghi compensated sau owner lookup/verify; compensation chưa đủ điều kiện domain thì blocked với reason ref, không tự bỏ qua. Khôi phục định tuyến/session/grants thuộc #56 và rehearsal #58, không gộp vào thao tác dữ liệu giả.

## Cutover và điểm bàn giao

PrepareCutover đòi mọi item/batch verified, source/target fence current, counts/per-item provenance/root khớp và callbacks #56/#58 cung cấp proof hiện hành cho identity/session/grants, target readiness và compensation rehearsal. Nó chỉ append cutover_prepared + permit ref/hash có hạn; không đổi Keycloak, route traffic hoặc giải fence. Phần hoàn tất cutover phải nhận durable receipt từ owner thật, kiểm lại qua server callback; thiếu là pending/deny, không boolean caller tự đặt.

Owner child (KN) cần đăng ký typed namespaces/exporter/importer/fence/receipt/compensation, artifact access và filters loại HTX/MRV/MKG; full producer fence matrix gồm CLI/offline/job; authority/binding lookup theo hai actual contexts. Shared forms/reports/accounts/notify có dữ liệu KN cần adapter thuộc owner phù hợp và field-level ID refs, không scan cả schema. #56 giữ actor identity/vault references, chuyển assignments/delegations/job khóa và revoke nguồn. #58 giữ snapshot counts/checksums/foreign refs, rollback đã diễn tập. Core tests với fake owner chỉ chứng minh protocol, không chứng minh production transfer, 5 boundary gates hay G1 hoàn tất.

## Kế hoạch kiểm trước ready

- Pure: canonical order/alias/two instances, unknown/duplicate namespace/version/dependency cycle, invalid hashes/IDs/refs, missing callbacks/fences/approvals, expired/revoked proofs, caller giả receipt, replay altered body.
- PG r_party thật: source tenant S chứa hai scopes A/B, target TA/TB; intent/offer actor đúng tenant, A→TA/B→TB, A→TB/copy metadata/foreign source key/target-scope mismatch deny. RLS direct foreign SELECT/INSERT, UPDATE/DELETE/tenant_id changes bị từ chối, reused connection không giữ tenant/node/actor. Unique map/reservation sau30ngày, concurrent batches, hash conflict và crash trước/sau từng local commit.
- Owner protocol PG fixture dùng module role thật riêng và actual target context: owner ledger+receipt atomic; injected failure trước receipt rollback; crash sau owner commit/core receipt chưa ghi thì replay không nhân đôi; malformed/missing receipt không tiến; duplicate compensation không nhân đôi. Fixture không giả production importer.
- Fence tests: source writer đang chạy phải kết thúc trước fence ack; HTTP/CLI/offline/job write sau fence bị chặn; plan/advisory lock riêng không đủ; source mutation giữa scan/seal/verify buộc mismatch/deny, không lén cập nhật manifest.
- Actual owner child tests: không có dòng HTX/MRV/MKG trong artifact, references/licensing/chain/provenance đúng, KHTC semantics/goldens giữ; transfer hai Trung tâm với real role/foreign refs. Rehearsal và5 boundary hard PASS thuộc tổng đợt4b, không được mock để đóng55.

Chỉ triển khai sau root review API/module ownership, hai-context metadata permission, artifact transport, source/target fence contract, saga/idmap strategy. Hiện chưa có migration, chưa chạy PG/test production, chưa release/claim ready, chưa cutover hoặc chuyển dữ liệu thật.