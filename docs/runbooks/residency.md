# RB-7: kiểm đích trong lãnh thổ lúc khởi động

Mục 21 (danh mục nhà cung cấp được phép) và RB-7. Mã: [internal/shared/residency](../../internal/shared/residency). Gọi đầu tiên trong `run()` của `api-platform`, `identity-vault`, `worker`, `outbox-publisher` (và `objsync`, `drill`); `TestServiceMainsCallGuard` hỏng build nếu thiếu.

Khi `AG_ENV=production` (hoặc `prod`), tiến trình **dừng ngay** nếu một biến `AG_*` có tên kết thúc bằng `_URL`, `_URLS`, `_ENDPOINT`, `_HOST`, `_ISSUER` (trừ `AG_TEST_*`) trỏ tới máy chủ ngoài danh mục. Như vậy DB (`AG_PF_DB_URL`, `AG_DB_URL`, `AG_DB_DIRECT_URL`, `AG_VAULT_DB_URL`), S3 (`AG_S3_ENDPOINT`, `AG_BACKUP_S3_ENDPOINT`), OTLP (`AG_OTLP_ENDPOINT`), Keycloak (`AG_OIDC_ISSUER`, `AG_OIDC_JWKS_URL`), kho định danh (`AG_VAULT_URL`), RabbitMQ (`AG_AMQP_URL`, `AG_RABBITMQ_MGMT_URL`) đều được kiểm; module mới (SMTP `AG_SMTP_HOST`, FCM `AG_FCM_ENDPOINT`…) chỉ cần đặt tên biến theo quy ước. Môi trường khác production: không kiểm.

| Biến | Ý nghĩa |
|---|---|
| `AG_RESIDENCY_ALLOWLIST` | **Bắt buộc** ở production. Danh sách phân tách bằng dấu phẩy: hậu tố tên miền (`agrigreen.vn` khớp `agrigreen.vn` và `*.agrigreen.vn`), IP, CIDR (`10.0.0.0/8`), từ khóa `single-label` (tên dịch vụ nội cụm một nhãn như `pgbouncer`). Không nhận `*`. |
| `AG_NONPII_ALLOWLIST` | Tùy chọn. Đích ngoài lãnh thổ chỉ nhận dữ liệu **không định danh**, và chỉ áp cho biến `AG_FCM_*`, `AG_APNS_*`, `AG_PUSH_*` (vd FCM data-only: `fcm.googleapis.com`). Mỗi mục cần ADR (mục 21). Không mở được cho OTLP, DB, S3. |

Thông báo lỗi chỉ nêu tên biến và máy chủ (`RB-7: … AG_S3_ENDPOINT → s3.ap-southeast-1.amazonaws.com`), không in URL (có thể chứa mật khẩu).

Ví dụ production:

```
AG_ENV=production
AG_RESIDENCY_ALLOWLIST=agrigreen.vn,svc.cluster.local,10.20.0.0/16,single-label
AG_NONPII_ALLOWLIST=fcm.googleapis.com
```

Giới hạn: guard kiểm **cấu hình**, không kiểm DNS phân giải về đâu hay nhà cung cấp có pháp nhân trong lãnh thổ — phần đó vẫn là hồ sơ nhà cung cấp và rà soát triển khai.
