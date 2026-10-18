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

## Feed theo cursor và tập dữ liệu dựng sẵn (W5.3)

ADR-0033 §2, ADR-0035. Kho feed thuộc `pf/apiclient` (schema `pf_apiclient`, pf 000126–000127): `feed_head`, `feed_entry` (chỉ ghi thêm, giữ 30 ngày), `dataset_version` (bất biến), `dataset_record`, `dataset_cell_state`.

**Dựng (cmd/worker, leader).** `-jobs apiclient_feed_build,apiclient_feed_retention` (hoặc `all`), cần `AG_PUBLIC_OPERATOR_TENANT`.

| Biến | Ý nghĩa |
|---|---|
| `AG_INTEGRATION_CONTRIBUTORS` | Tenant chủ dữ liệu đóng góp tập tổng hợp, phân tách bằng dấu phẩy. **Tạm** tới khi `pf/party` có cấp phép `grant_class = aggregate` (ADR-0035 §1). Rỗng → tập tổng hợp rỗng |
| `AG_INTEGRATION_BUILD_INTERVAL` | Chu kỳ dựng, mặc định `15m` (tối thiểu `1m`) |
| `AG_S3_*` / `AG_OBJSTORE_DIR` | Kho đối tượng cho ảnh chụp. Không có → phiên bản không có ảnh chụp (log `integration.snapshot_skipped`) |

Tập có nguồn: `admin_units` (bảng chiếu `k_ref.admin_unit` của tenant vận hành; kế thừa `basis_status = pending_c9`), `harvest_progress_by_commune`, `collection_volume_by_commune` (ô thô từ `pf/cropcycle`, quy tắc k áp ở `pf/apiclient`). k = tham số `publicdata.k_min` của tenant vận hành, chưa đặt → `k_default` của hợp đồng (10, tạm — C-4/K-8); k nhỏ hơn `k_default` → job từ chối (`integration.k_below_floor`). Không có thay đổi → không phiên bản mới. `season_harvest_dates`, `waterway_routes` chưa có nguồn (W5.5 / khi `pf/waterway` công bố phiên bản mạng).

**Phục vụ (cổng).** `GET /v1/feeds/{feed}?cursor=&limit=` và `GET /v1/snapshots/{dataset}/{version}`, scope `feed:<tên>:read` (đăng ký sản phẩm tên = tên feed, bốn mắt như sản phẩm dữ liệu). Hợp đồng `contracts/integration` còn `proposed`: production luôn trả 403 `integration.contract_not_servable`; dev/test/UAT đặt `AG_GATEWAY_FEED_ALLOW_PROPOSED=1` (cổng từ chối khởi động nếu đặt ở production). Mỗi trang và mỗi lần cấp URL ảnh chụp ghi sổ đo lường trước khi trả (URL ảnh chụp tính theo kích thước tệp, ≤ 15 phút, không lưu/ghi log).

**Sự cố.**
- `integration.record_rejected` trong log job: một bản ghi dựng ra lệch hợp đồng — cả phiên bản không ghi; sửa dữ liệu nguồn hoặc hợp đồng (bốn mắt), job chạy lại ở chu kỳ sau.
- 500 `apiclient.contract_violation` ở `GET /v1/feeds/*`: bản ghi trong kho lệch hợp đồng hiện hành (thường do đổi hợp đồng không tăng phiên bản) — không sửa kho (trigger chặn); phát phiên bản hợp đồng mới `v<N+1>`.
- Đối tác nhận 410 `integration.cursor_expired`: tải ảnh chụp ở `snapshot.href`, kiểm `checksum.sha256`, rồi đọc feed không tham số `cursor`.
- Kho feed không được xóa tay: chỉ `pf_apiclient.purge_feed_entries` (job) xóa được mục đã quá 30 ngày.

## W5.5 — MKG đọc danh bạ HTX/thửa qua feed

ADR-0033 §2, §5. Feed lớp `partner` `cooperative_directory`, `plot_directory` thay `/v1/partner/cooperatives`, `/plots`; chạy **song song** với bề mặt cũ một kỳ phát hành (W5.10 gỡ).

**Cấp quyền + khách hàng API (bốn mắt mỗi bước).**
1. Bên cấp (tenant chương trình) đề xuất + duyệt cấp quyền chia sẻ có loại `cooperative_directory`, `plot_directory` cho tenant MKG (Admin API `/v1/share-grants`, như ADR-0020) — thường đã có từ G3.
2. Đơn vị vận hành tạo khách hàng API `client_tenant_id` = tenant MKG, đăng ký `product = cooperative_directory` (scope `feed:cooperative_directory:read`) và `plot_directory` (scope `feed:plot_directory:read`), gói hạn mức, phát hành khóa; giao bí mật khóa qua kho bí mật vùng mkg.
3. Ngoài production, hợp đồng còn `proposed` cần `AG_GATEWAY_FEED_ALLOW_PROPOSED=1` ở cổng; production chờ A7 (hợp đồng `approved`).

**Dựng (cmd/worker).** Cùng job `apiclient_feed_build`: mỗi bên cấp đang có cấp quyền cho một bên nhận có đăng ký → một phân vùng; bản ghi còn hiệu lực phát lại mỗi ≤ 7 ngày. Không cấu hình thêm. Log `integration.dataset_built dataset=cooperative_directory …` mỗi bên cấp.

**MKG (cmd/api-internal).**

| Biến | Ý nghĩa |
|---|---|
| `AG_INTEGRATION_GATEWAY_URL` | Gốc cổng tích hợp (vd `https://partner.<domain>`) |
| `AG_FIELDOPS_FEED_API_KEY` | Khóa API của khách hàng API tenant MKG (bí mật — không log) |
| `AG_PARTNER_GRANTOR_TENANT` | Bên cấp (tham số `grantor`); rỗng → chỉ khi đúng một bên cấp |
| `AG_FIELDOPS_DIRECTORY_FEED_INTERVAL` | Chu kỳ job `fieldops_directory_feed`, mặc định `15m` |
| `AG_FIELDOPS_DIRECTORY_SOURCE` | `partner` (mặc định — bản sao cũ) \| `feed` — nguồn đối chiếu của lập/sửa lệnh gom. `feed` mà thiếu hai biến đầu → không khởi động |

Job `fieldops_directory_feed` (`AG_MKG_JOBS`, cần `AG_MKG_TENANT_ID`) kéo hai feed vào `mkg_fieldops.directory_feed_*` (mkg 000036) rồi đối soát với bản sao cũ (`directory_*_replica`, job `fieldops_directory`).

**Chuyển nguồn (một kỳ phát hành).**
1. Bật job feed, giữ `AG_FIELDOPS_DIRECTORY_SOURCE=partner`.
2. Theo dõi log `fieldops.directory_source_compare` (`outcome=match`) và `fieldops.directory_source_diff` (`kind` = `feed_missing` | `legacy_absent` | `mismatch`, `field`). Khác biệt kéo dài quá một chu kỳ dựng (15 phút) + một chu kỳ làm mới bản sao cũ → điều tra (cấp quyền khác nhau giữa hai đường, dữ liệu bên cấp đổi giữa hai lượt).
3. Khớp liên tục ≥ 14 ngày → đặt `AG_FIELDOPS_DIRECTORY_SOURCE=feed`, khởi động lại `api-internal`. Lùi: đặt lại `partner`.
4. Điều kiện W5.10 (ADR-0033 §7.3) thêm metric `ag_partner_legacy_calls_total` = 0 — xem mục kế tiếp.

**Sự cố.**
- Log job `outcome=revoked`: cổng trả 403 `party.share_grant_missing` — bên cấp thu hồi / cấp quyền hết hạn. Bản sao feed đã xóa; ở nguồn `feed` mọi lệnh gom mới bị từ chối `fieldops.htx_not_found`. Cấp lại → lượt sau đọc lại từ đầu.
- Log job `outcome=stale` / lệnh gom có cảnh báo `fieldops.directory_stale`: dữ liệu quá `max_age` (PT6H) — cổng ngừng, khóa hết hạn (401), hạn mức (429), hoặc job dựng phía nền tảng ngừng (as_of không tiến). Xem `last_error` trong `mkg_fieldops.directory_feed_state`.
- 410 `integration.cursor_expired`: bộ điều hợp tự xóa và đọc lại từ đầu trong cùng lượt (feed lớp `partner` không có ảnh chụp; phần còn giữ là trạng thái đầy đủ).
- Đồng bộ lại tay: xóa dòng trạng thái của feed (`DELETE FROM mkg_fieldops.directory_feed_state WHERE feed = …` dưới vai `r_fieldops`, tenant MKG) và bản ghi tương ứng — lượt sau đọc từ đầu. Bảng đệm, không phải nguồn sự thật.

## Ngừng dùng `/v1/partner/*` và danh sách cutover W5.10 (ADR-0033 §7.2–§7.4)

`api-platform` gắn vào MỌI phản hồi của route `/v1/partner/*` đã khớp (kể cả 401/403):

| Header | Giá trị |
|---|---|
| `Deprecation` | `@1791504000` (RFC 9745 — mốc W5.5, 09/10/2026 00:00 UTC; hằng `assembly.PartnerDeprecatedAt`) |
| `Sunset` | `AG_PARTNER_SUNSET` dạng HTTP-date (RFC 8594); **không đặt → không phát** |
| `Link` | `<AG_PARTNER_SUCCESSOR_BASE_URL/v1/feeds/<feed>>; rel="successor-version"` — `cooperatives[/:ref]` → `cooperative_directory`, `plots[/:ref]` → `plot_directory`, `season-harvest-dates` → `season_harvest_dates`, `waterway/route` → `waterway_routes`, `htx-collection/responses` → `bilateral` (feed cặp); `grants` không có kế nhiệm (không Link). Không đặt gốc URL → đường dẫn tương đối gốc (là đường dẫn trên cổng, không phải api-platform) |

| Biến (`api-platform`) | Ý nghĩa |
|---|---|
| `AG_PARTNER_SUNSET` | Ngày gỡ dự kiến `YYYY-MM-DD` (hoặc RFC 3339). **Không bắt buộc ở production**: điều kiện gỡ §7.3 là metric + một kỳ phát hành, không phải ngày — đặt khi đã chốt lịch W5.10, để đối tác thấy trước. Sai dạng hoặc không sau mốc ngừng dùng → không khởi động |
| `AG_PARTNER_SUCCESSOR_BASE_URL` | Gốc URL công khai của cổng (vd `https://partner.<domain>`); production bắt buộc `https` nếu đặt |

Metric: `ag.partner.legacy_calls{route, client}` → Prometheus `ag_partner_legacy_calls_total` (tên `partner_legacy_calls_total` của ADR). `route` = mẫu gin (`/v1/partner/plots/:ref`), `client` = tenant của token (bên nhận) hoặc `anonymous` (chưa xác thực). Đếm mọi lời gọi đã khớp route, cả bị từ chối. Truy vấn điều kiện gỡ:

```promql
sum(increase(ag_partner_legacy_calls_total[14d])) == 0
sum by (route, client) (increase(ag_partner_legacy_calls_total[1d]))   # ai còn gọi
```

**Danh sách cutover (đánh dấu từng mục trước khi mở gói W5.10):**
1. Đã qua **ít nhất một kỳ phát hành** kể từ W5.5 có header `Deprecation` (kiểm `curl -sI` một route `/v1/partner/*` ở production).
2. Đặt `AG_PARTNER_SUNSET` (≥ 30 ngày sau khi đặt) + `AG_PARTNER_SUCCESSOR_BASE_URL`; báo đối tác qua kênh quản trị.
3. MKG: `AG_FIELDOPS_DIRECTORY_SOURCE=feed` ổn định ≥ 14 ngày (mục W5.5 ở trên); job `fieldops_bilateral` (W5.6) chạy, van `valve_collection` không còn gửi mới.
4. `season_harvest_dates`, `htx_collection_responses` đã có đường feed phục vụ ở production (W5.5 ghi lệch: còn qua `/v1/partner/*`) — chưa có thì metric KHÔNG thể về 0, không gỡ.
5. `sum(increase(ag_partner_legacy_calls_total[14d])) == 0` (mọi `route`, mọi `client`, kể cả `anonymous`).
6. Đối soát tổng kiểm hai đường khớp (`GET /v1/feeds/{feed}/checksums` ↔ bản sao cũ; log `fieldops.directory_source_compare outcome=match`).
7. Gỡ (W5.10): route `/v1/partner/*` cùng `assembly.PartnerSuccessors`, client `mkg_sync` ở Keycloak, consumer `pf.cropcycle.htx_collection_valve`, biến `AG_PARTNER_SUNSET`/`AG_PARTNER_SUCCESSOR_BASE_URL`; giữ cảnh báo 410/404 của đối tác cũ trong một kỳ.

Lùi: trước bước 7 không có gì để lùi (header chỉ mang thông tin). Sau bước 7, khôi phục bằng triển khai lại phiên bản trước.
