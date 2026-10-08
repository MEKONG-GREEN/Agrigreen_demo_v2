# Runbook: cổng tích hợp `integration-gateway` (W5.2, ADR-0033)

`cmd/integration-gateway` là Public API mở rộng cho mọi đối tác, gồm MKG ([ADR-0033](../adr/0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md) §1). Nó giữ nguyên các lớp của `api-public` ([ADR-0022](../adr/0022-public-api-ben-thu-ba.md)): khóa API HMAC có pepper theo phiên bản, giới hạn tần suất và `429`, sổ đo lường ghi trước khi trả byte, điểm kiểm soát xuất đóng khi lỗi. Cổng thêm một listener mTLS cho đối tác ghi khối lượng lớn.

`cmd/api-public` là **bí danh một kỳ**: cùng mã (`gateway.Run`), cùng vai DB, cùng biến. Chỉ khác tên đơn vị trong log, telemetry và `application_name`. Mỗi lần khởi động, bí danh log `event=deprecated_alias`.

## 1. Đơn vị, vai DB, luồng mạng

| Mục | Giá trị |
|---|---|
| Vai đăng nhập DB | `app_public` (ADR-0028, ADR-0033 §1): cùng đơn vị triển khai với `api-public`. Không có vai, bí mật, `userlist` PgBouncer hay khóa chart mới. `dbadmin.LoginUnits`: `Unit: integration-gateway`, `Aliases: [api-public]` |
| Secret | `pf-public-db-roles` (`AG_DB_PASSWORD_APP_PUBLIC`), `pf-apikey-pepper`, `pf-public-s3` (chỉ đọc). Thêm `pf-gateway-mtls` (tùy chọn) khi bật mTLS |
| Luồng vào | F08 Ingress → `:8085` (chỉ khóa API). F09 Ingress → `:8443` (mTLS, Ingress chuyển thẳng TLS) |
| Luồng ra | F19 PgBouncer pf, F29 kho đối tượng 443, F26 Valkey (khi cấu hình), DNS, OTLP. Không tới RabbitMQ, identity-vault, DB mkg/ts/vault, ứng dụng khác hay Internet (`check_flows.py` → `gateway_checks`) |
| Bản sao | 0 ở mọi hồ sơ cho tới khi chuyển host (mục 5) |

## 2. Biến môi trường

Tên `AG_GATEWAY_*` được ưu tiên. Tên `AG_PUBLIC_*` cũ vẫn đọc được trong một kỳ.

| Biến | Ý nghĩa |
|---|---|
| `AG_GATEWAY_ENABLED` (`AG_PUBLIC_API_ENABLED`) | Phải đúng `"1"`. Khác → thoát lỗi (đóng khi lỗi) |
| `AG_GATEWAY_HTTP_ADDR` (`AG_PUBLIC_HTTP_ADDR`) | Listener HTTP sau Ingress, mặc định `:8085` |
| `AG_GATEWAY_OPERATOR_TENANT` (`AG_PUBLIC_OPERATOR_TENANT`) | Tenant đơn vị vận hành, bắt buộc |
| `AG_GATEWAY_REPLICAS` (`AG_PUBLIC_REPLICAS`) | Số bản sao, dùng để chia giới hạn tần suất khi chưa có Valkey |
| `AG_GATEWAY_MTLS_ADDR` | Listener mTLS, vd `:8443`. Rỗng = tắt |
| `AG_GATEWAY_TLS_CERT`, `AG_GATEWAY_TLS_KEY` | Chứng thư máy chủ của listener mTLS |
| `AG_GATEWAY_CLIENT_CA` | CA **đối tác** ký chứng chỉ client. Không dùng CA dịch vụ nội bộ |
| `AG_GATEWAY_MTLS_BINDINGS` | Tệp JSON gắn chứng chỉ với khách hàng API. Bắt buộc khi bật mTLS |

Các biến còn lại giữ như `api-public`: `AG_PF_DB_URL`, `AG_APIKEY_PEPPER_*`, `AG_S3_*`, `AG_EGRESS_MODE`, `AG_VALKEY_URL`.

## 3. Bật mTLS cho đối tác ghi lớn

Quy tắc kiểm, do `gateway.MTLSAuthenticator` thực hiện:

- khóa API luôn bắt buộc;
- request có chứng chỉ client đã kiểm → chứng chỉ phải gắn với đúng khách hàng của khóa;
- request không có chứng chỉ → khách hàng `require_mtls` bị từ chối.

Mọi lần từ chối trả `401 apiclient.unauthenticated` và không ghi sổ đo lường. Log có `event=gateway.mtls_rejected` và `reason` là một trong `cert_unbound`, `cert_mismatch`, `mtls_required`.

1. **Khách hàng API** đã có (Admin API, bốn mắt, [public-api-onboarding.md](public-api-onboarding.md)). Ghi lại `client_id`.
2. **Chứng chỉ client:** đối tác gửi CSR. Người vận hành ký bằng CA đối tác (ngoại tuyến hoặc step-ca riêng, không dùng CA dịch vụ). Yêu cầu EKU `clientAuth`, hạn ≤ 825 ngày.
3. **Vân tay:** `openssl x509 -in client.crt -noout -fingerprint -sha256`. Có dấu `:` và chữ hoa cũng được.
4. **Tệp gắn** `bindings.json`. Khóa lạ, vân tay trùng giữa hai khách hàng, hoặc `require_mtls` mà không có chứng chỉ → cổng không khởi động.

   ```json
   {"version": 1, "bindings": [
     {"client_id": "<uuid>", "cert_sha256": ["<vân tay hiện tại>"], "require_mtls": true, "note": "MKG"}
   ]}
   ```

5. **Kho bí mật** `<remotePrefix>/tls/gateway-mtls`: `tls.crt`, `tls.key` (chứng thư máy chủ cho host của cổng), `client-ca.crt` (CA đối tác), `bindings.json`.
6. **Values:**
   - `externalSecrets.secrets.pf.pf-gateway-mtls.enabled: true`;
   - `apps.integration-gateway.env.AG_GATEWAY_MTLS_ADDR: ":8443"`;
   - `AG_GATEWAY_MTLS_BINDINGS: /certs/gateway/bindings.json`.

   Ingress phải chuyển thẳng TLS tới `integration-gateway:8443` (Traefik `IngressRouteTCP` theo SNI, `passthrough: true`). Route này **chưa có trong chart**; thêm khi bật đối tác đầu tiên.
7. **Kiểm:**
   - gọi `GET /v1/data-products` với chứng chỉ và khóa → `200`;
   - không chứng chỉ qua host HTTP → `401`;
   - có một dòng `pf_metering.usage_event` cho lời gọi thành công.

**Dev:** `docker compose --profile gateway up -d --build --wait` chạy cổng ở `127.0.0.1:8086`. Mặc định mTLS tắt. Bật thử bằng `AG_GATEWAY_MTLS_ADDR=:8443` cùng tệp gắn mount vào container. Kiểm thử tự động sinh PKI thử lúc chạy (`cmd/integration-gateway/internal/testpki`), nên repo không chứa khóa nào.

## 4. Xoay chứng chỉ client

1. Thêm vân tay mới vào `cert_sha256` của khách hàng, giữ vân tay cũ. Cập nhật kho bí mật. ESO đồng bộ Secret, Reloader khởi động lại Pod.
2. Đối tác chuyển sang chứng chỉ mới.
3. Xóa vân tay cũ.

Thu hồi khẩn cấp: xóa vân tay, hoặc thu hồi khóa API ở Admin API. Thu hồi khóa có hiệu lực ngay ở lời gọi kế tiếp và không cần khởi động lại.

## 5. Chuyển từ `api-public` và gỡ bí danh

Bản W5.2 giữ nguyên `api-public`: Deployment, image, `values-phasek.yaml`, host `partner.<domain>` → `api-public:8085`.

Ở bản phát hành kế tiếp:

1. Chồng `values-phasek.yaml`:
   - `apps.integration-gateway.replicas: 2`, `AG_GATEWAY_ENABLED: "1"`, `AG_GATEWAY_OPERATOR_TENANT`, `AG_EGRESS_MODE: enforce`;
   - `ingress.optionalHosts.partner.service: integration-gateway`;
   - `apps.api-public.replicas: 0`.

   Cập nhật `check_flows.py` (`public_api_checks` cho đơn vị mới, `PHASEK_ALLOWED`).
2. Theo dõi một chu kỳ:
   - `kube_deployment_status_replicas_available{deployment="integration-gateway"}`;
   - 5xx/429 ở host `partner`;
   - log `deprecated_alias` không còn.
3. Gỡ các thành phần của bí danh:
   - `cmd/api-public`;
   - `apps.api-public` cùng các luồng F27/F28 chuyển sang cổng;
   - image `api-public` trong hai ma trận CI;
   - dịch vụ compose `api-public`;
   - `Aliases` trong `dbadmin.LoginUnits`;
   - `unitAliases` trong `tools/modgen`;
   - `cmd/api-public` trong `tools/archlint` (`zoneRoots`, `noVaultRoots`, testdata);
   - dòng `api-public` của `app/PF/app.yaml`.

   Đổi tên biến `AG_PUBLIC_*` trong chart sang `AG_GATEWAY_*`.

Lùi bản: đặt lại `partner.service: api-public` và đổi số bản sao. Hai Deployment dùng chung Secret và vai DB, nên lùi bản không cần đổi bí mật.

## 6. Sự cố

| Triệu chứng | Kiểm |
|---|---|
| `CrashLoopBackOff` khi bật | Log khởi động. Nguyên nhân thường gặp: cờ bật ≠ `1`, thiếu tenant vận hành, pepper thiếu hoặc < 32 byte, `AG_GATEWAY_MTLS_ADDR` đặt nhưng thiếu `AG_GATEWAY_MTLS_BINDINGS`/chứng thư, tệp gắn sai (lỗi liệt kê từng dòng) |
| Đối tác mTLS nhận `401` | Log `gateway.mtls_rejected`. `mtls_required`: đối tác đi qua host HTTP thay vì `:8443`. `cert_unbound`: vân tay chưa có trong tệp gắn. `cert_mismatch`: chứng chỉ của khách hàng khác |
| Bắt tay TLS hỏng | Chứng chỉ không do CA trong `client-ca.crt` ký, hết hạn, hoặc thiếu EKU `clientAuth`. Ingress không chuyển thẳng TLS (khi Ingress kết thúc TLS, cổng không thấy chứng chỉ) |
| `5xx` sau khi qua mTLS | Lỗi kho định danh máy hoặc DB. Cổng đóng khi lỗi, không trả dữ liệu |
