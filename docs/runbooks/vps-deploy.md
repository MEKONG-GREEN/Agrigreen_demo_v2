# Deploy lên một VPS (VNPT hoặc nhà cung cấp trong lãnh thổ)

Toàn bộ nền tảng trên **một** máy bằng Docker Compose: `deploy/compose/docker-compose.yml` + ghi đè `deploy/compose/docker-compose.vps.yml`. Dùng cho demo và thử nghiệm với người dùng. Một VPS không có dự phòng nên **không** phải đích production của kiến trúc v3.0. CI (`compose-smoke`) dựng lại stack với file ghi đè này ở mỗi lần push.

## 0. Trước khi làm

| Việc | Ghi chú |
|---|---|
| Dữ liệu | Mặc định `AG_ENV=staging`, chỉ dùng **dữ liệu tổng hợp** (`cmd/seed-synthetic`). Dữ liệu thật cần rà soát bảo mật riêng, `AG_ENV=production` và bản sao lưu đặt ngoài VPS. |
| RB-7 | VPS phải ở trung tâm dữ liệu trong nước. Không đặt S3, sao lưu hay giám sát ở nhà cung cấp nước ngoài. |
| Cấu hình | Tối thiểu 4 vCPU, 8 GB RAM, 80 GB SSD. Build image lần đầu trên máy 4 vCPU mất khoảng 15–20 phút. |
| Hệ điều hành | Ubuntu 24.04 LTS. |
| Tên miền | Bốn bản ghi A trỏ về IP của VPS: `app.`, `api.`, `auth.`, `admin.` + `<domain>`. |
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

## 6. Sao lưu dữ liệu

Bật `docker-compose.backup.yml` (pgBackRest) theo [backup.md](backup.md). Kho S3 đích phải nằm **ngoài VPS này** và ở trong nước, ví dụ kho lưu trữ đối tượng của VNPT hoặc một máy thứ hai. Mỗi quý chạy diễn tập theo [restore-drill.md](restore-drill.md).

## 7. Cập nhật phiên bản

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
