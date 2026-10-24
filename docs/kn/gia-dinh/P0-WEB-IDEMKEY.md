## P0-WEB-IDEMKEY — Một Idempotency-Key cho mỗi lần gửi form (#129)

Báo cáo Ecombox §5: `apps/web-ops/src/kn/client.ts` và `src/kn/cycle/api.ts` sinh uuidv7 mới cho MỌI request ghi, nên bấm Lưu lại sau hết giờ/503 thành thao tác mới — tạo HTX lần hai ra 409 và để lại node tổ chức mồ côi; lệnh ghi khác có thể nhân đôi. Gói này chỉ sửa web (`apps/web-ops`), không đổi hợp đồng OpenAPI hay backend Go.

### Cơ chế

- `IdempotentOperation` (`src/api/idempotency.ts`): một "phiên thao tác" giữ khóa theo **dấu vân tay yêu cầu** = phương thức + đường dẫn (kèm query) + `X-AG-Context` + thân. Cùng dấu → cùng khóa cho đến khi có kết quả dứt khoát.
- Kết quả **chưa rõ** (lỗi mạng/hết giờ, status 0, 408, 425, 429, 5xx, `idempotency_in_progress`) → GIỮ khóa: bấm lại cùng dữ liệu gửi lại đúng khóa, máy chủ phát lại kết quả cũ. Kết quả **dứt khoát** (2xx, 4xx nghiệp vụ) → bỏ khóa: lần gửi sau là thao tác mới. Đổi dữ liệu form → thân khác → khóa mới (không bị 422 `idempotency_key_reused`).
- Phạm vi: `withIdempotentOperation(op, fn)` đặt phiên hiện hành trong lúc fn chạy đồng bộ; client đọc phiên ngay khi khởi phát request (trước await đầu tiên): `knHttp` (cycle/collect/admin KN), `createKnApi` (kn/client.ts), và client openapi-fetch qua `idempotencyScopeMiddleware` đứng ĐẦU chuỗi middleware (trước `authMiddleware` có await), `idempotencyMiddleware` gắn khóa + nhận kết quả qua `onResponse`/`onError`.
- Thứ tự ưu tiên khóa: khóa tường minh nơi gọi truyền (`knHttp` tham số `idem`; `createCooperative/addHousehold/putMachines/recordMaterials/submit` nhận thêm `idem?`) > khóa phiên thao tác > khóa mới (chỉ cho lời gọi một lần, ngoài form). Riêng openapi-fetch: trong phiên, khóa phiên thay khóa do helper `idem()` của từng module api sinh (không phân biệt được khóa helper với khóa form), ngoài phiên giữ nguyên hành vi cũ.
- Hook: `useAction()` (`src/kn/cycle/common.tsx`, dùng ở ~40 màn KN) tự giữ một phiên cho mỗi màn — mọi `act.run(() => api.ghi(...))` được bảo vệ, không phải sửa từng màn. Lời gọi ghi đứng SAU một await khác trong f phải bọc bằng tham số `idem` của f hoặc `act.idem(...)`. `useIdempotentAction()` (`src/api/useIdempotentAction.ts`) cho màn không dùng useAction. `act.resetIdempotency()`/`reset()` khi muốn coi lần mở lại form là thao tác mới.
- Backend giả lập: `src/api/mock/handler.ts` vốn phát lại theo khóa (cùng khóa + cùng thân → cùng phản hồi + `Idempotent-Replayed: true`; khác thân → 422). Bổ sung: cùng khóa đang xử lý (khoảng await trước khi ghi bản phát lại) → 409 `idempotency_in_progress`, không chạy lần hai.

### Quy tắc cho màn mới (thay cho mục trong docs/kn/ui-kit.md — tệp đó chưa có trên dev)

Lệnh ghi do người dùng bấm phải chạy trong phiên thao tác: `act.run(() => api.x(...))` hoặc `idem.run(() => api.x(...))`. Không tự sinh `newIdempotencyKey()`/`uuidv7()` cho mỗi lần bấm. Không đưa giá trị đổi theo mỗi lần bấm (giờ hiện tại, số ngẫu nhiên) vào thân request — dấu vân tay sẽ khác và mất chống trùng. Gửi nhiều lệnh nối tiếp: bọc từng lệnh bằng `idem(() => …)`.

### Luồng đã phủ

- Tự động qua `useAction`: HTX (thêm, ngừng hoạt động), nông hộ, khai báo máy (PUT), vật tư; phát hành/thu hồi đợt, hạn nộp, nhắc, chốt/mở lại kỳ, tổng hợp, gửi công văn; quản trị KN; CNTS (danh mục, kỳ/công bố, ổ dịch, mô hình, hỗ trợ/grants, lịch trực/tiêm, tổng hợp, cảnh báo); giá (mặt hàng, điểm, phiếu nhập, kiểm duyệt, lịch giá, báo cáo); KHTC (danh mục, kế hoạch, biểu mẫu, kỳ, nguồn, báo cáo); HRM; liên hệ.
- Sửa tay (gọi ngoài `act.run` hoặc sau await): nộp/lưu nháp báo cáo Xã `kn/grid` (XaScreens), nộp CNTS (CntsEntryScreen), lập/cập nhật/xác nhận ổ dịch (OutbreakScreen), sự kiện làm việc HRM (WorkInfoScreens), lịch công tác tuần lưu/gửi/sao chép/duyệt/yêu cầu sửa/duyệt hàng loạt (RegisterSchedule, ApproveSchedules, ScheduleDetail).

### Chưa phủ / giới hạn

- Trang ngoài KN dưới `src/app/*` (HTX cũ, quản trị chung) giữ cơ chế khóa theo form riêng đã có; không đổi.
- Nạp tệp `importKnUsers` (đọc tệp bằng await trước khi gửi) và thân FormData/multipart (ranh giới ngẫu nhiên) → khóa mới mỗi lần như trước.
- Phiên sống theo màn: rời màn/tải lại trang → phiên mới (coi như mở lại form). Phiên giữ tối đa 64 khóa treo.
- Máy chủ thật phải lưu phản hồi theo khóa (R-51) và trả `idempotency_in_progress` khi khóa đang chạy; web không kiểm được phía Go trong gói này.

### Kiểm thử

`src/api/mock/idempotency-retry.test.ts`: tạo HTX mất phản hồi → bấm lại cùng khóa → phát lại, không 409, đúng một HTX (đối chứng không phiên → 409); 503 → thử lại cùng khóa, thành công → khóa mới; đổi dữ liệu → khóa mới; bấm hai lần liên tiếp → một HTX; `useAction` thật qua renderHook; nộp báo cáo Xã mất phản hồi → lịch sử chỉ thêm một bản; openapi-fetch 503/422/lỗi mạng/ngoài phiên.

### Xung đột dự kiến với nhánh chưa gộp

`bf-sweep-cnts` (#101): `kn/cnts/outbreak/OutbreakScreen.tsx` (hàm submit của OutbreakEditor), `kn/cnts/periods/CntsEntryScreen.tsx` (submit của Entry) — xung đột nhỏ, giữ bản của nhánh kia rồi bọc lại lời gọi ghi bằng `act.idem(...)`. `bf-sched` (#99): `kn/schedule/RegisterSchedule.tsx` (khác vùng: thêm hook + bọc `ws.save`/`ws.copyPrevious`). `bf-sweep-tt` (#100): chung `kn/cycle/screens/DataScreens.tsx` nhưng gói này không sửa tệp đó. Nhánh nào gộp sau chỉ cần giữ quy tắc trên.
