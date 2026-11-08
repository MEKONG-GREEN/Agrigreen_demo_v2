## KHTC-DASHBOARD-AFTER-PENDING-INBOX — lát hộp phát sinh #245

Căn cứ duy nhất: BRD KHTC v2.3, Function chi tiết FN-TI-02.01 P742–774 và FN-TI-02.04 P886–920; DOCX SHA-256 `15859351f90574bf667190b1a12160465ba03f8ca9e408b94c90d285a3502e25`. Không dùng khung nghiệm thu v2.2 hoặc prototype làm đặc tả.

- Giữ hộp thường niên mặc định của #243. Thêm chế độ phát sinh, lọc đúng kỳ/phòng trước LIMIT và liên kết tới reviewer hiện hữu; kỳ chọn từ nhóm phiếu đang hiển thị. Tối đa 25 lời gọi public native Progress cho mỗi trang; trang trống vẫn có thể còn cursor. Không trả hoặc suy tổng toàn cục.
- Dữ liệu nộp là ID/attempt/time của lần gửi cuối native có trạng thái submitted, chưa có quyết định; nháp, bảng niêm phong và ý định thao tác không chứng minh đã gửi. Chỉ giao của kỳ và đơn vị còn trong native projection được chiếu. Đóng/hủy native hoặc kỳ KN không còn trong hộp; phiếu thành viên kỳ hủy tiếp tục dùng 404 của policy hiện hành.
- Phát sinh giữ policy riêng: Province KHTC hoặc Admin có MFA, không TW hoặc phiên chỉ xem. Principal, tenant, actor, selected context, phòng, assignment, scope và node gốc được giữ nguyên khi gọi native ngoài KN transaction. Phiên hết hạn bị từ chối; native denial hoặc facts không nhất quán làm cả trang thất bại, không trả thành tổng 0.
- Request KN được đọc batch chỉ trong cửa sổ kỳ đã giới hạn. Nếu latest native còn chờ rà soát và có active request thì chỉ nhận fulfilled có đúng native submission ref và attempt lớn hơn base. Intent/issued/unknown hoặc correlation chưa hoàn tất trả unavailable; không suy đã hoàn tất hoặc lùi về lần gửi cũ. Phiếu returned/accepted không ở hộp pending.
- Không migration/dependency/menu/route mới, không sửa Forms private SQL/native policy, max submissions, reopen, budget hoặc baseline. API được sinh lại bằng owner generator. Đổi mode/filter/context/client bỏ cursor và phản hồi lifetime cũ; GET no-store.

### Phần còn lại chính thức

ROOT đã tạo các child giữ phần còn lại trước khi đóng parent: public Forms batch/count có quyền caller hiện tại rồi consumer global badge; lọc cảnh báo theo nội dung native và BRD; điều hướng trước/tiếp theo xuyên hộp; lãnh đạo phòng/BGĐ ở context gốc (#215/#227); nhắc officer với idempotency/audit/outbox sau commit và disabled-channel; public TW live reader; đầy đủ cards/lĩnh vực/semantics BRD. Không dùng page length, N Progress toàn lịch sử, giả vai hoặc nguồn chưa gộp.

Lát này chưa hoàn tất toàn FN-TI-02.01/FN-TI-02.04, UAT khách hoặc G1. Whole CI và tích hợp công khai chờ authority/release hiện hành; kết quả local phải kèm source SHA và log kết thúc.

Phần còn lại đã có child chính thức do ROOT tạo: [#293 public Forms batch/count](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/293) là supplier độc lập nguồn #245 chưa gộp; [#294 global badge/cảnh báo/điều hướng xuyên hộp](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/294) chỉ dùng sau actual #245 + #293 và nguồn cảnh báo thật, phối hợp #290/#246; [#295 context gốc lãnh đạo/BGĐ và TW](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/295), [#296 nhắc officer/audit/outbox](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/296) tiếp sau #245. Chưa child nào được gói này nhận hoặc triển khai; không suy hoàn tất toàn FN/UAT/G1.

### Chạy và kiểm

- Không migration mới. Chạy API theo profile KN hiện hành với public Forms và KN sessions được lắp ráp bình thường; vào menu Phiếu số liệu các phòng, chọn Phiếu phát sinh/phòng/kỳ.
- Go: đặt toolchain trong go.mod; chạy `scripts/pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/extension/adhocintegration ./internal/kn/extension/khtcperiod/...` và các consumer trực tiếp theo Tests CSV của packet.
- Web: trong apps/web-ops chạy `npm ci`, `npm run check:api`, `npm run lint`, `npx tsc --noEmit`, `npx vitest run --maxWorkers=2`, `npm run build:mock`; production `next start` trên cổng test riêng, browser `kn-khtc-pending245.spec.ts`.
- Native tests `TestKHTCPendingInbox245_*` chứng minh submission/lifecycle/cancellation, tenant/current-authority, bộ lọc trước LIMIT, cap25 và pool1/no-I/O-TX. UI tests giữ race mode/filter/context và quyền chỉ xem; không suy PASS từ tên test.
