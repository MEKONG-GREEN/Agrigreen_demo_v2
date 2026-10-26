# Idempotency: một khóa cho mỗi thao tác người dùng

Bắt buộc ([AGENTS.md §9 mục 3](../../AGENTS.md#9-lỗi-bắt-buộc-tránh)). Ticket gốc: [#129](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/129). Quy tắc server R-51: [g1-conventions §4](../dev/g1-conventions.md#4-api), mã `internal/shared/idempotency`, runbook [idempotency-scope-v2](../runbooks/idempotency-scope-v2.md).

## Server (đã có)

- Mọi POST của `/v1` bắt buộc header `Idempotency-Key` (`^[A-Za-z0-9._:-]{8,200}$`). Gọi lặp cùng khóa trả lại **đúng** phản hồi lần đầu; khóa lưu 48 giờ theo (tenant, khóa).
- Handler ghi sổ dùng `idempotency.KeyFromContext(ctx)` làm `idem_key`, nên tiến trình chết giữa chừng rồi chạy lại cũng không ghi trùng.
- Cùng khóa, khác thân request → lỗi (không âm thầm chạy lại).

## Client: khóa thuộc về **thao tác**, không thuộc về request

1. Sinh khóa **một lần** khi người dùng bắt đầu thao tác (mở form tạo, mở hộp thoại xác nhận, bắt đầu nộp biểu).
2. Mọi lần gửi của thao tác đó (bấm Lưu lần hai, thử lại sau lỗi mạng/5xx/timeout) **dùng lại cùng khóa**, đặt sẵn vào header — middleware `idempotencyMiddleware` (`apps/web-ops/src/api/idempotency.ts`) giữ nguyên khóa nơi gọi đã đặt, chỉ tự sinh khi thiếu.
3. **Đổi khóa** sau khi thao tác thành công (2xx) hoặc khi người dùng sửa nội dung form (thân request khác = thao tác mới).
4. Áp cho mọi lệnh ghi: tạo HTX/tổ chức, nộp/lưu nháp biểu, lịch, duyệt…

```ts
// SAI: khóa mới mỗi lần gọi → bấm lại tạo bản ghi thứ hai / 409
await api.POST("/v1/…", { body, headers: { "Idempotency-Key": newIdempotencyKey() } });

// ĐÚNG: khóa sống theo thao tác
const opKey = useRef(newIdempotencyKey());          // khi mở form
async function submit() {
  const res = await api.POST("/v1/…", { body, headers: { "Idempotency-Key": opKey.current } });
  if (res.response.ok) opKey.current = newIdempotencyKey(); // thao tác kế tiếp
}
function onChange() { opKey.current = newIdempotencyKey(); } // nội dung đổi
```

Ứng dụng di động/ngoại tuyến: id bản ghi do client sinh (UUIDv7, R-11) đóng vai trò tương tự — giữ nguyên id khi đồng bộ lại.

## Kiểm thử

- Vitest: gửi hai lần cùng thao tác (giả lập lỗi mạng lần đầu) → hai request mang **cùng** khóa; sau thành công, thao tác mới mang khóa khác.
- Go (bắt buộc theo g1-conventions): POST lặp cùng khóa trả cùng phản hồi, không ghi trùng.
