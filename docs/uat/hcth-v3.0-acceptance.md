# Nghiệm thu HCTH v3.0 — W11.28

Phạm vi: báo cáo Sở NS01–NS06 (HC-FN-12) và dashboard nhân sự (HC-FN-06).
Đây là ma trận cho W11.28, không xác nhận các chức năng HCTH ngoài gói.
Nguồn giả định: [W11.28](../kn/gia-dinh/W11.28.md).

| Tiêu chí | Bằng chứng tự động | Trạng thái |
|---|---|---|
| Danh sách sáu biểu; chỉ HCTH/Admin có quyền toàn Trung tâm; biểu có trường nhạy cảm cần quyền bổ sung | `TestHRMReportsHTTP_W1128`; Vitest `src/kn/hrm/reports.test.tsx` | Đạt |
| NS01 theo ngày chốt, NS02 phản ánh nghỉ việc trong tháng; thứ tự chức vụ/đơn vị và nhãn kỳ | `TestHRMReportsHTTP_W1128`, `TestBuildNS01OrderAndChecks`, `TestPeriodLabel` | Đạt |
| NS05/NS06 tổng hợp và nhấn ô xem người theo phạm vi; thiếu ngày sinh chặn, dữ liệu chưa phân loại cảnh báo | `TestBuildNS05NS06Rules`, `TestHRMReportsHTTP_W1128` | Đạt |
| Xuất Excel NS01: đọc lại nội dung tệp, người lập/ký, kỳ báo cáo; tải lại đối chiếu SHA-256 và báo drift | `TestHRMReportsHTTP_W1128` | Đạt |
| Lịch sử xuất/tải và nhật ký trường nhạy cảm; đọc PII theo lô qua vault | `TestHRMReportsHTTP_W1128`, `TestReadBatch_W1128`, `TestBatchReadHTTP_W1128` | Đạt |
| Dashboard khớp NS01 cùng ngày, lãnh đạo phòng chỉ thấy phòng mình; khi thiếu quyền vault vẫn có số tổng hợp | `TestHRMReportsHTTP_W1128`; Vitest `src/kn/hrm/reports.test.tsx` | Đạt |
| Dashboard dưới 5 giây với 1.000 hồ sơ | `TestHRMDashboardBenchmark_W1128` (PostgreSQL thật, vault giả trong bộ nhớ) | Đạt |
| Menu mở dashboard/báo cáo HCTH và giữ các mục CNTS đã tích hợp | `TestWebMenuFixture`, `TestW1106_MenuV3DepartmentTemplates`; Vitest `src/api/mock/kn-menu.contract.test.ts` | Đạt |

Giới hạn nghiệm thu: bố cục mẫu Sở chính thức vẫn chờ khách xác nhận theo giả định
W11.28; không suy diễn việc đọc lại NS01 thành đối chiếu tệp vàng cho cả sáu biểu.
Hiệu năng cục bộ với vault giả chưa thay thế đo trên môi trường có vault thật.
Xuất Word và lưu bản gốc trong kho tệp mã hóa thuộc W10.17, ngoài phạm vi gói này.

Kiểm tra tiếp nhận Codex ngày 15/10/2026: các ca trong bảng đã PASS;
`TestHRMReportsHTTP_W1128` chạy trên PostgreSQL thật, `TestReadBatch_W1128` PASS;
507 test web / 56 tệp, TypeScript và build mock đạt.
Dashboard 1.000 hồ sơ đo 82,76 ms, NS06 đo 91,97 ms (vault giả trong bộ nhớ).
Golden menu sinh lại bằng `TestWebMenuFixture` giữ nguyên nội dung sau rebase.
API batch-read đã kiểm qua HTTP client/router thật: tenant khác bị loại, vai chỉ hiển thị bị từ chối, mã tham chiếu sai bị chặn; phản hồi no-store và log không chứa PII.

## W11.24 — Kho tài liệu nhân sự

Nguồn: [giả định W11.24](../kn/gia-dinh/W11.24.md), BRD FN-14/UR-HC-25/UR-ADM-10.
Kết quả chạy từng lệnh và mã thoát: [sổ hồi quy](../REGRESSION.md).

| Tiêu chí | Bằng chứng | Phạm vi kiểm tra |
|---|---|---|
| Bảy thư mục mặc định không nhân đôi; gốc chỉ Admin; thư mục con, tên trùng và ẩn khi rỗng | `TestHRMDocsHTTP_W1124` | PostgreSQL thật, HTTP |
| Phân loại theo nhân viên, năm, loại, đơn vị; lọc đệ quy, tìm số quyết định | `TestHRMDocsHTTP_W1124` | FN-14 AC-01 |
| Lãnh đạo chỉ xem phòng mình; tenant khác không thấy; thư mục kế thừa quyền và ưu tiên người dùng | `TestHRMDocsHTTP_W1124`, `TestDocsDomain_W1124` | FN-14 AC-02 |
| Tệp mã hóa, bản tải đúng nội dung, no-store, vai kho bắt buộc; nhật ký tải/xem | `TestHRMDocsHTTP_W1124` | FN-14 AC-03; evidence/vault là cổng giả ở ca HTTP HRM |
| Evidence thật cho phép `hr_records` không cần đồng ý; MRV giữ yêu cầu đồng ý | `TestOpenHRRecordsWithoutConsent_W1124` | DB thật, kho định danh giả |
| HRM-DOC-FILE-001: biết mã tệp và thấy thư mục vẫn không được mở tài liệu ngoài phòng/tenant; evidence bắt buộc gọi bộ kiểm từng tệp của chủ | `TestHRMDocsHTTP_W1124`, `TestOpenHRRecordsWithoutConsent_W1124` | DB thật |
| HRM-DOC-PURPOSE-001: không thể dùng hr_records mở tệp hiện trường, tệp cũ hoặc sai loại; mọi từ chối có sổ | `TestOpenHRRecordsWithoutConsent_W1124` | DB thật |
| HRM-DOC-CRYPTO-001: mở khóa/giải mã đúng qua client/router/vault thật; chặn sai vai, sai tenant, sai mục đích; log không chứa bản rõ/token | `TestHRRecordsUnwrapHTTP_W1124` | PostgreSQL thật, HTTP, KMS cục bộ dành cho test |
| Ẩn/hiện lại có lý do, lịch sử phân loại; sổ không UPDATE/DELETE | `TestHRMDocsHTTP_W1124` | G1 |
| Cây thư mục, danh sách, chi tiết, ẩn/hiện lại và sửa quyền trên web | `src/kn/hrm/docs.test.tsx`, `docs-api.test.ts` | Giả lập API |

Tự lưu quyết định nâng bậc và bản ký đánh giá thuộc W11.25/W11.26 sau G1.
Kiểm thử KMS cục bộ không thay thế diễn tập kho khóa và kho tệp trên môi trường triển khai.

## W11.30 — Chuyển đổi dữ liệu nhân sự ban đầu (D3 H10, A2)

Nguồn giả định: [W11.30](../kn/gia-dinh/W11.30.md); runbook [hrm-import.md](../runbooks/hrm-import.md).

| Tiêu chí | Bằng chứng | Phạm vi kiểm tra |
|---|---|---|
| Chạy thử đọc Excel qua `shared/tabular`, chuẩn hóa, báo cáo dòng lỗi khớp tệp vàng; không ghi sổ, không ghi kho định danh; báo cáo không chứa dữ liệu cá nhân | `TestHRMImport_W1130` (`testdata/nhan-su-tong-hop.xlsx`, `bao-cao-chay-thu.golden.csv`) | PostgreSQL thật, HTTP `/v1/hrm`, kho định danh giả |
| Ghi bị chặn khi còn dòng lỗi; `-skip-errors` ghi dòng hợp lệ qua `POST /v1/hrm/staff`, mã tự sinh theo thứ tự tệp, quá trình ban đầu (đơn vị, chức vụ, ngạch – bậc, văn bằng, vào làm/nghỉ việc) đúng như tệp | `TestHRMImport_W1130` | PostgreSQL thật |
| PII (họ tên, SĐT, ngày sinh, email) ở kho định danh, thẻ TK ở ngăn `hrm.sensitive` | `TestHRMImport_W1130`; sổ `pf_hrm` không PII: `TestHRMRecordsHTTP_W1122` | Kho định danh giả |
| Chạy lại không ghi đôi (báo "Đã có", số hồ sơ và dòng quá trình không đổi) | `TestHRMImport_W1130` | PostgreSQL thật |
| Ngày nhiều định dạng, bậc bị đổi thành ngày, hệ số dạng chữ, ghi chú trong họ tên, SĐT mất số 0; ánh xạ cột; tệp mẫu gửi khách | `TestNormalize_W1130`, `TestTemplate_W1130` | Đơn vị |

Giới hạn nghiệm thu: chưa có tệp nguồn thật (Q-069) — ánh xạ cột và bảng hệ số phải chỉnh theo tệp HCTH gửi; HCTH xác
nhận báo cáo cuối (A2) là bước vận hành, không tự động.
## W11.25 — Nâng bậc lương (FN-09) và sổ quyết định (FN-10)

Nguồn: [giả định W11.25](../kn/gia-dinh/W11.25.md) (Q-069, Q-070, Q-075 dùng hành vi prototype làm giả định tạm), BRD FN-09/FN-10, UR-HC-12…16.

| Tiêu chí | Bằng chứng | Phạm vi kiểm tra |
|---|---|---|
| FN-09 AC-01: năm KHTNV trong thời gian giữ bậc → "Chưa đủ" kèm lý do; thiếu xếp loại → "Cần xem xét"; đưa người chưa đủ vào danh sách bắt buộc lý do | `TestHRMPromotionHTTP_W1125`, `TestComputeNLTX_W1125`; Vitest `src/kn/hrm/promotion.test.tsx` | PostgreSQL thật, HTTP |
| FN-09 AC-02: Mẫu số 1 đúng bậc, hệ số mới, hệ số chênh lệch, đủ 20 cột, cột 19 trống (đọc lại tệp .xlsx) | `TestHRMPromotionHTTP_W1125`, `TestBuildForm_W1125` | Dữ liệu mẫu chuyên viên bậc 5 → 6 (3,66 → 3,99; 0,33) |
| FN-09 AC-03: không xuất/lập được NLTH khi vượt chỉ tiêu; Mẫu số 2 chặn thành tích đã dùng, ngày nâng ngoài năm chỉ tiêu | `TestHRMPromotionHTTP_W1125`, `TestComputeTNVKAndNLTH_W1125` | Chỉ tiêu = ⌊20 % × 5⌋ = 1 |
| FN-09 AC-04: file đã xuất xem lại được, có ngày và người xuất; tải lại dựng lại đúng SHA-256 | `TestHRMPromotionHTTP_W1125` | PostgreSQL thật |
| FN-09 AC-05: thẻ/khung "1 tháng", "3 tháng" mở đúng người có ngày đến kỳ trong khung (kể cả quá hạn chưa xử lý) | `TestHRMPromotionHTTP_W1125` | Ngày xét 12/10/2026 |
| FN-09 AC-06: trạng thái xử lý từng người được lưu và hiển thị (Chưa xử lý → Đã đưa vào danh sách → Đã xuất danh sách → Đã có quyết định; hủy → Đã xuất) | `TestHRMPromotionHTTP_W1125`; Vitest | Sổ `promotion_event` |
| Tự chia Mẫu số 1/2/4 theo loại; TNVK lần đầu % khởi điểm, lần sau đọc % từ sổ quyết định | `TestHRMPromotionHTTP_W1125`, `TestComputeTNVKAndNLTH_W1125` | — |
| FN-10 AC-01: lưu quyết định cập nhật bậc, hệ số trong hồ sơ cùng giao dịch; người không có tên trở lại bảng theo dõi kèm ghi chú (BR-02) | `TestHRMPromotionHTTP_W1125`; Vitest | PostgreSQL thật |
| FN-10 AC-02: tìm theo số, năm, tên viên chức; AC-03: xuất sổ ra Excel | `TestHRMPromotionHTTP_W1125` | — |
| FN-10 BR-03: hủy có lý do, là sự kiện bù (khôi phục bậc, hệ số), không xóa; hủy lại → 409; sổ chỉ ghi thêm | `TestHRMPromotionHTTP_W1125` | UPDATE/DELETE bị chặn |
| Tệp quyết định vào Kho tài liệu (thư mục "Quyết định nâng bậc", gắn người có tên) | `TestHRMPromotionHTTP_W1125` | evidence giả của ca HTTP HRM |
| Nhập quyết định đã ban hành: số trùng cảnh báo, bậc vượt ngạch chặn | `TestHRMPromotionHTTP_W1125` | — |
| Job tính mốc hằng ngày: một dòng/ngày, chạy lại không ghi thêm; màn hiển thị lần tính gần nhất | `TestHRMPromotionHTTP_W1125` | Thông báo pf/notify chưa nối (giả định W11.25) |
| Không PII trong sổ nâng bậc; quyền chỉ HCTH/Admin; tenant khác không thấy | `TestHRMPromotionHTTP_W1125` | — |
| Màn Danh sách nâng bậc, Sổ quyết định theo bố cục prototype HCTH; menu HCTH | Vitest `promotion.test.tsx`, `kn-menu.contract.test.ts`; `TestWebMenuFixture` | Giả lập API |

Giới hạn nghiệm thu: quy chế nâng bậc, chỉ tiêu, bố cục Mẫu số 1/2/4 chính thức chờ Phòng HCTH (Q-070); xuất Word thuộc W10.17; thông báo đẩy cho HCTH chờ loại thông báo ở pf/notify.

## W11.29 — web-ops HCTH theo prototype (D3 H9)

Nguồn: [giả định + bảng kiểm kê màn W11.29](../kn/gia-dinh/W11.29.md), prototype `Khuyen_nong_so_HCTH.html`, BRD FN-06/07/08/13.
Chỉ web (giả lập API theo hợp đồng có sẵn), không đổi backend.

| Tiêu chí | Bằng chứng | Phạm vi kiểm tra |
|---|---|---|
| Kiểm kê mọi màn prototype HCTH → trang web-ops → API → trạng thái (Xong/Một phần/Chặn API W11.26/W11.27) | `docs/kn/gia-dinh/W11.29.md` | Tài liệu |
| FN-08 Thông tin làm việc: 3 thẻ bấm lọc nhanh, lọc đơn vị/trạng thái/tháng/biến động; cập nhật ngày nghỉ việc + lý do → "Đã nghỉ việc", thẻ "Đang làm việc" giảm | Vitest `src/kn/hrm/work-info.test.tsx`; Playwright `tests/e2e/hc-nhan-su.spec.ts` (hc-hs-tl) | Giả lập API, trình duyệt thật |
| Sửa/bỏ ngày đã ghi là `replace`/`void` có lý do 10–500 ký tự (sổ chỉ ghi thêm); ngày nghỉ trước ngày vào làm bị chặn | Vitest `work-info.test.tsx` (planWork, validateWork) | Logic màn |
| Chi tiết: "Ảnh hưởng đến báo cáo" NS02 tháng nghỉ, NS01 không còn từ tháng sau; mở hồ sơ nhân viên | Vitest `work-info.test.tsx`; Playwright hc-hs-tl | — |
| Hồ sơ nhân sự: lọc Chức vụ, Ngạch/CDNN; cột Trình độ cao nhất, SĐT che | Playwright hc-hs-tl; Vitest `staff.test.tsx` (hồi quy) | — |
| Dashboard: thẻ chỉ số mở danh sách người; Cảnh báo nâng bậc 3 tháng (HCTH/Admin), biến động tháng | Vitest `work-info.test.tsx` (hrmDashboardAlerts); Playwright `hc-nhan-su.spec.ts` (hc-dash) | — |
| Contact cho mọi vai: Cấp Xã chỉ Họ tên/Năm sinh/SĐT, Cấp Tỉnh đủ cột (cùng TW/Admin ở `kn-contact.spec.ts`) | Playwright `tests/e2e/hc-contact.spec.ts` | Giả lập `kn-contact.ts` |

Giới hạn nghiệm thu: nhóm màn Đánh giá (W11.26) và Kế hoạch nhân lực (W11.27) chưa có API; xuất Excel danh sách, xuất
Contact, lọc giới tính/tuổi/trình độ/chuyên ngành, chỉ số Đảng viên/DTTS chờ API (liệt kê ở giả định W11.29).

## W11.27 — Kế hoạch nhân lực năm (FN-11)

Nguồn giả định: [W11.27](../kn/gia-dinh/W11.27.md). Kỳ kế hoạch là kỳ native `cap/forms` profile `hrm_plan` (đơn vị lá = đơn vị nội bộ).

| Tiêu chí | Bằng chứng | Trạng thái |
|---|---|---|
| B1: Phòng HCTH mở kỳ năm (hạn gửi, ngày chốt số liệu, đơn vị); một kỳ mỗi năm; lãnh đạo phòng không mở kỳ; đơn vị ngừng dùng bị chặn | `TestHRMPlansHTTP_W1127` (cap/forms + pf/hrm thật, PostgreSQL) | Đạt |
| AC-01 / BR-01: kế hoạch phòng hiển thị đúng số hiện có và dự kiến nghỉ hưu tự tính tại ngày chốt (cán bộ xã không tính), danh sách người dự kiến nghỉ hưu | `TestHRMPlansHTTP_W1127`, `TestRetiringIn_W1127` | Đạt |
| B2: lãnh đạo đơn vị lưu nháp ("Đang lập"), gửi; đơn vị khác/HCTH không phải lãnh đạo không gửi thay; lãnh đạo Phòng HCTH lập kế hoạch của HCTH; khóa lạc quan khi gửi lại | `TestHRMPlansHTTP_W1127` | Đạt |
| B3: HCTH yêu cầu điều chỉnh (ghi chú) → gửi lại → xác nhận; người gửi không tự rà soát (bốn mắt) | `TestHRMPlansHTTP_W1127` | Đạt |
| AC-02: bảng tổng hợp cộng đúng các phòng (dòng Toàn Trung tâm); lãnh đạo phòng chỉ thấy đơn vị mình | `TestHRMPlansHTTP_W1127`; Vitest `src/kn/hrm/plans.test.tsx` | Đạt |
| Chốt: đơn vị chưa gửi → "Không gửi"; sau chốt không gửi/nháp được; BR-02 chỉ HCTH điều chỉnh có lý do, lưu lịch sử, ảnh chụp chốt giữ nguyên | `TestHRMPlansHTTP_W1127` | Đạt |
| Luồng phụ: xuất Excel (HCTH), sao chép từ năm trước (sau điều chỉnh) | `TestHRMPlansHTTP_W1127` | Đạt |
| Validator profile hrm_plan chặn bài nộp sai hình dạng gửi thẳng qua cap/forms; cách ly tenant | `TestHRMPlansHTTP_W1127`, `TestPlanValuesRoundTrip_W1127` | Đạt |
| Phiên chỉ xem (lãnh đạo, Ban Giám đốc, Admin) bị từ chối khi gọi thẳng `/v1/forms` với profile `hrm_plan` (định nghĩa biểu, phát hành, đọc kỳ, nộp, rà soát, chốt, hủy) và ở `/v1/hrm/plans`; phiên thường vẫn làm được | `TestHRMPlanFormsDirectViewOnly_W1127` (PostgreSQL thật, HTTP) | Đạt |
| Màn web: bảng tổng hợp, mở kỳ, chốt, chi tiết đơn vị theo bố cục prototype HCTH; menu HCTH và "Kế hoạch của phòng" | Vitest `src/kn/hrm/plans.test.tsx`, `src/api/mock/kn-menu.contract.test.ts`; `TestWebMenuFixture` | Đạt |

Giới hạn: "Nhắc phòng chưa lập" (pf/notify) chưa làm; trạm/trại không có lãnh đạo có phiên chỉ được ghi qua điều chỉnh
sau chốt; tuổi nghỉ hưu dùng tham số mẫu Q-070 tại ngày chốt (chưa theo lộ trình từng năm).

## BF-SWEEP-OTHERS — phần đầu #117 (chờ review/gộp)

1. Admin/HCTH có quyền sửa: mở Thông tin làm việc, bấm bút hoặc Sửa ở chi tiết. URL kết thúc `/[id]/sua`; không có dialog. Có Danh sách, Lưu/Hủy và Lịch sử.
2. Xóa ngày vào làm rồi Lưu: lỗi ngay dưới trường, aria-invalid; không ghi sự kiện. Ngày nghỉ trước ngày vào làm và thiếu lý do nghỉ cũng bị chặn trước API.
3. Nhập ngày nghỉ hợp lệ và lý do: Lưu về trang chi tiết, trạng thái/ngày nghỉ cùng ảnh hưởng NS01/NS02 đúng; lịch sử giữ dòng cũ. Hủy về danh sách.
4. Cấp Xã truy cập trực tiếp route sửa: không có form ghi. Quyền của hồ sơ vẫn được kiểm từ máy chủ.
5. Phần còn lại của sweep nằm ở #163/#164/#165; không đánh dấu đã nghiệm thu toàn bộ HCTH/KHTC/Báo giá bằng phần này.

## BF-SWEEP-HCTH-REST — phần đầu #163: mục danh mục FN-19

1. Admin mở Danh mục nhân sự (route chính và alias phòng ban), chọn một loại. Bảng có STT; Thêm/Sửa/Lịch sử mở route con, không có dialog. Quay về Danh sách giữ đúng loại danh mục và alias.
2. Thêm thiếu mã/tên/thứ tự, thiếu thứ hạng trình độ hoặc thiếu trình độ của nhóm chuyên ngành: dấu * và lỗi ngay tại ô; không gọi API ghi. Thử `1e100000`, `1e-100000`, 100000 chữ số, số âm và số rất lớn ở thứ tự/thứ hạng/hệ số phụ cấp, cả blur lẫn Lưu: chặn tại ô.
3. Giá trị biên đúng: thứ tự 0–100000; thứ hạng nguyên 1–100; phụ cấp để trống hoặc >0 đến 10, hai chữ số thập phân. Nhập `0,30` được gửi theo chuỗi decimal chuẩn. Mã ngạch bỏ khoảng trắng; trùng mã vẫn do máy chủ chặn.
4. Lưu chuyển tới chi tiết, Sửa không đổi mã/trạng thái ngừng, Log và bảng lịch sử giữ phiên bản cũ; không hiển thị tên người thực hiện giả vì API chưa cung cấp. Giữ nhãn Mẫu và câu hỏi Q-070.
5. Cấp Tỉnh–HCTH chỉ xem; cấp Xã/phòng khác không được ghi, truy cập trực tiếp route cũng không có form. Quyền chưa tải được không mở form. Thử lại sau lỗi mạng/500 cùng nội dung giữ khóa thao tác; lỗi API có trường hiển thị tại ô.
6. Vitest `catalog-items.test.tsx`, `hrm.test.tsx`, shared `form.test.tsx`; Playwright `hc-catalog.spec.ts` và hồi quy route FN-08 `hc-nhan-su.spec.ts`. Đây là bằng chứng tự động, chưa thay UAT khách.
7. Bảng hệ số/quy tắc và sáu màn HCTH còn lại ở #179; phần này không xác nhận tất cả A1–A5 của HCTH đã đạt. Giữ mẫu giấy/FormGrid và M1–M7.

## W11.26 — Đối chiếu từng AC FN-15–18 và FN-21 (checkpoint 30/10/2026)

Phạm vi phần này là 17 AC của đánh giá HCTH, bổ sung vào cùng ma trận; các mục lịch sử phía trên
vẫn chỉ xác nhận phạm vi gói tương ứng. Nguồn là BRD HCTH v3.0 ngày 07/10/2026, bản text
`brd-hanhchinh-v3.md`, các bảng FN-15, FN-16, FN-17, FN-18 và FN-21;
nguồn/giả định chi tiết tại [W11.26](../kn/gia-dinh/W11.26.md).
Không dùng trạng thái issue hoặc kiểm web giả lập để kết luận toàn HCTH đã đạt.

Cách đọc bằng chứng: bảy ca HTTP đánh giá dưới đây đã có `--- PASS` trên PostgreSQL tạm với
`-race` tại `ee2992f427b54f29567e625873086c51a509ec6d`. Toàn lượt affected/consumer vẫn **FAIL**:
533 PASS, bốn FAIL ở hợp đồng Notification.kind và assertion menu cũ, không SKIP, không DATA RACE.
Các assertion đã sửa; lint mới = 0, tám cổng Go và pure `-race` năm package đã PASS sau sửa.
Native retry và cổng trên nguồn kết hợp với dev có W11.68 (#33) **chưa hoàn tất**.
Web 120 tệp/993 kiểm và Playwright đã PASS; Playwright dùng mock, không chứng minh phát email,
OTP khách, kho triển khai hoặc tải native. Các trạng thái dưới đây giữ đúng giới hạn này, chưa phải READY.

Các tệp bằng chứng chính:

- [Workflow HTTP](../../internal/kn/hrm/handler/appraisal_workflow_integration_test.go): `TestHRMAppraisalWorkflowHTTP_W1126`, fixture ba người (hai MS05, một MS06), QIII/2026, phiếu sáu tiêu chí × 15 điểm.
- [Xuất, bản ký, năm và snapshot HTTP](../../internal/kn/hrm/handler/appraisal_export_integration_test.go): `TestHRMAppraisalExportAndAuditHTTP_W1126`, `TestHRMAppraisalAnnualAndRacesHTTP_W1126`, `TestHRMAppraisalProofAndSignedHTTP_W1126`, `TestHRMAppraisalScoreSummaryHTTP_W1126`, `TestHRMAppraisalPositionSnapshotHTTP_W1126`.
- [Mẫu HTTP](../../internal/kn/hrm/handler/appraisal_integration_test.go): `TestHRMAppraisalTemplatesHTTP_W1126`; [nghiệp vụ](../../internal/kn/hrm/domain/appraisal_test.go), [quyền/trạng thái](../../internal/kn/hrm/domain/appraisal_entry_test.go), [tám cột](../../internal/kn/hrm/domain/appraisal_summary_test.go).
- [Đích thông báo](../../internal/kn/hrm/module_appraisal_notify_test.go): `TestAppraisalNotificationTargetsAndOwnerDirectory_W1126`; [web](../../apps/web-ops/src/kn/hrm/appraisal.test.tsx); [Playwright](../../apps/web-ops/tests/e2e/hrm-appraisal.spec.ts).

| AC BRD | Tiêu chí cần nghiệm thu | Mã nguồn và bằng chứng thực tế | Trạng thái / phần chưa được chứng minh |
|---|---|---|---|
| FN-15 AC-01 | Xem trước MS05 đúng tiêu đề, tám cột, phần ký như biểu Sở | `domain/appraisal_summary.go`, `app/appraisal_export.go`, `app/appraisal_word.go`; `TestAppraisalMS05MS06EightColumns_W1126`; ExportAndAudit HTTP kiểm preview hai người/tám cột, đọc lại Word có kỳ QIII/2026, tên hai người và phần ký; Playwright kiểm preview tám cột | Có bằng chứng mẫu tự động PASS; chờ native cuối và đối chiếu biểu Sở chính thức Q-070. Không suy kiểm cấu trúc thành xác nhận bố cục của khách. |
| FN-15 AC-02 | Đổi thành Phó Trưởng phòng → thuộc MS06 ở đợt mới | `app/appraisal_period.go` chụp chức vụ tại ngày chốt; PositionSnapshot HTTP đổi chức vụ hiệu lực 01/10/2026, mở QIV/2026 và kiểm MS05 ở đợt cũ, MS06 ở đợt mới | Ca native mẫu PASS; chờ native cuối trên nguồn kết hợp. |
| FN-16 AC-01 | QIII/2026 có 470 MS05 + 24 MS06; cán bộ xã vào MS05 theo HS-37 | `app/appraisal_period.go`, `domain.AppraisalSummaryCode`; Workflow HTTP kiểm ba người và quyền lãnh đạo với cán bộ xã; PositionSnapshot HTTP kiểm đổi chức vụ | Một phần: đã kiểm quy tắc chia mẫu/snapshot/HS-37 ở mẫu nhỏ; chưa nạp và đối chiếu đủ 494 người H11. |
| FN-16 AC-02 | Tiến độ đơn vị cập nhật ngay khi phòng nhập | `app/appraisal_entry.go`, `app/appraisal_period_action.go`; Workflow HTTP ghi tự chấm, quản lý, gửi đơn vị rồi đọc lại kết quả | Một phần: ghi/đọc lại API đã kiểm; chưa có assertion riêng cho số tiến độ đơn vị trước/sau mỗi lần nhập và thời gian cập nhật trên UI native. |
| FN-16 AC-03 | Đúng số người ở từng trạng thái theo đơn vị | `domain/appraisal_entry.go`, `app/appraisal_entry.go`; Workflow HTTP kiểm luồng, quyền đơn vị và chốt/mở lại từng người | Một phần: chưa kiểm đầy đủ bảng đếm theo mọi đơn vị × mọi trạng thái; kiểm số phiếu nhìn thấy không thay bảng tiến độ. |
| FN-17 AC-01 | KHTNV thiếu tóm tắt căn cứ không lưu được | `domain.ValidateAppraisalProposal`, được luồng đổi phiếu gọi; `TestAppraisalProposalAndSummaryForms_W1126` chặn KHTNV không căn cứ, nhận căn cứ hợp lệ | Validator PASS; cần ca HTTP âm đúng thao tác lưu KHTNV và native cuối. Không lấy ca đơn vị làm bằng chứng HTTP đã chạy. |
| FN-17 AC-02 | Lãnh đạo Phòng Trồng trọt không thấy viên chức phòng khác | `app/appraisal_period.go`/`app/appraisal_entry.go`, `domain/appraisal_entry.go`; Workflow HTTP: `ld_tt` thấy đúng hai MS05, `ld_cn` không mở được cán bộ xã do Trồng trọt quản lý; self/tenant/TW/chỉ xem bị giới hạn | Có bằng chứng phân quyền API ở fixture ba người PASS; chờ native cuối và ca UAT theo tài khoản thực. |
| FN-17 AC-03 | Sau gửi, HCTH thấy ngay kết quả phòng tại FN-18 | `app/appraisal_period_action.go` gửi đơn vị; Workflow HTTP gửi Trồng trọt rồi HCTH đọc lại đủ sáu điểm quản lý; ExportAndAudit HTTP đọc preview MS05 sau gửi | Luồng API mẫu PASS; thời gian cập nhật và toàn luồng màn tổng hợp native chưa được đo. |
| FN-17 AC-04 | Cán bộ xã 82/90 → HS-37 thấy phiếu, DG-04 gợi ý HTXSNV, chấm quản lý và nhập tập thể đề xuất | `domain/appraisal.go`, `domain/appraisal_entry.go`, `app/appraisal_entry.go`; Workflow HTTP: tự nộp 82/90, `self_rating=HTXSNV`, `ld_tt` chấm sáu tiêu chí và đề xuất HTTNV; `TestAppraisalScopeUsesSubjectAndHS37_W1126` | Luồng mẫu theo đơn vị quản lý PASS; dữ liệu là tổng hợp, chưa thay xác nhận người/xã Mỹ Hòa Hưng và native cuối. |
| FN-18 AC-01 | MS05 đủ người của đợt, tám cột và phần ký | `app/appraisal_export.go`, `app/appraisal_word.go`; ExportAndAudit HTTP kiểm đủ hai MS05 của fixture, XLSX tám cột, Word tám cột/tên/phần ký; không thêm điểm vào mẫu giấy | Ca mẫu PASS; chưa đối chiếu 470 MS05 H11 hoặc bố cục Sở chính thức, còn native cuối. `PHIEU_DIEM` mười cột là bảng nội bộ riêng. |
| FN-18 AC-02 | Chốt đợt → cột xếp loại ở danh sách nâng bậc hiển thị kết quả quý | `app/appraisal_period_action.go`, `app/appraisal_quarter.go`; Workflow HTTP kiểm chốt quý không tạo `annual_rating`; AnnualAndRaces HTTP kiểm tham chiếu quý được ghim, kết luận năm độc lập và dòng năm bù khi mở lại | **Chưa đạt nguyên văn AC**: AC nói kết quả quý, trong khi FN-18 BR-03/FN-09 dùng kết quả năm. Quyết định hiện hành giữ quý ở lịch sử, chỉ kết luận tập thể của đợt năm ghi `annual_rating`; không suy công thức từ bốn quý. Chờ xác nhận cách hiển thị/cách hiểu AC; ca năm không thay bằng chứng quý ở danh sách nâng bậc. |
| FN-18 AC-03 | Bản đã ký xem lại được tại Hồ sơ dữ liệu | `app/appraisal_period_action.go` nối port kho tài liệu; ProofAndSigned HTTP upload PDF theo hồ sơ, gắn MS05/MS06 và chốt; `TestHRMDocsHTTP_W1124` kiểm quyền/mở tệp kho ở phạm vi riêng | Một phần: gắn bản ký và chốt đã PASS; cần ca xuyên luồng từ đợt đã chốt quay lại Hồ sơ dữ liệu và tải đúng bản ký. Port evidence/vault của fixture là giả, chưa chứng minh kho triển khai. |
| FN-21 AC-01 | Sáu tiêu chí, 90 điểm; ngưỡng ≥81 → HTXSNV; tự chấm 82 → HTXSNV | `domain/appraisal.go`; `TestAppraisal82Of90AndRatingBoundaries_W1126` kiểm cả 81/80,99; Workflow HTTP dùng sáu tiêu chí ×15, tự nộp tổng 82 và nhận HTXSNV | Đơn vị và native mẫu PASS; còn native cuối trên nguồn kết hợp. Mức gợi ý không tự trở thành kết luận chính thức. |
| FN-21 AC-02 | Mở QIV/2026 → mọi cán bộ xã có tài khoản nhận thông báo và thấy ở Đánh giá của tôi | `app/appraisal_notify.go`, `module_appraisal_notify.go`; NotificationTargetsAndOwnerDirectory kiểm payload QIV/2026, AccountRef, loại thông báo, đường dẫn và tenant/node/HS-37; Workflow HTTP kiểm self chỉ thấy phiếu mình | Một phần: kiểm builder/đường dẫn và quyền self, chưa kiểm toàn bộ cán bộ xã QIV/2026 qua outbox/inbox thật hoặc gửi email thật. Playwright mock không bù khoảng trống này. |
| FN-21 AC-03 | Sau cán bộ xã nộp, lãnh đạo HS-37 thấy trong danh sách cần đánh giá | `app/appraisal_entry.go`, `domain/appraisal_entry.go`; Workflow HTTP tự nộp, quản lý được chấm và đơn vị khác bị chặn; NotificationTargetsAndOwnerDirectory kiểm chọn đúng quản lý | Luồng API mẫu PASS; chưa có assertion native riêng cho bộ lọc danh sách “cần đánh giá” ngay sau nộp; còn native cuối. |
| FN-21 AC-04 | Tổng hợp đúng số đã/chưa nộp theo đơn vị và bước | `app/appraisal_score_export.go`, `app/appraisal_entry.go`; ScoreSummary HTTP kiểm ba phiếu trước/sau nộp, điểm 82/82, lãnh đạo chỉ xuất hai người được giao và lần xuất cũ giữ tham chiếu | Một phần: kiểm tập người/điểm/phạm vi xuất, chưa kiểm đầy đủ ma trận số đã/chưa nộp theo mọi đơn vị × bước. |
| FN-21 AC-05 | Khách nhận email/link, OTP đúng chỉ mở phiếu mình; khóa bước làm link hết hạn | Handler `guest-link` hiện trả 503; Workflow HTTP kiểm trạng thái fail-closed. Capability khách theo [#184 W10.18-GUEST-OTP](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/184) | **PENDING #184 / chưa đạt**: chưa có OTP khách qua email và ca hết hạn/nhật ký mở link. TOTP đăng nhập và nhập thay có scan không thay AC này. |

Giới hạn dữ liệu và hiệu năng phải nghiệm thu riêng ở W11.31: D3 H11 yêu cầu QIII/2026 có
470 MS05 + 24 MS06, bốn người NLTX tháng 7/2026 và khoảng 500 lượt nộp phiếu trong cùng ngày hạn.
Các ca ba người ở trên chưa chứng minh quy mô H11; phần NLTX/quyết định bốn người cũng chưa được đối chiếu ở checkpoint này.
BRD VII còn yêu cầu thao tác thường <3 giây, dashboard <5 giây với **500 người dùng đồng thời**,
tra cứu/lọc <2 giây và xuất NS01–06/NL02–04 khoảng 500 người <10 giây. 500 lượt nộp/ngày
và 500 người đồng thời là hai tiêu chí riêng. Không có kết quả đo tải đồng thời HCTH ở đây;
benchmark dashboard 1.000 hồ sơ/vault giả ở W11.28 không chứng minh yêu cầu đó.
