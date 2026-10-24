# Cấu hình fail-closed

Bắt buộc ([AGENTS.md §9 mục 2, 4, 5](../../AGENTS.md#9-lỗi-bắt-buộc-tránh)). Ticket gốc: [#127](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/127), [#128](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/128), [#130](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/130).

## Quy tắc

1. **Thiếu cấu hình kênh ngoài không được thành "thành công giả".** SMTP (`AG_SMTP_*`), FCM, URL công khai, khóa/bí mật: ngoài `AG_DEPLOY_ENV=demo`/dev, thiếu cấu hình → tiến trình **không khởi động** (lỗi nêu tên biến) hoặc kênh trả lỗi rõ. Bộ giả (`LogMailer`, pusher ghi log) chỉ được chọn khi môi trường là demo/dev **một cách tường minh**.
2. **Trạng thái gửi phản ánh sự thật**: chỉ ghi `sent` khi kênh đã nhận; kênh chưa cấu hình → `failed`/`skipped` kèm lý do, và màn quản trị hiện cảnh báo.
3. **Worker kiểm phụ thuộc lúc khởi động**: pool DB, broker, kênh gửi phải khác nil và kết nối được; thiếu → thoát với lỗi rõ, không panic giữa chừng. Mất kết nối broker → tự kết nối lại, không thoát im lặng.
4. **Nhận việc nguyên tử**: `UPDATE … SET status='sending', claimed_at=now() WHERE id=$1 AND status='pending' RETURNING …`; việc `sending` quá hạn được trả về `pending` bởi job dọn. Không gửi khi chưa claim được.
5. **RLS fail-closed**: policy không có nhánh "bảng chiếu rỗng thì cho qua" (`k_ref`, closure…). Chiếu chưa có dữ liệu → không thấy gì, không phải thấy hết. Sửa bằng migration mới (forward-only).
6. **Cờ nới lỏng an ninh** chỉ ở demo: `AG_MFA_DISABLED` chỉ hợp lệ khi `AG_DEPLOY_ENV=demo` (CI `tools/mfaflagcheck`, `authz.DemoMFABypassAllowed`).

## Vì sao

Báo cáo 22–24/10/2026: thông báo hiện "Đã gửi" trong khi không có SMTP (mặc định `LogMailer`); worker báo cáo khởi tạo với DB nil → panic; gửi trùng 6 email do claim không nguyên tử; tenant chưa có `k_ref` thì RLS bỏ qua kiểm. Lỗi im lặng khiến UAT tưởng chức năng chạy.

## Mẫu

```go
func MailerFromEnv(env string) (channel.Mailer, error) {
    cfg, ok := smtpConfigFromEnv()
    switch {
    case ok:
        return channel.NewSMTP(cfg), nil
    case env == "demo" || env == "dev":
        return channel.LogMailer{Log: slog.Default()}, nil
    default:
        return nil, fmt.Errorf("thiếu AG_SMTP_HOST/AG_SMTP_FROM cho môi trường %q", env)
    }
}
```

## Kiểm thử

- Bảng test theo môi trường: production/staging thiếu biến → lỗi khởi tạo; demo/dev → bộ giả; đủ biến → kênh thật.
- Kênh lỗi → bản ghi gửi không ở trạng thái `sent`.
- Worker: hai worker cùng claim một việc → đúng một lần gửi; restart broker → worker tiếp tục.
- RLS: truy vấn với vai module khi bảng chiếu rỗng trả **0 hàng**; có dữ liệu thì đúng phạm vi.
