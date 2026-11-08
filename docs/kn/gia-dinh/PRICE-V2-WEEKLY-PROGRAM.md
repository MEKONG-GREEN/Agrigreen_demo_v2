## PRICE-V2-WEEKLY-PROGRAM — FN-TI-02.13

Chỉ theo Function/data Báo giá v2.0 final; hai màn Chương trình thông tin và Đào tạo huấn luyện và hợp tác. Không hồ sơ lớp, học viên, hợp đồng, kết nối website hay nguồn ngoài.

- Danh mục 19 dòng thông tin (2 nhóm, 17 mục nhập số) và 6 mục đào tạo lấy tên/quy mô từ hai bảng I/II trong **BÁO CÁO TIẾN ĐỘ 12 CT KHUYẾN NÔNG.xlsx**, đối chiếu phụ lục kế hoạch 12 chương trình năm 2026 theo Quyết định 2855/QĐ-UBND ngày 19/6/2026. Tên nhóm “Báo và PTTH An Giang” theo chữ Function chi tiết, sửa lỗi gõ “PTTTH” trong workbook. Chỉ nạp danh mục/quy mô; tenant mới có số tuần và đầu kỳ bằng 0. Không chép số thực hiện lịch sử của workbook vào runtime hoặc kho mã.
- Tuần đầu được khởi tạo nguyên tử khi người có quyền mở chương trình lần đầu, theo cơ chế mặc định theo tenant hiện có của module; không ghi lại danh mục của tenant đã có sổ. Lịch lấy `TicketService.Weeks` do hệ thống nối sẵn, còn profile native không nối dùng cùng quy tắc tuần mặc định hiện hữu. Không thêm màn tham số. Tiêu đề lấy số tuần/khoảng ngày thật của kỳ, không gắn cứng ngày hiện tại.
- Quy mô gốc năm 2026 được giữ nguyên. Điều chỉnh có lý do áp cho bản nháp hiện tại và được kế thừa sang tuần sau; các input tuần đã chốt giữ nguyên. Quy mô bằng 0 có tỷ lệ `null` (không chia 0), hiển thị chưa có quy mô. Tỷ lệ làm tròn 2 số lẻ; KPI đếm các mục leaf, không cộng mục nhóm.
- Mỗi chương trình có một stream snapshot chỉ ghi thêm. CAS dùng phiên bản đầu stream, vì thay đổi danh mục hoặc xóa ảnh hưởng nhiều tuần. Mọi lưu/chốt/nhập/điều chỉnh/thêm/xóa có actor và thời điểm; idempotency được kiểm lại quyền/phạm vi trước phát lại và có khóa riêng theo tenant/profile/chương trình.
- Ngoại lệ xóa theo Function: thao tác chỉ từ bản nháp mới nhất, cảnh báo ảnh hưởng cả tuần cũ và xác nhận khi có dữ liệu. Sự kiện tombstone lọc mục và mục con khỏi các phép chiếu/tổng mọi tuần, không UPDATE/DROP input, payload hoặc băm ledger đã chốt. Không có thao tác mở lại tuần đã chốt.
- Tiền là số nguyên đồng (tối đa 18 chữ số), tiến độ/quy mô tối đa 15 chữ số nguyên và 4 số lẻ. Kiểm cú pháp/độ dài trước parse decimal; không nhận số mũ, âm, NaN/Infinity. Cờ đã nhập phân biệt ô trống với số 0 để cảnh báo ghi đè đúng dữ liệu hiện có.
- Nhập Excel/CSV ghép tên chuẩn hóa dấu/hoa thường/khoảng trắng. Mọi bản trùng tên trong cùng tệp đều lỗi; mục nhóm không ghi số. Dòng hợp lệ vẫn ghi dù có lỗi ở dòng khác; ô trống giữ nguyên, toàn bộ cột lũy kế (kể cả công thức) bỏ qua. Công thức trong cột nhập làm lỗi riêng dòng đó. Khi biểu mẫu chứa nhiều tuần, chọn cột của tuần đang xem nếu có; nếu không có, dùng cặp tuần cuối trong biểu mẫu. Đây là lựa chọn parser: preview nêu rõ cột tiến độ/kinh phí/đánh giá cùng kỳ và cảnh báo fallback để cán bộ xem trước khi Ghi nhận; không lấy số lũy kế hoặc trộn cột của tuần khác. Cột đánh giá có số tuần được ghép theo cùng kỳ kể cả khi đặt sau tất cả các cặp tiến độ/kinh phí như workbook chính thức; thiếu đánh giá của kỳ đó thì giữ nội dung đã nhập, không lấy đánh giá của tuần khác. Preview và parse/render/file I/O nằm ngoài transaction; xác nhận kiểm mã băm tệp và CAS.
- Template giữ danh mục và quy mô, xóa số liệu tuần/lũy kế/đánh giá/ghi chú. Xuất kỳ đang xem có tiêu đề hai tầng, ô gộp, viền, thứ tự nhóm/con; chỉ thông tin có cột Ghi chú. Lý do điều chỉnh của đào tạo hiển thị cùng ô quy mô, không tạo cột Ghi chú ngoài Function.
- Chỉ Cấp Tỉnh phòng TTTT và Admin đã vào ngữ cảnh hỗ trợ được đọc/ghi. Ngữ cảnh chỉ xem và Admin chưa vào hỗ trợ bị chặn; hỗ trợ phải có MFA. Xã/TW đọc nhận bộ rỗng và không có menu. Owner truyền department/support qua port, module giá không diễn giải payload phiên của KN.
- Web yêu cầu lưu hoặc hủy các ô chưa lưu trước khi đổi kỳ, thay đổi danh mục hoặc nhập/xuất, để không mất số liệu đang gõ. Chốt lưu các ô đang gõ và khóa trong cùng CAS. Bản mô phỏng dùng cùng hợp đồng, danh mục và phép tính; chỉ nhận CSV hoặc workbook mẫu ZIP không nén do nó xuất. Parser Excel thật và lỗi từng dòng được kiểm trên backend native; không dùng tệp ZIP giả làm bằng chứng xuất thật.

Giữ local preparation theo chỉ đạo điều phối: chưa đăng tự review/ready/PR, chưa dispatch whole CI hoặc gộp. Surface rows chỉ cập nhật sau khi BRD-SURFACE-MAP có trên dev thật và ROOT cho phép; Linux race/whole CI thực hiện trên SHA tích hợp cuối theo release của ROOT.

### Triển khai và cách chạy

FN-TI-02.13 Báo giá v2.0: hai báo cáo tuần theo chương trình, chốt/chỉ xem, cộng dồn và nhập tay thay thế kinh phí lũy kế, điều chỉnh quy mô, nhóm/con/thêm/xóa, Excel partial rows/template/export, quyền Tỉnh TTTT và Admin hỗ trợ.

1. Dependency/cấu trúc: dùng clock/calendar/decimal/excelize/ledger và port session hiện hữu, không thêm dependency.
2. Data: migration mới `pf_price_weekly_program`, RLS theo tenant và ledger append-only; catalog kế hoạch chính thức, tombstone giữ lịch sử chốt bất biến.
3. Service: arithmetic, tuần đầu/tuần kế, CAS/idempotency/audit, từng dòng Excel; parse/render ngoài DB transaction.
4. API: `/v1/price/weekly-programs/{information|training}` và actions/imports/export/template; quyền kiểm trước idempotency.
5. UI: `/kn/chuong-trinh-khuyen-nong/thong-tin`, `/kn/chuong-trinh-khuyen-nong/dao-tao-hop-tac`, menu Tỉnh TTTT, bảng hai tầng, lỗi theo ô, dialog có xác nhận/hủy, tuần cũ chỉ xem.

Kiểm: native `TestWeekly*284` phủ AC01–07, decimal guard, closed-state direct writes, tenant/support/RLS, concurrent CAS một người thắng, override sang tuần sau, partial import/blank/ignore cumulative/error export và OOXML. Chạy hồi quy cả price, các consumer session/menu/assembly và migrations trên pg-ephemeral riêng; đủ static gates, full web và production browser. Bằng chứng kiểm chi tiết, controlled fault và manifest source/artifact được giữ theo gói trong packet điều phối.

Chạy lại từ gốc worktree với toolchain trong go.mod:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/price/... ./internal/kn/extension/menu/... ./internal/platform/party/domain/... ./internal/platform/party/handler/... ./cmd/api-platform/assembly/... ./migrations/...
```

Migration dùng runner chuẩn của repo (`go run ./cmd/migrate up`, rồi `go run ./cmd/migrate verify` với profile/DSN đã được cấp); không áp vào DB dùng chung khi kiểm gói. Web: trong `apps/web-ops`, `npm ci`, `npm run gen:api`, `npm run check:api`, `npm run lint`, `npm run typecheck`, `npm test`, `npm run build:mock`. Chạy API bằng lắp ráp `cmd/api-platform` hiện có; production web bằng `npm start` sau build và ngữ cảnh Tỉnh TTTT/hỗ trợ.

Điều phối local preparation: giữ barrier actual dev #287/#286/#261/#262 và ROOT release trước whole CI/public self review/PR/merge. Sau release chạy Linux race/whole CI trên SHA cuối, đồng bộ surface rows và regenerate từ dev thật.
