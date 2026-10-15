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
| HRM-DOC-PURPOSE-001: không thể dùng hr_records mở tệp hiện trường, tệp cũ hoặc sai loại; mọi từ chối có sổ | `TestOpenHRRecordsWithoutConsent_W1124` | DB thật |
| HRM-DOC-CRYPTO-001: mở khóa/giải mã đúng qua client/router/vault thật; chặn sai vai, sai tenant, sai mục đích; log không chứa bản rõ/token | `TestHRRecordsUnwrapHTTP_W1124` | PostgreSQL thật, HTTP, KMS cục bộ dành cho test |
| Ẩn/hiện lại có lý do, lịch sử phân loại; sổ không UPDATE/DELETE | `TestHRMDocsHTTP_W1124` | G1 |
| Cây thư mục, danh sách, chi tiết, ẩn/hiện lại và sửa quyền trên web | `src/kn/hrm/docs.test.tsx`, `docs-api.test.ts` | Giả lập API |

Tự lưu quyết định nâng bậc và bản ký đánh giá thuộc W11.25/W11.26 sau G1.
Kiểm thử KMS cục bộ không thay thế diễn tập kho khóa và kho tệp trên môi trường triển khai.
