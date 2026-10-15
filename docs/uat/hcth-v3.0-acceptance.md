# Nghiệm thu HCTH v3.0 — W11.28

Phạm vi: báo cáo Sở NS01–NS06 (HC-FN-12) và dashboard nhân sự (HC-FN-06).
Đây là ma trận cho W11.28, không xác nhận các chức năng HCTH ngoài gói.
Nguồn giả định: [W11.28](../kn/gia-dinh/W11.28.md).

| Tiêu chí | Bằng chứng tự động | Trạng thái |
|---|---|---|
| Danh sách sáu biểu; chỉ HCTH/Admin có quyền toàn Trung tâm; biểu có trường nhạy cảm cần quyền bổ sung | `TestHRMReportsHTTP_W1128`; Vitest `src/kn/hrm/reports.test.tsx` | Chờ kiểm tra tích hợp |
| NS01 theo ngày chốt, NS02 phản ánh nghỉ việc trong tháng; thứ tự chức vụ/đơn vị và nhãn kỳ | `TestHRMReportsHTTP_W1128`, `TestBuildNS01OrderAndChecks`, `TestPeriodLabel` | Chờ kiểm tra tích hợp |
| NS05/NS06 tổng hợp và nhấn ô xem người theo phạm vi; thiếu ngày sinh chặn, dữ liệu chưa phân loại cảnh báo | `TestBuildNS05NS06Rules`, `TestHRMReportsHTTP_W1128` | Chờ kiểm tra tích hợp |
| Xuất Excel NS01: đọc lại nội dung tệp, người lập/ký, kỳ báo cáo; tải lại đối chiếu SHA-256 và báo drift | `TestHRMReportsHTTP_W1128` | Chờ kiểm tra tích hợp |
| Lịch sử xuất/tải và nhật ký trường nhạy cảm; đọc PII theo lô qua vault | `TestHRMReportsHTTP_W1128`, `TestReadBatch_W1128` | Chờ kiểm tra tích hợp |
| Dashboard khớp NS01 cùng ngày, lãnh đạo phòng chỉ thấy phòng mình; khi thiếu quyền vault vẫn có số tổng hợp | `TestHRMReportsHTTP_W1128`; Vitest `src/kn/hrm/reports.test.tsx` | Chờ kiểm tra tích hợp |
| Dashboard dưới 5 giây với 1.000 hồ sơ | `TestHRMDashboardBenchmark_W1128` (PostgreSQL thật, vault giả trong bộ nhớ) | Chờ kiểm tra tích hợp |
| Menu mở dashboard/báo cáo HCTH và giữ các mục CNTS đã tích hợp | `TestWebMenuFixture`, `TestW1106_MenuV3DepartmentTemplates`; Vitest `src/api/mock/kn-menu.contract.test.ts` | Chờ kiểm tra tích hợp |

Giới hạn nghiệm thu: bố cục mẫu Sở chính thức vẫn chờ khách xác nhận theo giả định
W11.28; không suy diễn việc đọc lại NS01 thành đối chiếu tệp vàng cho cả sáu biểu.
Hiệu năng cục bộ với vault giả chưa thay thế đo trên môi trường có vault thật.
Xuất Word và lưu bản gốc trong kho tệp mã hóa thuộc W10.17, ngoài phạm vi gói này.
