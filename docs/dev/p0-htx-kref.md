# P0-HTX-KREF (#130): tạo Nút và bản chiếu cùng giao dịch

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
- Toàn bộ `party/app` và `party/handler` PASS trên ứng viên `080c174e` có cùng
  byte mã production/migration với nguồn cuối. Lượt ghép đó có hai lỗi fixture
  đọc nhầm `seq` thay vì `event.seq`; e92 sửa đúng fixture và chạy lại đủ 10 ca.
  Không gọi toàn bộ lệnh ghép cũ là PASS.
- [Cổng tĩnh của 080c](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/actions/runs/40058807292/job/120767667765)
  và [Compose VPS thật cùng guard/migration](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/actions/runs/40058807178)
  PASS. Runtime này kiểm cây kết hợp #130/#131/#132, không thay nghiệm thu VPS đích.
- Cổng ledger upgrade PF/MKG, schema isolation và EXPLAIN 498 bảng của d3 đã PASS
  trước khi thêm guard cha; đây là bằng chứng cũ, không thay kiểm của cây cuối.

Gói sẵn sàng review mã, chưa gộp. Full CI vẫn phải xanh sau các dependency fixture
#81/#140 và web #134/#138; cần Claude bình luận Review OK tại SHA cuối trước
`ticket.ps1 merge`. Không dùng kết quả riêng từng package để bỏ cổng CI tổng thể.
