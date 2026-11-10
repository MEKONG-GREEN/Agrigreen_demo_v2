# Nghiệm thu Phòng Chăn nuôi – Thủy sản theo BRD 2.0 (CNTS v4.0)

Gói KN-STANDALONE-11-UAT-DOCS (#273). Tài liệu này thay cho [cnts-v3.7-acceptance.md](cnts-v3.7-acceptance.md) và
[cnts-v4.0-acceptance.md](cnts-v4.0-acceptance.md) khi nghiệm thu với người dùng; hai tệp cũ giữ lại làm bảng bằng chứng
kiểm thử tự động theo từng gói.

## 1. Phạm vi và căn cứ

- Căn cứ duy nhất: BRD 2.0 – Module Chăn nuôi – Thủy sản **v4.0** (CR-CNTS-001), Mục V "Chi tiết yêu cầu chức năng",
  đủ 21 chức năng FN-01 … FN-21. Mã UR/BR/AC/B (bước)/trường (HCN-, DB-, CV-…) giữ nguyên như BRD.
- Mỗi FN một mục: vai dùng, điều kiện đầu, màn thật trên app, rồi bảng từng BR/AC → bước kiểm → kết quả mong đợi.
- Cột **Trạng thái trên dev** ghi theo mã trên nhánh `dev` ngày 11/11/2026:
  - **Đạt**: đã thấy mã và kiểm thử tự động (tên kiểm thử ở dòng "Kiểm thử tự động" cuối mỗi FN).
  - **Đạt (cơ chế)**: mã có, nhưng con số thời gian/tải của AC ("trong 1 phút", "500 người dùng") phải đo khi UAT.
  - **Thiếu**: chưa thấy mã; ghi rõ thiếu gì và issue (nếu chưa có issue thì ghi "chưa có issue").
- Dòng bước, luồng phụ, ngoại lệ ghi Đạt dựa trên kiểm thử của FN; ý nào kiểm thử không nêu đích danh thì người kiểm xác nhận lại khi UAT.
- Cột **Kết quả UAT** để trống cho người kiểm điền (Đạt / Không đạt + ghi chú, ngày, người kiểm).
- Tài liệu sẽ chạy trên **profile chỉ-KN** (Compose profile `kn`, realm Keycloak `kn`, tenant KN mới, module HTX/CGH/MKG/Public
  API tắt) khi gói KN-STANDALONE-10B-PROFILE-CI (#272) gộp. Trước đó chạy trên môi trường dev chung; kết quả phải giống nhau vì
  CNTS không đọc dữ liệu hệ ngoài (lớp thửa HTX ở bản đồ và kiểm HTX ở điểm trình diễn đang được gỡ ở #266).

## 2. Chuẩn bị dữ liệu trên một tenant KN mới

KN tự nhập toàn bộ dữ liệu, không lấy từ hệ ngoài. Làm theo đúng trình tự khởi tạo của BRD (Mục V.1):

| Bước | Người làm | Việc | Màn | FN |
|---|---|---|---|---|
| 1 | Admin | Nạp danh mục ĐVHC An Giang (102 xã, phường, đặc khu) | `/kn/quan-tri/don-vi-hanh-chinh` | FN-01 |
| 2 | Admin | Tạo Nút TTKN tỉnh An Giang và các Nút xã; gán Khu vực cho Nút xã | `/kn/quan-tri/co-cau-to-chuc` | FN-02 |
| 3 | Admin | Bật Phòng Chăn nuôi – Thủy sản "Đang áp dụng" | `/kn/quan-tri/phong-ban` | FN-03 |
| 4 | Admin | Tạo tài khoản kiểm (bảng dưới), gán vai trò, Nút, phòng ban, xã phụ trách | `/kn/quan-tri/nguoi-dung` | FN-04 |
| 5 | Admin | Nạp danh mục CNTS: vật nuôi, giống, vaccine, quy trình tiêm, bệnh, cơ sở giết mổ (mộc dấu GS/GC), đối tượng thủy sản, loại mô hình | `/kn/phong-ban/chan_nuoi_thuy_san/danh-muc-cnts` | FN-06 |
| 6 | Admin | Cài 19 biểu chuẩn | `/kn/phong-ban/chan_nuoi_thuy_san/bieu-ky-cnts` | FN-07 |
| 7 | Cấp Tỉnh (cán bộ tổng hợp) | Cấp quyền cán bộ xã theo xã, cơ sở giết mổ, biểu | `/kn/cnts/cap-quyen` | FN-14 |
| 8 | Cấp Tỉnh | Phát hành BM-01, BM-04, BM-05, BM-17 cho kỳ hiện tại; lập lịch trực tháng | `/kn/cnts/bieu-mau`, `/kn/cnts/lich-truc` | FN-07, FN-15 |
| 9 | Cấp Tỉnh (chủ nhiệm) | Tạo 1 dự án KNQG, 1 điểm trình diễn, 2 hộ mô hình | `/kn/cnts/du-an`, `/kn/cnts/ho-mo-hinh` | FN-19, FN-10 |
| 10 | Cấp Xã | Thêm 12 hộ chăn nuôi ở xã A (8 nông hộ, 3 trang trại nhỏ, 1 trang trại vừa), check-in tại chỗ | `/kn/cnts/so-lieu-xa/ho-chan-nuoi` | FN-10 |

Tài khoản kiểm tối thiểu (mật khẩu do Admin đặt khi tạo, không ghi vào tài liệu):

| Ký hiệu | Vai trò | Thuộc tính / phạm vi |
|---|---|---|
| AD | Admin | Toàn hệ thống |
| T-TH | Cấp Tỉnh · Phòng CNTS | Cán bộ tổng hợp |
| T-LD | Cấp Tỉnh · Phòng CNTS | Lãnh đạo phòng |
| T-BGD | Cấp Tỉnh · Phòng CNTS | Lãnh đạo phòng + cờ "Chỉ xem" (Ban Giám đốc) |
| T-CN | Cấp Tỉnh · Phòng CNTS | Chủ nhiệm dự án |
| T-VC | Cấp Tỉnh · Phòng CNTS | Viên chức (người theo dõi lịch cán bộ xã) |
| X-A | Cấp Xã · Phòng CNTS | Phụ trách xã A và xã B; viên chức trực KSGM cơ sở S1 |
| X-C | Cấp Xã · Phòng CNTS | Phụ trách xã C |
| HC | Phòng nhận báo cáo | Tài khoản Phòng HCTH |
| TW | Cấp Quốc gia | Bên A dự án KNQG |

## 3. Chức năng hoãn, chờ đầu vào và khoảng trống

| Loại | Nội dung | FN | Issue |
|---|---|---|---|
| Chờ đầu vào | Vai Nông hộ: tài khoản theo SĐT + OTP, tự ghi nhật ký, xác nhận mũi tiêm, báo nghi dịch, xem quy trình trên Mobile | FN-04, FN-05, FN-10, FN-11, FN-20 | #173 (phần lịch tiêm); phần còn lại chưa có issue |
| Chờ đầu vào | In ảnh thực địa vào Word Mẫu 03; đối chiếu mẫu gốc Mẫu 01 (chờ PO) | FN-19 | #221 |
| Đang làm | Mẫu nhật ký động, loại biểu mã mới | FN-07 | #275, #303, #304, #305 |
| Đang làm | Lịch sử hộ hiện họ tên cũ → mới, chỉ che SĐT (HCN-09) | FN-10 | #276 |
| Đang làm | Màn nhập biểu đợt điều tra hiện mã hộ HCN | FN-21 | #277 |
| Đang làm | Bỏ lớp thửa HTX ở bản đồ, bỏ kiểm HTX ở điểm trình diễn | FN-18, FN-19 | #266 |
| Khoảng trống, chưa có issue | Ánh xạ xã cũ → xã mới (FN-01 BR-03); ngày hiệu lực Khu vực trên màn (FN-02 BR-02); việc nhật ký/mũi tiêm/lịch đến hạn trong "Việc cần làm" (FN-08); nhập BM-04 từ Excel (FN-09); cảnh báo nhật ký trễ, lịch tiêm trễ (FN-13); email công vụ khi phòng nhận chưa có tài khoản (FN-16); bộ lọc và chỉ số trang chủ (FN-17); tải KML, xuất ảnh, ranh giới xã (FN-18) | — | chưa có issue |

Không có FN nào của CNTS thuộc nhóm "hoãn pha kết nối".

## 4. Kịch bản theo chức năng

### FN-01 — Quản trị danh mục đơn vị hành chính

- Vai: Admin (AD). Điều kiện đầu: có tệp danh mục ĐVHC chính thống.
- Màn: Quản trị hệ thống › Đơn vị hành chính — `/kn/quan-tri/don-vi-hanh-chinh`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| BR-01 | Mã ĐVHC duy nhất | Nạp tệp có 2 dòng trùng mã | Dòng trùng bị từ chối, dòng hợp lệ vẫn nạp | Đạt | |
| BR-02 | Không xóa, chỉ "Hết hiệu lực" kèm ngày | Mở một xã, tìm nút Xóa; đặt Hết hiệu lực | Không có Xóa; xã chuyển Hết hiệu lực, vẫn tra được | Đạt | |
| BR-03 | Sắp xếp ĐVHC: bảng ánh xạ xã cũ → xã mới có ngày hiệu lực | Khai báo xã mới hợp nhất 2 xã cũ; xem báo cáo so sánh năm | Số liệu trước ngày hiệu lực giữ mã cũ; báo cáo nhiều năm cộng theo ánh xạ | Thiếu: chưa thấy mã ánh xạ/hợp nhất ĐVHC (chưa có issue) | |
| AC-01 | Có đủ 102 xã, phường, đặc khu của An Giang | Lọc tỉnh An Giang, đếm dòng | Đúng 102 | Thiếu: kiểm thử hiện chỉ khẳng định ≥ 100 Nút xã (`TestKN77AnGiangTuan36`); đếm tay khi UAT | |
| AC-02 | Không chọn được ĐVHC ngoài danh mục ở FN-06, FN-10, FN-18 | Thêm cơ sở giết mổ / hộ chăn nuôi với mã xã lạ (gọi API) | Bị chặn 422 | Đạt | |
| Ngoại lệ | Tệp có dòng lỗi → chỉ nạp dòng hợp lệ | Nạp tệp có 1 dòng thiếu mã | Báo dòng lỗi, các dòng khác vào danh mục | Đạt | |

Kiểm thử tự động: `TestFN_AD_04_01_Catalog`, `TestFN_AD_04_01_NormalizeCatalog`, `TestLivestockHouseholds_251`, `TestCNTSModel_W1147`.

### FN-02 — Quản lý cơ cấu tổ chức 3 cấp

- Vai: Admin. Điều kiện đầu: FN-01 đã nạp.
- Màn: Quản trị hệ thống › Cơ cấu tổ chức 3 cấp — `/kn/quan-tri/co-cau-to-chuc`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B4 | Gán Khu vực cho Nút xã | Chọn Nút xã, chọn Khu vực, nhập lý do, Lưu khu vực | Cây hiện Khu vực mới; có dòng nhật ký | Đạt | |
| BR-01 | Xem xuống, không xem ngang, không xem lên | X-A mở dữ liệu xã C; T-TH mở dữ liệu tỉnh khác | Không thấy | Đạt | |
| BR-02 | Khu vực có hiệu lực từ ngày – đến ngày; đổi Khu vực không đổi tổng hợp kỳ trước | Đổi Khu vực xã A sau khi kỳ tuần trước đã gửi; mở lại Phụ lục 2 kỳ trước | Kỳ trước vẫn gom xã A ở Khu vực cũ | Đạt phần giữ Khu vực tại lúc gửi/chốt. Thiếu: ô nhập ngày hiệu lực từ – đến trên màn (chưa có issue) | |
| AC-01 | BM-17, BM-18 gom đúng theo Khu vực; BM-02 gom theo xã | Mở `/kn/cnts/phu-luc-2` và `/kn/cnts/ksgm` sau khi 2 xã khác Khu vực đã nộp | Phụ lục 2 có dòng nhóm Khu vực; KSGM một dòng một xã | Đạt | |
| Ngoại lệ | Xóa Nút đang có dữ liệu → chặn | Xóa Nút xã A | Bị chặn, gợi ý vô hiệu hóa | Đạt | |

Kiểm thử tự động: `TestFN_AD_04_02_ImpactAndTreeScope`, `TestFN_AD_04_02_RegionAssignmentCompensating`, `TestFN_AD_04_02_SumByRegion`,
`TestCNTSAggregate_BM18_PL2_CloseShare_W1144`, `TestCNTSAggregate_BM02_Adjustment_W1144`.

### FN-03 — Quản lý danh mục phòng ban

- Vai: Admin. Điều kiện đầu: có Nút TTKN tỉnh.
- Màn: Phòng ban, mở module — `/kn/quan-tri/phong-ban`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B2 | Bật Phòng CNTS "Đang áp dụng" | Đổi trạng thái Phòng CNTS sang Đang áp dụng | Lưu được, có log | Đạt | |
| BR-01 | Phòng ban "Chưa mở" hiện mờ ở màn chọn phòng ban | Đăng nhập tài khoản 2 phòng (1 chưa mở) | Thẻ phòng chưa mở hiện mờ, không bấm được | Đạt | |
| AC-01 | Sau khi bật, người được gán Phòng CNTS thấy thẻ ở màn chọn phòng ban | T-TH đăng nhập lại (hoặc tải lại `/chon-vai-tro`) | Có thẻ Phòng CNTS | Đạt | |
| Ngoại lệ | Tắt phòng ban đang có dữ liệu → cảnh báo, chỉ ẩn khỏi màn chọn | Ngưng Phòng CNTS khi đã có báo cáo | Có cảnh báo số bản ghi; dữ liệu cũ còn nguyên | Đạt | |

Kiểm thử tự động: `TestFN_AD_04_03_DepartmentLifecycleAuditLog`, `TestUR_KT_02_MenuByConfigDepartmentToggle`.

### FN-04 — Quản lý tài khoản người dùng

- Vai: Admin. Điều kiện đầu: FN-02, FN-03.
- Màn: Quản trị hệ thống › Tài khoản, vai trò, địa bàn — `/kn/quan-tri/nguoi-dung`, `/kn/quan-tri/nguoi-dung/them-moi`,
  `/kn/quan-tri/nguoi-dung/[id]/vai-tro/them-moi`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B4 | Thêm tài khoản hoặc nạp Excel; gán vai trò, Nút, phòng ban, xã/cơ sở; kích hoạt | Tạo X-A (Cấp Xã, CNTS, xã A + B); nạp Excel 2 tài khoản | Tài khoản xuất hiện, có thư kích hoạt | Đạt (kích hoạt qua email) | |
| Mô tả | Thuộc tính Cấp Tỉnh: lãnh đạo phòng, chủ nhiệm, viên chức, cán bộ tổng hợp; BGĐ = lãnh đạo phòng + cờ "Chỉ xem" | Tạo T-BGD với cờ Chỉ xem | T-BGD xem như Lãnh đạo phòng, không có nút duyệt/sửa | Đạt | |
| Mô tả | Vai khách "Phòng nhận báo cáo" cho HCTH, KHTC | Đăng nhập HC | Chỉ thấy hộp "Báo cáo đến" `/kn/bao-cao-den` | Đạt | |
| Mô tả | Vai Nông hộ: tài khoản theo số điện thoại, OTP | Tạo tài khoản Nông hộ bằng SĐT | Nông hộ đăng nhập app bằng OTP | Thiếu: vai Nông hộ trên App KN chưa có (#173 và phần chưa có issue) | |
| BR-01 | Admin tạo tài khoản; Cấp Tỉnh chỉ cấp quyền trong module (FN-14) | T-TH mở `/kn/quan-tri/nguoi-dung` | Không có menu, gọi thẳng bị 403 | Đạt | |
| BR-02 | Số điện thoại che một phần khi hiển thị | Xem danh sách tài khoản | SĐT dạng `09•• ••• •12` | Đạt | |
| BR-03 | Mọi thay đổi quyền có log | Thêm rồi thu hồi một vai trò, mở tab Log | Có 2 dòng: người làm, thời điểm, cũ → mới | Đạt | |
| BR-04 | Cấp Tỉnh không sửa số liệu gốc của Cấp Xã, Nông hộ | T-TH mở BM-04 của xã A | Chỉ xem, không có ô nhập | Đạt | |
| AC-01 | Tài khoản mới đăng nhập được ngay | Đăng nhập X-A ngay sau kích hoạt | Vào "Việc cần làm" | Đạt | |
| AC-02 | Nông hộ nhận SMS kích hoạt | Tạo tài khoản Nông hộ | Có SMS | Thiếu: chưa gửi SMS; chờ #173 | |
| Ngoại lệ | Số điện thoại trùng → chặn | Tạo tài khoản trùng SĐT X-A | Bị từ chối | Đạt | |

Kiểm thử tự động: `TestFN_AD_04_04_CreateAccountRules`, `TestFN_AD_04_04_ImportMultiRoleAccounts`, `TestFN_AD_04_04_AssignmentLog`,
`TestFN_AD_04_04_ScopeIsolationPerContext`, `TestCNTSBM11_W1145` (BGĐ chỉ xem, phòng nhận).

### FN-05 — Đăng nhập & chọn phòng ban

- Vai: mọi vai trò. Điều kiện đầu: tài khoản hoạt động; Phòng CNTS "Đang áp dụng".
- Màn: `/login` → `/chon-vai-tro` → trang mặc định. Mobile: màn đăng nhập và `kn_context_picker_screen` của App KN.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B4 | Đăng nhập, chọn vai trò, chọn thẻ phòng ban, vào trang mặc định (Tỉnh: Trang chủ; Xã: Việc cần làm; Quốc gia: Dự án tại An Giang) | Đăng nhập T-TH, X-A, TW | Tỉnh vào trang chủ; Xã vào `/kn/cnts/bao-cao-ky`; TW vào `/kn/cnts/du-an` | Đạt cho Tỉnh, Xã. Thiếu: trang mặc định Cấp Quốc gia hiện là Dashboard (menu đang thu gọn ở #265) | |
| B5 | Thanh trên hiện tên, vai trò, phòng ban; đổi vai trò, phòng ban, đăng xuất | Mở menu tài khoản, đổi phòng ban | Menu đổi theo, không đăng nhập lại | Đạt | |
| Luồng phụ | Chỉ 1 vai trò, 1 phòng ban → vào thẳng | Đăng nhập X-C | Không qua màn chọn | Đạt | |
| BR-01 | Mỗi phiên 1 vai trò, 1 phòng ban; Phòng nhận báo cáo không chọn phòng ban | Đăng nhập HC | Vào thẳng "Báo cáo đến" | Đạt | |
| AC-01 | Menu, dữ liệu đúng vai trò của phiên | So menu X-A với mục VI.3 của BRD | Đúng các mục của Cấp Xã | Đạt (còn mục ngoài BRD đang gỡ ở #265) | |
| AC-02 | Tài khoản Phòng nhận báo cáo vào thẳng hộp "Báo cáo đến" | Như BR-01 | `/kn/bao-cao-den` | Đạt | |
| AC-03 | Sai mật khẩu 5 lần thì khóa 15 phút | Nhập sai 5 lần, thử lần 6 đúng mật khẩu | Bị khóa; sau 15 phút vào được | Đạt (cấu hình realm: 5 lần, 900 giây; realm `kn` kiểm lại khi #272 gộp) | |
| Ngoại lệ | Nông hộ đăng nhập bằng SĐT + OTP; OTP sai 3 lần → gửi lại sau 60 giây | Đăng nhập Nông hộ trên app | Vào thẳng app Nông hộ | Thiếu: chưa có đăng nhập OTP (#173 và phần chưa có issue) | |

Kiểm thử tự động: `TestFN_CH_00_01_MultiRoleContextsAndSwitch`, `TestFN_CH_00_01_DefaultRouteMatchesMenuContract`, `TestKeycloakRealmCoversRolesAndClaims`;
Flutter `kn_context_test.dart`.

### FN-06 — Quản trị danh mục Chăn nuôi – Thủy sản

- Vai: Admin. Điều kiện đầu: FN-03 đã bật Phòng CNTS.
- Màn: Phòng CNTS › Danh mục CNTS — `/kn/phong-ban/chan_nuoi_thuy_san/danh-muc-cnts` (thêm: `…/[type]/them-moi`; sửa: `…/[type]/[code]/sua`).

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B4 | Chọn tab danh mục; thêm, sửa; nạp Excel; vô hiệu hóa | Thêm 1 vaccine; nạp tệp mẫu 3 dòng; vô hiệu hóa 1 mục | Mục mới có trong danh sách; mục vô hiệu hóa có nhãn | Đạt | |
| BR-01 | Mã không trùng | Thêm mục trùng mã | Lỗi theo trường Mã | Đạt | |
| BR-02 | Cơ sở giết mổ thuộc một xã | Thêm cơ sở không chọn xã | Lỗi "bắt buộc" ở trường Xã | Đạt | |
| BR-03 | Thuộc tính ảnh hưởng tổng hợp lưu theo hiệu lực từ ngày – đến ngày; sửa là tạo giá trị mới, không ghi đè | Sửa mộc dấu cơ sở S1 với ngày áp dụng 01/10; xem lịch sử | Có 2 giá trị, giá trị cũ còn nguyên đến 30/09 | Đạt | |
| BR-04 | Báo cáo đã gửi dùng giá trị tại thời điểm gửi | Mở báo cáo KSGM đã gửi trước ngày sửa | Vẫn hiện mộc dấu cũ | Đạt | |
| AC-01 | Mục mới dùng được ngay trong FN-07, FN-15 | Thêm vaccine rồi mở `/kn/cnts/lich-tiem` | Vaccine mới chọn được | Đạt | |
| AC-02 | Cơ sở A có mộc dấu GS đến 30/09, thêm GC từ 01/10: KSGM tháng 9 không có cột gia cầm của A; tháng 10 có | Cấu hình như AC; mở `/kn/cnts/ksgm` tháng 9 và tháng 10 | Tháng 9 không có cột gia cầm của A; tháng 10 có | Thiếu: chưa thấy kiểm thử cho đúng tình huống này; kiểm tay khi UAT (chưa có issue) | |
| Ngoại lệ | Mục đang dùng trong biểu → chỉ vô hiệu hóa, không xóa | Tìm nút Xóa ở mục đang dùng | Không có Xóa | Đạt | |

Kiểm thử tự động: vitest `src/kn/cnts/catalog/cnts.test.tsx`; `TestCNTSAggregate_BM02_Adjustment_W1144`.

### FN-07 — Thiết kế & phát hành biểu mẫu

- Vai: Cấp Tỉnh (cán bộ tổng hợp T-TH), Admin. Điều kiện đầu: FN-06 có danh mục; FN-14 đã cấp quyền.
- Màn: Biểu mẫu › Thiết kế biểu mẫu — `/kn/cnts/thiet-ke-bieu-mau`; Phát hành, theo dõi nộp — `/kn/cnts/bieu-mau`;
  Admin cài biểu chuẩn — `/kn/phong-ban/chan_nuoi_thuy_san/bieu-ky-cnts`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Chọn hoặc tạo biểu, sửa cấu trúc; xem trước, phát hành phiên bản | Mở BM-17, thêm 1 cột, lưu phiên bản mới | Có phiên bản 2 trong lịch sử | Đạt | |
| B3–B4 | Phát hành đợt: biểu, người phụ trách, kỳ, hạn nộp, chốt kỳ, nhóm xã; xem trước số việc | Phát hành BM-04 tuần hiện tại cho Khu vực 1, người phụ trách T-TH | Xem trước đúng số xã; sau Phát hành có dòng theo dõi | Đạt | |
| Luồng phụ | Sao chép biểu; nạp cấu trúc từ Excel | Sao chép BM-04 thành biểu điều tra; nạp tệp cấu trúc | Biểu `CNTS-DT-…` phiên bản 1 | Đạt | |
| Ngoại lệ | Xã chưa có cán bộ được cấp quyền → cảnh báo trước khi phát hành | Chọn nhóm có xã chưa cấp quyền | Có cảnh báo tên xã | Đạt | |
| BR-01 | Phiên bản mới không ảnh hưởng kỳ đã nộp | Sau khi xã A nộp BM-17, sửa cấu trúc; mở lại kỳ đã nộp | Kỳ cũ giữ cột và số cũ | Đạt | |
| BR-02 | Mẫu KNQG không sửa cấu trúc | Thử sửa cấu trúc BM-01 | Bị từ chối | Đạt | |
| AC-01 | Phát hành BM-04 tuần 40 cho 99 xã: 99 cán bộ xã thấy việc "BM-04 tuần 40, hạn 12:00 06/10" trong 1 phút | Phát hành cho mọi xã; X-A, X-C mở Việc cần làm | Việc hiện trong ≤ 1 phút, đúng hạn | Đạt (cơ chế); đo 1 phút khi UAT | |
| AC-02 | Đợt chưa chọn người phụ trách thì không bấm Phát hành được | Bỏ trống người phụ trách | Nút Phát hành khóa/lỗi theo trường | Đạt | |
| AC-03 | Sửa cấu trúc biểu không làm đổi số các kỳ đã nộp | Như BR-01 | Số kỳ đã nộp không đổi | Đạt | |
| UR-TINH-07 | Mẫu nhật ký động (chỉ tiêu, tần suất, bắt buộc GPS/ảnh), loại biểu mã mới | Tạo mẫu nhật ký mới | Mẫu dùng được ở FN-10 | Thiếu: đang làm ở #275 (gói con #303, #304, #305) | |

Kiểm thử tự động: `TestCNTSDesignEdit_FN07_192`, `TestCNTSDesignReadAndImport_BF_CN_FORM_DESIGN`, `TestCNTSPublish_TargetOwnerDraft_BF_CN_PUBLISH`,
`TestCNTSTrackingScope_BF_CN_PUBLISH`; vitest `periods/publish.test.tsx`, `design/`; Playwright `kn-cnts-design.spec.ts`.

### FN-08 — Công việc cần làm

- Vai: Cấp Xã (X-A). Điều kiện đầu: có đợt phát hành, lịch trực, lịch tiêm.
- Màn: Việc cần làm (trang mặc định) — `/kn/cnts/bao-cao-ky`. Mobile: `cnts_tasks_screen` (Hình 47a).

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B3 | Danh sách: xã, công việc, kỳ, hạn, trạng thái; lọc; bấm Nhập/Tiếp tục/Sửa mở đúng màn | Mở Việc cần làm, lọc theo biểu, bấm Nhập ở BM-04 | Mở đúng biểu, đúng xã, đúng kỳ | Đạt cho biểu kỳ (BM-01, 04, 05, 17) và biểu đợt điều tra | |
| Mô tả | Gồm cả lần ghi nhật ký đến hạn, mũi tiêm đến hạn, lịch công tác cần đăng ký | Tạo mũi tiêm đến hạn hôm nay; mở Việc cần làm | Có dòng việc cho mũi tiêm, nhật ký, lịch | Thiếu: các việc này chưa gom vào danh sách, đang xem ở màn riêng (`/kn/cnts/lich-tiem`, `/kn/cnts/nhat-ky`, `/kn/nhiem-vu/lich-cong-tac`) — chưa có issue | |
| Luồng phụ | Thông báo đẩy khi có việc mới, đến hạn, bị nhắc | T-TH bấm Nhắc; X-A xem chuông/app | Có thông báo | Đạt (cơ chế) | |
| BR-01 | Trạng thái do hệ thống cập nhật | Gửi BM-04 | Việc chuyển "Đã nộp", không sửa tay được | Đạt | |
| BR-02 / AC-03 | Chỉ thấy việc của phạm vi được phân công; cán bộ phụ trách 2 xã chỉ thấy việc của 2 xã đó | X-A mở danh sách | Chỉ có xã A, xã B; không có xã C | Đạt | |
| AC-01 | Việc mới hiện trong 1 phút sau phát hành; gửi xong chuyển "Đã nộp" | Phát hành rồi xem ở X-A | Hiện trong ≤ 1 phút | Đạt (cơ chế); đo khi UAT | |
| AC-02 | Quá 12:00 thứ Ba mà BM-04 chưa gửi thì việc hiện nhãn "Trễ hạn" | Để BM-04 quá hạn | Nhãn Trễ hạn | Đạt | |
| Ngoại lệ | Không có việc → "Chưa có công việc" | Đăng nhập cán bộ xã chưa được giao biểu | Hiện câu trống | Đạt | |

Kiểm thử tự động: `TestCNTSGrantScopeForms_49_71`, `TestCNTSAlerts_W1143` (nhắc); vitest `periods/periods.test.ts`; Flutter `cnts_period_test.dart`.

### FN-09 — Nhập báo cáo chăn nuôi – thủy sản theo kỳ

- Vai: Cấp Xã (Admin nhập thay khi hỗ trợ). Điều kiện đầu: biểu đã phát hành; BM-01 cần được cấp quyền cơ sở.
- Màn: Báo cáo › KSGM hằng ngày, Nhập liệu tuần chăn nuôi — `/kn/cnts/bao-cao-ky`; Khai thác thủy sản tuần —
  `/kn/cnts/khai-thac-thuy-san`. Cấp Tỉnh xem: `/kn/cnts/so-lieu-xa/nhap-lieu-tuan`, `/kn/cnts/so-lieu-xa/khai-thac-thuy-san`.
  Mobile: `cnts_entry_screen` (Hình 47b).

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B5 | Mở từ Việc cần làm, nhập theo bảng trường, xem Kiểm tra số liệu, Lưu nháp hoặc Kiểm tra & gửi | X-A nhập BM-04 xã A, lưu nháp, mở lại, gửi | Nháp giữ số; gửi xong trạng thái Đã nộp | Đạt | |
| Luồng phụ | Sao chép kỳ trước; Báo không phát sinh | Bấm Sao chép kỳ trước; ở BM-01 chọn Báo không phát sinh | Số kỳ trước được điền; bài không phát sinh gửi được, tính 0 | Đạt | |
| Luồng phụ | Nhập từ Excel (BM-04) | Bấm Nhập từ Excel | Số từ tệp vào lưới | Thiếu: chưa thấy nạp Excel ở màn nhập BM-04 (chưa có issue) | |
| Luồng phụ | Nhập trên Mobile khi mất mạng | Tắt mạng, nhập BM-04 trên app, bật mạng | Bài đồng bộ, không trùng | Đạt | |
| Ngoại lệ | Gửi sau chốt kỳ → từ chối; bản offline → "Chờ mở kỳ" | Chốt kỳ rồi gửi | Bị từ chối; bản offline có nhãn Chờ mở kỳ | Đạt | |
| BR-01 | Lũy kế = tổng các kỳ hợp lệ từ đầu năm (quý); kỳ không nộp tính 0, hiện "Thiếu kỳ" | Xem AC-03 | Như AC-03 | Đạt | |
| BR-02 | Được sửa và gửi lại đến khi chốt kỳ; mỗi lần gửi lưu phiên bản | Gửi, sửa, gửi lại; mở lịch sử | Có 2 phiên bản | Đạt | |
| AC-01 | Cột, thứ tự cột, đơn vị trùng mẫu Excel hiện hành | So màn BM-04, BM-17 với tệp Excel gốc theo chữ cái cột | Trùng | Đạt (kiểm thử khẳng định thứ tự trường và tên sheet; đối chiếu tệp gốc làm tay) | |
| AC-02 | Số liệu gửi hiện ở tổng hợp Cấp Tỉnh trong 1 phút | Gửi BM-04; T-TH mở Số liệu cấp xã | Có số trong ≤ 1 phút | Đạt (cơ chế); đo khi UAT | |
| AC-03 | Tuần 37 (10 liều), bỏ tuần 38, tuần 39 (5 liều) → lũy kế tuần 39 = 15 và "Thiếu kỳ 38"; nộp bù tuần 38 (3 liều) → 18 | Làm đúng 3 kỳ rồi mở lại kỳ 38 và nộp | 15 + nhãn Thiếu kỳ 38, rồi 18 | Đạt | |
| AC-04 | Hai người cùng mở báo cáo, người thứ hai Lưu nhận thông báo xung đột, không ghi đè | Mở cùng bài ở 2 trình duyệt, lưu lần lượt | Người thứ hai được báo tải bản mới | Đạt | |
| AC-05 | Sửa BM-04 sau hạn không nhập lý do thì không gửi được; có lý do thì gửi được, Lịch sử sửa hiện giá trị cũ → mới | Sau hạn, sửa 1 ô, gửi không lý do, rồi có lý do | Lần 1 lỗi ở ô lý do; lần 2 gửi được; lịch sử có cũ → mới | Đạt | |
| AC-06 | Sửa bản Đã xác thực thì báo cáo về Đã nộp, có cờ "Đã sửa sau xác thực" | T-TH xác thực; X-A sửa và gửi lại | Trạng thái Đã nộp + cờ | Đạt | |

Kiểm thử tự động: vitest `periods/bm04.test.tsx` (AC-03, AC-05, AC-06), `periods/periods.test.ts`, `catch/catch.test.tsx`;
`TestCNTSReviewOwnerOnly_175`, `TestCNTSAndReportCycleRequireOwnedSessionThroughHTTP`; Flutter `cnts_period_test.dart`.

### FN-10 — Hộ mô hình & nhật ký sản xuất

- Vai: Cấp Xã, Nông hộ; Cấp Tỉnh (cán bộ tổng hợp) với danh sách hộ chăn nuôi. Điều kiện đầu: hộ thuộc điểm trình diễn (FN-19),
  mẫu nhật ký đã gán; với danh sách hộ: cán bộ có xã phụ trách, thiết bị có GPS, danh mục vật nuôi.
- Màn: Dữ liệu › Hộ mô hình — `/kn/cnts/ho-mo-hinh`; Nhật ký sản xuất — `/kn/cnts/nhat-ky`; Lịch tiêm phòng — `/kn/cnts/lich-tiem`;
  Danh sách hộ chăn nuôi — `/kn/cnts/so-lieu-xa/ho-chan-nuoi` (`…/them-moi`, `…/[id]`, `…/[id]/sua`).
  Cấp Tỉnh xem nhật ký: `/kn/cnts/so-lieu-xa/nhat-ky`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B4 | Chọn hộ/điểm; "Ghi lần mới" theo bảng trường; vị trí, thời điểm tự ghi; chụp ảnh; gửi | X-A ghi 1 lần nhật ký gà và 1 lần thủy sản có ảnh | Lần ghi có vị trí, thời điểm, ảnh | Đạt | |
| H1–H3 | Thêm hộ: HCN-02…HCN-06; check-in tại nhà hộ; lưu → Đang dùng | Thêm hộ, check-in, lưu | Hộ có mã `HCN…`, trạng thái Đang dùng | Đạt | |
| H4 | Đổi vị trí phải check-in lại và nhập lý do | Sửa vị trí hộ | Bắt check-in lại + lý do | Đạt | |
| Luồng phụ | Bật "Hộ tự nhập" → đề nghị cấp quyền nông hộ; Nông hộ tự ghi nhật ký, xác nhận mũi tiêm trên Mobile | Bật Hộ tự nhập; Nông hộ ghi trên app | Nông hộ ghi được | Thiếu: có ô "Hộ tự nhập", chưa có app Nông hộ (#173 và phần chưa có issue) | |
| Luồng phụ | Mất mạng → lưu trên máy, tự đồng bộ (nhật ký) | Ghi nhật ký trên App KN khi mất mạng | Đồng bộ đúng thời điểm, vị trí lúc ghi | Thiếu: App KN chưa có màn nhật ký; máy chủ đã nhận mã lần ghi và giờ thiết bị (chưa có issue) | |
| Ngoại lệ | Thiếu ảnh bắt buộc → chặn; vị trí giả lập → đánh dấu | Gửi lần cân đo thủy sản không ảnh | Bị chặn | Đạt (cờ giả lập từ thiết bị: Thiếu, chưa có issue) | |
| Ngoại lệ | GPS > 50 m → cảnh báo, vẫn lưu; trùng họ tên, ấp, xã → cảnh báo trùng | Check-in với sai số lớn; thêm hộ trùng | Có cảnh báo; xác nhận hộ khác thì lưu | Đạt | |
| BR-01, BR-02 | Vị trí, thời điểm, ảnh không sửa; lần ghi đã xác thực không sửa; sửa trong 7 ngày nếu chưa xác thực | Sửa lần ghi sau 8 ngày | Bị chặn | Đạt | |
| BR-03 | Vị trí hộ lấy GPS khi check-in, không nhập tay | Tìm ô nhập tọa độ | Không có | Đạt | |
| BR-04 | Danh sách hộ là master data; không xóa, chỉ vô hiệu hóa (cán bộ tổng hợp Cấp Tỉnh) | T-TH vô hiệu hóa hộ có lý do; X-A thử | T-TH được; X-A không có nút | Đạt | |
| BR-05 / HCN-09 | Mỗi lần sửa lưu lịch sử người sửa, thời điểm, giá trị cũ → mới | Sửa họ tên và loại chăn nuôi | Lịch sử hiện cũ → mới | Đạt cho loại chăn nuôi. Thiếu: họ tên cũ → mới còn che bớt, sửa ở #276 | |
| BR-06 | Báo cáo đã gửi giữ số liệu tại thời điểm lập | Đổi loại hộ sau khi BM-11 đã gửi | Báo cáo cũ không đổi | Đạt | |
| BR-07 / AC-07 | Hộ vô hiệu hóa không còn trong DB-04; dữ liệu cũ vẫn hiện | Vô hiệu hóa hộ; tạo báo cáo dịch bệnh mới | Không chọn được hộ đó | Đạt | |
| BR-08 / AC-09 | Hộ nhập mới tại BM-10 ở trạng thái "Chờ bổ sung vị trí"; check-in → Đang dùng; chưa đếm ở báo cáo định kỳ trước đó | Nhập hộ mới ở DB-04; lập BM-11; check-in; lập BM-11 kỳ sau | Kỳ đầu chưa đếm; kỳ sau đếm | Đạt | |
| AC-01 | Lần ghi hiện ở rà soát Cấp Tỉnh trong 1 phút sau đồng bộ | Gửi lần ghi; T-CN mở `/kn/cnts/ra-soat-nhat-ky` | Có trong ≤ 1 phút | Đạt (cơ chế); đo khi UAT | |
| AC-02 | Lần đo pH 8,7 tạo cảnh báo cho chủ nhiệm ngay khi gửi | Gửi lần đo pH 8,7 | T-CN có cảnh báo môi trường nước | Đạt | |
| AC-03 | Ghi nhật ký khi mất mạng, đồng bộ đúng thời điểm, vị trí lúc ghi | Như luồng phụ mất mạng | Như trên | Thiếu: như luồng phụ mất mạng | |
| AC-04 | Lần ghi đã xác thực không có nút Sửa | Mở lần ghi đã xác thực | Không có Sửa | Đạt | |
| AC-05 | Thêm hộ chưa check-in thì không lưu được | Lưu không check-in | Lỗi yêu cầu check-in | Đạt | |
| AC-06 | Cán bộ xã chỉ thấy, chỉ thêm hộ của xã mình | X-A thêm hộ xã C | Không chọn được xã C | Đạt | |
| AC-08 | Sửa loại chăn nuôi thì lịch sử ghi người sửa, thời điểm, cũ → mới | Sửa loại | Có dòng lịch sử | Đạt | |

Kiểm thử tự động: `TestLivestockHouseholds_251`, `TestIdentityRevisions_251`, `TestCNTSDiary_W1148`, `TestCNTSModel_W1147`, `TestCNTSBM11_W1145`,
`TestFN11_MultiHousehold_252`; vitest `livestock/`, `diary/`. Giả định: [CNTS-V4-HOUSEHOLD-LIST](../kn/gia-dinh/CNTS-V4-HOUSEHOLD-LIST.md),
[ADR-0056](../adr/0056-ho-chan-nuoi-cnts-v4.md).

### FN-11 — Báo cáo dịch bệnh & báo nghi dịch

- Vai: Cấp Xã, Nông hộ. Điều kiện đầu: danh mục bệnh (FN-06).
- Màn: Báo cáo › Báo cáo dịch bệnh — `/kn/cnts/dich-benh`, `/kn/cnts/dich-benh/tao-moi`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B5 | Tạo báo cáo, nhập theo bảng trường, vị trí, đính kèm, gửi Cấp Tỉnh, cập nhật diễn biến | X-A tạo ổ dịch 3 hộ, gửi; cập nhật diễn biến 1 lần | Có 2 phiên bản | Đạt | |
| Mô tả | Nông hộ báo nghi dịch kèm vị trí, ảnh trên Mobile; cán bộ xã xác nhận và tạo báo cáo | Nông hộ gửi nghi dịch; X-A xác nhận | Thành báo cáo dịch bệnh | Thiếu: bước xác nhận/bỏ qua nghi dịch có trên web; chưa có app Nông hộ để gửi (chưa có issue) | |
| Luồng phụ | Xuất Word theo mẫu | Bấm xuất Word | Tệp có DB-15, bảng hộ, DB-16 | Đạt | |
| Luồng phụ | Lưu nháp offline | Tạo báo cáo khi mất mạng | Nháp giữ trên máy | Thiếu: chưa thấy nháp ngoại tuyến cho BM-10 (chưa có issue) | |
| BR-01 | Báo cáo mới tạo cảnh báo Khẩn | Gửi báo cáo; T-TH mở `/kn/cnts/canh-bao` | Có cảnh báo Khẩn | Đạt | |
| BR-02 | Mỗi lần cập nhật lưu phiên bản | Như B5 | Phiên bản giữ tập hộ của nó | Đạt | |
| BR-03 | Một ổ dịch nhiều hộ (DB-04); loại chăn nuôi theo từng hộ (DB-14); số hộ tự đếm (DB-15); thủy sản ghi diện tích (DB-16); tổng hợp lấy lần cập nhật mới nhất | Chọn 3 hộ; cập nhật còn 2 hộ; xem bản đồ | Vùng dịch hiện 2 hộ, không cộng dồn | Đạt | |
| AC-01 | Ổ dịch hiện trên bản đồ (FN-18) sau khi gửi và tạo cảnh báo Khẩn | Mở `/kn/cnts/ban-do-dich` | Có điểm ổ dịch | Đạt | |
| AC-02 | Gửi báo cáo chưa có vị trí thì bị chặn | Bỏ trống DB-05, gửi | Bị chặn | Đạt | |
| AC-03 | Số con tiêu hủy lớn hơn số con mắc bệnh thì bị chặn | DB-10 > DB-06 | Bị chặn | Đạt | |
| AC-04 | Chọn 3 hộ ở DB-04 thì DB-15 hiện 3, không sửa tay | Chọn 3 hộ | DB-15 = 3, ô khóa | Đạt | |
| AC-05 | Bệnh nhóm thủy sản mà để trống DB-16 thì không gửi được | Chọn bệnh thủy sản, bỏ trống DB-16 | Bị chặn | Đạt | |

Kiểm thử tự động: `TestFN11_MultiHousehold_252`, `TestFN11_OutbreakVersions`, `TestFN11_SuspectConfirmDismiss`, `TestFN11_LegacyOutbreak_252`,
`TestWordDataHouseholds`; vitest `outbreak/`.

### FN-12 — Lịch công tác & báo cáo công việc tuần, duyệt lịch

- Vai: Cấp Xã, Cấp Tỉnh (người theo dõi T-VC, Lãnh đạo phòng). Điều kiện đầu: cán bộ có xã phụ trách.
- Màn: Báo cáo công việc tuần — `/kn/cnts/cong-viec-tuan`; Lịch công tác (Xã gửi duyệt, Tỉnh duyệt, Lịch phòng) —
  `/kn/nhiem-vu/lich-cong-tac`, `/kn/nhiem-vu/lich-cong-tac/[id]`. Mobile: `work_schedule_screen` (Hình 47c, Hình 49).

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B3 | Thứ Năm/Sáu đăng ký Phần II; thứ Hai nhập Phần I; kế hoạch thành lịch tuần sau | X-A gửi Phần II; mở Lịch công tác | Có lịch tuần sau từ kế hoạch | Đạt | |
| B6–B7 | Lịch gửi ở Chờ duyệt; người theo dõi Duyệt (từng/hàng loạt) hoặc Yêu cầu chỉnh sửa kèm lý do | T-VC mở tab Chờ duyệt, yêu cầu chỉnh sửa, rồi duyệt | X-A nhận kết quả kèm lý do | Đạt | |
| B4, B4b | Check-in tại địa điểm; Check-out khi rời, chụp ảnh; nút Check-out chỉ hiện sau check-in | Trên app: check-in có ảnh, rồi check-out có ảnh | Lưu ảnh, GPS, thời điểm | Đạt | |
| Luồng phụ | Sao chép kế hoạch tuần trước; xuất Excel lịch phòng | Bấm Sao chép; T-VC xuất Excel | Kế hoạch điền sẵn; có tệp | Đạt | |
| BR-01 | Mỗi phần có hạn, trạng thái riêng | Gửi Phần II, chưa gửi Phần I | Hai trạng thái độc lập | Đạt | |
| BR-03 | Nháp → Chờ duyệt → Đã duyệt / Yêu cầu chỉnh sửa; sửa lịch đã duyệt về Chờ duyệt, đánh dấu "Đã điều chỉnh" | Sửa lịch đã duyệt | Về Chờ duyệt, có nhãn | Đạt | |
| BR-04 | Chỉ người theo dõi được phân công và Lãnh đạo phòng duyệt; lưu người, thời điểm, lý do | X-C thử duyệt lịch X-A | Bị 403; lịch sử duyệt đủ thông tin | Đạt | |
| BR-05 | Lịch tuần đã kết thúc bị khóa | Sửa lịch tuần trước | Bị chặn | Đạt | |
| BR-06 | Check-in, check-out lưu ảnh, GPS, thời điểm, độ chính xác; không sửa được | Check-out lần hai | Bị từ chối | Đạt | |
| AC-01 | Thứ Năm nhắc đăng ký kế hoạch; thứ Hai nhắc nộp kết quả, đúng giờ Cấp Tỉnh đặt | Đặt giờ nhắc ở Cấu hình nhắc nhở; chờ tới giờ | X-A nhận nhắc | Đạt (cơ chế nhắc theo cấu hình biểu); kiểm đúng giờ khi UAT | |
| AC-05 | Kế hoạch đăng ký 02/10 hiện ở lịch tuần 05/10–09/10; sửa sau đó phải nhập lý do | Đăng ký rồi sửa | Bắt nhập lý do | Đạt | |
| AC-02 | Người theo dõi thấy check-in trong 1 phút | X-A check-in; T-VC mở tab Check-in, check-out | Có dòng trong ≤ 1 phút | Đạt (cơ chế); đo khi UAT | |
| AC-03 | Check-in cách địa điểm hơn 500 m tạo cảnh báo | Check-in xa địa điểm CV-06 | Có cảnh báo khoảng cách | Đạt | |
| AC-04 | Thời điểm, vị trí check-in không sửa được | Tìm nút sửa check-in | Không có | Đạt | |
| AC-06 | Lịch gửi thứ Sáu hiện ngay ở tab Chờ duyệt | Như B6 | Hiện ngay | Đạt | |
| AC-07 | Yêu cầu chỉnh sửa không nhập lý do thì không gửi được | Bỏ trống lý do | Bị chặn | Đạt | |
| AC-08 | Sửa lịch đã duyệt thì trạng thái về Chờ duyệt | Như BR-03 | Chờ duyệt | Đạt | |
| AC-09 | Bấm check-out mà chưa chụp ảnh thì không lưu được | Hủy chụp ảnh | Không lưu | Đạt | |
| AC-10 / BR-07 | Check-in 08:00, chưa check-out đến 23:59 → sáng hôm sau người theo dõi thấy nhãn "Chưa check-out" | Check-in, không check-out, xem hôm sau | Có nhãn và cảnh báo | Đạt | |
| AC-11 | Check-out mất mạng 11:30, có mạng 14:00 → ghi 11:30 | Tắt mạng, check-out, bật mạng sau | Thời điểm 11:30 | Đạt | |
| AC-12 | Check-in 08:00, check-out 11:30 → thời gian ở địa điểm 3 giờ 30 phút | Xem chi tiết lịch | 3 giờ 30 phút | Đạt | |
| AC-13 / BR-08 | Check-out offline 16:30 ngày 06/10, đồng bộ 07:10 ngày 07/10 → gỡ nhãn, cảnh báo tự đóng, ghi "Đồng bộ trễ" | Làm đúng tình huống | Nhãn gỡ, có Đồng bộ trễ | Đạt (cảnh báo tự đóng trong ≤ 5 phút theo chu kỳ job) | |

Kiểm thử tự động: `TestWorkScheduleCheckin`, `TestWorkScheduleCheckoutPhoto`, `TestWorkScheduleNoCheckout`, `TestFN_XA_03_08_RegisterSubmitReviewFlow`,
`TestFN_TI_02_07_UnregisteredBulkRemindersExport`; vitest `weekly/`, `schedule/`; Flutter `work_schedule_checkin_test.dart`;
Playwright `kn-lich-cong-tac.spec.ts`. Giả định: [CNTS-V4-CHECKOUT-PHOTO](../kn/gia-dinh/CNTS-V4-CHECKOUT-PHOTO.md),
[CNTS-V4-CHECKIN-MONITOR](../kn/gia-dinh/CNTS-V4-CHECKIN-MONITOR.md).

### FN-13 — Theo dõi công việc cấp xã, rà soát & cảnh báo

- Vai: Cấp Tỉnh. Điều kiện đầu: có đợt phát hành; có lần ghi nhật ký.
- Màn: Phát hành, theo dõi nộp — `/kn/cnts/bieu-mau`; Rà soát nhật ký — `/kn/cnts/ra-soat-nhat-ky`; Cảnh báo, Cấu hình nhắc nhở,
  Ngưỡng — `/kn/cnts/canh-bao`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1 | Xem trạng thái theo đợt, kỳ; gửi nhắc | Mở theo dõi BM-04, bấm Nhắc xã chưa nộp | Có đếm theo trạng thái; xã nhận nhắc | Đạt | |
| B2 | Mở lần ghi chờ rà soát; xác thực hoặc yêu cầu bổ sung có lý do | T-CN xác thực 1 lần ghi, yêu cầu bổ sung 1 lần | Trạng thái đổi; xã nhận lý do | Đạt | |
| B3 | Xem cảnh báo, giao người xử lý, đóng có kết quả | Giao, nhận, đóng 1 cảnh báo | Vòng đời đủ 3 bước | Đạt | |
| B4 / BR-03 | Chốt kỳ; mở lại kỳ: Lãnh đạo phòng hoặc người được ủy quyền, có lý do và thời hạn | T-TH chốt kỳ; T-LD mở lại cho xã A có lý do | Xã A gửi lại được trong thời hạn | Đạt | |
| Luồng phụ | Cấu hình nhắc nhở theo biểu; xem số nhắc đã gửi trong ngày | Mở tab Cấu hình nhắc nhở | Sửa được giờ, nhắc lặp | Đạt | |
| BR-01 | Không sửa số liệu gốc của xã | Tìm ô nhập ở màn Tỉnh | Không có | Đạt | |
| BR-04 | Cảnh báo theo công thức Mục III.5; không tạo trùng | Gửi 2 lần đo vượt ngưỡng cùng ngày | Chỉ 1 cảnh báo | Đạt cho dòng 1–9, 13. Thiếu: dòng 10–11 (nhật ký trễ, lịch tiêm trễ) — #173 phần lịch tiêm, phần nhật ký chưa có issue | |
| AC-01 | Cảnh báo tạo trong 1 phút sau khi dữ liệu đồng bộ | Gửi số vượt ngưỡng | Cảnh báo trong ≤ 1 phút | Đạt (cơ chế); đo khi UAT | |
| AC-02 | pH 8,6 tại điểm X tạo đúng 1 cảnh báo Cần chú ý; lần đo khác cùng ngày pH 8,7 không tạo cảnh báo thứ hai | Gửi 2 lần đo | 1 cảnh báo | Đạt | |
| AC-03 | Đến thời điểm chốt kỳ, báo cáo Lưu nháp chuyển "Không nộp" và xã không bấm Gửi được | Để nháp qua giờ chốt | Không nộp; Gửi bị từ chối | Đạt | |
| AC-04 | Tài khoản không phải Lãnh đạo phòng, không được ủy quyền thì không thấy nút "Mở lại kỳ" | T-VC mở màn theo dõi | Không có nút | Đạt (kiểm lại nút theo vai khi UAT) | |
| AC-05 | Người nhận cảnh báo biến động tổng đàn là người phụ trách của đợt phát hành | Gửi BM-04 biến động lớn | Người phụ trách đợt nhận | Đạt | |
| AC-06 | BM-04 trễ hạn: nhắc lặp mỗi ngày đến khi gửi; trễ 2 kỳ liên tiếp → người phụ trách biểu và lãnh đạo phòng nhận cảnh báo | Để trễ 2 kỳ | Có nhắc lặp và cảnh báo trễ liên tiếp | Đạt | |
| Ngoại lệ | Yêu cầu bổ sung không có lý do → chặn | Bỏ trống lý do | Bị chặn | Đạt | |

Kiểm thử tự động: `TestCNTSAlerts_W1143`, `TestProvinceAlertScope_W1143`, `TestFormReminderConfig`, `TestCNTSDiary_W1148`,
`TestCNTSTrackingScope_BF_CN_PUBLISH`, `TestCNTSReviewOwnerOnly_175`; vitest `alert/alert.test.ts`, `periods/publish.test.tsx`.

### FN-14 — Cấp quyền cán bộ xã, nông hộ

- Vai: Cấp Tỉnh. Điều kiện đầu: tài khoản đã có (FN-04).
- Màn: Biểu mẫu › Cấp quyền — `/kn/cnts/cap-quyen`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B3 | Lọc người dùng; chọn, Cấp quyền; chọn vai trò, địa bàn, biểu mẫu, hiệu lực; lưu | Cấp cho X-A: xã A, B; cơ sở S1; BM-01, BM-04 | Có dòng quyền, có nhật ký | Đạt | |
| Luồng phụ / AC-02 | Thu hồi; chuyển việc chưa nộp cho người mới | Thu hồi X-A, chọn người nhận X-C | Việc chưa nộp của A sang C | Đạt | |
| Ngoại lệ | Gán địa bàn ngoài tỉnh → chặn | Gán xã tỉnh khác (API) | Bị chặn | Đạt | |
| BR-01 | Chỉ cấp vai trò Xã, Nông hộ trong module CNTS | Xem danh sách vai chọn được | Chỉ vai Xã, Nông hộ | Đạt | |
| BR-02 / AC-03 | Phạm vi nhập KSGM là danh sách cơ sở được cấp ở đây; viên chức chỉ thấy cơ sở được cấp trong BM-01 | X-A mở BM-01 | Chỉ có cơ sở S1 | Đạt | |
| BR-03 | Chủ nhiệm chỉ cấp quyền trong chương trình mình | T-CN cấp quyền ở dự án khác | Bị chặn | Đạt | |
| AC-01 | Quyền có hiệu lực ở lần đăng nhập kế tiếp | Cấp quyền rồi tải lại ở X-A | Thấy việc mới | Đạt | |
| — | Cấp quyền cho mã biểu động có phiên bản | Cấp biểu điều tra mới | Chọn được | Thiếu: #304 | |

Kiểm thử tự động: `TestCNTSModel_W1147`, `TestCNTSGrantOwnerSession_71`, `TestCNTSGrantNarrowingStaysExplicit_71`, `TestCNTSGrantScopeForms_49_71`.

### FN-15 — Lịch trực KSGM & lịch tiêm phòng

- Vai: Cấp Tỉnh, Cấp Xã. Điều kiện đầu: danh mục cơ sở, vaccine; hộ mô hình.
- Màn: Lịch › Lịch trực KSGM — `/kn/cnts/lich-truc` (Xã: "Lịch trực KSGM của tôi" cùng route); Lịch tiêm phòng — `/kn/cnts/lich-tiem`.
  Admin: `/kn/phong-ban/chan_nuoi_thuy_san/lich-truc`, `…/lich-tiem`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B3 | Chọn tháng; sao chép tháng trước hoặc nạp Excel; đổi ca trên lưới; lưu → thông báo | Sao chép tháng trước, đổi 1 ca, lưu | X-A nhận thông báo đổi ca | Đạt | |
| B4 | Lịch tiêm: "Tạo lịch theo quy trình của mô hình" | Tạo lịch cho hộ mô hình gà | Có danh sách mũi theo quy trình | Đạt | |
| Luồng phụ | Xuất Excel đúng mẫu | Xuất .xlsx | Tệp có tiêu đề tháng, ghi chú ca | Đạt (đối chiếu mẫu BM-03 gốc làm tay) | |
| Ngoại lệ | Ngày chưa có người trực → cảnh báo | Bỏ trống 1 ngày, lưu | Có cảnh báo | Đạt | |
| BR-01 | Trực 2 cơ sở cùng ca → cảnh báo; lịch trực không chặn nhập BM-01 | Xếp X-A 2 cơ sở cùng ca | Cảnh báo; vẫn nhập BM-01 được | Đạt | |
| BR-02 | Quy trình tiêm gắn loại mô hình/giống | Đổi loại mô hình | Quy trình đổi theo | Đạt | |
| BR-03 | Ca đêm qua nửa đêm thuộc ngày bắt đầu ca | Xem ca Đ ngày 05 | Thuộc ngày 05 | Đạt | |
| BR-04 / AC-02 | Viên chức trong lịch chưa được cấp quyền cơ sở → cảnh báo khi lưu | Xếp X-C vào cơ sở chưa cấp | Cảnh báo | Đạt | |
| AC-01 | Lịch hiện ở Cấp Xã trong 1 phút sau khi lưu | X-A mở Lịch trực của tôi | Có ca trong ≤ 1 phút | Đạt (cơ chế); đo khi UAT | |
| AC-03 | Hộ nhận gà 19/09, Gumboro lần 1 ở 7 ngày tuổi → ngày dự kiến tự tính đúng | Tạo lứa nhận 19/09 | Ngày dự kiến theo quy trình | Đạt | |

Kiểm thử tự động: `TestDutyRoster`, `TestDutyRosterProvinceIsolation_W1146`, `TestCNTSDutyChanged_W1146`, `TestVaccinationBatch`, `TestGenerateDosesAC03`;
vitest `schedules/`; Playwright `kn-cnts-schedules.spec.ts`.

### FN-16 — Báo cáo tổng hợp CNTS theo kỳ

- Vai: Cấp Tỉnh (cán bộ tổng hợp, lãnh đạo phòng). Điều kiện đầu: kỳ có số liệu đã nộp.
- Màn: Báo cáo định kỳ (lập, trình, duyệt) — `/kn/cnts/bao-cao-dinh-ky`; KSGM tuần, tháng — `/kn/cnts/ksgm`; Thủy sản (Phụ lục 2) —
  `/kn/cnts/phu-luc-2`; hộp Báo cáo đến của phòng nhận — `/kn/bao-cao-den`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Xem KSGM tuần, tháng; mở Phụ lục 2, nhập nuôi trồng | Mở KSGM tháng; nhập nuôi trồng ở Phụ lục 2 | Tổng = khai thác BM-17 + nuôi trồng | Đạt | |
| B3–B5 | Tạo báo cáo từ dữ liệu; bổ sung phần viết; trình duyệt; lãnh đạo duyệt, gửi | T-TH lập báo cáo tuần, trình; T-LD duyệt | Trạng thái Đã gửi | Đạt | |
| Luồng phụ | Xuất Excel, Word đúng mẫu | Xuất Word báo cáo đã gửi | Có số `{n}/BC-CNTS` | Đạt | |
| Ngoại lệ | Tỷ lệ nộp chưa đạt và kỳ chưa chốt → không cho tạo báo cáo | Lập khi mới 1/3 xã nộp | Bị chặn | Đạt | |
| Ngoại lệ | Phòng nhận chưa có tài khoản → gửi email công vụ, trạng thái "Đã gửi email" | Duyệt khi phòng nhận chưa có tài khoản | Có email, trạng thái Đã gửi email | Thiếu: chưa thấy mã gửi email cho BM-11 (chưa có issue) | |
| BR-01 | Phần tự động không sửa tay; điều chỉnh BM-02 là dòng riêng có lý do | Thêm dòng điều chỉnh KSGM | Dòng riêng, bắt lý do | Đạt | |
| BR-02 | Báo cáo đã gửi không xóa; số nộp trễ đưa vào báo cáo kỳ sau | Xã nộp bù sau khi gửi; lập kỳ sau | Có mục "Số liệu kỳ trước nộp bổ sung" | Đạt | |
| BR-05 | Ban Giám đốc không duyệt, trả lại | T-BGD mở báo cáo chờ duyệt | Không có nút Duyệt/Trả lại | Đạt | |
| BR-07 / AC-05 | Bảng theo loại chăn nuôi; xã A 12 hộ (3 trang trại nhỏ, 1 vừa) → 12 hộ, 4 trang trại; nông hộ 8, nhỏ 3, vừa 1, công nghiệp 0 | Lập báo cáo với dữ liệu bước 10 mục 2 | Đúng các số | Đạt | |
| AC-01 | Tổng mỗi chỉ tiêu bằng tổng số liệu hợp lệ của kỳ | So tổng với bảng Số liệu cấp xã | Bằng nhau | Đạt | |
| AC-02 | Lãnh đạo phòng duyệt báo cáo tuần → trong 1 phút có trong "Báo cáo đến" của Ban Giám đốc và Phòng HCTH | T-LD duyệt; HC mở `/kn/bao-cao-den` | Có báo cáo, trạng thái Đã gửi | Đạt cho Phòng HCTH (cơ chế; đo 1 phút khi UAT). Ban Giám đốc xem qua tài khoản T-BGD | |
| AC-03 | 99/102 xã đã nộp → "Tổng hợp từ 99/102 xã" và liệt kê 3 xã thiếu | Lập khi thiếu xã | Có dòng n/N và danh sách xã thiếu | Đạt | |
| AC-04 | Trả lại mà để trống ý kiến thì không lưu được | T-LD Trả lại, bỏ trống | Bị chặn | Đạt | |

Kiểm thử tự động: `TestCNTSBM11_W1145`, `TestBM11HerdsOf_AC05`, `TestBM11WordTemplates`, `TestCNTSAggregate_BM18_PL2_CloseShare_W1144`,
`TestCNTSAggregate_BM02_Adjustment_W1144`; vitest `bm11/bm11.test.tsx`, `aggregate/aggregate.test.ts`.

### FN-17 — Trang chủ & dashboard phòng

- Vai: Cấp Tỉnh (cán bộ phòng, Lãnh đạo phòng, Ban Giám đốc). Điều kiện đầu: có tài khoản, chương trình, lịch.
- Màn: Điều hành › Trang chủ — `/kn/trang-chu`; Admin: `/kn/phong-ban/chan_nuoi_thuy_san/trang-chu`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1, B3 | Mở trang chủ; bấm thẻ để mở màn chi tiết | Bấm thẻ Cảnh báo, thẻ Hộ chăn nuôi | Mở đúng màn | Đạt | |
| B2 | Lọc kỳ, khu vực, lĩnh vực | Đổi bộ lọc | Số liệu đổi theo | Thiếu: chưa có bộ lọc kỳ/khu vực/lĩnh vực và chỉ số tổng đàn, tiêm phòng, KSGM tuần (chưa có issue) | |
| Luồng phụ | Lãnh đạo phòng sửa giới thiệu, chức năng nhiệm vụ | T-LD sửa; T-VC thử | T-LD lưu được; T-VC không có nút | Đạt | |
| BR-01 | Số liệu chỉ từ biểu đã nộp | So thẻ với số đã nộp | Khớp | Đạt | |
| BR-02 / AC-02 | Ban Giám đốc thấy menu Lãnh đạo phòng trừ "Duyệt báo cáo"; gọi trực tiếp chức năng duyệt bị từ chối | T-BGD xem menu; gọi API duyệt | Không có mục duyệt; 403 | Đạt | |
| Mô tả | Lãnh đạo phòng thấy thêm thẻ "Báo cáo chờ duyệt" | So T-LD với T-VC | Chỉ T-LD có thẻ | Đạt | |
| AC-01 | 500 người dùng đồng thời, trang chủ mở dưới 5 giây trên 3G | Chạy kiểm tải trên môi trường triển khai | < 5 giây | Thiếu: chưa đo tải (hạ tầng; chưa có issue) | |

Kiểm thử tự động: `TestFN17_DeptHome`, `TestFN17_LivestockCounts_253`; vitest `depthome/depthome.test.ts`.

### FN-18 — Bản đồ vùng nuôi, vùng dịch

- Vai: Cấp Tỉnh; Cấp Xã xem phạm vi xã mình. Điều kiện đầu: có tọa độ hộ, cơ sở, ổ dịch.
- Màn: Dữ liệu › Bản đồ vùng dịch — `/kn/cnts/ban-do-dich`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B2 | Bật, tắt lớp; lọc; bấm đối tượng xem chi tiết | Bật lớp trang trại, bấm 1 trang trại | Hiện loại, vật nuôi; không có họ tên, SĐT | Đạt | |
| B3–B4 | Vẽ vùng dịch, vùng đệm, vùng bị uy hiếp, hướng; lưu | Khoanh vùng từ ổ dịch (500 m / 3 km / 10 km) | 3 vùng gắn ổ dịch | Đạt | |
| Luồng phụ | Tải KML/GeoJSON; xuất ảnh | Tải GeoJSON; tải KML; xuất ảnh | Có tệp | Đạt cho GeoJSON. Thiếu: KML, xuất ảnh (chưa có issue) | |
| Ngoại lệ | Vẽ ngoài ranh giới tỉnh → cảnh báo | Vẽ đa giác ngoài tỉnh | Có cảnh báo | Thiếu: cần ranh giới xã trong kho địa lý, chưa có kiểm thử với ranh giới thật (chưa có issue) | |
| BR-01 / AC-03 | Vùng dịch lưu theo ngày; chọn ngày cũ thấy vùng đã lưu của ngày đó | Kéo thanh ngày về hôm qua | Thấy phiên bản hôm qua | Đạt | |
| BR-02 | Vùng dịch hiện số hộ bị ảnh hưởng, diện tích thủy sản bị ảnh hưởng | Bấm vùng dịch | Có DB-15, DB-16 của lần cập nhật mới nhất | Đạt | |
| BR-03 | Ranh giới xã theo ĐVHC sau sắp xếp 2025 | Bật lớp ranh giới xã | Có ranh giới | Thiếu: chưa có lớp ranh giới xã (chưa có issue) | |
| AC-01 | Điểm dịch mới hiện trong 1 phút sau xác nhận | Gửi BM-10; mở bản đồ | Có điểm | Đạt | |
| AC-02 | Cán bộ xã chỉ thấy bản đồ xã mình | X-A mở bản đồ | Chỉ đối tượng xã A, B; không vẽ được | Đạt | |
| — | Lớp thửa HTX (dữ liệu hệ ngoài) | — | Không còn trong app KN | Đang gỡ ở #266 | |

Kiểm thử tự động: `TestCNTSMap_W1152`, `TestCNTSMap_FarmLayerAndImpact_FN18`, `TestImpactOf`, `TestZoneNormalize`; vitest `map/map.test.ts`.

### FN-19 — Chương trình, dự án KNQG & mẫu biểu dự án

- Vai: Cấp Tỉnh (chủ nhiệm T-CN, Lãnh đạo phòng), Cấp TW; Cấp Xã xem điểm của xã. Điều kiện đầu: có tài khoản chủ nhiệm.
- Màn: Dữ liệu › Dự án KNQG — `/kn/cnts/du-an` (dự án, điểm trình diễn), `/kn/cnts/du-an-mau-bieu` (Mẫu 01, 03, 04, 05, 07);
  Cấp Xã: Điểm trình diễn — `/kn/cnts/ho-mo-hinh`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1 | Tạo dự án, điểm, hộ | T-CN tạo dự án, 1 điểm ở xã A, 2 hộ | Có mã dự án, điểm, hộ | Đạt | |
| B2–B3 | Lập mẫu từ chi tiết dự án, số liệu điền sẵn; Mẫu 01: chủ nhiệm trình, Lãnh đạo phòng duyệt hoặc trả lại có ý kiến | Lập Mẫu 01 tháng này, trình; T-LD trả lại rồi duyệt | Trả lại bắt ý kiến; duyệt xong khóa sửa | Đạt | |
| B4 | Xuất Word, ký, tải bản đã ký | Xuất Mẫu 05, tải bản scan đã ký | Trạng thái Đã ký | Đạt | |
| B5 | Mẫu 07 gửi bên A; bên A xác nhận hoặc trả lại | Gửi Mẫu 07; TW xác nhận | Mẫu 07 khóa | Đạt | |
| Luồng phụ | Nạp Excel điểm, hộ | Nạp tệp mẫu | Xem trước, lỗi theo dòng, ghi một lần | Đạt | |
| BR-01 / AC-04 | Chủ nhiệm sửa dự án mình phụ trách; chủ nhiệm dự án khác không sửa được | Chủ nhiệm khác mở dự án | Chỉ xem | Đạt | |
| BR-02 | Mẫu 07 đã xác nhận khóa sửa | Sửa Mẫu 07 đã xác nhận | Bị chặn | Đạt | |
| BR-03 | Mẫu 01 đã duyệt không sửa; điều chỉnh tạo bản mới và trình duyệt lại | Điều chỉnh Mẫu 01 đã duyệt | Tạo bản mới ở trạng thái chờ trình | Đạt | |
| AC-01 | File xuất có đủ mục, bảng của mẫu Word gốc | So 5 tệp Word với mẫu gốc | Đủ mục, bảng | Đạt cho Mẫu 03, 04, 05, 07. Thiếu: đối chiếu mẫu gốc Mẫu 01 và in ảnh thực địa vào Mẫu 03 — #221 (chờ PO) | |
| AC-02 | Bên A thấy Mẫu 07 trong 1 phút sau khi gửi | TW mở `/kn/cnts/du-an-mau-bieu` | Có Mẫu 07 | Đạt (cơ chế); đo khi UAT | |
| AC-03 | Thiếu Mẫu 05 đã ký của một điểm thì không gửi được Mẫu 07 | Gửi Mẫu 07 khi 1 điểm chưa ký Mẫu 05 | Bị chặn | Đạt | |
| — | Điểm trình diễn kiểm mã HTX qua công bố HTX (hệ ngoài) | — | Không còn phụ thuộc HTX | Đang gỡ ở #266 | |

Kiểm thử tự động: `TestCNTSModel_W1147`, `TestCNTSKNQG_W1153`, `TestCNTSKNQGNotifyPhotos_W1153b`, `TestWordTemplatesRender`, `TestCNTSModelImport_W1153c`;
vitest `knqg/`, `model/`; Playwright `kn-cnts-knqg.spec.ts`.

### FN-20 — Quy trình kỹ thuật, văn bản

- Vai: Cấp Tỉnh (Xã, Nông hộ xem).
- Màn: Dữ liệu › Quy trình, văn bản — `/kn/van-ban`; Admin: `/kn/phong-ban/chan_nuoi_thuy_san/van-ban`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1–B3 | Lọc, tìm; xem, tải; thêm văn bản | T-VC thêm 1 văn bản PDF; X-A tìm và tải | Tải được | Đạt | |
| BR-01 / AC-03 | Hết hiệu lực vẫn lưu, vẫn tìm được, có nhãn "Hết hiệu lực" | Đặt ngày hết hiệu lực hôm qua; tìm | Có nhãn | Đạt | |
| AC-01 | Văn bản mới hiện trong 1 phút cho người được xem | X-A tải lại danh sách | Có văn bản | Đạt | |
| AC-02 | Tệp trên 20 MB bị chặn | Tải tệp 21 MB | Bị chặn | Đạt | |
| Menu | Nông hộ: Mobile › Quy trình | Nông hộ mở app | Thấy quy trình của mô hình | Thiếu: chưa có app Nông hộ (chưa có issue) | |

Kiểm thử tự động: `TestFN20_DeptLibrary`, `TestFileType`, `TestValidity`.

### FN-21 — Chiến dịch điều tra mở rộng

- Vai: Cấp Tỉnh (tạo, theo dõi, đóng); Cấp Xã (nhập); Lãnh đạo phòng (mở lại); Admin. Điều kiện đầu: biểu điều tra đã thiết kế (FN-07);
  cán bộ xã đã được cấp quyền (FN-14).
- Màn: Chiến dịch điều tra — `/kn/cnts/chien-dich`, `/kn/cnts/chien-dich/tao`; Cấp Xã nhập trong Việc cần làm `/kn/cnts/bao-cao-ky`.

| Mã | Yêu cầu BRD (trích) | Bước kiểm | Kết quả mong đợi | Trạng thái trên dev | Kết quả UAT |
|---|---|---|---|---|---|
| B1 | "+ Tạo đợt điều tra": tên, mục tiêu, lĩnh vực, biểu và phiên bản, phạm vi (toàn bộ xã, Khu vực, danh sách xã, hộ), thời gian, hạn, người phụ trách | Tạo đợt "Điều tra tổng đàn", phạm vi Khu vực 1, người phụ trách T-TH | Đợt ở trạng thái Nháp | Đạt | |
| B2 | Xem trước số việc → Phát hành; đợt chuyển Đang thu thập | Phát hành | Số việc = số xã; trạng thái Đang thu thập | Đạt | |
| B3 | Cán bộ xã nhập, gửi trong Việc cần làm | X-A nhập biểu đợt | Gửi được | Đạt. Màn nhập hiện mã hộ HCN thay mã nội bộ: #277 | |
| B4 | Theo dõi tiến độ theo Khu vực, xã; nhắc xã chưa nộp | Mở tiến độ, bấm Nhắc | Đúng số đã nộp; xã nhận nhắc | Đạt | |
| B5 | "Đóng đợt"; xem tổng hợp, xuất Excel | Đóng đợt, xuất Excel | Có tệp kết quả | Đạt | |
| Luồng phụ | Gia hạn có lý do; sao chép đợt cũ; mở lại đợt đã đóng (Lãnh đạo phòng) | Gia hạn; sao chép; T-LD mở lại | Đợt mới ở Nháp, không mang số liệu | Đạt | |
| Ngoại lệ | Đợt chưa có biểu hoặc chưa chọn xã → không phát hành được | Phát hành đợt thiếu xã | Bị chặn | Đạt | |
| BR-01 | Nháp → Đang thu thập → Đã đóng; mở lại chỉ Lãnh đạo phòng, có lý do và thời hạn | T-TH thử mở lại | Bị từ chối | Đạt | |
| BR-02 / AC-04 | Số liệu điều tra tách khỏi lũy kế báo cáo định kỳ; không đổi lũy kế BM-04 | So lũy kế BM-04 trước và sau đợt | Không đổi | Đạt | |
| BR-04 / AC-03 | Sau khi đóng đợt, xã không gửi được và xã chưa gửi chuyển Không nộp | X-C gửi sau khi đóng | Bị từ chối; trạng thái Không nộp | Đạt | |
| AC-01 | Phát hành cho 102 xã thì cán bộ của 102 xã thấy việc trong 1 phút | Phát hành toàn tỉnh | Việc hiện trong ≤ 1 phút | Đạt (cơ chế); đo khi UAT | |
| AC-02 | Tiến độ hiển thị đúng số xã đã nộp theo Khu vực | Như B4 | Đúng | Đạt | |

Kiểm thử tự động: `TestCNTSCampaign_LifecycleSeparateFromCumulative_W1150`, `TestCNTSCampaignOwnerAndCopy_236`, `TestCNTSCampaignHouseholdScope_236`,
`TestBFCNTSCampaignCAS_*`; vitest `campaign/`. Giả định: [CNTS-CAMPAIGN-SCOPE](../kn/gia-dinh/CNTS-CAMPAIGN-SCOPE.md).

## 5. Tổng hợp

| FN | Tên | Trạng thái trên dev |
|---|---|---|
| FN-01 | Đơn vị hành chính | Thiếu: ánh xạ xã cũ → mới; khẳng định đủ 102 xã |
| FN-02 | Cơ cấu tổ chức 3 cấp | Thiếu: ngày hiệu lực Khu vực trên màn |
| FN-03 | Danh mục phòng ban | Đạt |
| FN-04 | Tài khoản người dùng | Thiếu: vai Nông hộ, SMS (#173) |
| FN-05 | Đăng nhập & chọn phòng ban | Thiếu: OTP Nông hộ; trang mặc định Cấp Quốc gia (#265) |
| FN-06 | Danh mục CNTS | Thiếu: kiểm thử AC-02 (mộc dấu theo hiệu lực trong KSGM) |
| FN-07 | Thiết kế & phát hành biểu mẫu | Thiếu: mẫu nhật ký động (#275) |
| FN-08 | Công việc cần làm | Thiếu: việc nhật ký, mũi tiêm, lịch đến hạn |
| FN-09 | Nhập báo cáo theo kỳ | Thiếu: nhập BM-04 từ Excel |
| FN-10 | Hộ mô hình & nhật ký | Thiếu: Nông hộ tự ghi, nhật ký ngoại tuyến trên app, HCN-09 (#276) |
| FN-11 | Báo cáo dịch bệnh | Thiếu: Nông hộ báo nghi dịch trên Mobile; nháp ngoại tuyến |
| FN-12 | Lịch công tác, công việc tuần | Đạt |
| FN-13 | Theo dõi, rà soát, cảnh báo | Thiếu: cảnh báo nhật ký trễ, lịch tiêm trễ |
| FN-14 | Cấp quyền | Đạt theo BRD; mã biểu động chờ #304 |
| FN-15 | Lịch trực, lịch tiêm | Đạt |
| FN-16 | Báo cáo tổng hợp theo kỳ | Thiếu: email công vụ cho phòng nhận chưa có tài khoản |
| FN-17 | Trang chủ phòng | Thiếu: bộ lọc, chỉ số; đo tải AC-01 |
| FN-18 | Bản đồ vùng nuôi, vùng dịch | Thiếu: KML, xuất ảnh, ranh giới xã |
| FN-19 | Dự án KNQG | Thiếu: Mẫu 01 gốc, ảnh Mẫu 03 (#221) |
| FN-20 | Quy trình, văn bản | Thiếu: Nông hộ xem trên Mobile |
| FN-21 | Chiến dịch điều tra | Đạt |

Đạt trọn: 5/21 (FN-03, FN-12, FN-14, FN-15, FN-21). Còn thiếu ít nhất một ý: 16/21; lý do chính là vai Nông hộ trên Mobile chưa có
(FN-04, 05, 10, 11, 20), các ý nhỏ chưa có issue (mục 3) và hai gói đang làm (#275, #276).
