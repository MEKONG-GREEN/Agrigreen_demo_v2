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
