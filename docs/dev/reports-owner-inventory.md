# Kiểm kê chủ sở hữu reports — KNSPLIT-2-REPORTS-OWNER (#54)

Đối chiếu trên `dev` nguồn `a6d9db28527670e838f4bbae10322591256c5bdd` (21/10/2026). Phạm vi gói là kiểm kê và xác minh các điểm đảo phụ thuộc đã có; không thêm registry, sửa quyền, di chuyển engine hay thay đổi tệp đã xuất. Nguồn yêu cầu: [ADR-0055](../adr/0055-tach-he-thong-khuyen-nong.md), [quy tắc module](modules.md), issue #54 trong `MEKONG-GREEN/Agrigreen_demo_v2`.

## Các điểm đăng ký và lắp ráp đã có

| Điểm mở rộng do reports định nghĩa | Chủ KN cung cấp | Ràng buộc hiện có |
|---|---|---|
| `port.SourceBuilderRegistration`, `app.SourceBuilderRegistry` | `extension.ReportBuilders` trong `internal/kn/extension/module_reports_builder.go` | Registry theo instance, từ chối thiếu hàm/trùng nguồn. Truy vấn tạo từ job đã đóng băng, tenant tường minh; sao chép phạm vi/cột. Lỗi builder là kết quả cuối, không rơi về nguồn khác. |
| `port.CatalogRegistration`, `app.CatalogRegistry` | `extension.ReportCatalog` trong `module_reports_catalog.go` | Schema cột, nhãn, cấp và mẫu KN thuộc owner. Registry sao chép dữ liệu, từ chối đăng ký mơ hồ; thiếu contribution chỉ còn nguồn/mẫu native. |
| `port.Policy`, `app.PolicyRegistry` và `port.DispatchProfile` | `extension.ReportPolicy` trong `module_reports_policy.go` | Vai đọc/lập lịch/nguồn, tham số đính kèm và nhãn gửi thư do owner đăng ký. Không suy ra quyền hay branding KN khi không đăng ký; profile trùng hoặc không hợp lệ bị từ chối. |
| `port.FormSeedRegistration`, `app.FormSeedRegistry` | `extension.ReportFormSeeds` trong `module_reports_seeds.go` | 14 mẫu lưới nạp sẵn của KN do owner giữ. Contribution được sao chép; thiếu bộ mặc định trả lỗi phụ thuộc thay vì tự nạp mẫu KN. |
| `port.SessionResolver` | `extension.ReportSessionResolver` trong `module_reports_session.go` | Chỉ chiếu quyền/phòng/phạm vi; reports giữ nguyên actor và tenant đã xác thực. Dispatch yêu cầu resolver đã xử lý và cho phép. Owner từ chối hoặc lỗi không rơi về vai realm; forms native chỉ dùng đường realm khi owner không nhận request. Owner chặn ghi dispatch trong phiên chỉ đọc. |
| `port.Consolidations` | `kn.Consolidations()` | Kỳ và lần tổng hợp bất biến kèm nhãn kỳ, lịch/khe, phòng, tỉnh, dòng và tổng. Cả hai hàm nhận tenant tường minh để worker không phải giả lập token. `DispatchHistory` là chiều đọc ngược do reports cung cấp. |

`cmd/api-platform/assembly/modules.go` lắp ráp cả sáu contribution vào `reports.Deps`. Đây là composition root được phép biết KN; reports không import owner. Các dependency dùng chung như Units, Orgs, Params và Store vẫn là cổng nền tảng, không phải registry KN mới.

`app/build.go` chỉ gọi builder đã đăng ký hoặc hai nguồn native `mechanization`/`cropcycle`; không có fallback dữ liệu KN. Metadata KN và dữ liệu tham chiếu nền tảng được dựng ở adapter owner; kiểm thử `tests/platformdata/kn_consumers_test.go` đối chiếu reader native/published theo tenant. Các luồng native HTX/MKG và lịch gửi native được giữ nguyên.

## Những chỗ còn chữ KN nhưng không còn nhánh owner

- `app/dispatch.go` và `app/dispatch_link.go` còn chú thích, lỗi và log nhắc Chi cục, TTKN hoặc tên cũ `pf/extension`; `port/consolidations.go` còn tiền tố chuỗi lỗi `extension:`. Đây là từ ngữ của luồng xuất/gửi từ ảnh chụp tổng hợp hiện hữu, không phải luồng export native HTX/MKG hay phép chọn vai, phòng, schema hoặc lịch theo KN. Gói kiểm kê không đổi mã lỗi/nội dung đang dùng.
- Branding email, lời chào, đơn vị gửi, tiền tố khóa mẫu và khóa Google Sheet đã nằm trong `DispatchProfile` do KN đăng ký. Không thêm bộ cấu hình thông điệp thứ hai.
- Lưới `per_commune`, dòng `region`/`province`, các cột cố định và phép sắp dòng là hợp đồng engine lưới đang dùng. Nhãn/cột/mẫu cụ thể đến từ contribution và ảnh chụp owner; không chuyển engine dùng chung sang KN.
- `domain.Schedule.Slot` là lịch chạy export native. Lịch nghiệp vụ KN và nhãn kỳ đi qua `Consolidations`/builder; không thay lịch native theo M7 của KHTC.
- Van Google Sheet chỉ cho cột số và danh sách nhãn tổ chức cố định, vẫn tắt mặc định và cần điều kiện ADR/cấu hình đang có. Không mở rộng allowlist để làm mất kiểm soát dữ liệu.

Trên nguồn đối chiếu, `tools/knboundary/baseline.yaml` không có mục xuất phát từ `internal/platform/reports`; `reports/module.yaml` không phụ thuộc module KN. Không dùng việc bỏ chữ trong chú thích để tuyên bố giảm coupling. Cổng boundary vẫn phải chạy và không tăng baseline.

## Bằng chứng cần giữ khi nghiệm thu

- Tree Git `tests/golden` nguồn là `06a780646532b38221d14cea61d636b1f78c5033` (35 tệp). So tree hash và danh sách blob trước/sau, không sinh lại golden.
- Pure reports kiểm tính bất biến/cách ly và thiếu registration: `TestRegisteredSourceBuilderBindsJobAndCopies`, `TestRegisteredSourceBuilderErrorIsFinal`, `TestSourceBuilderRequiresRegistration`, `TestReportCatalogRequiresRegistration`, `TestDispatchProfileRequiresRegistration`, `TestFormSeedsRequireRegistration`, `TestReportPolicyRequiresRegistration`, `TestReportSessionRequiresRegistration` và `TestRegisteredFormSessionPrecedesRealmPrivileges`.
- Kiểm thử PostgreSQL: `./tests/reports` cho job bất đồng bộ, tenant/scope, quota/DLP, replay, lịch một lần mỗi slot, thư và export HTX; `./tests/reportforms` cho engine/import; `./tests/platformdata` cho reader published; `./cmd/api-platform/assembly` cho wiring thật.
- Owner HTTP: `TestCapabilitiesRequireOwnedSessionThroughHTTP` kiểm phiên thiếu, sai owner, payload hỏng và legacy không vượt phiên được chọn, từ chối trước transaction. `TestReportSessionPolicyRolesAndDepartments` kiểm policy owner và ghi dispatch trong phiên chỉ đọc. Hồi quy KN77 giới hạn vào full cycle, dispatch/email/link, KHTC frozen/final/cancel, phiên bản kế hoạch và ảnh chụp giá M4.
- Chạy trên PostgreSQL tạm riêng, Go `1.26.9`, `-count=1 -p 1 -v`; chỉ coi đạt khi exit thực bằng 0 và không có `SKIP`/`INFRA`. Chạy build/vet, modgen, migratecheck, archlint, contractcheck, docscheck, knboundary và lint theo quy tắc chung. Log và trạng thái thực được gửi kèm ticket trước review/gộp; danh sách này không tự chứng nhận kết quả chưa chạy.

## Phạm vi còn lại ngoài #54

#64 AUDIT-LOG chưa hoàn tất bởi kiểm kê này. Export audit cần bộ lọc riêng đóng băng vào job, kiểm quyền owner khi yêu cầu/replay/lịch sử/tải, reader worker được cấp quyền và biên nhận đọc bền vững. `domain.Params` hiện là bộ lọc native và được chiếu ra HTTP; không nhét payload riêng vào đó rồi làm lộ qua DTO. Nếu triển khai, mở rộng contribution builder hiện hữu với snapshot riêng/callback kiểm quyền và giữ nguyên checkpoint DLP, không dựng registry hay sổ audit thứ hai. Nguồn audit tùy biến/lập lịch cho nó chưa được cung cấp bởi #54.

Gói không chứng nhận toàn bộ KNSPLIT/G1, tenant migration, retention audit, Google Sheet production hay thay đổi M1–M7. Không migration, OpenAPI, web, dữ liệu hay hành vi runtime nào được sửa.
