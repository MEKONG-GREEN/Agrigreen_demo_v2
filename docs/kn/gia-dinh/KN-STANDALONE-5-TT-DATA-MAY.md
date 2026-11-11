## KN-STANDALONE-5-TT-DATA-MAY — Máy móc HTX về kho KN tự quản, ẩn ứng viên HTX đã công bố (#306)

- **Căn cứ:** BRD 2.0 Trồng trọt v7.7 FN-XA-03.04 (Quản lý máy móc của HTX), FN-XA-03.03 (HTX của KN); kế hoạch
  `kn-handoff/PLAN-KN-STANDALONE-20261107.md` quyết định D1 (KN tự nhập, tự quản), audit
  `reports/kn-standalone-audit-20261107.md` §3 điểm 2 và 5. Gói con của #271 (phần thửa đã gộp), làm theo đúng mẫu đó.
- **Dữ liệu (migration pf):**
  - `20261111002853_pf_extension_kn_machine_count`: sổ chỉ ghi thêm `pf_extension.kn_machine_count_event`
    (`shared.apply_ledger`, chuỗi `kn_machines:<cooperative_ref>`; sự việc `migrated | set | removed`, mỗi loại máy đổi trong
    một lần lưu là một dòng mang số cũ → mới) và bảng chiếu `pf_extension.kn_machine_count` (RLS tenant, chặn
    DELETE/TRUNCATE). `cooperative_ref` là mã hồ sơ KN loại HTX (`kn_producer.producer_ref`).
  - `20261111002852_pf_mechanization_pub_machine_count_export`: view công bố `pf_mechanization_pub.v1_machine_count_export`
    (chỉ VIEW, chủ ag_migrator NOBYPASSRLS, chỉ cấp `r_mechanization_pub`; khai `//ag:pubview`, hợp đồng
    `contracts/pubview`) — nguồn đọc của lần chép, KN không nhắc bảng riêng của CGH.
  - `20261111002854_pf_extension_kn_machine_count_copy`: chép MỘT LẦN các dòng đang khai báo theo từng tenant (đặt
    `app.tenant_id`; danh sách tenant từ `shared.chain_head`). HTX nền tảng → hồ sơ KN loại HTX đang liên kết (`htx_ref`
    của phiên bản mới nhất; nhiều hồ sơ thì mã nhỏ nhất). Mã dòng sổ là UUIDv7 dựng trong SQL. Chạy lại được. Không
    UPDATE/DELETE, không DROP; bảng CGH giữ nguyên.
  - **HTX nền tảng chưa có hồ sơ KN liên kết:** không tự tạo được hồ sơ KN trong migration (người đại diện bắt buộc, ở kho
    định danh), nên dòng được chép GIỮ khóa là mã HTX cũ (`cooperative_ref = legacy_htx_ref`). Nhờ vậy tổng số máy theo xã
    không đổi sau chép; màn hiện "HTX cũ — chưa có hồ sơ KN", Cấp Xã vẫn xem, sửa, bỏ dòng. Cán bộ tạo hồ sơ HTX ở Dữ liệu
    › HTX rồi khai báo lại dưới hồ sơ đó và bỏ dòng cũ.
  - **Năm sản xuất** (`pf_extension.kn_machine_year`, sổ của KN, trước đây khóa theo HTX nền tảng): với HTX có hồ sơ KN
    liên kết, migration chép ghi THÊM dòng `set` hiện hành sang chuỗi của hồ sơ KN (dòng cũ giữ làm lịch sử); HTX chưa có
    hồ sơ KN giữ khóa cũ nên không cần chép. Không gộp năm vào sổ số lượng: năm tách theo phòng ban, số lượng là dữ liệu
    chung — giữ hai hợp đồng API như cũ.
  - Kịch bản vận hành `scripts/kn306-machine-copy/count.sql` (số dòng và tổng số máy theo tenant × xã × loại máy, nguồn so
    với ảnh chụp đã chép — dòng sổ `migrated`, không đổi khi KN sửa tiếp; rỗng = khớp) và `rollback.sql` (siêu người dùng;
    dừng nếu khai báo đã chép có ghi mới; gỡ đúng phần đã chép, đầu chuỗi băm, khóa idem; chạy lại migration chép để áp lại).
- **API:** `/v1/kn/machine-counts`, `/{ref}`, `/{ref}/log` do chủ KN (`internal/kn/extension/knmachine`) đăng ký; hợp đồng
  chuyển sang `contracts/openapi/modules/extension/kn-machines`, DTO và mã lỗi `mechanization.*` giữ nguyên để web và
  field-app không đổi. `{ref}` là mã hồ sơ KN loại HTX; mã HTX nền tảng → 404. `/v1/kn/machine-profiles/{ref}` (năm sản
  xuất) tra HTX theo cùng khóa. Module CGH không còn đăng ký route `/v1/kn/*` (404); route App CGH `/v1/mechanization/…`
  giữ nguyên. Mã KN cũ trong `pf/mechanization/app/machine_count.go` còn phục vụ cảnh báo vô hiệu hóa HTX nền tảng
  (`MachineCounts`), giữ nguyên theo nguyên tắc không xóa mã hệ khác.
- **Hành vi giữ nguyên:** Cấp Xã và Admin thêm/sửa/bỏ dòng trong xã phụ trách; Cấp Tỉnh, Cấp Quốc gia chỉ xem (ghi → 403
  `mechanization.read_only`); Admin ở chế độ hỗ trợ phải có lý do; khai báo mang phòng ban của người ghi, đọc không lọc
  phòng ban; loại máy thuộc danh mục đang dùng; trùng loại → 422 gợi ý cập nhật dòng đã có; số lượng nguyên 1–1.000.000;
  ghi vào HTX đã ngừng hoạt động → 422; gửi lại cùng `Idempotency-Key` không ghi thêm; tổng theo loại của xã = Σ các HTX;
  năm sản xuất 1900 – năm hiện tại.
- **Thay đổi nhỏ về cú pháp số:** số lượng kiểm bằng biểu thức chính quy `^[0-9]{1,7}$` TRƯỚC khi parse (không dùng thư
  viện thập phân): `1e3`, `2.0`, `+5` nay bị chặn như `1.5` (trước đây `1e3`, `2.0` lọt vì là số nguyên về giá trị);
  `1e100000`, `1e-100000`, chuỗi rất dài bị chặn mà không tính toán. Web và field-app chỉ gửi số nguyên JSON thường.
- **Ẩn liên kết HTX đã công bố (hoãn pha kết nối):** gỡ `GET /v1/kn/producer-htx-candidates` (404) và bộ đọc
  `pf_party_pub.v1_org_tree` của hồ sơ KN; `htx_ref` không còn ghi được (gửi khi tạo, hoặc đổi khi sửa → 422
  `validation_failed`); liên kết đã có từ trước giữ nguyên qua các lần sửa (chỉ đọc), `htx_name` luôn null. Web bỏ ô "Liên
  kết HTX đã công bố", field-app bỏ ô chọn tương ứng; `HtxRecordPicker` chỉ còn một chế độ (giá trị = mã hồ sơ KN).
- **Route nông hộ của KN ở module HTX:** gỡ `GET /v1/kn/cooperatives/{ref}/households`, `GET /v1/kn/households/{ref}/log`
  khỏi pf/cooperative (404). Nông hộ của KN là hồ sơ KN riêng (`/v1/kn/producer-records?kind=household`). Route App HTX
  `/v1/cooperatives/{ref}/households`, `/v1/households…` giữ nguyên.
- **Menu:** mục "Máy móc" (`/kn/du-lieu/may-moc`) gắn module `pf/extension` thay `pf/mechanization` trong `menuModules` —
  tenant tắt `pf/mechanization` vẫn thấy mục này. Sinh lại `menu_departments.go`, `compat-menus.json`.
- **Nghiệm thu khi module CGH/HTX tắt:** `tests/knmachine` gắn cổng bật module của pf/party như gốc lắp ráp, tắt
  `pf/mechanization` và `pf/cooperative` cho tenant (đối chứng: route của hai module trả 403 `module_not_enabled`) —
  FN-XA-03.04 trọn luồng chạy, bảng CGH không đổi.
- **knboundary:** baseline giảm 11 mục (route-owner, kn-symbol-in-shared, kn-write-htx của máy móc và nông hộ); bỏ
  `/v1/kn/machine-counts` khỏi `kn_htx_write_prefixes`.
- **Ngoài gói:** "Nạp Excel" của FN-XA-03.04 (luồng phụ BRD) chưa có từ trước, không thuộc gói chuyển kho này. Tab Máy móc
  trong chi tiết HTX hiện cho mọi hồ sơ HTX (trước chỉ hồ sơ đã liên kết).
- **BRD tự mâu thuẫn:** dòng "Người dùng" ghi Cấp Xã "(thêm, sửa)", luồng chính và ma trận quyền ghi thêm/sửa/xóa dòng và
  Admin CRUD — theo ma trận quyền (hành vi đã có). BRD không nêu "năm sản xuất" và chỉ chặn số âm/không nguyên; số lượng
  > 0 và năm sản xuất là hành vi đã có, giữ nguyên theo yêu cầu gói.
