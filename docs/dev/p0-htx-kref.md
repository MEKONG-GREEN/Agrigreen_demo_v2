# P0-HTX-KREF (#130): tạo Nút và bản chiếu cùng giao dịch

**Đã kiểm các sửa hồi quy riêng; chưa đủ điều kiện gộp.** Nguồn kiểm cuối là
`f67c8cd86631e1af00b96de679000a74ff70fc87`. Full CI của ứng viên cũ `080c174e`
FAIL; cần CI mới và Claude Review OK tại SHA bàn giao cuối. Các kết quả theo SHA
được tách dưới đây, không dùng PASS từng package thay cổng tổng thể.

## Phạm vi

Giữ k_ref và outbox. Trigger k_ref.project_created_org thuộc NOLOGIN own_kref,
chỉ gắn AFTER INSERT của pf_party.org. Nguồn INSERT, org_event, bản chiếu org/closure
và outbox cùng commit/rollback. Không cấp r_party quyền DML trên k_ref, không thêm
membership r_kref cho app_platform; không mở đường ghi HTX cho phiên KN.

Hàm không có tham số dữ liệu gọi trực tiếp, kiểm nguồn trigger, search_path cố định,
REVOKE EXECUTE FROM PUBLIC, kiểm tenant của nguồn và khôi phục app.tenant_id của
caller. Chỉ dòng NEW đã qua RLS nguồn được chiếu; bootstrap bằng vai quản trị có thể
không đặt tenant, hàm chỉ đặt tenant theo NEW trong phạm vi lời gọi. Khóa tenant
trùng khóa worker/Rebuild (k_ref:<tenant>), source_seq=NEW.last_seq giữ sự kiện cũ
không ghi đè; outbox tiếp tục phục vụ projection ở cụm khác và share_scope.

Retry cùng Idempotency-Key được tuần tự hóa trong giao dịch nguồn trước đọc sổ,
kiểm trùng tên và cấp mã. Không giữ kết nối này khi gọi pf/cooperative ở bước hai.
Nếu yêu cầu cùng khóa commit giữa bước đọc sổ ban đầu và kiểm MST qua port, kết quả
đã commit được đọc lại trước khi trả lỗi trùng. Xung đột nghiệp vụ thật (hai thao tác
khác nhau cùng tên/MST) vẫn bị từ chối.

Trước INSERT có guard SECURITY INVOKER đọc metadata nguồn và đường tổ tiên của cha
bằng quyền SELECT sẵn có của caller, cùng khóa tenant với worker. Nếu một move còn
đợi outbox hoặc đường closure thiếu, giao dịch nguồn rollback trước bước hồ sơ.
Gọi lại cùng khóa sau khi worker chiếu xong; không để lại Nút thiếu hồ sơ. Guard so
parent_ref/admin_unit_code/active và tính đầy đủ của đường, không so last_seq: đổi
tên nguồn không có trong projection vẫn cho tạo bình thường. Không thêm quyền cho
caller/app. Migration cấp CREATE trên pf_party cho own_party NOLOGIN để chuyển
quyền sở hữu hàm đúng module; tương tự own_kref, app không được membership vai này.

## RLS

Migration mới ALTER 19 p_scope RESTRICTIVE, giữ p_tenant. Bỏ nhánh tenant chưa có
projection thì đọc cả tenant. Phiên có node cần node tự chiếu trước khi xét hậu duệ,
xã hoặc dòng nháp; node thiếu/khác tenant/closure trống đều đóng. Phiên không node
của job/Admin vẫn có ngữ nghĩa native được ghi ở W3.4, không suy từ vai KN.
Household được INSERT trước membership: WITH CHECK chỉ đòi projection của caller,
không đòi dòng membership chưa tồn tại; USING vẫn kiểm thành viên/xã.

Giữ nhánh đọc qua cấp phép W4.2 trong USING của bảng đã có p_share FOR SELECT:
foreign tenant vẫn phải qua p_share (grant, purpose, owner, node, thời hạn).
WITH CHECK không có nhánh foreign tenant; p_tenant và quyền ghi không đổi.
Đây là sửa hồi quy của bản ALTER ban đầu, không phải cấp thêm quyền đọc nội bộ.

## Tương thích native khi projection luôn có dữ liệu

Không dùng Session giả hoặc nới RLS để giữ các test cũ xanh. Fixture plot/season
phải tạo cây tổ chức qua nguồn thật; fixture HTX không INSERT projection trùng
sau source INSERT. Các số liệu dương và ca âm về tenant/phạm vi vẫn được giữ.

Các consumer native cần truyền phạm vi do server giải xuống transaction của module:
member chỉ mở phạm vi xã cho bước lookup hộ, khôi phục biến trước ghi/commit;
plot GIS/đối soát và season staff chỉ bind xã ở boundary phù hợp. Resolver chạy
trước BeginTx; joined/direct transaction bị chặn trước lời gọi port. Không xóa
node, không thay tenant/vai, không thay Principal.KN hoặc selected-session projection.
Manager/farmer, job, partner và selected KN giữ boundary riêng của mình.

Nhật ký vụ mùa phân biệt quyền trước trạng thái: với manager/farmer, vụ ẩn hoặc
không tồn tại cùng bị từ chối. Chỉ trả “chưa mở vụ” sau khi port thửa, ngoài
transaction, xác nhận quyền ghi. Thửa của hộ khác không trở thành oracle về vụ.

### Tombstone khi thửa rời HTX

Migration `20261024050100_pf_coop_plot_scope_tombstone` thêm projection sáu cột:
tenant, ID thửa, HTX cũ, xã, revision và sync_seq. Không có hình, tên, chủ mới.
Trigger AFTER UPDATE ghi mất quyền cùng transaction nguồn; chỉ owner NOLOGIN,
NOBYPASSRLS được INSERT, app chỉ SELECT. FORCE RLS đòi đúng node HTX cũ và self
projection; xã đơn thuần, thiếu node/tenant/projection đều không đọc được.
UPDATE/DELETE/TRUNCATE bị chặn, hàm không có PUBLIC EXECUTE hoặc tham số trực tiếp.

Native manager lấy current và tombstone bằng một SQL statement snapshot, chung
ORDER/LIMIT; chỉ fetch geometry sau page, không join tombstone vào thửa của chủ
mới. Hai SELECT Read Committed có thể đẩy cursor qua một upsert ghi xen kẽ;
regression HTTP writer đã kiểm trường hợp này. Thửa quay lại A sau A→B→A trả
upsert mới, không phát tombstone cũ đè lên. KN/selected và farmer giữ feed sẵn có;
contract cấp tài khoản farmer không có org_ref, scope theo household.

Backfill đọc danh sách tenant từ metadata chain_head, rồi đọc nguồn FORCE RLS
trong từng tenant của migration. Cấp cursor mới cho từng chủ cũ/thửa DISTINCT để
thiết bị đã đi qua cursor nguồn vẫn nhận lệnh xóa. Giữ nguyên nguồn, sổ và k_ref;
khôi phục tenant/node/xa của caller. Khóa writer bảng plot trong migration, không
lấy advisory lock sau table lock. Không tắt FORCE RLS hoặc cấp BYPASSRLS.

## Triển khai forward-only

1. Giữ writer tổ chức và thửa ở chế độ bảo trì trong cửa sổ migration.
2. Áp bốn migration timestamp: 20261024022324/22325/22326 và 20261024050100
   (kref function, party trigger, coop policies, ID-only tombstone/backfill).
3. Với tenant cũ chưa chiếu hoặc parent thiếu, chạy worker -kref-rebuild bằng vai
   worker đã cấu hình, trong khi writer nguồn vẫn khóa; không dựng lại đồng thời
   với nguồn đang đổi. So khớp nguồn/projection theo runbook k_ref trước mở lại.
4. Bật lại nguồn và chạy ca tạo HTX native có ngữ cảnh node, retry cùng khóa và
   nhiều request đồng thời. Không đổi tenant hoặc dữ liệu sổ để chữa lỗi quyền.

Parent chưa có projection làm giao dịch tạo con rollback hoàn toàn, không sinh
closure tự nó mà thiếu tổ tiên. Lỗi này trả 503 party.dependency_unavailable với
chi tiết không chứa dữ liệu cá nhân; quản trị viên cần khôi phục projection cha trước
khi thử lại, không nới RLS. Không hứa hỗ trợ nguồn đang chạy cùng Rebuild từ
snapshot cũ; cửa sổ bảo trì là điều kiện vận hành cho Rebuild hiện hữu.

## Bằng chứng

Nguồn `f67c8cd8`, Go 1.26.10, CI=true, bỏ PgBouncer, PostgreSQL riêng qua
`scripts/pg-ephemeral.ps1`. Log và proof giữ tại TEMP, tiền tố
`agrigreen-p0130-combined-f67c8cd8-*` và `agrigreen-p0130-combined-final-proof-20261024.json`:

| Kiểm | Kết quả |
|---|---|
| `go test -json -count=1 -p 1 -timeout 30m ./internal/platform/cooperative/plot/...` | 54 ca chính PASS, 0 FAIL, 0 test SKIP; 3 package có test và 2 package không có test đã compile. Handler 47 ca chính gồm tombstone, cursor HTTP xen kẽ, tenant/node và scope rỗng. |
| `go test -json -count=1 -p 1 -timeout 30m ./migrations -run '^(TestCoopScopeRLS_W34\|TestShareScopeRLS_W42\|TestHTXAnchorRLSAfterClaim_D2\|TestKNPrincipalHTXDataAccess_KNSPLIT0\|TestNoCrossModuleSchemaPrivileges_W91\|TestRLSExplainNoPerRowFunctions_W91\|TestRLSExplainGateDetects_W91)$'` | 7 ca chính PASS, 0 FAIL/SKIP; EXPLAIN 499 bảng, không bỏ qua/ngoại lệ. Năm XFAIL legacy tenant lưu ký KNSPLIT0 vẫn báo riêng; không coi KNSPLIT-4 hoàn tất. |
| `go test -json -count=1 -p 1 -timeout 30m ./tests/p0kref ./cmd/drill -run '^(TestP0KRef.*\|TestDrillKRefRebuildMatchesSource_W25)$'` | 11 ca chính PASS, 0 FAIL/SKIP; cả P0 và drill đều chạy thật. |

P0 kiểm 24 request tạo 13 Nút/hồ sơ thật không503/409/orphan trong ca đồng thời;
projection hiện hữu, rollback nguồn/ledger/outbox/closure, race MST, cha chờ move
rồi worker/retry cùng khóa, rename và thiếu đường cha. Drill giả lập tenant chưa
chiếu bằng xóa đúng projection tenant test, không tắt trigger hoặc đổi rebuild.
Bộ tổng hợp log đầu dừng nhầm vì hai package `[no test files]`; đã phân loại lại
từ log nguyên vẹn, chỉ chạy tiếp phần chưa chạy, không gọi test SKIP là PASS.

Kiểm native độc lập, cùng byte production trong cây cuối:

- Member `d7a02c07`/cherry `25a1a973`: 14 ca chính + 4 ca con PASS, 0 FAIL/SKIP;
  lookup cùng pool một kết nối, restore trước ghi/commit, KN/selected đóng nguyên.
- Season `9d9b5bf8`/cherry `8ec08e31`: 33 ca chính PASS, 0 FAIL/SKIP; gồm scope
  rỗng, joined/foreign context, quyền log/batch và sync. Fixture “chưa mở vụ” dùng
  thửa của hộ đã liên kết, giữ expected409; thêm ca hộ ngoài quyền bị từ chối.
- Build và vet toàn repo, modgen -check, migratecheck -base origin/dev, archlint,
  knboundary, contractcheck, docscheck, diff --check tại `f67c8cd8` đều đạt.

Lịch sử RED được giữ: CI080c 58 ca chính FAIL/9 package; plot trước sửa2FAIL,
season30b 2FAIL, season2ea 1FAIL (fixture hộ ngoài quyền), tombstone lượt đầu
1FAIL fixture assign cùng chủ. Các sửa không hạ expected hoặc tắt kiểm quyền.
Fixture HTX `0d4c355d`/cherry `5569907a`: 11PASS/3FAIL thuộc contract/context
publication KN, đã chuyển lane:kn #145; không giả Session hoặc nới RLS để chữa.

Chờ cổng #81/#140/#145, web #134/#138, full CI mới và Claude Review OK tại SHA cuối.
Chỉ `ticket.ps1 merge` vào dev khi đủ cổng; roadmap vẫn chưa đánh hoàn tất.
