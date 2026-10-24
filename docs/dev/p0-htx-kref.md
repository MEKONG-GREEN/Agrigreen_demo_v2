# P0-HTX-KREF (#130): tạo Nút và bản chiếu cùng giao dịch

**Đang sửa hồi quy; chưa sẵn sàng tích hợp.** Full CI ứng viên `080c174e`
đã kết thúc FAIL (58 ca chính, 9 package), không còn là một lượt đang chờ.
Mã production đã thay đổi sau ứng viên đó; các PASS cũ chỉ là bằng chứng lịch sử.
Không dùng chúng thay kiểm thử và Review OK của SHA cuối.

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

CI còn lộ hai đường cần sửa trước khi chốt gói: tombstone khi thửa rời HTX phải
chỉ mang ID/phiên bản, không đọc hình/thuộc tính của HTX mới; log vụ mùa bị RLS
che phải phân loại lỗi quyền trước lỗi trạng thái. Các bản sửa đang được kiểm
riêng, chưa được tính là PASS của toàn bộ cooperative.

## Triển khai forward-only

1. Giữ endpoint tạo/di chuyển tổ chức ở chế độ bảo trì trong cửa sổ migration.
2. Áp ba migration timestamp của gói (kref function, party trigger, coop policies).
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

- Nguồn và fixture `e92fd017`: Go 1.26.10, `scripts/pg-ephemeral.ps1`,
  `go test -v -count=1 -p 1 -timeout 20m ./tests/p0kref` — 10 ca PASS,
  0 FAIL, 0 SKIP; database và cluster tạm đã dọn. Gồm bản chiếu có dữ liệu,
  rollback nguồn/outbox/closure, 24 yêu cầu đồng thời với 13 hồ sơ thật,
  replay cùng khóa khi kiểm MST chạy đua, RLS đóng, quyền module/cross-tenant,
  cha chờ di chuyển rồi worker đồng bộ và retry, đổi tên nguồn, đường cha thiếu.
- Fixture drill `b8e1a3c5`: `go test -v -count=1 -p 1 -timeout 20m
  ./cmd/drill -run ''^TestDrillKRefRebuildMatchesSource_W25$''` trên Go 1.26.10
  qua PostgreSQL tạm — 1 PASS, 0 FAIL, 0 SKIP; cluster đã dọn. Fixture xóa đúng
  tenant giả khỏi projection sau INSERT nguồn để mô phỏng dữ liệu cũ chưa chiếu,
  giữ yêu cầu Before=0/After=6 và lần rebuild lặp không drift. Không sửa mã rebuild
  hoặc tắt trigger. Lượt trước ở `72a8dfb9` FAIL vì fixture giả định INSERT chưa
  tự chiếu; không dùng kết quả đó làm PASS.
- Toàn bộ `party/app` và `party/handler` PASS trên ứng viên lịch sử `080c174e`.
  Nguồn cuối nay có thêm sửa policy và boundary native, cần kiểm lại theo SHA mới. Lượt ghép đó có hai lỗi fixture
  đọc nhầm `seq` thay vì `event.seq`; e92 sửa đúng fixture và chạy lại đủ 10 ca.
  Không gọi toàn bộ lệnh ghép cũ là PASS.
- [Cổng tĩnh của 080c](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/actions/runs/40058807292/job/120767667765)
  và [Compose VPS thật cùng guard/migration](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/actions/runs/40058807178)
  PASS. Runtime này kiểm cây kết hợp #130/#131/#132, không thay nghiệm thu VPS đích.
- Cổng ledger upgrade PF/MKG, schema isolation và EXPLAIN 498 bảng của d3 đã PASS
  trước khi thêm guard cha; đây là bằng chứng cũ, không thay kiểm của cây cuối.

- Nguồn `ee296397`: Go 1.26.10/private PG, bốn nhóm migration W3.4/W4.2/D2/KN
  và `tests/p0kref`, drill: 15 ca chính PASS, 0 FAIL, 0 SKIP. Năm XFAIL legacy
  KNSPLIT0 có sẵn vẫn được báo riêng; không gọi toàn bộ KNSPLIT hoàn tất.
- Sửa member `d7a02c07` (cherry `25a1a973`): toàn bộ handler member 14 ca chính
  + 4 ca con PASS, 0 FAIL/SKIP; kiểm lookup cùng pool một kết nối, restore trước
  ghi/commit và KN/selected/empty projection. PostgreSQL tạm đã dọn.
- Native plot `50ee8c07` (cherry `90f80277`): 36 ca chính + 6 ca con PASS,
  2 FAIL, 0 SKIP; hai lỗi là consumer conflict và tombstone transfer, chưa gọi
  toàn bộ plot PASS. Bổ sung consumer ở `8b6d1442` (cherry `74c9e2dc`) mới kiểm
  tĩnh; chờ ghép tombstone rồi chạy toàn bộ handler.
- Fixture HTX `0d4c355d` (cherry `5569907a`): 11 PASS, 3 FAIL, 0 SKIP. Lỗi còn lại
  thuộc contract/context publication KN; đã mở lane:kn #145, không sửa assertion
  dương hoặc tạo đường ghi HTX từ KN để chữa test.
- Season `30b92145`: 29 PASS, 2 FAIL, 0 SKIP; đang kiểm sửa classification và
  joined preflight ở `2eaeea08`, chưa có kết quả cuối.

Chưa ready lại hoặc gộp. Full CI phải xanh sau sửa hồi quy và các dependency
#81/#140/#145, web #134/#138. Cần Claude bình luận Review OK tại SHA cuối trước
`ticket.ps1 merge`. Không dùng PASS từng package để bỏ cổng tổng thể; không
đổi test FAIL/SKIP thành đạt và không dùng toolchain host 1.27 thay Go 1.26.10.
