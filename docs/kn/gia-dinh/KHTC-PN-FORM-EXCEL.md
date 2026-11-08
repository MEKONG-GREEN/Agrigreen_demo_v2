## KHTC-PN-FORM-EXCEL — codec Excel kỹ thuật theo snapshot bất biến (#216)

### Phạm vi đã chốt

Căn cứ duy nhất: KHTC v2.3, Function FN-PN-03.02, BR-DL-20, BR-DL-11/12 và cấu trúc FN-AD-04.02. Function chi tiết thắng tiêu chí cũ của issue: ô cố định/tự điền/công thức bị đổi được bỏ qua, giữ giá trị hệ thống; dòng lỗi không làm mất dòng đúng. Mã hạng mục nhận diện dòng trong phạm vi sheet/phòng; không dựa tên hoặc vị trí.

Đây là lát kỹ thuật trọn vẹn đầu của #216 theo CONTEXT §3.2. Chỉ thêm hợp đồng và codec thuần trong bộ nhớ. Không có API/UI Excel mới, không sinh mẫu từ Seed/Generic, không thay manual entry, schema, migration, quyền, save/submit hoặc shared retry client. Không tuyên bố hoàn tất FN-PN-03.02 hoặc UAT chức năng.

### Hợp đồng và giới hạn

- `port.KHTCFormExcelSnapshot` do chủ ứng dụng cung cấp rõ: tenant/kỳ/phòng, ID snapshot; mỗi sheet có ID/version/hash mẫu thực, cột/chữ cái/thuộc tính, mã hạng mục hoặc section ID và giá trị hệ thống, các kỳ đóng được điều chỉnh. Codec không xác minh hay tự suy ra phát hành/gán mẫu. `New` kiểm giới hạn trước khi sao chép/băm, rồi giữ bản sao riêng. Hash/ID/metadata không phải quyền truy cập, mã import hay receipt tệp.
- Một sheet cho mỗi tab được cung cấp. Giữ đúng chữ cái cột, kể cả khoảng trống. Cột Mã hạng mục (hoặc Mã mục lời văn) nối phía phải, **hiển thị**, không đẩy lệch cột mẫu. Hàng metadata ẩn chỉ chống lẫn snapshot. Chèn/đổi thứ tự dòng không đổi mã nhận diện; mọi dòng trùng mã đều lỗi; mã ngoài sheet không được nhập. Thiếu/đổi tên sheet/cột/identity làm sai cả workbook.
- Thuộc tính cột gồm text/number/integer, bắt buộc, không âm, danh sách chọn, độ dài. Số dùng chuỗi thập phân thường tối đa 15 chữ số nguyên/6 chữ số lẻ; integer 9 chữ số. Kiểm trước mọi phép chuyển đổi; không dùng float hoặc đánh giá công thức upload.
- Phần công thức hỗ trợ **rõ ràng** projection `server_value`: chủ mẫu đã tính giá trị chuẩn, codec xuất giá trị khóa. Không hứa bảo toàn biểu thức Excel hay fidelity đầy đủ của mẫu đã phát hành. Yêu cầu `excel_expression`, thiếu mode hoặc biểu thức đặt làm giá trị số trả `ErrFormulaProjection`; không âm thầm đổi biểu thức thành literal. Bộ chuyển/sinh biểu thức an toàn thuộc #300.
- Giá trị ô khóa bị đổi, kể cả formula upload, là cảnh báo ignored và không vào nội dung nhập. Formula ở ô editable là lỗi dòng. Mọi chuỗi xuất dùng SetCellStr, gồm lời văn bắt đầu bằng `=`. Dòng đúng vẫn có thể Merge; phần điều chỉnh cùng sheet bắt buộc mã hợp lệ, kỳ đóng được cấp, số khác 0 và lý do ≤500 ký tự; `AdjustmentLocked` là quyết định riêng của chủ dữ liệu.
- `Preview` là opaque object chỉ ở RAM. `Result` trả bản sao. `Merge` từ chối preview tự tạo/codec khác, đọc và kiểm lại bytes riêng của preview trên snapshot đã ghim; không tin report/fingerprint do bên ngoài sửa. `keep` giữ từng ô đã có, lấp ô trống; `replace` thay từng ô hợp lệ, gồm giá trị rỗng hợp lệ. Điều chỉnh ghép theo nhóm sheet/mã/kỳ. Input, output, snapshot và upload bytes không dùng chung map/slice có thể sửa.
- Giới hạn trước parser: workbook 5 MiB, ZIP 512 entries, giải nén tổng 32 MiB và mỗi XML 16 MiB; 32 sheet, 64 cột nghiệp vụ, tổng 2000 dòng dữ liệu và 200 điều chỉnh, 32000 byte mỗi ô. Giới hạn riêng chỉ số dòng/cột, token/depth XML, shared strings và cell count; chặn ZIP trùng/path traversal, DTD/entity, external relationships/URI, macro/binary, embedded/active content, duplicate XML row/cell. Sai độ dài trường trong giới hạn byte là lỗi dòng. File quá giới hạn tài nguyên bị từ chối cả file.
- ErrorWorkbook gom mọi lỗi/cảnh báo theo dòng gốc để giới hạn số ô xuất; giữ sheet/dòng/mã/cột/lý do, không sao chép giá trị hay formula upload vào thông báo.

### Phần còn lại có chủ

- [#300 KHTC-PN-FORM-SCHEMA](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/300), sau actual #216: publisher/assignment mẫu phòng/chương trình, phiên bản và kỳ hiệu lực thực; một schema/validator cho manual và Excel; snapshot producer có thẩm quyền và formula-expression emission an toàn. Seed/Generic v1, mẫu báo cáo mau_1..5 và biểu native KHTC-PHIEU/bang_so_lieu không thay thế supplier này.
- [#301 KHTC-PN-FORM-FILE](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/301), sau actual #216+#300: seal/open workbook gốc bằng key owner thực, objstore ciphertext R-40, SHA256/file metadata thật, gắn provenance nguyên tử vào lần Save draft/Submit cụ thể, receipt bền vững và multipart lost-ACK/current-authority, hoàn chỉnh live UI/API. Import chỉ đưa vào editor chưa lưu; không tự lưu/gửi hoặc tính lượt gửi. Admin theo ngữ cảnh và MFA thật. Không giả household envelope, importID hay thành công từ latest head.
- [#291](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/291) phải đợi provenance native thật của #301 cùng các phụ thuộc đã ghi; hash kỹ thuật của #216 không phải bằng chứng tệp gốc được lưu.

### Tệp → bước → căn cứ và kiểm chứng

| Tệp | Bước | Căn cứ / trách nhiệm |
|---|---|---|
| `internal/kn/extension/port/khtc_form_excel.go`, `internal/kn/extension/module.yaml` sinh bằng modgen | 1 | Hợp đồng supplier; không thêm dependency/API/quyền |
| `khtcform/excel/codec.go` | 3 | Snapshot bất biến, schema/bounds rõ, formula subset |
| `khtcform/excel/preflight.go` | 3 | Kiểm ZIP/XML trước parser, hostile bounds |
| `khtcform/excel/generate.go` | 3 | Chữ cái cột, mã hiển thị, text-safe generation/error export |
| `khtcform/excel/preview.go` | 3 | BR-DL-20 partial valid/ignored/field/adjustment rules |
| `khtcform/excel/merge.go` | 3 | BR-DL-20 keep/replace trong RAM, opaque preview kiểm lại |
| `khtcform/excel/*_test.go` | 3 | RED→GREEN, mapping/precision/hostile bounds/isolation/concurrency |

Không có bước 2/4/5 mới: không migration/data write, route hay UI. Ma trận kỹ thuật: [kiểm chứng #216](../../uat/khtc-form-excel-technical216.md).

### Chạy và kiểm

Từ worktree, đặt `GOTOOLCHAIN` theo `toolchain` của go.mod, GOTMPDIR/TEMP/TMP theo môi trường KN và GOMAXPROCS=2. Chạy `go version`, `go run ./tools/modgen -check` và `scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/extension/khtcform/... ./internal/kn/extension/port ./internal/kn/extension`. Chỉ PG riêng; 0 SKIP. Chạy đủ cổng CONTEXT §3.3, lint-new origin/dev và merge-tree. Không có web/OpenAPI thay đổi để tuyên bố kiểm browser hoặc sinh API. Linux race/whole CI, public self-review và tích hợp chờ ROOT release đúng SHA; kết quả thực thi nằm trong packet, tài liệu không thay log.
