# RB-7: kiểm đích trong lãnh thổ lúc khởi động

Mục 21 (danh mục nhà cung cấp được phép) và RB-7. Mã: [internal/shared/residency](../../internal/shared/residency). Gọi đầu tiên trong `run()` của `api-platform`, `identity-vault`, `worker`, `outbox-publisher` (và `objsync`, `drill`); `TestServiceMainsCallGuard` hỏng build nếu thiếu.

Khi `AG_ENV=production` (hoặc `prod`), tiến trình **dừng ngay** nếu một biến `AG_*` có tên kết thúc bằng `_URL`, `_URLS`, `_ENDPOINT`, `_HOST`, `_ISSUER` (trừ `AG_TEST_*`) trỏ tới máy chủ ngoài danh mục. Như vậy DB (`AG_PF_DB_URL`, `AG_DB_URL`, `AG_DB_DIRECT_URL`, `AG_VAULT_DB_URL`), S3 (`AG_S3_ENDPOINT`, `AG_BACKUP_S3_ENDPOINT`), OTLP (`AG_OTLP_ENDPOINT`), Keycloak (`AG_OIDC_ISSUER`, `AG_OIDC_JWKS_URL`), kho định danh (`AG_VAULT_URL`), RabbitMQ (`AG_AMQP_URL`, `AG_RABBITMQ_MGMT_URL`) đều được kiểm; module mới (SMTP `AG_SMTP_HOST`, FCM `AG_FCM_ENDPOINT`…) chỉ cần đặt tên biến theo quy ước. Môi trường khác production: không kiểm.

`AG_ENV` là **bắt buộc** với mọi tiến trình gọi `residency.CheckEnv` (dịch vụ và công cụ `objsync`, `drill`, `rebuilddrill`): một trong `production|prod|staging|test|development|dev|local`. Thiếu hoặc gõ sai (vd `prd`) → dừng khởi động với `AG_ENV thiếu hoặc không hợp lệ` — guard production không "mở cửa" chỉ vì quên đặt biến. Compose dev đặt `local`, Helm đặt `global.env`, VPS mặc định `staging`; khi chạy công cụ bằng `go run` trên máy dev hãy đặt `AG_ENV=local` (hoặc `test`).

| Biến | Ý nghĩa |
|---|---|
| `AG_RESIDENCY_ALLOWLIST` | **Bắt buộc** ở production. Danh sách phân tách bằng dấu phẩy: hậu tố tên miền (`agrigreen.vn` khớp `agrigreen.vn` và `*.agrigreen.vn`), IP, CIDR (`10.0.0.0/8`), từ khóa `single-label` (tên dịch vụ nội cụm một nhãn như `pgbouncer`). Không nhận `*`. |
| `AG_NONPII_ALLOWLIST` | Tùy chọn. Đích ngoài lãnh thổ chỉ nhận dữ liệu **không định danh**, và chỉ áp cho biến `AG_FCM_*`, `AG_APNS_*`, `AG_PUSH_*`, `AG_EXTDATA_*` (proxy khử định danh, mục dưới) (vd FCM data-only: `fcm.googleapis.com`). Mỗi mục cần ADR (mục 21). Không mở được cho OTLP, DB, S3. |

Thông báo lỗi chỉ nêu tên biến và máy chủ (`RB-7: … AG_S3_ENDPOINT → s3.ap-southeast-1.amazonaws.com`), không in URL (có thể chứa mật khẩu).

Ví dụ production:

```
AG_ENV=production
AG_RESIDENCY_ALLOWLIST=agrigreen.vn,svc.cluster.local,10.20.0.0/16,single-label
AG_NONPII_ALLOWLIST=fcm.googleapis.com
```

## Nguồn dữ liệu ngoài qua proxy khử định danh (ADR-0007)

Thời tiết, độ mặn, tile bản đồ/vệ tinh **chỉ** đi qua `pf/extdata` (`GET /v1/extdata/*`, mã: [internal/platform/extdata](../../internal/platform/extdata)). Client (App HTX, app hiện trường, web) không gọi thẳng nhà cung cấp.

| Biến | Ý nghĩa |
|---|---|
| `AG_EXTDATA_SOURCES` | Mã nguồn bật, phân tách dấu phẩy. Rỗng → không nguồn nào (mặc định chặn) |
| `AG_EXTDATA_<ID>_URL` | Mẫu URL: `{lat}` `{lon}` `{cell}` (tâm và cạnh ô lưới) cho nguồn điểm; `{z}` `{x}` `{y}` cho tile. `fake` = nhà cung cấp giả (cấm ở production). Tên kết thúc `_URL` nên guard RB-7 ở trên kiểm đích |
| `AG_EXTDATA_<ID>_KIND` | `weather` \| `salinity` \| `tile` |
| `AG_EXTDATA_<ID>_ADR` | ADR riêng của nguồn **ngoài lãnh thổ** (`ADR-NNNN`) — bắt buộc ở production |
| `AG_EXTDATA_<ID>_CELL_DEG` | Cạnh ô lưới (độ), 0,05–1, mặc định 0,1 (≈ 11 km) |
| `AG_EXTDATA_<ID>_MAX_ZOOM` | Trần zoom tile, 0–18, mặc định 16 |
| `AG_EXTDATA_<ID>_TTL` | Hạn đệm, ≥ 1m; mặc định 30m (điểm), 24h (tile) |

Ở production: đích trong `AG_RESIDENCY_ALLOWLIST` → được dùng; đích ngoài lãnh thổ chỉ được dùng khi **vừa** thuộc `AG_NONPII_ALLOWLIST` **vừa** có `AG_EXTDATA_<ID>_ADR`; chỉ `https`. Sai → api-platform dừng lúc khởi động. Proxy gửi đi: tâm ô lưới hoặc z/x/y, `User-Agent: agrigreen-extdata/1`, `Accept` — không token, cookie, IP người dùng, mã thiết bị/thửa/nông hộ; không theo chuyển hướng; bộ nhớ đệm theo ô dùng chung giữa người gọi. Nhà cung cấp cụ thể chờ khách chọn (Q2-10).

Ví dụ (dev, nhà cung cấp giả):

```
AG_EXTDATA_SOURCES=wx,sat
AG_EXTDATA_WX_URL=fake
AG_EXTDATA_WX_KIND=weather
AG_EXTDATA_SAT_URL=fake
AG_EXTDATA_SAT_KIND=tile
```

Giới hạn: guard kiểm **cấu hình**, không kiểm DNS phân giải về đâu hay nhà cung cấp có pháp nhân trong lãnh thổ — phần đó vẫn là hồ sơ nhà cung cấp và rà soát triển khai.
