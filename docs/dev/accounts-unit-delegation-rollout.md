# Accounts: nối owner đơn vị và ủy quyền (#196)

Gói `KNSPLIT-2-ACCOUNTS-POLICY-ROLLOUT` nối hợp đồng `accounts/port.UnitDelegationPolicy` và adapter `extension.UnitDelegationPolicy()` đã vào dev ở #195. Đây là thay đổi nơi cung cấp policy; không bổ sung luật KN, vai realm, quyền Admin hay màn hình.

## Thứ tự triển khai

1. **Dependency/cấu trúc:** không thêm thư viện. Thêm `app/unit_delegation.go`; `accounts.Deps` và `app.Service` nhận `UnitDelegations`. API assembly và cả năm đường worker qua `workerAccounts` cấu hình factory owner. Cấu hình định nghĩa sai dừng khởi tạo.
2. **Data:** không migration. Giữ `internal_unit_event`/`internal_unit`, `delegation_event`/`delegation_current`, sổ chỉ ghi thêm, RLS tenant và khóa idempotency hiện hành.
3. **Service:** owner cung cấp loại đơn vị, loại có phòng ban, loại tổ chức, seed/trạng thái, quyền, thời hạn, vai người nhận, quyết định cấp/thu hồi và phạm vi nhân sự. Shared giữ kiểm mã/tên/thời gian/lý do, tenant, người giao, lần gán người nhận đang hiệu lực cùng đơn vị/phòng phủ toàn bộ hạn, truy vấn người hiện hành và ledger. Mọi lời gọi provider/port được thực hiện ngoài transaction; transaction chỉ dùng giá trị đã lấy và store accounts.
4. **API:** `kind_label`/`kinds`, `power_label`/`powers` và `delegation_max_days` dùng cùng định nghĩa với service. Giữ route, DTO, kiểm Admin/MFA và mã lỗi. Provider xử lý phiên chủ do middleware giải; không chọn policy từ header, role realm hay dữ liệu client.
5. **UI:** không thay đổi; các API và thứ tự danh mục KN hiện hành giữ parity.

`UnitDelegations=nil` giữ bridge cho consumer chưa chuyển. Khi đã cấu hình, lỗi là kết quả cuối; `handled=false` với phiên đã chọn hoặc ràng buộc compatibility dở dang bị từ chối. Chỉ request native thật giữ kết quả native (428 cho thao tác cần phiên KN). Quyết định cấp thiếu đơn vị/phòng và phạm vi nhân sự thiếu dữ liệu không được dùng để mở truy vấn rộng. `ErrOwnerAttributesRevoked` được ánh xạ thành `accounts.context_revoked`.

## Nguồn và phạm vi

Nghiệp vụ đối chiếu phần Function chi tiết BRD 2.0 HCTH v4.0: FN-04 (dòng phân quyền theo Nút/phòng và lịch sử), FN-05 BR-04/AC-01 (dữ liệu theo phiên), FN-07 BR-04/BR-07/AC-02 và HS-37 (phạm vi người thuộc đơn vị và cán bộ xã được phân công). #195 là hợp đồng kỹ thuật đã kiểm, không thay thế BRD 2.0. Gói này nối consumer, không tuyên bố hoàn tất toàn bộ các FN đó.

Luật owner sản xuất, seed và 30 ngày giữ nguyên #195. Chỉ fixture kiểm thử có định nghĩa khác để chứng minh consumer thực sự dùng dữ liệu owner. HTX/MKG vẫn dùng catalog native, quyền quản trị và MFA gốc. Giữ compatibility order của #193, globals và domain helpers; việc retire thuộc #57. Các writer/lease/lifecycle và tenant cutover #87–94 vẫn là gói riêng; không suy diễn G1/4b đã hoàn tất.

## Kiểm lại

```powershell
$env:GOTOOLCHAIN='go1.26.10'
go run ./tools/modgen -check
go run ./tools/migratecheck -base origin/dev migrations
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/platform/accounts/... ./internal/kn/extension ./tests/knaccounts ./tests/contextdata ./tests/notify ./cmd/api-platform/assembly ./cmd/worker ./internal/kn/hrm/... ./migrations
```

Chạy thêm build/vet, archlint, contractcheck, knboundary `-base origin/dev`, docscheck, mfaflagcheck và lint mới bằng 0. Race chạy trong CI Linux trên đúng SHA; không coi Windows unit test là race. PG/HTTP dùng issuer và Keycloak bộ nhớ, không thay thế chứng nhận OIDC/realm sống. Bằng chứng kiểm và mutation FAIL/control PASS gắn với SHA trong bình luận tự review #196.
