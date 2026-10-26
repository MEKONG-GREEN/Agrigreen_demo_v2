# Cấu hình fail-closed

Bắt buộc ([AGENTS.md §9 mục 2, 4, 5](../../AGENTS.md#9-lỗi-bắt-buộc-tránh)). Ticket gốc: [#127](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/127), [#128](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/128), [#130](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/130).

## Quy tắc

1. **Thiếu cấu hình kênh ngoài không được thành "thành công giả"** — ở **mọi** môi trường, kể cả demo. Hợp đồng kênh tắt do [#127](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/127) đưa vào (`internal/platform/notify/channel/disabled.go`; #127 chưa vào `dev` lúc viết — mã trên `dev` trước đó còn mặc định `LogMailer`, chính là lỗi #127 sửa, không phải mẫu để theo):
   - **Không cấu hình** (vd không có `AG_SMTP_HOST`, hoặc `AG_MAILER=disabled`) → lắp **kênh tắt** (`DisabledMailer`/`DisabledPusher`): `Send` luôn trả `ErrChannelDisabled`; bản gửi ghi `dropped` với mã `channel_disabled:<lý do>` (vd `smtp_not_configured`), không thử lại, **không bao giờ** `sent`; màn sức khỏe gửi hiện lý do.
   - **Cấu hình một phần hoặc sai** (`AG_MAILER=smtp` mà thiếu `AG_SMTP_HOST`; có host mà thiếu `AG_SMTP_FROM`; cổng sai; giá trị `AG_MAILER` lạ) → tiến trình **không khởi động**, lỗi nêu tên biến. Không bao giờ âm thầm hạ về kênh tắt hay bộ giả.
   - **Bộ giả ghi log** (`LogMailer`, `LogPusher` — `Send` trả nil, tức sẽ bị ghi `sent`) chỉ khi đặt **rõ** `AG_MAILER=log`/`AG_PUSHER=log` trên **máy phát triển** (`AG_ENV=development|dev|local|test`); nơi khác (kể cả demo) → lỗi khởi động. Không chọn bộ giả theo `AG_DEPLOY_ENV=demo`.
   - Cùng nguyên tắc cho URL công khai, khóa/bí mật: thiếu → kênh tắt có lý do hoặc không khởi động, không giả thành công.
2. **Trạng thái gửi phản ánh sự thật**: chỉ ghi `sent` khi kênh thật đã nhận; kênh tắt → `dropped` kèm lý do; kênh lỗi → `failed`. Màn quản trị hiện cảnh báo.
3. **Worker kiểm phụ thuộc lúc khởi động**: pool DB, broker, kênh gửi phải khác nil và kết nối được; thiếu → thoát với lỗi rõ, không panic giữa chừng. Mất kết nối broker → tự kết nối lại, không thoát im lặng.
4. **Nhận việc nguyên tử**: `UPDATE … SET status='sending', claimed_at=now() WHERE id=$1 AND status='pending' RETURNING …`; việc `sending` quá hạn được trả về `pending` bởi job dọn. Không gửi khi chưa claim được.
5. **RLS fail-closed**: policy không có nhánh "bảng chiếu rỗng thì cho qua" (`k_ref`, closure…). Chiếu chưa có dữ liệu → không thấy gì, không phải thấy hết. Sửa bằng migration mới (forward-only).
6. **Cờ nới lỏng an ninh** chỉ ở demo: `AG_MFA_DISABLED` chỉ hợp lệ khi `AG_DEPLOY_ENV=demo` (CI `tools/mfaflagcheck`, `authz.DemoMFABypassAllowed`).

## Vì sao

Báo cáo 22–24/10/2026: thông báo hiện "Đã gửi" trong khi không có SMTP (mặc định `LogMailer`); worker báo cáo khởi tạo với DB nil → panic; gửi trùng 6 email do claim không nguyên tử; tenant chưa có `k_ref` thì RLS bỏ qua kiểm. Lỗi im lặng khiến UAT tưởng chức năng chạy.

## Mẫu

Rút gọn từ `channel.MailerFromEnv` của [#127](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/127) (áp dụng khi #127 vào `dev`; tên hàm/kiểu theo mã thật khi đó):

```go
// AG_MAILER: "" (tự chọn) | smtp | disabled | log (chỉ máy phát triển). Không bao giờ trả nil.
func MailerFromEnv(getenv func(string) string, log *slog.Logger) (Mailer, error) {
    switch m := strings.ToLower(strings.TrimSpace(getenv("AG_MAILER"))); m {
    case "disabled":
        return DisabledMailer{Reason: "disabled_by_config"}, nil
    case "log": // bộ giả: Send trả nil -> bản gửi thành "sent"; chỉ được phép trên máy phát triển
        if !residency.LocalDev(getenv) {
            return nil, errors.New("AG_MAILER=log chỉ dùng trên máy phát triển (AG_ENV=development|dev|local|test)")
        }
        return LogMailer{Log: log}, nil
    case "", "smtp":
        s, err := smtpFrom(getenv) // có AG_SMTP_HOST mà thiếu AG_SMTP_FROM / cổng sai -> err
        if err != nil {
            return nil, err // cấu hình một phần: dừng khởi động, không hạ về kênh tắt/bộ giả
        }
        if s != nil {
            return s, nil
        }
        if m == "smtp" {
            return nil, errors.New("AG_MAILER=smtp nhưng chưa đặt AG_SMTP_HOST")
        }
        return DisabledMailer{Reason: "smtp_not_configured"}, nil // Send -> ErrChannelDisabled
    default:
        return nil, fmt.Errorf("AG_MAILER=%q — chỉ nhận smtp, log, disabled", m)
    }
}

// Bên gửi: kênh tắt -> "dropped" kèm lý do, không thử lại, không "sent".
if errors.Is(err, channel.ErrChannelDisabled) {
    return domain.DeliveryDropped, code(channel.DisabledCode(reason)) // "channel_disabled:smtp_not_configured"
}
```

## Kiểm thử

- Bảng test theo cấu hình: không có biến → kênh tắt, bản gửi `dropped` với `channel_disabled:<lý do>` (không `sent`); cấu hình một phần/sai/giá trị lạ → lỗi khởi động; `AG_MAILER=log` ngoài máy phát triển (kể cả demo) → lỗi khởi động; đủ biến → kênh thật.
- Kênh lỗi → bản ghi gửi `failed`; kênh tắt → `dropped`; không trường hợp nào thành `sent` khi chưa gửi thật.
- Worker: hai worker cùng claim một việc → đúng một lần gửi; restart broker → worker tiếp tục.
- RLS: truy vấn với vai module khi bảng chiếu rỗng trả **0 hàng**; có dữ liệu thì đúng phạm vi.
