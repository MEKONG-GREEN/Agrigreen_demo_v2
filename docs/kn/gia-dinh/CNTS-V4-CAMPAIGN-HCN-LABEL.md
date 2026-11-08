## CNTS-V4-CAMPAIGN-HCN-LABEL — Nhãn hộ ở công việc chiến dịch (#277)

Căn cứ: BRD CNTS Final v4.0, FN-21 luồng B3 (Cấp Xã nhập biểu của đợt được giao), FN-10 bảng trường HCN-01–03 và ma trận quyền chi tiết; kế hoạch KN độc lập ngày 07/11/2026, Đợt 3. Không dùng văn bản v3.7 trong issue cũ làm đặc tả.

| Mục | Cách xử lý |
|---|---|
| Nguồn công việc | Giữ hợp đồng public GET /v1/forms/tasks, profile kn_cnts của #236. Unit household là household_ref, parent là xã; gửi biểu/nháp vẫn dùng định danh gốc. |
| Nhãn hộ | Chỉ công việc unit_kind=household gọi public GET /v1/extension/cnts/livestock-households/{ref} của #251 trong ApiClient/ngữ cảnh thật. Đối chiếu cả id và commune_code với công việc trước khi hiển thị. Không liệt kê mọi hộ để giải quyền. |
| HCN-01–03 | Hiện mã HCN bất biến và owner_name nếu owner cho phép, nếu thiếu dùng owner_label được phép. Không tự che tên thêm, không tự cấp quyền Resolve. Không hiển thị SĐT ở màn công việc; endpoint owner vẫn giữ SĐT che một phần. |
| Thiếu nguồn/quyền | HTTP 401/403: Không có quyền xem hộ; 404: Không có hộ trong phạm vi hiện tại; lỗi nguồn: Chưa tải được thông tin hộ. Tên/mã thiếu có thông báo riêng, không đoán HCN/tên từ UUID. |
| Phiên/đổi quyền | Đổi principal, tenant, lần gán/ngữ cảnh hoặc phạm vi đã nhận: bỏ màn con và biểu đang mở. Lần đọc lại kiểm quyền tại owner; phản hồi muộn từ màn cũ không đi vào màn mới. Không bổ sung bộ nhớ đệm PII hoặc đưa nhãn vào localStorage, Forms, receipt/ledger. |
| Ranh giới | Không migration, quyền mới, SQL riêng, HTX, lịch sử HCN-09, copy/lifecycle chiến dịch, #173 hay đổi snapshot/băm báo cáo cũ. Menu không đổi; bản nền chưa có brd-surface.yaml nên chưa thêm dòng vào tệp chưa tồn tại. |

### Chạy và kiểm

Trong apps/web-ops: npm ci, npm run check:api, npm run typecheck, npm run lint, npm test, npm run build:mock. Test consumer riêng: npm exec -- vitest run src/kn/cnts/periods/household-label277.test.tsx. Mở /kn/cnts/bao-cao-ky bằng ngữ cảnh Cấp Xã CNTS có công việc theo hộ đã phát hành; kiểm mã HCN, tên/nhãn được phép, xã và định danh gửi giữ nguyên.

Native: đặt GOTOOLCHAIN theo go.mod, GOTMPDIR/TEMP/TMP theo môi trường KN rồi chạy scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/extension/cntslivestock/... ./internal/kn/extension/cntscampaign/... ./internal/platform/forms/... ./tests/kn77. TestCampaignHouseholdReadContract_277 kiểm tên hiện hành, mã bất biến, Vault/mục đích, quyền chỉ xem, đổi phạm vi/thu hồi và chéo tenant; TestCNTSCampaignHouseholdScope_236 kiểm công việc native unit/parent theo hộ. Mô phỏng web chỉ kiểm consumer, không thay whole Linux CI/race/G1/UAT.

### Seam kiểm consumer trên production mock

Các tệp mock campaign/periods/design được nối tối thiểu để publish phạm vi hộ tạo FormsTask có ref, xã và snapshot template ID/version/fields hiện hữu. Sổ hộ vẫn được đọc riêng qua API owner, không chép nhãn vào kỳ. Endpoint mock GET forms/templates chỉ trả registry phiên bản của profile kn_cnts và quyền đọc hiện hành. Reset campaign bỏ kỳ theo hộ và nháp của kỳ; replay publish giữ nguyên period ID. Fixture tài khoản được chuẩn bị qua API/UI Admin hiện hữu. Projection này là bằng chứng consumer/browser, không thay kiểm phân quyền native/DB.

Browser production tái hiện viewport 390 px bị tràn tới 464 px bởi bộ lọc theo chiều rộng tự nhiên và nhãn dài bị co thành cột quá hẹp. Màn đích giới hạn bộ lọc theo container, giữ cột nhãn đủ rộng để đọc mã HCN, cho tên dài xuống dòng đầy đủ trong vùng cuộn bảng của UI kit; không đổi CSS chung. Kiểm consumer dùng giờ giả lập cố định và giữ nguyên assertion text/escaping/không lưu nhãn vào nháp.
