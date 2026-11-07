## HCTH-V4-APPRAISAL-LITE — Đề xuất xếp loại v4.0 (HC-2, #281)

Nguồn duy nhất là Function và bảng trường HCTH v4.0 trong BRD 2.0 đã chốt:
[BRD HCTH v4.0](https://drive.google.com/file/d/1r0TqvDU8yRZSdm5BwabvWkgElm8ynC7Q/view).
Phụ thuộc HC-0 (#256); tích hợp sau HC-3 (#278) để hoàn tất tách đánh giá khỏi nâng bậc.

### Lựa chọn theo Function chi tiết

- FN-16/UR-HC-20: nhóm viên chức, lãnh đạo hoặc cán bộ xã; chụp chức vụ và đơn vị quản lý tại ngày chốt, chọn Mẫu số 05/06 từ chức vụ. Chỉ có hạn gửi của các phòng; không có hạn hoặc bước tự chấm.
- FN-17/UR-LDP-01, DG-04–08: lãnh đạo nhập cả hai mức đề xuất từ tài liệu cá nhân và cuộc họp đơn vị; HCTH nhập Mẫu số 06. Tài khoản cá nhân, phiếu điểm và bản scan không phải điều kiện nhập.
- Theo bảng trường DG-06 và AC-01, thiếu căn cứ khi **một trong hai** mức là HTXSNV/KHTNV bị chặn ngay lúc lưu. Căn cứ tối đa 2.000 ký tự; không tự đặt giới hạn tối thiểu 10 ký tự cho căn cứ. Lý do trả/mở lại vẫn tối thiểu 10 ký tự theo ràng buộc sổ hiện hữu.
- DG-10 có năm trạng thái: `not_started`, `entered`, `sent`, `change_requested`, `closed`. Các mã trạng thái cũ chỉ được giải mã trong lịch sử; không có writer mới sinh chúng.
- FN-18/UR-HC-21, DG-09: chốt dùng kết luận chính thức nếu có, nếu không dùng mức tập thể. Kết luận quý và năm được đọc trong hồ sơ từ sổ đánh giá. Không ghi `annual_rating`, không cung cấp dữ liệu điều kiện nâng bậc.
- Chặn xuất/chốt nếu trong đợt còn người chưa gửi đủ hoặc thiếu căn cứ. Thiếu bản ký là cảnh báo có xác nhận; không tự thay thành điều kiện bắt buộc có tệp. Có thể mở lại đúng một người với lý do, không mở người khác.

### Triển khai theo năm bước

1. **Khởi tạo:** không thêm dependency. Dùng `shared/tabular`, ledger, vault, export và UI kit hiện hữu.
2. **Data:** migration `20261107203926_pf_hrm_appraisal_proposal_v4` chỉ thêm guard INSERT và thu quyền ghi `annual_rating`. Không thay schema/payload của sổ cũ, không UPDATE/DELETE/DROP dữ liệu. Các đợt mới mang `workflow_version=hrm.appraisal-proposal/v4`; đợt cũ chỉ đọc. Đợt cũ chỉ đọc không tính là workflow đang mở để chặn chuyển đổi cùng kỳ; không sửa trạng thái cũ. Hai yêu cầu mở v4 cùng kỳ/nút đồng thời chỉ có một lần thành công.
3. **Service:** snapshot/HS37, lưu hai mức, gửi nguyên tử theo đơn vị, trả/chốt/mở lại và kết quả hồ sơ. Nhập Excel kiểm toàn bộ dòng rồi ghi nguyên tử; tên phải khớp duy nhất trong phân công, có thể dùng mã hồ sơ khi trùng tên. Kho định danh và I/O tệp nằm ngoài giao dịch ghi.
4. **API:** bỏ POST mẫu `form`, ghi điểm/scan và POST `/staff/{id}/ratings`; giữ GET lịch sử/phiên bản/lần xuất cũ có phân quyền. Thêm nhập/tải mẫu Excel FN-17 và GET `/staff/{id}/appraisal-results`. Phạm vi tenant và nút được kiểm trên mọi đường đợt, kể cả cùng actor/key thử lại sau khi đổi phân công; không trả metadata cũ trước bước kiểm quyền.
5. **UI:** nhóm/hạn gửi/loại trừ, đề xuất DG, lọc và tiến độ, Excel, Mẫu 05/06, bản ký, chốt/mở lại, thẻ kết quả hồ sơ; route đề xuất dùng `/de-xuat/{staff}`. Route `/phieu/{staff}` cũ trả404; deep link thông báo cũ được chuẩn hóa sang reader mới. Màn thiết kế phiếu đã bỏ; dữ liệu phiếu/điểm cũ vẫn xem được ở chế độ chỉ đọc.

### Bảo toàn dữ liệu và quay lui

`TestHRMAppraisalLegacyReadAndMigration281` tái dựng dữ liệu trước nâng cấp trên owner transaction của **database kiểm thử riêng**, áp guard mới, đối chiếu số dòng, toàn bộ JSONB và chuỗi băm của phiếu/điểm/lần xuất và `annual_rating` lịch sử. Savepoint diễn tập rollback rồi áp lại migration; cả ba thời điểm giữ nguyên count/hash, kiểm trigger và thu quyền INSERT. Reader chi tiết, lịch sử và phiên bản đều có cờ chỉ đọc; tải lại bảng điểm cũ không đổi byte. Bộ HC-0 tiếp tục kiểm hai action cá nhân đã bị bỏ. Các fixture owner không phải đường ghi của ứng dụng.

Mock của bốn nhóm writer thành công (mẫu, mở đợt, đề xuất và thao tác đợt) dùng journal riêng và kiểm lại phân công trước replay. Cache replay chung chỉ nhường nhóm `/v1/hrm/appraisals/` này cho owner; xuất/nhập tệp giả lập trả 503 và không ghi thành công giả.

Fault có kiểm soát bỏ guard căn cứ trong `ValidateAppraisalProposal` phải làm `TestAppraisalV4RejectsIncompleteAndScoring281` thất bại bằng assertion; khôi phục byte/hash chính xác rồi kiểm lại PASS, không SKIP.

Migration forward-only không có thao tác xóa dữ liệu hoặc phục hồi writer cũ. Khi quay lui bản ứng dụng, giữ guard DB và dữ liệu lịch sử chỉ đọc; không hạ quyền bảo vệ để chạy lại luồng chấm điểm.

### Kiểm tra và chạy lại

Đặt toolchain từ `go.mod`; cấu hình PostgreSQL cục bộ theo hướng dẫn `pg-ephemeral`, không dùng cụm dùng chung:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/hrm/... ./tests/hc30/... ./tests/hcplan/... ./tests/notifyconsumers/... ./tests/notify/... ./cmd/api-platform/assembly/... ./cmd/hrmimport/... ./internal/platform/party/... ./internal/kn/extension/menu/... ./tests/kn77/... ./migrations/... ./internal/shared/dbadmin/... ./tools/migratecheck/... ./internal/shared/tabular/...
```

Kiểm tĩnh: gofmt, build, vet, modgen/check, migratecheck với `origin/dev`, archlint, contractcheck, knboundary, docscheck, mfaflagcheck và lint chỉ lỗi mới. Windows native không hỗ trợ `-race` trong cấu hình không CGO; Linux CI trên SHA tích hợp phải chạy race.

```powershell
cd apps/web-ops
npm ci
npm run gen:api
npm run check:api
npm run lint
npm run typecheck
npm test
npm run build:mock
npm run e2e -- tests/e2e/hrm-appraisal.spec.ts
```

E2E dùng bản production và clock giả; mock không tạo bản ký hoặc tệp xuất giả thành công. Word/Excel, quyền tệp và checksum được kiểm qua native HTTP trên PostgreSQL thật. Kết quả local không thay xác nhận CI/tích hợp trên SHA cuối.
