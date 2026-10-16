# ADR-0055: Tách hệ thống Khuyến nông

**Trạng thái:** Chốt ranh giới và phạm vi G1 theo PO; D2-KN-01 đã chốt: G1 chỉ hồ sơ KN, luồng đề nghị để sau G1. **Ngày:** 16/10/2026 (KNSPLIT-0). **Nguồn:** trả lời PO 14–15/10/2026 và CODEX-G1 mục 5–6.

**Thay thế một phần:** [ADR-0049](0049-cap-price-pf-price.md) (Báo giá thuộc KN), [ADR-0054](0054-cap-directory-pf-contact.md) (Contact thuộc KN).
**Liên quan:** [ADR-0052/D2](0052-dong-so-huu-mkg-htx-neo-mst.md), [ADR-0041](0041-mfa-theo-nhom-d24.md), [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), [ADR-0031](0031-vai-tong-hop-r-agg.md), [ADR-0035](0035-cap-phep-tong-hop-theo-xa-nguong-k.md), [ADR-0053](0053-hop-dong-so-lieu-da-chot-closed-figures.md).
**Bằng chứng:** [kiểm kê](../kn/tach-he-thong/kiem-ke.md), `tools/knboundary`, `migrations/kn_split_boundary_integration_test.go`.

## Bối cảnh

Nghiệp vụ KN đang nằm cạnh nền tảng trong internal/platform; phần vai/ngữ cảnh, menu, danh mục, biểu, thông báo và nguồn báo cáo KN còn nằm trong module dùng chung. Một số route KN đang do module HTX phục vụ và ghi trực tiếp dữ liệu HTX. Một tenant lưu ký chung làm ranh giới dữ liệu phụ thuộc vào phạm vi Nút thay vì cấp phép giữa hai hệ.

Các số kiểm kê ban đầu ngày 14/10 là lịch sử. Baseline ban đầu của cổng được sinh lại từ dev `988febd9` sau W11.46; số hiện hành có tại bảng kiểm kê. Đây là lần khởi tạo cổng theo CODEX-G1, không phải cho phép tăng baseline sau tích hợp.

## Trả lời của chủ sản phẩm

| Câu hỏi | Quyết định đã chốt |
|---|---|
| Q-KNS-01 | KN là hệ thống độc lập; chỉ trao đổi với hệ khác qua nền tảng dữ liệu dùng chung (hợp đồng _pub, data product, lớp tích hợp); không gọi trong tiến trình vào module hệ khác. |
| Q-KNS-02 | Đích là độc lập logic. Không yêu cầu ranh giới vật lý; binary/realm/cụm riêng chỉ sau G1 khi vận hành cần. Đây là quyết định phạm vi của PO. |
| Q-KNS-03 | Một tenant KN cho mỗi Trung tâm tỉnh. Chuyển dữ liệu KN cũ sang tenant KN trong G1, forward-only; không giữ dữ liệu KN hoạt động ở tenant lưu ký như phương án cũ. |
| Q-KNS-04 | KN không ghi dữ liệu HTX, kể cả đi vòng qua port hoặc lệnh tích hợp. Dữ liệu KN tạo về HTX là bản ghi riêng của KN, chia sẻ qua nền tảng dữ liệu. D2-KN-01 đã chốt riêng: G1 chỉ hồ sơ KN, luồng đề nghị sang MKG để sau G1 (mục Ảnh hưởng). |
| Q-KNS-05 | Hệ nào có chức năng thì sở hữu bản riêng. Theo BRD, Báo giá thuộc Phòng TTTT của KN và Contact FN-13 thuộc Phòng HCTH; cả hai module thuộc KN. |
| Q-KNS-06 | Thu thập MRV hiện ở apps/field-app thuộc HTX/MKG. Không chuyển dữ liệu/quyền MRV vào tenant KN khi di chuyển. |

PO cũng đã chốt KH-UR-AD-02: mẫu phiếu KHTC nạp sẵn đủ G1; M1–M7 theo [quy đổi tuần](../uat/khtc-week-mapping.md). Không hỏi lại các lựa chọn này.

## Quyết định

### 1. Hệ KN độc lập về logic trong G1

- Namespace đích: internal/kn/{extension,price,hrm,contact}; giữ tên module DB pf/extension, pf/price, pf/hrm, pf/contact, vai và schema vật lý. Không đổi ledger name, chuỗi băm hay SQL migration đã áp dụng.
- KN dùng năng lực nền tảng qua port/registry do nền tảng định nghĩa: tài khoản/xác thực, Nút/cây/ĐVHC, forms, notify, reports, masterdata, sync, geo, metrics, kho định danh.
- KN không import module HTX (cooperative, cropcycle, mechanization) hoặc MKG, kể cả port. Chiều ngược lại nền tảng không import KN; phần riêng KN đăng ký vào điểm mở rộng chung.
- Nền tảng dùng chung binary/cụm/realm vẫn hợp lệ. Frontend KN riêng, thư viện mobile KN riêng và ranh giới vật lý để sau G1.
- Tạo tổ chức chỉ trên web; Xã do Admin nền tảng tạo, Admin KN không tạo tổ chức. Hồ sơ riêng KN về HTX không đồng nghĩa với tạo Nút cooperative.

### 2. Bố cục đích và phạm vi triển khai

| Thành phần | Trong G1 | Sau G1 / tùy chọn |
|---|---|---|
| Module KN | internal/kn/{extension,price,hrm,contact}, adapter KN và nghiệp vụ bản ghi riêng | — |
| Nền tảng | Registry menu/ngữ cảnh/profile/danh mục/thông báo/nguồn báo cáo, không biết nghiệp vụ KN | — |
| Web | Tiếp tục apps/web-ops, client Keycloak kn-web trong realm agrigreen | apps/web-kn, gỡ import KN khỏi vỏ chung |
| Mobile | Phân định KN và HTX/MKG theo ngữ cảnh/quyền; MRV thuộc HTX/MKG | Tách packages/agrigreen_kn_mobile hoặc frontend khi cần |
| DB | Chung cụm; tenant KN riêng cho từng Trung tâm tỉnh, schema/vai vật lý giữ nguyên | Cụm riêng chỉ khi có nhu cầu vận hành |
| Binary/realm | api-platform, worker và realm agrigreen chung; realm quản trị agrigreen-kn-admin giữ theo D25 | api-kn/worker-kn, realm riêng nếu PO quyết |

Các ghép nối web được kiểm kê nhưng việc xóa hết chúng thuộc phạm vi frontend sau G1. Không dùng việc chưa tách frontend để bỏ qua ranh giới Go/DB/tenant bắt buộc trong G1.

### 3. Đảo phụ thuộc qua registry

| Năng lực chung | Phần phải do KN đăng ký |
|---|---|
| accounts | Luật vai, KNContext, ủy quyền/đơn vị nội bộ và chính sách/job khóa KN |
| party | Menu/phòng ban KN và các route KN đang ở party |
| forms | Profile kn, kn_cnts, kn_khtc; module forms không chứa luật KN |
| masterdata | Danh mục/tham số CNTS |
| notify | Bộ dựng thông báo Báo giá, CNTS, kỳ báo cáo, lịch công tác |
| reports | Nguồn số liệu và xuất báo cáo KN |
| shared/authz | Cơ chế ngữ cảnh ứng dụng chung; KNContext/RequireKN và luật vai ở KN |

Mỗi gói chuyển theo hai bước: thêm adapter đăng ký, sau đó xóa đường cũ, giữ dev chạy được. WH-A đăng ký menu/mock/OpenAPI trước khi chuyển namespace.

### 4. Ranh giới dữ liệu với HTX

- Dữ liệu canh tác HTX thuộc MKG/HTX theo D2, KN là bên đọc ngoài hệ.
- Hợp đồng v1 qua nền tảng dữ liệu: cooperatives_v1, plots_v1, seasons_v1, confirmations_v1; nền tảng cung cấp admin_units_v1, org_tree_v1. Có schema/hợp đồng, consumers và kiểm thử quyền; không đọc bảng nội bộ hoặc gọi port HTX.
- Dòng chi tiết chỉ đọc khi có share_grant hiệu lực, purpose_code=khuyen-nong, basis_ref=thỏa thuận HTX hợp lệ theo D2 (agreement_ref). Thu hồi/hết hạn phải mất quyền ngay, kể cả kết nối tái sử dụng. Số tổng hợp tuân theo ngưỡng công bố của hợp đồng tương ứng.
- Không cấp quyền ghi HTX cho vai/người dùng KN. Route tạo/cập nhật về HTX của KN chuyển sang bản ghi riêng KN và công bố qua outbox/data product. Không biến sự kiện công bố thành lệnh ghi HTX tự động.
- Chuyển các route /v1/kn/* ở party/cooperative/mechanization về module KN. Trả dữ liệu đúng sở hữu; danh mục HTX được chia sẻ và bản ghi KN phân biệt rõ nguồn/xác nhận.

### 5. Tenant và di chuyển forward-only trong G1 (KNSPLIT-4b)

1. Một tenant đích gắn Trung tâm tỉnh; không trộn dữ liệu hai Trung tâm. Có bảng ánh xạ Trung tâm→tenant, ánh xạ ID nguồn→đích và nhật ký mỗi đợt chuyển.
2. Chuyển bản ghi/tài khoản/phân công/ủy quyền/job khóa KN sang đích. Sổ append-only không UPDATE tenant_id: ghi bút toán chuyển, tạo các bản ghi đích theo quy tắc sổ và giữ liên kết nguồn/hash nguồn trong bằng chứng chuyển. Dữ liệu MRV/HTX/MKG giữ đúng hệ sở hữu.
3. Cấp quyền đọc dữ liệu HTX dựa trên thỏa thuận D2; không suy ra quyền toàn tỉnh hoặc tạo căn cứ giả. Việc duyệt cấp phép giữ phân tách nhiệm vụ hiện có.
4. Chạy trên bản chụp: đối chiếu số dòng và checksum theo module/Trung tâm, khóa tham chiếu, trạng thái vai/ngữ cảnh, phân loại quyền và dữ liệu không chuyển. Chạy lại cùng đợt không nhân đôi.
5. Diễn tập rollback bằng bút toán bù/chuyển tiếp và khôi phục định tuyến/cấp phép có nhật ký; không xóa hoặc sửa sổ nguồn/đích. Runbook: docs/runbooks/kn-tenant-move.md.
6. Năm ca XFAIL hiện tại thành khẳng định bắt buộc PASS, fail-on-skip ở 4b; không xóa ca hoặc đổi sang SKIP để đóng gói.

### 6. Gói việc theo thứ tự G1

| Gói | Kết quả bắt buộc |
|---|---|
| KNSPLIT-0 | Kiểm kê/baseline ban đầu, ADR này, cổng ranh giới10 quy tắc và kiểm thử âm; không tuyên bố đã tách hệ |
| WH-A | Menu/mock/OpenAPI đăng ký theo module; đầu ra/golden giữ byte; checklist thêm gói |
| KNSPLIT-1 | Namespace bốn module, giữ schema/tên DB, modgen/archlint/knboundary đạt |
| KNSPLIT-2 | Registry chung; hợp đồng dữ liệu v1; route KN về KN; kn-import-htx=0, kn-write-htx=0 và phần riêng KN rời module chung |
| KNSPLIT-4b | Tenant mỗi Trung tâm, chuyển thật forward-only/cấp phép, đối chiếu/rollback diễn tập và5 ca XFAIL→PASS |
| KNSPLIT-3 tối thiểu | Client kn-web + runbook import realm; frontend riêng sau G1 |
| W11.66 | Màn soạn tối thiểu, Excel Mẫu 1–5, Bản cuối và snapshot/nguồn/phiếu theo M1–M7; ưu tiên xen sau WH-A khi nền cho phép |

### 7. Khi nào cân nhắc tách vật lý

Chỉ mở sau G1 khi có nhu cầu vận hành, hợp đồng/cấp độ được PO duyệt hoặc số đo quá tải/sự cố cho thấy cần. Quyết định hiện tại không yêu cầu tách cụm/binary/realm.

## Ảnh hưởng — D2-KN-01 đã chốt với PO ngày 16/10/2026

PO chọn: **“G1 chỉ hồ sơ KN; luồng đề nghị để sau G1”.** Quyết định này thay trạng thái chờ của đề xuất D2-KN-01, không đổi quyết định đồng sở hữu D2 của CEO.

1. Trong G1, KN chỉ tạo và quản lý hồ sơ riêng trong tenant Trung tâm tỉnh: MST, địa bàn, thông tin nghiệp vụ, nguồn/bằng chứng và tham chiếu HTX đã được cấp phép chia sẻ. KN không tạo Nút cooperative, Neo MST hay dữ liệu/sổ HTX; không gọi đường ghi HTX qua port hoặc lệnh tích hợp.
2. Luồng KN gửi đề nghị → MKG kiểm thỏa thuận/MST → duyệt và tạo HTX tạm/Neo bằng quyền MKG **để sau G1**. Không có API/outbox tiếp nhận, phê duyệt hoặc tác vụ tự tạo HTX từ hồ sơ KN trong phạm vi bàn giao G1.
3. HTX tạm do MKG tạo ngoài luồng KN và quy trình HTX kích hoạt/nhận quyền tiếp tục theo ADR-0052. Đồng sở hữu, MST duy nhất, thỏa thuận và phân tách số chưa xác nhận giữ nguyên. Không sửa ADR-0052 để áp dụng phương án đề nghị đã hoãn.
4. KN đọc trạng thái xác nhận qua sản phẩm dữ liệu có cấp phép; M6 lấy tại cuối ngày chốt. Báo cáo đã gửi/Bản cuối không tự phát hành lại.
5. HTX/Nút/Neo và sổ HTX cũ giữ nguyên chủ/tenant trong đợt chuyển KN. Hồ sơ riêng KN chuyển forward-only và liên kết lịch sử qua ánh xạ ID/agreement_ref; không sửa lịch sử HTX.

Đề xuất ban đầu “KN gửi đề nghị, MKG duyệt và tạo” được giữ là hướng sau G1; không còn là quyết định đang chờ cho KNSPLIT-2/4b. Các gói tách tiếp tục theo phạm vi G1 đã chốt ở trên.

## Hệ quả

- Cổng knboundary nối make gates và CI -base; baseline sau tích hợp chỉ giảm. Quy tắc không được nới để làm xanh.
- Các ghép nối hiện hữu là việc phải gỡ, không phải ngoại lệ vĩnh viễn. Các mục web sau G1 giữ phạm vi riêng; migration lịch sử không đổi hash chỉ vì đổi namespace.
- ADR-0049/0054 thay thế một phần về sở hữu module, không thay hợp đồng/nghiệp vụ bên trong.
- KNSPLIT-0 có 5 ca XFAIL được chạy thật và ghi rõ vi phạm; chúng chỉ được đóng khi4b đạt. G1 chưa hoàn tất chỉ vì cổng khớp baseline.

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| KNSPLIT-0 14–16/10/2026 | Giữ 3 tệp dở thành commit, rebase dev sau W11.46; chốt các trả lời PO; sinh baseline ban đầu trên988febd9; cổng10 quy tắc, kiểm thử scanner/không tăng baseline, kiểm thử âm DB; D2-KN-01 chốt 16/10: G1 chỉ hồ sơ KN, đề nghị sau G1 | Không có migration mới |
| KNSPLIT-1 16/10/2026 | Namespace bốn module đã ở internal/kn; module/schema/role DB giữ nguyên. Dev ddd2671f, helper migration thật PASS304,766s;56/56 gói hồi quy và cổng tĩnh đạt, baseline390 không đổi | Không migration mới |
