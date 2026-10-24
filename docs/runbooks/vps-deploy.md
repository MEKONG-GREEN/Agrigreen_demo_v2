# Deploy lên một VPS (VNPT hoặc nhà cung cấp trong lãnh thổ)

Toàn bộ nền tảng trên **một** máy bằng Docker Compose: `deploy/compose/docker-compose.yml` + ghi đè `deploy/compose/docker-compose.vps.yml`. Dùng cho demo và thử nghiệm với người dùng. Một VPS không có dự phòng nên **không** phải đích production của kiến trúc v3.0. CI (`compose-smoke`) dựng lại stack với file ghi đè này ở mỗi lần push.

## 0. Trước khi làm

| Việc | Ghi chú |
|---|---|
| Dữ liệu | Mặc định `AG_ENV=staging`, chỉ dùng **dữ liệu tổng hợp** (`cmd/seed-synthetic`). Dữ liệu thật cần rà soát bảo mật riêng, `AG_ENV=production` và bản sao lưu đặt ngoài VPS. |
| RB-7 | VPS phải ở trung tâm dữ liệu trong nước. Không đặt S3, sao lưu hay giám sát ở nhà cung cấp nước ngoài. |
| Cấu hình | Baseline 4 vCPU, 8 GB RAM, 80 GB SSD; cần đo RSS và tải trên VPS thật trước UAT, đây chưa phải cấu hình đã nghiệm thu. Dành RAM riêng lúc build. Hạn mức runtime của từng container nằm trong file VPS; tổng cap không phải mức dùng thường trực hoặc mức RAM đặt trước. Build image lần đầu trên máy 4 vCPU mất khoảng 15–20 phút. |
| Hệ điều hành | Ubuntu 24.04 LTS. |
| Tên miền | Năm bản ghi A trỏ về IP của VPS: `app.`, `api.`, `auth.`, `admin.`, `files.` + `<domain>`. |
| IP quản trị | Một hoặc nhiều CIDR (IP văn phòng, VPN). Chỉ các IP này vào được `admin.<domain>` và `/admin` của Keycloak. |

## 1. Máy chủ

```sh
# Người dùng riêng, đăng nhập SSH bằng khóa; tắt mật khẩu và root
sudo adduser --disabled-password agrigreen && sudo usermod -aG sudo agrigreen
sudo sed -i 's/^#\?PasswordAuthentication .*/PasswordAuthentication no/; s/^#\?PermitRootLogin .*/PermitRootLogin no/' /etc/ssh/sshd_config
sudo systemctl restart ssh

# Tường lửa: chỉ 22, 80, 443
sudo ufw default deny incoming && sudo ufw allow 22/tcp && sudo ufw allow 80/tcp && sudo ufw allow 443
sudo ufw enable
sudo timedatectl set-timezone Asia/Ho_Chi_Minh
sudo apt-get update && sudo apt-get install -y unattended-upgrades git curl jq

# Docker Engine + Compose plugin (>= 2.20)
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker agrigreen
```

Ở cổng quản lý VNPT, security group / tường lửa mạng cũng chỉ mở 22, 80 và 443. Docker ghi rule iptables vượt qua ufw. Stack chỉ publish cổng của Caddy ra `0.0.0.0`, mọi dịch vụ khác nghe `127.0.0.1`. **Không** sửa `ports:` của các dịch vụ khác.

## 2. Mã nguồn

Repo là private. Dùng **deploy key chỉ đọc**: GitHub → Settings → Deploy keys, không bật "Allow write access".

```sh
ssh-keygen -t ed25519 -f ~/.ssh/agrigreen_deploy -N ""
# thêm ~/.ssh/agrigreen_deploy.pub vào Deploy keys
GIT_SSH_COMMAND="ssh -i ~/.ssh/agrigreen_deploy" git clone git@github.com:MEKONG-GREEN/Agrigreen_demo_v2.git
cd Agrigreen_demo_v2
```

## 3. Cấu hình và dựng

```sh
sh deploy/vps/prepare.sh demo.agrigreen.vn ops@mekonggreen.vn "203.0.113.10/32"
```

`prepare.sh`:

- sinh `deploy/compose/.env` với bí mật ngẫu nhiên, quyền 600, không in giá trị;
- thêm `KC_DB_PASSWORD`, `AG_DOMAIN`, `ACME_EMAIL`, `ADMIN_ALLOW_CIDR`;
- sinh realm Keycloak cho VPS ở `deploy/compose/.vps/keycloak/`: redirect và origin theo tên miền, client chỉ-dev `agrigreen-seed` bị **tắt**.

```sh
C="-f deploy/compose/docker-compose.yml -f deploy/compose/docker-compose.vps.yml"
docker compose $C up -d --build --wait --wait-timeout 1200
docker compose $C ps
```

Caddy tự lấy chứng chỉ Let's Encrypt khi DNS đã trỏ đúng. Nếu DNS chưa trỏ, xem `docker compose $C logs caddy`.

Kiểm tra:

```sh
curl -fsS https://api.<domain>/readyz
curl -fsS https://auth.<domain>/realms/agrigreen/.well-known/openid-configuration | jq -r .issuer   # https://auth.<domain>/realms/agrigreen
curl -s -o /dev/null -w '%{http_code}\n' https://admin.<domain>/healthz                            # 403 nếu gọi ngoài ADMIN_ALLOW_CIDR
```

Mở `https://app.<domain>`.

### 3.1 Thư, FCM và URL tải (P0-VPS-OPS)

Trước khi `up`, đặt trong `.env` trên VPS (hoặc kho bí mật của máy):

- `AG_SMTP_HOST`, `AG_SMTP_PORT` (587), `AG_SMTP_USERNAME`, `AG_SMTP_PASSWORD`, `AG_SMTP_FROM`, `AG_SMTP_TLS=starttls`; `AG_SMTP_DIGEST_TO` là các hộp thư công vụ cần nhận tóm tắt. Notify không cho gửi SMTP bản rõ (`AG_SMTP_ALLOW_PLAIN=false`). Reports dùng STARTTLS hoặc TLS theo `AG_SMTP_TLS`.
- Keycloak dùng cùng máy chủ thư; `AG_KC_SMTP_STARTTLS=true`, `AG_KC_SMTP_SSL=false`, `AG_KC_SMTP_AUTH=true`. Mặc định toàn stack dùng 587/STARTTLS; không chuyển riêng sang SMTPS 465 trước khi adapter Notify tương ứng đã được kiểm. Realm đã tồn tại cần cập nhật SMTP trong Realm settings → Email; import khi khởi động chỉ áp cho realm mới.
- `AG_FCM_PROJECT_ID` và `AG_FCM_CREDENTIALS_FILE=/run/secrets/notify/fcm.json`. Cài tệp tài khoản dịch vụ vào `deploy/compose/.vps/notify/fcm.json`, chủ `65532:65532`, mode `0400`; thư mục cho phép container duyệt. Chỉ API và worker mount thư mục này read-only. Không commit, chép vào image hoặc đưa nội dung tệp vào log. FCM chỉ truyền payload dữ liệu theo ADR-0015. Môi trường production cần `AG_NONPII_ALLOWLIST=fcm.googleapis.com,oauth2.googleapis.com` được phê duyệt theo [residency.md](residency.md); file VPS truyền giá trị được cung cấp, không tự mở ngoại lệ. Máy SMTP trong nước ngoài tên miền của dự án phải được thêm tường minh vào `AG_RESIDENCY_ALLOWLIST` cùng tên miền dự án và `single-label`; không dùng wildcard hoặc danh mục non-PII cho SMTP.
- `AG_WEB_OPS_URL=https://app.<domain>` và `AG_REPORT_LINK_BASE=https://api.<domain>` được file VPS cấp trực tiếp cho API/worker. Không dùng URL localhost trong email.

S3 I/O tiếp tục qua `seaweedfs:9000`; URL GET được **ký ngay từ đầu** bằng `AG_S3_PUBLIC_ENDPOINT=files.<domain>`, TLS bật và `AG_S3_REGION` mặc định `us-east-1`. Không thay host của URL sau khi ký. Caddy giữ nguyên Host tới SeaweedFS; chỉ cho GET/HEAD, bucket không chuyển sang công khai. Kiểm một URL do API cấp từ máy người dùng, tải được nội dung và URL bị sửa chữ ký phải bị từ chối. Không đưa URL có chữ ký vào issue/log.

### 3.2 Hạn mức và khóa đăng nhập

Keycloak giới hạn 1536 MiB, heap `-Xms256m -Xmx768m`. PostgreSQL 1536 MiB, API 768 MiB, worker 512 MiB và các dịch vụ khác đều có `mem_limit`/`cpus` cụ thể. Theo dõi `docker stats --no-stream` sau dựng và khi kiểm tải; OOM hoặc restart là lỗi cần xử lý, không tăng cap mà bỏ đo RAM tổng của VPS.

Realm mới dùng khóa **tạm 15 phút sau mỗi 5 lần sai**, `permanentLockout=false`; không tự biến lỗi nhập mật khẩu thành khóa hành chính. Quên mật khẩu hiện trên trang đăng nhập và gửi liên kết qua SMTP. Realm gắn luồng `agrigreen password recovery` chỉ đổi mật khẩu: **giữ nguyên credential TOTP**, không dùng nhánh `reset-otp` của luồng mặc định. Đăng nhập với mật khẩu mới vẫn cần OTP; khôi phục thiết bị MFA mất là quy trình quản trị riêng, không thuộc luồng này. Người bị Admin vô hiệu hóa vẫn phải được Admin mở lại. Realm đã nhập: cập nhật đúng các trường này trong Security defenses → Brute force detection và bật Forgot password và gắn luồng password-only tương ứng; không xóa volume/realm để nâng cấu hình.

Realm mới cũng khai báo scope `profile` mà web yêu cầu. `amr` đến từ execution mật khẩu/OTP thành công (`default.reference.value=pwd/otp`, `default.reference.maxAge=86400`), không phải claim gán cứng để vượt MFA. Realm cũ phải thêm scope/profile mapper và reference tương ứng cho browser/direct-grant execution; giữ nguyên điều kiện TOTP theo vai D24. Đăng xuất/đăng nhập lại trước khi kiểm token; không tắt MFA để khắc phục `invalid_scope`/`amr` trống.

## 4. Sao lưu bí mật — làm NGAY sau lần dựng đầu

Chép `deploy/compose/.env` ra kho bí mật ngoài VPS, ví dụ trình quản lý mật khẩu của công ty hoặc USB mã hóa cất két. Trong đó `AG_VAULT_KEK` và `AG_VAULT_BLIND_KEY` là khóa của kho định danh: **mất hai khóa này là mất vĩnh viễn dữ liệu định danh đã mã hóa**, dù còn bản sao lưu DB. Không gửi file này qua chat hay email.

## 5. Người dùng và dữ liệu tổng hợp

Keycloak chỉ nghe `127.0.0.1:8180` trên VPS. Các script dưới đây chạy **trên VPS**, qua SSH:

```sh
sh scripts/dev-users.sh          # tạo người dùng demo; mật khẩu ngẫu nhiên in ra MỘT lần — lưu lại ngay
```

Đăng nhập `https://app.<domain>` bằng người dùng quản trị và cấu hình TOTP. Sau đó nạp dữ liệu tổng hợp. Việc này cần **bật tạm** client `agrigreen-seed` và tắt lại ngay sau khi chạy xong:

```sh
. deploy/compose/.env
kc() { docker compose $C exec -T keycloak /opt/keycloak/bin/kcadm.sh "$@"; }
kc config credentials --server http://localhost:8080 --realm master --user admin --password "$KEYCLOAK_ADMIN_PASSWORD"
id=$(kc get clients -r agrigreen -q clientId=agrigreen-seed --fields id --format csv --noquotes)
kc update "clients/$id" -r agrigreen -s enabled=true

docker build --build-arg CMD=seed-synthetic -t agrigreen/seed-synthetic .
read -rs AG_SEED_KC_PASSWORD && export AG_SEED_KC_PASSWORD
docker run --rm --network host -e AG_SEED_KC_PASSWORD agrigreen/seed-synthetic \
  -kc-token-url http://127.0.0.1:8180/realms/agrigreen/protocol/openid-connect/token \
  -kc-user <người-dùng-quản-trị> -kc-otp <mã-6-số> -scale small

kc update "clients/$id" -r agrigreen -s enabled=false        # BẮT BUỘC
```

### 5.1 Nền dữ liệu khi dựng tenant mới

Không coi API healthy là đã có danh mục, cây tổ chức và ranh giới xã. Tạo tài khoản Admin nền tảng có `masterdata_admin`, `registry_admin`, `geo_admin` cùng TOTP; dùng token thật của tenant đích, lưu vào tệp riêng mode 600. Chuẩn bị GeoJSON ranh giới đã được phê duyệt, mỗi feature có `properties.code` là mã xã hiện hành. Mã An Giang hiện hành trong seed là **91** (89 là mã trước 01/07/2025).

```sh
python3 deploy/vps/bootstrap.py --token-file /run/user/1000/agrigreen-admin.token \
  --province 91 --boundaries /srv/approved/xa-an-giang.geojson \
  --source "Tên nguồn và phiên bản dữ liệu được phê duyệt"
```

Lệnh gọi API có kiểm quyền: nạp masterdata, xác nhận xã thuộc tỉnh, xem trước hình, tạo cây Quốc gia → Trung tâm tỉnh → Tổ KN xã rồi nhập/xem lại ranh giới. Không tạo HTX/nông hộ/thửa, không gọi route KN, không chép dữ liệu HTX. Khi thiếu hình hoặc hình bị từ chối, dừng để cung cấp tệp đúng; không tự vẽ hình chữ nhật cho dữ liệu thật. Chạy lại an toàn theo mã tổ chức và Idempotency-Key; nếu đã có tổ chức cùng mã nhưng khác đơn vị/cha thì dừng để kiểm tra.

Gắn tài khoản vào `org_ref` vừa tạo qua Admin API/Keycloak theo quy trình tài khoản; `tenant_id` phải là tenant đã provision, không UUID ngẫu nhiên cho mỗi lần chạy. Dữ liệu tổng hợp nghiệp vụ trong `seed-synthetic` là bước tùy chọn riêng, không thay bước nền này.

### 5.2 Bằng chứng kiểm thật

Workflow [vps-p0-smoke.yml](../../.github/workflows/vps-p0-smoke.yml) chạy `AG_P0_SMOKE_DISPOSABLE=1 sh scripts/test-vps-p0.sh` trên Linux có Docker. Nó dựng project/volume riêng, kiểm merged Compose/resource caps, Keycloak password+TOTP thật và claim AMR, bootstrap qua Admin API với hình **tổng hợp chỉ-CI**, hết hạn khóa tạm (probe 2 giây rồi phục hồi chính sách 900 giây), đổi mật khẩu qua email Mailpit (mật khẩu cũ bị từ chối, credential OTP giữ nguyên, mật khẩu mới vẫn cần OTP để có quyền Admin) và S3 ký host công khai qua Caddy thật. SMTP/FCM chỉ-CI không đi ra nhà cung cấp. Thiếu Docker hoặc lệnh hỏng không được ghi PASS; không dùng overlay `docker-compose.vps-smoke.yml` trên VPS thật. Khi smoke lỗi, chỉ lấy tối đa 80 dòng mỗi job migration và lọc bí mật trước khi dọn project; không in cấu hình hay log API/Keycloak. Không đọc/lọc an toàn được thì bỏ log bằng thông báo cố định, giữ mã thoát lỗi gốc.

Smoke này không thay nghiệm thu SMTP/FCM công vụ, DNS/TLS công khai, dữ liệu ranh giới thật và tải 500 CCU trên VPS. Ghi URL run CI và kết quả kiểm môi trường đích vào ticket #131 trước khi chốt; không đính kèm bí mật, token hoặc email reset.

## 6. Sao lưu dữ liệu

Bật `docker-compose.backup.yml` (pgBackRest) theo [backup.md](backup.md). Kho S3 đích phải nằm **ngoài VPS này** và ở trong nước, ví dụ kho lưu trữ đối tượng của VNPT hoặc một máy thứ hai. Mỗi quý chạy diễn tập theo [restore-drill.md](restore-drill.md).

## 7. Cập nhật phiên bản

**Cổng nâng PostgreSQL của P0-SEC-CI (#132):** image mới chuyển từ nền PostGIS 17-3.5/bullseye đã đóng băng sang 17-3.6/trixie. Dù cùng PostgreSQL major 17, thay glibc/PostGIS không cho phép gắn thẳng volume cũ rồi chạy tiếp. Phải logical dump toàn bộ database và globals, dựng **volume PGDATA mới**, restore, dựng lại/kiểm chỉ mục và extensions, đối chiếu số dòng rồi mới chuyển ứng dụng. Giữ volume cũ để quay lại khi chưa chuyển ghi. [Hướng dẫn đổi nền PostgreSQL](../dev/p0-postgres17-upgrade.md) của #132 là cổng bắt buộc; startup guard của image mới từ chối PGDATA cũ không có marker nền hiện hành. Không xóa volume cũ, không tự thêm marker để vượt guard và không dùng lệnh dưới đây với volume chưa chuyển đổi.


```sh
GIT_SSH_COMMAND="ssh -i ~/.ssh/agrigreen_deploy" git pull --ff-only
docker compose $C up -d --build --wait --wait-timeout 1200     # migrate-up tự chạy trước các dịch vụ
docker image prune -f
```

## 8. Giám sát tối thiểu

- Đĩa: `df -h /`, `docker system df`. Postgres, SeaweedFS và log tăng dần. Đặt cảnh báo khi dùng trên 80%.
- Log: `docker compose $C logs -f --since 10m api-platform worker`.
- OTel collector hiện chưa đẩy dữ liệu đi đâu. Muốn có chỉ số và cảnh báo (mục "Chỉ số và cảnh báo" trong [README.md](README.md)) thì nối collector vào hệ giám sát **trong nước**.

## Lưu ý

- **Đổi tên miền sau lần dựng đầu:** realm chỉ được nhập khi chưa tồn tại. Chạy lại `prepare.sh` rồi sửa Redirect URI và Web origins của client `agrigreen-web-ops` trong Keycloak. Web phải build lại (`up -d --build web-ops`) vì `NEXT_PUBLIC_*` được nhúng lúc build.
- **Không** chạy `docker compose down -v` trên VPS: lệnh này xóa volume `pgdata` và `objects`.
- **Chuyển sang dữ liệu thật:** đặt `AG_ENV=production` trong `.env`. Tiến trình sẽ dừng nếu có đích ngoài `AG_DOMAIN` hoặc ngoài dịch vụ nội bộ, xem [residency.md](residency.md). Trước đó phải xong rà soát bảo mật, sao lưu ngoài VPS, SMTP trong nước cho email kích hoạt, và xóa client `agrigreen-seed` khỏi realm.
