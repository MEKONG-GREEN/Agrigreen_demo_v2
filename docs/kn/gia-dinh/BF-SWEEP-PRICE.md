## BF-SWEEP-PRICE — Lát cấu hình Báo giá (#165)

### Phạm vi và nguồn

Lát đầu của #165 chuẩn hóa **toàn luồng nhập/xem trong CalendarConfig**, gồm FN-AD-04.04 (kỳ/giờ khóa, ngoại lệ),
FN-AD-04.05 (nhắc nhập giá), FN-AD-04.06 (ngưỡng). Phần items/points/tickets/reports/surveys còn lại ở
**#182 BF-SWEEP-PRICE-FORMS**; không coi #165 đã phủ toàn bộ Báo giá hoặc toàn R-E1.

Nguồn: PRD `kn-bugfix-brd-alignment` R-E1/R-A1–A5, `docs/kn/ui-kit.md`, ma trận đối chiếu bugfix và BRD Báo giá
**v1.3 ngày 07/10/2026**, `06. THÔNG TIN TUYÊN TRUYỀN/02. BRD/BRD_He_thong_Khuyen_nong_Module_Bao_gia_nong_san_v1_3.docx`,
SHA-256 `80148e86e1685dd02835f7c628fa2c960af034511d4ebebfc7e9adf7dab99053`.
Ma trận #116 dẫn bản `Copy of …` ở `1.2 App Khuyến Nông/01. BRD` có hash khác `17984388…`; đây là hai nguồn tệp
khác nhau, không khẳng định chênh hash chỉ do metadata ZIP. Đã đối chiếu phiên bản/ngày, FN-AD-04.04–06 và các câu
BR/AC về thứ tự giờ, biến mẫu, miền ngưỡng, hiệu lực/lịch sử với `kn-handoff/analysis/src/brd_baogia.txt`: khớp,
không thấy khác biệt nghiệp vụ trong lát này. Không dùng BRD KHTC thay nguồn Báo giá.

### Quyết định triển khai

| Mục | Hành vi | Bằng chứng |
|---|---|---|
| Trang riêng | Danh sách/tab chỉ xem; thêm/sửa/xem mở DetailPage có breadcrumb, quay về danh sách, nút phải, cột Log. Giữ cả canonical `/kn/bao-gia/cau-hinh` và alias `/kn/phong-ban/[dept]/bao-gia-lich`; không đổi menu | `calendar-routes.ts`, `CalendarConfig.tsx`, `CalendarDetail.tsx` |
| Kỳ và nhắc | Giữ lịch T2–T6 tạm/Q-086, nhắc1 < nhắc2 < khóa < phản hồi, ngày áp dụng sớm nhất do API trả; sửa một phần giữ các trường lịch/nhắc còn lại. Gửi thử dùng cấu hình đã lưu trên máy chủ | `validateSchedule`, test UI và `TestPriceCalendarHTTP_W1174` |
| Ngoại lệ | Chỉ thêm từ ngày mai, lý do bắt buộc/tối đa300 ký tự; ngày nghỉ/bổ sung và xóa vẫn do API kiểm. Dialog chỉ xác nhận xóa | Test UI/E2E và test HTTP cũ |
| Ngưỡng | Giữ ưu tiên mặt hàng → nhóm → mặc định, (0;100]%, tối đa2 số lẻ; N1..60. Không xóa mặc định. Mã nhóm/mặt hàng theo miền hiện hành, sửa không đổi định danh; trùng phạm vi gợi ý sửa | `validateThreshold`, `TestConfigDecimalBounds`, HTTP/E2E |
| Số trước tính | NumberField + validateForm. Guard cục bộ kiểm độ dài32, số mũ, cú pháp, dấu, độ chính xác và biên bằng chuỗi trước NumberField blur/Decimal.parse. Server kiểm raw-length/cú pháp/biên trước decimal.NewFromString cho pct/flat_pct/survey_pct; giữ Problemcode `validation_failed`, thêm lỗi đúng trường | Test spy Decimal.parse, hostile 1e±100000/chuỗi100.000 chữ số/biên, HTTP trực tiếp |
| Quyền/context | Quyền ghi lấy từ can_manage; direct edit URL của phiên chỉ xem không có form ghi. Đổi tenant/người/context làm mới loader/form/useAction, bỏ draft và khóa cũ. Giữ useAction để retry cùng dữ liệu dùng lại khóa | Test context/capability, retry và các test quyền của module Price |
| Log lịch/ngưỡng | Dùng history thật: phiên bản, UUID người ghi, thời điểm và giá trị trước→sau; không suy diễn họ tên. Không có bản trước trong response thì giá trị cũ để trống, không bịa cấu hình cũ | `scheduleLog`, `thresholdLog`, test log |
| Giới hạn Log ngoại lệ | API hiện chỉ trả day/kind/reason/version, không có actor/time/history. Cột Log nói rõ thiếu metadata; không lấy giờ tải trang hay log lịch làm sự kiện ngoại lệ. Giữ API theo phạm vi gói | Detail ngoại lệ; câu hỏi PO dưới đây |
| STT/typography | DataTable đánh STT cho lịch30 ngày, ngoại lệ, ngưỡng, job và lịch sử; DetailPage/h2 dùng typography chung. Không thay FormGrid, mẫu giấy, M1–M7 hoặc API nghiệp vụ khác | UI/E2E |

### Ranh giới và kiểm

Không sửa `components/form.tsx` đang thuộc #163; guard nằm trong feature Price. Không sửa navigation test #181,
TT/CNTS, KHTC/HCTH, registry, OpenAPI, role/scope/tenant, PII hoặc dữ liệu HTX. Không thêm migration.

Test Go/DB khi ready: `./internal/kn/price/...`, `./cmd/api-platform/...` và tám test cổng migration;
PostgreSQL riêng `-v -count=1 -p1 -timeout55m`, phải 0 SKIP. Tám cổng Go, gofmt/lint theo baseline chứng minh,
web tsc/eslint/Vitest đầy đủ một worker/check:api/build:mock, E2E `price-config.spec.ts` và phép mutation phải đạt
trước marker READY. Kết quả thực chạy/log nằm trong bình luận ready của #165, không dùng tài liệu này thay bằng chứng.

PO: Có cần mở rộng hợp đồng API để xem đủ người/thời điểm/lịch sử của từng ngoại lệ? Gói này hiển thị giới hạn rõ,
không tự thêm endpoint hoặc sự kiện giả. Các giả định lịch/ngưỡng mặc định và việc khách chấp nhận UAT vẫn theo
BRD/Q-086 và bảng nghiệm thu hiện hành.
