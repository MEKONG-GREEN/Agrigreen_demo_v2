## BF-CN-FORM-DESIGN-EDIT-DIARY — phần selected-template native của #275

Căn cứ: CNTS Final v4.0 FN-07 B1–B4, BR-01/BR-02/AC-03, UR-TINH-07 và ma trận quyền chi tiết FN-07; standalone plan đã duyệt. Parent #191/#192 đã có trên actual dev. Đây là phần đầu trọn vẹn của #275, chưa nghiệm thu toàn bộ FN-07.

- Mỗi lần phát hành biểu kỳ CNTS phải gửi cặp `template_id` + `template_version` đã chọn từ public `TemplateCatalog`. Thiếu, không khớp code/profile/tenant hoặc version không tồn tại bị từ chối trước intent/kỳ. Preview lịch không ghi có thể bỏ cả hai; preview có chọn thì trả đúng tuple. Không chọn latest thay cho người dùng.
- Runtime nộp bài của Forms kiểm theo schema của ID đã gắn với kỳ. Cột bắt buộc, kiểu và giới hạn số của version mới có hiệu lực ở kỳ dùng version đó. Bài/kỳ cũ giữ schema, số liệu và identity cũ; chọn version cũ vẫn dùng được sau khi head tăng.
- KN ghi command phát hành bất biến trước I/O native; public `DurablePublisher` của actual dev phục hồi đúng creation receipt sau mất ACK. Audit `cnts_publication` và kết quả KN ghi cùng giao dịch. Actor, selected session, quyền hiện tại, payload, target đã giải và tuple phải khớp trước replay; không tạo thêm kỳ/event/audit. Đây là quyền hiện tại do accounts/KN cấp, không suy từ AuthTime hoặc realm alias.
- Màn BM-17 dùng bộ nhập schema thực khi cấu trúc khác lưới chuẩn; các giá trị và trường của kỳ cũ vẫn đọc theo task đã gắn template. Màn phát hành yêu cầu chọn version, hiển thị version ở preview và gửi nguyên tuple. Đổi biểu/version làm mất preview cũ. Đây là lựa chọn mẫu tại FN-07, không phải màn tham số chung (Q5).
- Ma trận ghi: selected account context Cấp Tỉnh CNTS phải có nhóm `tong_hop` tường minh; Admin quản lý định nghĩa theo native policy; lãnh đạo, viên chức, nhóm rỗng, chỉ xem, hết hạn, sai phòng/xã không mượn quyền. Template KNQG vẫn khóa qua KN và native REST/Publisher. Native policy kiểm cấu trúc như màn KN.
- Save kiểm thay đổi với `base_version` thật thay vì head hiện tại, rồi native CAS quyết định. Create mã mới giao kiểm trùng/replay cho durable definition receipt của Forms; retry canonical definition sau head tăng trả đúng original ID/version. Copy phải chọn source_version bất biến. Receipt native hiện chỉ ràng buộc canonical definition; **chưa có chứng cứ provenance copy source ID/version atomic**, phần này thuộc supplier bên dưới.

### Chuỗi phần còn lại chính thức, chờ triển khai

Actual public Field/catalog chưa có versioned kind/frequency/GPS/photo/programme settings; Fields được decode thành mảng trường trước khi lưu. Public usage chưa chứng minh có dữ liệu ở mọi runtime. FN-14 tường minh chỉ ánh xạ BM cố định. Cần generic owner-validated runtime definition + durable metadata/receipt + public usage recorder/witness (kể cả owner runtime ngoài Forms), giải first-use/sửa cạnh tranh không dựa stale read; rồi assignment theo exact identity và consumer nhật ký/lịch/mã báo cáo mới. Không đoán legacy chưa dùng, không mở vai Nông hộ đang chờ #173, không dùng role/code-prefix làm grant.

ROOT đã công bố và primary readback chuỗi [#303 runtime/settings/usage](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/303) → [#304 assignment](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/304) → [#305 runtime nhật ký/lịch/mã mới](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/305). **Các issue mới được tạo, chưa triển khai hoặc có trên actual dev**. #304 chờ actual #275 + #303; #305 chờ actual #275 + #303 + #304. Không dùng số issue giả hay coi gói khác là supplier CNTS. Hạn chế add-only của #192 còn tồn tại do thiếu public usage, không phải quy tắc nghiệp vụ mới. Nhật ký động, GPS/ảnh/tần suất theo mẫu, genuine programme owner của mẫu, loại lịch và mã báo cáo mới chưa đạt trong phần đầu này.

### Tệp → bước → Function

| Tệp | Bước PLAN-BRD2 | FN/UR |
|---|---|---|
| `migrations/pf/*_pf_extension_cnts_publish275.up.sql`, `reportcycle/store/cnts_publish275.go` | 2: dữ liệu append-only/RLS, intent/result | FN-07 B4, BR-01 |
| `reportcycle/app/cnts_selection275.go`, `cnts_publish275.go`, `cnts_forms.go`, `cnts_publish.go`, `cnts_design.go` | 3: chọn version, current authority, durable replay/CAS | FN-07 BR-01/02, AC-03, UR-TINH-07 |
| `reportcycle/handler/cnts*.go`, `module_cnts275.go`, `module_reportcycle.go`, `module.go`, OpenAPI module fragments | 4: contract, guard trước HTTP cache, native owner policy | FN-07, ma trận FN-07 |
| `apps/web-ops/src/kn/cnts/periods/*`, `catch/CatchScreen.tsx`, `catch/catch.ts`, mock CNTS design/periods, generated OpenAPI | 5: chọn/giữ version, preview, consumer | FN-07 B1–B4 |
| `tests/kn77/cnts_*`, `tests/kn77/closed_figures_pub_integration_test.go`, `adhocintegration/dashboard202_live_integration_test.go`, web CNTS tests, tài liệu UAT này | 6: native regression, replay/current authority, consumer nguồn công bố và UI | FN-07; giữ hành vi consumer nguồn |

Đường dẫn service/store/handler trong bảng tính từ `internal/kn/extension/`. Dependency mới: không có. Không thay menu hay baseline. File sinh được generator tạo lại.

### Cách chạy và giới hạn bằng chứng

Đặt GOTOOLCHAIN theo go.mod (go1.26.10), GOTMPDIR/TEMP/TMP theo môi trường kiểm đã cấp; GOMAXPROCS=2. Áp migration mới qua bộ migrate của API, chạy API/web như hướng dẫn dev của repo.

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./tests/kn77 ./internal/kn/extension/... ./internal/platform/forms/... ./cmd/api-platform/assembly ./migrations
cd apps/web-ops
npm ci
npm run check:api
npx tsc --noEmit
npm run lint
npm test
npm run build:mock
```

Native fixtures dùng real KN selected-session registry, Forms/KN và PG thật; chúng thay account middleware, không chứng minh OIDC triển khai. Mock/web/browser bằng chứng riêng với native PG. Full Linux CI/race đúng SHA, public self-review và integration vẫn do ROOT thực hiện sau gói cục bộ; Windows CGO0 không cung cấp race.
