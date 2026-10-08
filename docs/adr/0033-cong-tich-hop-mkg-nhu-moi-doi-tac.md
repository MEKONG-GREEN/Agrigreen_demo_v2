# ADR-0033: Cổng tích hợp `cmd/integration-gateway` — MKG như mọi đối tác, feed theo cursor, nạp theo lô, webhook mỏng

**Trạng thái:** Đề xuất — chờ MKG ký (A7, H-3) · **Ngày:** 04/10/2026 (gói W0.2) · **Phụ lục B v4.1:** mục 15 và 18 · **Xung đột:** X-02 (phần MKG), X-03, X-15, X-16 · **Sửa đổi:** [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md) §3–§5, [ADR-0022](0022-public-api-ben-thu-ba.md) §1 và §8, [ADR-0025](0025-ho-so-cap-phat-phase-k.md) (tên đơn vị, thời điểm bật) · **Thành phần:** `cmd/api-public` → `cmd/integration-gateway`, `pf/apiclient`, `pf/metering`, điểm kiểm soát xuất ([ADR-0024](0024-diem-kiem-soat-xuat-du-lieu-dlp.md)), `pf/party` (cấp phép), `contracts/integration/` (mới), `mkg/fieldops`, `mkg/hub`, `mkg/measurement` (bộ điều hợp), consumer van `pf.geo.hub_valve`, `pf.cropcycle.htx_collection_valve`, `pf.mrv.collection_evidence` · **Liên quan:** [ADR-0018](0018-su-kien-noi-bo-mkg.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), [ADR-0034](0034-luong-song-phuong.md), [ADR-0035](0035-cap-phep-tong-hop-theo-xa-nguong-k.md), kiến trúc v4.1 Phần VI (§37–§44)

## Bối cảnh

MKG hiện có **hai lối riêng** vào nền tảng, cộng một bề mặt thứ ba cho bên thứ ba (bản đối chiếu, dòng R-INT-01):

| # | Lối | Hiện thực | ADR |
|---|---|---|---|
| 1 | MKG → nền tảng | Van công bố: `valve_export` + outbox cụm mkg → RabbitMQ vhost `/` → consumer `pf.geo.hub_valve`, `pf.cropcycle.htx_collection_valve`, `pf.mrv.collection_evidence`; ghi vào tenant bên cấp bằng `tenant.Within` | ADR-0018 (vhost), ADR-0020 §4 |
| 2 | Nền tảng → MKG | `/v1/partner/*` trên **Application API** của `api-platform`, OIDC client credentials `agrigreen-mkg-sync`, vai `mkg_sync`; `mkg/fieldops` gọi qua `adapter.PlatformHTTP` | ADR-0020 §3, §5 |
| 3 | Bên thứ ba | `cmd/api-public` (`partner.<domain>`): khóa API HMAC có pepper theo phiên bản, scope, hạn mức + `429` (Valkey tùy chọn), mỗi lời gọi ghi `pf_metering`, điểm kiểm soát xuất `egress_event`, hợp đồng `contracts/dataproduct` mặc định từ chối, `202` + job cho việc dài; tắt ở ĐTK 1 | ADR-0022…0025 |

v4.1 R-INT-01: MKG đi **đúng cổng, đúng hợp đồng, đúng hạn mức, đúng xác thực** như mọi đối tác tương lai; không DB, không hàng đợi chung, không endpoint riêng. R-INT-02…11 thêm feed theo cursor giữ 30 ngày, nạp theo lô `202` + mã lô, vách ngăn theo đối tác, tập dữ liệu dựng sẵn, webhook mỏng, ngắt mạch, đối soát tổng kiểm, kênh tệp cùng lược đồ. R-API-07: không đường riêng theo ứng dụng.

Lối (3) là mầm của cổng: đã có xác thực máy, hạn mức theo khách, đo lường, kiểm soát xuất. Lối (1) và (2) là lối riêng. Lối (1) đúng chữ R-EVT-05 ("không vượt vùng qua broker trừ van") nhưng trái §38.1 và R-INT-01.

## Quyết định

### 1. Một cổng, mở rộng từ `cmd/api-public`

`cmd/api-public` đổi tên thành **`cmd/integration-gateway`** (X-15). Giữ mọi lý do tách tiến trình của ADR-0022 §1 (bề mặt tấn công, luồng mạng riêng, pepper chỉ ở đây, co giãn độc lập). Không dựng tiến trình thứ hai.

| Giữ nguyên từ G5-0 | Thêm |
|---|---|
| Khóa API lưu HMAC + pepper theo phiên bản (`pf/apiclient`) | **mTLS bắt buộc** với đối tác ghi khối lượng lớn, gồm MKG (§41.2); chứng chỉ gắn với khách hàng API, kiểm cả khóa lẫn chứng chỉ |
| Scope, hạn mức tần suất/khối lượng, `429` + `Retry-After` | Hạn mức **và** hàng đợi xử lý + DLQ riêng từng đối tác (R-INT-04); Valkey thành bắt buộc khi > 1 bản sao |
| Mỗi lời gọi ghi `pf_metering` trước khi trả byte (ADR-0023) | Đo cả lời gọi nạp và lần cấp URL ảnh chụp |
| Điểm kiểm soát xuất, đóng khi lỗi (ADR-0024) | Áp cho từng trang feed và mỗi lần cấp URL ảnh chụp |
| Hợp đồng mặc định từ chối, `contractcheck` | `contracts/integration/` cho feed và hợp đồng nạp (R-INT-08); `contracts/publication` dùng lại nguyên làm lược đồ nạp |
| RFC 9457, `Idempotency-Key`, `202` + `/v1/jobs/{id}` | `POST /v1/batches/{contract}` → `202` + mã lô; `GET /v1/feeds/{feed}?cursor=`; `GET /v1/snapshots/{dataset}/{version}`; đăng ký webhook |
| Tenant bên gọi lấy từ bản ghi khóa, không từ request | Như cũ; MKG là một khách hàng API với `client_tenant_id` = tenant MKG |

Định danh máy: v4.1 đặt ở `k/access`. Đến W7.2, khóa và chứng chỉ vẫn ở `pf/apiclient`; cổng gọi qua port `MachineIdentity` để W7.2 chuyển chủ mà không sửa cổng. Vai đăng nhập DB của cổng là `app_public` (ADR-0028).

Bề mặt quản trị (cấp khóa, chứng chỉ, đăng ký feed, webhook, hạn mức) vẫn ở Admin API của `api-platform`, MFA, bốn mắt như ADR-0022.

### 2. Feed theo cursor (R-INT-05, R-INT-06)

- **Kho feed** chỉ ghi thêm, chủ là `pf/apiclient` (schema `pf_apiclient`, không thêm module): `(feed, seq bigint, entity_key, event_id, dataset_version, payload, recorded_at)`. `seq` tăng đơn điệu trong mỗi feed; cursor = `seq` mã hóa mờ.
- Module chủ dữ liệu **không** ghi thẳng: job dựng tập dữ liệu của module chủ (hoặc job `r_agg`, ADR-0031) gọi port của `pf/apiclient` để nối thêm một phiên bản. Tính một lần, phát cho mọi đối tác có quyền; không lọc từng hàng theo đối tác, trừ feed song phương (ADR-0034).
- Giao ít nhất một lần, có thứ tự theo `entity_key`; bên nhận idempotent theo `event_id`.
- Giữ **30 ngày**. Cursor quá hạn → `410` `integration.cursor_expired` kèm liên kết ảnh chụp mới nhất; đối tác đồng bộ lại từ ảnh chụp rồi đọc tiếp.
- **Ảnh chụp lớn** (vd. `waterway_routes`, `admin_units`) ở kho đối tượng trong lãnh thổ, phục vụ bằng **URL ký có hạn** (≤ 15 phút, một khách hàng). Đây là **sửa ADR-0022 §8** (cấm URL ký cho bên thứ ba): URL chỉ được cấp qua cổng, **sau** khi ghi `pf_metering` theo kích thước ảnh chụp và qua điểm kiểm soát xuất, nên không vòng qua sổ đo lường. Việc xuất theo job của ADR-0022 giữ cách phát luồng qua cổng.

Feed đầu tiên (chi tiết hợp đồng ở W5.1):

| Feed | Loại (§38.1) | Thay cho |
|---|---|---|
| `harvest_progress_by_commune` | cấp phép, tổng hợp theo xã, ngưỡng k (ADR-0035) | — (mới) |
| `season_harvest_dates` | cấp phép | `GET /v1/partner/season-harvest-dates` |
| `cooperative_directory`, `plot_directory` | cấp phép (tập dữ liệu theo bên cấp) | `GET /v1/partner/cooperatives`, `/plots` |
| `waterway_routes` | tham chiếu, theo phiên bản | — (mới) |
| `admin_units` | tham chiếu dùng chung (ADR-0032) | — (mới) |
| feed của cặp | song phương (ADR-0034) | `GET /v1/partner/htx-collection/responses` |

### 3. Nạp theo lô (R-INT-03, §41.3)

Nhận lô → kiểm lược đồ đồng bộ → `202` + mã lô → hàng đợi riêng đối tác → **inbox** → kiểm hợp đồng (mặc định từ chối) → port của module chủ → kết quả từng bản ghi vào lô. Bản ghi lỗi không làm hỏng cả lô. Idempotent theo `Idempotency-Key` và khóa nghiệp vụ.

Hợp đồng nạp dùng lại `contracts/publication/<tên>.json` (`hub_facility`, `hub_capacity`, `mrv_collection_evidence`): trường, phiên bản, người đề xuất ≠ người duyệt giữ nguyên. Phía MKG, sổ `valve_export` vẫn là bằng chứng (RB-1); chỉ đổi đường gửi.

Ghi vào tenant bên cấp theo cấp phép như ADR-0020 §4 (`ValveTargets`, `tenant.Within` sau kiểm cấp phép) — logic chuyển từ consumer van sang bộ xử lý inbox của cổng, không viết lại.

### 4. Webhook mỏng, ngắt mạch, đối soát (R-INT-07, R-INT-09, R-INT-10)

- Thân webhook chỉ có `{feed, cursor, occurred_at}`; chữ ký HMAC-SHA256 kèm dấu thời gian trong header; không bao giờ có dữ liệu nghiệp vụ.
- N lần lỗi liên tiếp (mặc định 5, tham số theo đối tác) → ngắt, báo đối tác qua kênh quản trị; không mất dữ liệu vì đối tác đọc tiếp từ cursor.
- Mỗi phiên bản tập dữ liệu và mỗi ngày của luồng song phương có tổng kiểm (số bản ghi + băm) đọc qua `GET /v1/feeds/{feed}/checksums`.

### 5. Chuyển MKG sang cổng

| Hiện tại | Sau | Gói |
|---|---|---|
| Client OIDC `agrigreen-mkg-sync`, vai `mkg_sync` | Khách hàng API tenant MKG, khóa + mTLS, scope = loại cấp phép + hợp đồng nạp | W5.2 |
| `adapter.PlatformHTTP` gọi `/v1/partner/*` trên `api-platform` | Bộ điều hợp feed trong `mkg/fieldops` kéo `/v1/feeds/*` trên cổng; lưu `as_of`, `max_age`; bản sao cục bộ như hiện nay (R-INT-02) | W5.5 |
| Van `hub_facility`, `hub_capacity` qua RabbitMQ → `pf.geo.hub_valve` | `mkg/hub` gửi lô từ `valve_export` tới `POST /v1/batches/hub_facility` … | Sau W5.4, khi hợp đồng nạp tương ứng có |
| Van `collection_schedule`, `htx_collection_statement` → `pf.cropcycle.htx_collection_valve` | Luồng song phương (ADR-0034) | W5.6 |
| Van `mrv_collection_evidence` → `pf.mrv.collection_evidence` | Hợp đồng nạp `mrv_collection_evidence` | Sau W5.4 |
| NetworkPolicy: vùng mkg → `api-platform` | Vùng mkg → **chỉ** cổng; gỡ luồng tới `api-platform` (ADR-0008, `check_flows.py`) | W5.10 |

Sổ cấp phép của ADR-0020 (`share_grant_event`, bốn mắt, thu hồi, `partner_read_log`) **giữ nguyên** và là nguồn quyền của cổng: `grant_class = partner` (ADR-0030), `kinds` ánh xạ sang feed và hợp đồng nạp. Thêm loại mới vẫn theo ADR-0020 §2 (sửa ADR, `ShareKinds`, migration CHECK).

### 6. Ghi lệch có hạn: van RabbitMQ (X-03)

- Van RabbitMQ vhost `/` **giữ cho ĐTK 1** đến **cổng G2 của v4.1** (tên cổng chờ H-7). Không thêm hợp đồng van mới qua RabbitMQ: hợp đồng mới đi thẳng hợp đồng nạp.
- `contractcheck` giữ các kiểm hiện có (`//ag:publication`, payload kind `platform`) cho tới khi consumer cuối cùng bị gỡ.
- vhost `mkg.internal` (ADR-0018) không bị ảnh hưởng: đó là sự kiện nội bộ vùng mkg.

### 7. Cửa sổ ngừng dùng (R-API-01, X-16)

1. W5.2: cổng chạy song song; `/v1/partner/*` không đổi.
2. W5.5: MKG đọc feed. Mọi phản hồi `/v1/partner/*` thêm `Deprecation` và `Sunset` (ngày gỡ dự kiến); metric `partner_legacy_calls_total` theo route.
3. Điều kiện gỡ (W5.10): đã qua **ít nhất một kỳ phát hành** từ W5.5 **và** metric bằng 0 trong 14 ngày liên tiếp **và** đối soát tổng kiểm hai đường khớp.
4. W5.10 gỡ `/v1/partner/*`, client `mkg_sync`, consumer `pf.cropcycle.htx_collection_valve`. `hub_valve`, `mrv` gỡ khi hợp đồng nạp tương ứng chạy; muộn nhất G2 v4.1.
5. `cmd/api-public` giữ làm bí danh (cùng nhị phân, tên Deployment cũ) **một kỳ** để chart, DNS `partner.<domain>` và tài liệu đối tác chuyển.

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Giữ hai lối riêng cho MKG, cổng chỉ cho bên thứ ba (hiện trạng) | Trái R-INT-01; mỗi đối tác sau phải xây lại; MKG là đối tác tải lớn nhất nên là phép thử tốt nhất của cổng |
| Dựng cổng mới, để `api-public` song song | Hai nơi giữ khóa API, hai pepper, hai luồng mạng; nhân đôi metering và DLP |
| Mở rộng `/v1/partner/*` trên `api-platform` cho mọi đối tác | Application API là bề mặt người dùng (OIDC); mọi handler nằm trong tiến trình nghe đối tác — lý do ADR-0022 §1 tách `api-public` |
| Đẩy dữ liệu qua webhook | Bão giao hàng, mất thứ tự, dữ liệu nằm trong nhật ký lỗi của bên nhận (R-INT-07) |
| Gỡ van RabbitMQ ngay | Van đang chạy cho ĐTK 1 và có kiểm thử đầu–cuối; nạp theo lô chưa có. Chuyển cùng lúc với mốc 15/12/2026 làm tăng rủi ro đường găng |
| Không cấp URL ký (giữ ADR-0022 §8) | Ảnh chụp chục MB × 200 đối tác qua API là sóng tải §39 rủi ro 4; URL cấp qua cổng sau đo lường giữ được lý do của §8 |

## Hệ quả

- **Cổng chạy từ ĐTK 1** (không chờ Phase K) vì MKG dùng nó: `AG_PUBLIC_API_ENABLED` (đổi tên `AG_GATEWAY_ENABLED`, giữ tên cũ một kỳ) bật, số bản sao ≥ 2, host ingress riêng. Sửa ADR-0025: Phase K chỉ còn bật **đối tác thứ ba**, không bật cổng.
- MKG mất đặc quyền đọc theo vai `mkg_sync`; mọi lần đọc của MKG có metering và sổ xuất như bên thứ ba.
- Chi phí: mỗi trang feed một giao dịch ghi `pf_metering`. §39 ước 3,3 yêu cầu/giây cho 200 đối tác — đo ở W5.9.
- Thu hồi cấp phép có hiệu lực ngay ở lời gọi kế tiếp (kiểm tại cổng, như `/v1/partner/*`).
- Vùng mkg không còn gọi `api-platform`; một luồng mạng ít hơn.
- Cổng nhận ở vùng nội bộ cho VFT (§38.3) dùng chung mã, triển khai và dữ liệu riêng (R-MOD-10) — ngoài phạm vi ADR này.
- ADR-0020 giữ thân bài; §3–§5 thay bằng ADR này khi ký. ADR-0022 §1 (tên), §8 (URL ký) sửa. ADR-0024 không đổi nguyên tắc; danh sách điểm kiểm soát thêm feed và ảnh chụp.

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| W5.1 | `contracts/integration/` (feed, hợp đồng nạp, song phương), `contractcheck` mở rộng | — |
| W5.2 | `cmd/integration-gateway` từ `cmd/api-public`, mTLS, port `MachineIdentity`, bí danh, NetworkPolicy, chart | — |
| W5.3 | Kho feed, job dựng tập dữ liệu, ảnh chụp + URL ký, `GET /v1/feeds/*` | pf 000126–000127 |
| W5.4 | Nạp theo lô, hàng đợi + DLQ từng đối tác, inbox | pf 000125 |
| W5.5 | Bộ điều hợp feed `mkg/fieldops`, `as_of`/`max_age`, header `Deprecation`/`Sunset` | mkg 000024 |
| W5.6 | Luồng song phương (ADR-0034) | pf 000103, mkg 000025 |
| W5.7 | Webhook mỏng, ngắt mạch, tổng kiểm | pf 000104 |
| W5.8 | Kênh tệp cùng lược đồ, môi trường thử đối tác | pf 000128 |
| W5.9 | Kiểm thử tải §42 | — |
| W5.10 | Gỡ `/v1/partner/*`, `mkg_sync`, consumer `htx_collection_valve` | — |
| W0.3 | `docs/design/mkg.md` §1.1 "bốn cửa" theo ADR này | — |

### W5.1 (08/10/2026) — hợp đồng, chưa có mã chạy

- **`contracts/integration/`** ([README](../../contracts/integration/README.md)): package Go `integration` (nhúng hợp đồng, `Get`/`All`/`Servable`/`ValidateRecord`/`EntityKeyOf`/`CheckExample`/`EnvelopeFor`) dùng lại `publication.Node` và `Validate` (mặc định từ chối). Phong bì `envelope/feed_page.v1` (`cursor`, `next_cursor`, `as_of`, `max_age`, `dataset` có `k_used` + tổng kiểm, `snapshot.href` không chứa URL ký, `items[]` có `seq`, `event_id`, `entity_key`, `op` upsert/retract) và `envelope/ingest_batch.v1` (`batch_ref` = `Idempotency-Key`).
- **Feed:** `harvest_progress_by_commune`, `collection_volume_by_commune` (ADR-0035 W5.1), `season_harvest_dates`, `waterway_routes`, `admin_units` (có hiệu lực `[)` + `successors` với `basis_status`, ADR-0032). **Nạp song phương:** `collection_schedule` (lược đồ dùng nguyên hợp đồng van `collection_schedule`, ADR-0033 §3), `roll_count_statement` (`replaces: publication/htx_collection_statement`), `htx_roll_confirmation` (`author_side: b`). Mỗi hợp đồng có mẫu ở `examples/`.
- Tất cả `status: proposed` với `pending` (A7; C-4/K-8 cho k; C-9 cho `admin_units`; C-12 cho song phương) — `Servable(production)` = false. k là tham số `aggregation.k_param = publicdata.k_min`, `k_default` tạm 10.
- **`contractcheck`** (`tools/contractcheck/integration.go`): siêu dữ liệu, tên/phiên bản/kind khớp tệp, `publication`/`replaces` có thật, khóa trỏ trường có thật, mẫu khớp phong bì + hợp đồng (khóa lạ, kiểu, `entity_key`, `max_age`, `k_used ≥ k`), mẫu mồ côi, `-base`: xóa/đổi kiểu/thêm required → phiên bản mới.
- **Ghi lệch:** chưa sửa `contracts/openapi` (route `/v1/feeds`, `/v1/batches` thuộc W5.2–W5.4); feed của cặp không có hợp đồng riêng — dùng phong bì `feed_page` với `items[].contract`. `cooperative_directory`, `plot_directory` (bảng mục 2) chưa có hợp đồng — thêm khi W5.5 chuyển `/v1/partner/cooperatives`, `/plots`.

### W5.2 (09/10/2026) — cổng tích hợp

- **Tiến trình:** `cmd/integration-gateway` — `main` gọi `gateway.Run`; gốc lắp ráp chuyển nguyên từ `cmd/api-public/assembly` sang `cmd/integration-gateway/assembly` (cùng `integrationPorts`, một pool `app_public`). Handler = router Public của `pf/apiclient` bọc `gateway.WithPeerCert`; khóa API HMAC + pepper, giới hạn tần suất + `429`, sổ đo lường trước byte (ADR-0023), điểm kiểm soát xuất (ADR-0024) giữ nguyên vì không đổi đường ống. Feed/lô (W5.3/W5.4) thêm route vào cùng router nên kế thừa đủ các lớp này; W5.2 không thêm route.
- **mTLS (§41.2):** listener thứ hai `AG_GATEWAY_MTLS_ADDR` (vd `:8443`, rỗng = tắt), `RequireAndVerifyClientCert` với CA **đối tác** `AG_GATEWAY_CLIENT_CA` (không phải CA dịch vụ nội bộ). Vân tay SHA-256 của chứng chỉ lá **đã kiểm** đi vào context; không bao giờ đọc header. `gateway.MTLSAuthenticator` bọc `port.Authenticator`: khóa luôn bắt buộc; có chứng chỉ → phải gắn với đúng khách hàng của khóa; không chứng chỉ → khách hàng `require_mtls` bị từ chối. Từ chối = `401 apiclient.unauthenticated` (không ghi sổ, log `gateway.mtls_rejected` với `reason` `cert_unbound`/`cert_mismatch`/`mtls_required`); lỗi kho định danh máy → 5xx, không bao giờ 200.
- **Định danh máy:** interface `MachineIdentity` do **cổng khai** (bên tiêu thụ — cổng là `cmd`, không thuộc tầng module). Bộ điều hợp hiện có: `StaticIdentity` đọc tệp JSON `AG_GATEWAY_MTLS_BINDINGS` (`version: 1`, `bindings[]: client_id, cert_sha256[], require_mtls, note`; khóa lạ, vân tay trùng giữa hai khách hàng, `require_mtls` không chứng chỉ → không khởi động). Tệp ở kho bí mật (`tls/gateway-mtls`), không có migration. W7.2 (`k/access` = `pf/accounts`) thay bằng cổng đọc DB mà không sửa `gateway`.
- **Vai DB:** giữ `app_public` (mục 1) — integration-gateway là CÙNG đơn vị triển khai với api-public cũ: `dbadmin.LoginUnits` đổi `Unit` sang `integration-gateway`, `Aliases: [api-public]`; không vai, bí mật, `userlist` PgBouncer hay khóa chart mới. `modgen` ghép bí danh bằng bảng `unitAliases` (bí danh = module của đơn vị đích).
- **Bí danh một kỳ (mục 7.5):** `cmd/api-public` là `main` mỏng gọi `gateway.Run` với tên đơn vị `api-public` (log `event=deprecated_alias`). Biến `AG_GATEWAY_ENABLED/HTTP_ADDR/OPERATOR_TENANT/REPLICAS` thắng tên `AG_PUBLIC_*` cũ (vẫn đọc). Image `agrigreen-api-public`, Deployment `api-public`, `values-phasek.yaml` và host `partner.<domain>` không đổi ở bản này.
- **Triển khai:** CI build/quét/ký `integration-gateway` cạnh `api-public`; compose `--profile gateway` (host `:8086`); chart `apps.integration-gateway` 0 bản sao, `pf-public-db-roles` + `pf-apikey-pepper`, Secret tùy chọn `pf-gateway-mtls` (`certs[].optional`); NetworkPolicy dùng chung F08/F19/F26/F29 (`apps: [api-public, integration-gateway]`) và thêm **F09** Ingress → `:8443` (Ingress chuyển thẳng TLS); `check_flows.py` thêm `gateway_checks`. archlint `zoneRoots`/`noVaultRoots` có cả hai gốc.
- **Ghi lệch / còn lại:** route Traefik chuyển thẳng TLS (IngressRouteTCP theo SNI) cho F09 và host riêng của cổng chưa có trong chart — làm khi bật đối tác ghi lớn đầu tiên; F27/F28 (scrape, OIDC) mới chỉ cho `api-public`; Valkey vẫn chưa có adapter (bắt buộc khi > 1 bản sao theo mục 1 — chờ gói hạn mức W5.4). Chuyển host `partner` sang `integration-gateway`, gỡ `cmd/api-public` theo [runbook](../runbooks/integration-gateway.md).

### W5.3 (09/10/2026) — kho feed và tập dữ liệu dựng sẵn

- **Kho (pf 000126–000127, `pf/apiclient`):** `feed_head` (seq mỗi feed × phân vùng, cấp dưới khóa dòng nên thứ tự commit = thứ tự seq; `purged_through`), `feed_entry` (bảng THƯỜNG, không phải sổ: trigger chặn UPDATE/TRUNCATE và xóa mục chưa đủ 30 ngày; job `apiclient_feed_retention` gọi `pf_apiclient.purge_feed_entries`), `dataset_version` (bất biến), `dataset_record` (trạng thái đã phát — chỉ nối upsert khi đổi, retract khi biến mất), `dataset_cell_state`. Chọn bảng thường + job dọn thay vì phân vùng + `DROP PARTITION` vì job chạy dưới `r_apiclient` (không DDL) và lượng §38.3 nhỏ — xem lại ở W5.9.
- **Phục vụ:** `GET /v1/feeds/{feed}?cursor=&limit=&contract_version=` (phong bì `feed_page.v1`, cursor = `seq` mã hóa mờ, 410 `integration.cursor_expired` kèm `snapshot`), `GET /v1/snapshots/{dataset}/{version}` (URL ký ≤ 15 phút cấp SAU sổ đo lường, đo theo kích thước ảnh chụp; DB chỉ lưu khóa đối tượng). Cùng đường ống `handle` (khóa HMAC, hạn mức, metering, DLP — `data_tenant` = đơn vị vận hành). Mọi bản ghi qua `ValidateRecord` khi job ghi và khi cổng trả, cả trang qua phong bì. Hợp đồng `proposed`: production 403 `integration.contract_not_servable`; ngoài production cần `AG_GATEWAY_FEED_ALLOW_PROPOSED=1` (production từ chối khởi động nếu đặt). Scope `feed:<tên>:read`, đăng ký sản phẩm tên = tên feed.
- **Ghi lệch / còn lại:** feed lớp `partner` (`season_harvest_dates`, phân vùng theo bên cấp + sổ đọc K-05) và feed của cặp chưa phục vụ (W5.5/W5.6, cột `partition_key` đã có); `waterway_routes` chưa có nguồn; `admin_units` kế thừa từ `k_ref.admin_unit.successor_codes` với `basis_status = pending_c9` (căn cứ chính thức khi `pf/masterdata` công bố kế thừa qua port); `GET /v1/feeds/{feed}/checksums` thuộc W5.7.

### W5.4 (09/10/2026) — nạp theo lô

- **Chủ:** `pf/apiclient` (như kho feed ở mục 2 — không thêm module), gói con `internal/platform/apiclient/ingest`; route `POST /v1/batches/{contract}`, `GET /v1/batches/{id}` trong router Public (`public/batches.go`) nên kế thừa khóa API [+ mTLS], giới hạn tần suất, sổ đo lường trước byte. Migration **pf 000125** (số 000102 của kế hoạch đầu đã lỗi thời): sổ chỉ ghi thêm `ingest_batch` (thân lô jsonb, idem `batch:<client>:<batch_ref>`), `ingest_record_result`, `ingest_batch_outcome` (`completed` | `dead_lettered`), `ingest_staging`; RLS theo **tenant đối tác** (`client_tenant_id` từ bản ghi khóa); chỉ `r_apiclient` có quyền.
- **Quyền:** scope `ingest:<hợp đồng>:write`, cấp qua đăng ký có `product` = tên hợp đồng nạp (Admin API chấp nhận hợp đồng `contracts/integration/ingest`). Kiểm hợp đồng mặc định từ chối: không có / phiên bản lạ / hợp đồng feed → 404 `integration.contract_unknown`; `retired`, `approved` còn `pending`, `proposed`/`draft` không cờ dev/test → 403 `integration.contract_not_servable`; production + cờ `AG_GATEWAY_INGEST_ALLOW_PROPOSED` → cổng không khởi động; chưa nối port chủ → 403 `integration.no_owner_port`.
- **Đồng bộ (rẻ):** phong bì `ingest_batch.v1`, `contract` = path, `batch_ref` = `Idempotency-Key`, `max_batch` → 413; độ sâu hàng đợi của chính đối tác ≥ `AG_GATEWAY_INGEST_MAX_QUEUED` → 429 `integration.queue_saturated` + `Retry-After` (lô không ghi). Gọi lặp cùng thân → cùng mã lô (`Idempotent-Replayed`), phát lại nếu chưa có kết cục; khác thân → 422.
- **Hàng đợi (R-INT-04):** RabbitMQ `AG_AMQP_URL`: quorum queue `pf.apiclient.ingest.<client_id>` (`x-delivery-limit` 5, `x-max-length`, `x-overflow reject-publish`) + DLQ `.dlq` qua `ag.dlx`, phát thẳng qua exchange mặc định (không phải sự kiện công bố, không qua `ag.events`); mỗi đối tác một kênh tiêu thụ trong tiến trình cổng (`inbox.ConsumeWith`). Không có `AG_AMQP_URL`: dev/test dùng hàng đợi trong tiến trình cùng ngữ nghĩa; production tắt `/v1/batches` (503).
- **Xử lý (§41.3):** inbox (`shared.inbox`, một lần về hiệu quả) → kiểm lại hợp đồng → `ValidateRecord` → `entity_key` → khóa nghiệp vụ → port module chủ (`ingest.RecordSink`, do bên tiêu thụ khai, nối ở `assembly.Config.IngestOwners`) → kết quả từng bản ghi; problem 4xx của port = từ chối bản ghi; lỗi khác = lỗi tạm → giao lại → DLQ + kết cục `dead_lettered`.
- **Song phương (ADR-0034, C-12):** port chủ chưa có → `ingest.Staging` ghi `pf_apiclient.ingest_staging`, idempotent theo `(hợp đồng, phiên bản, khóa nghiệp vụ)` (khác nội dung → `integration.business_key_conflict`). **W5.6** nối port thật (pf_coop/pf_cropcycle) và chuyển dữ liệu đệm; van `valve_collection` của MKG không đổi (W5.10).
- **Ghi lệch:** sổ đo lường dùng `call_kind` có sẵn (`export_request` cho POST — 0 đơn vị, `job_status` cho GET; `product` = hợp đồng, `job_id` = mã lô) vì thêm loại `ingest` cần migration `pf_metering`; đơn vị tính cho lô nạp chờ W5.9 đo tải. Bộ chuyển RabbitMQ chưa có kiểm thử với broker thật (repo không có broker trong kiểm thử — như `inbox`); hàng đợi trong tiến trình được kiểm đầu–cuối. Tài khoản RabbitMQ của cổng (`rabbitmq.users`) chưa thêm vào chart.

### W5.7 (09/10/2026) — webhook mỏng, ngắt mạch, đối soát

- **Chủ:** `pf/apiclient`, gói con `internal/platform/apiclient/webhook`; migration **pf 000130** (số 000104 của kế hoạch đầu đã lỗi thời): sổ `webhook_event` (registered, secret_rotated, disabled, enabled, breaker_reset) + bảng chiếu `webhook_current`, `webhook_breaker`, `webhook_cursor` (seq đã báo mỗi webhook × feed, số lần thử, mốc thử kế), sổ `webhook_delivery` (mỗi lần giao: kết cục, mã HTTP, lớp lỗi, độ trễ, trạng thái ngắt sau đó — không thân phản hồi); chỉ `r_apiclient`; tenant đơn vị vận hành. Thêm chỉ mục `ingest_staging (tenant_id, contract, event_time)` cho đối soát ngày.
- **Đăng ký (Admin API, MFA, `api_client_admin`):** URL qua `webhook.Guard`: `https`, cổng 443/8443, không userinfo/fragment, không tên nội bộ, IP literal và mọi địa chỉ phân giải ngoài dải cấm; tùy chọn danh sách tên miền `AG_WEBHOOK_ALLOW_HOSTS`. Feed phải có scope `feed:<tên>:read` đã duyệt; ngưỡng N (mặc định 5, 1–100) theo webhook; tối đa 5 webhook active mỗi khách hàng. **Không bốn mắt** (ghi lệch so với "bốn mắt như ADR-0022"): webhook không mang dữ liệu, quyền dữ liệu đã duyệt bốn mắt ở scope feed.
- **Bí mật:** không lưu — `HMAC-SHA256(AG_WEBHOOK_KEY_V<k>, "agrigreen.webhook.v1:<webhook>:<phiên bản>:<nonce>")`, sổ chỉ có nonce + phiên bản khóa chủ; hiện `whsec_…` một lần (bản lưu Idempotency-Key không có bí mật). Xoay: bí mật cũ ký thêm 24 giờ (hai `v1`).
- **Giao (job `apiclient_webhook_dispatch`, leader `cmd/worker`, mặc định mỗi 30 giây):** so `feed_head.last_seq` với seq đã báo; thân CHỈ `{feed, cursor, occurred_at}` (cursor của mục mới nhất — gộp nhiều mục thành một chỉ báo); header `AG-Webhook-Id`, `AG-Webhook-Timestamp`, `AG-Webhook-Signature: t=<giây>,v1=<HMAC(t + "." + thân)>`. Client: `Dialer.Control` kiểm địa chỉ thật lúc kết nối (chống DNS rebinding), không proxy, không theo chuyển hướng, TLS ≥ 1.2, timeout 10 giây; 8 webhook song song. Lỗi → lùi hàm mũ có nhiễu (10 giây → 10 phút); N lỗi liên tiếp → mở mạch; sau thời gian chờ (`AG_WEBHOOK_BREAKER_COOLDOWN`, mặc định 5 phút, ×2 mỗi lần thử lỗi, trần 1 giờ) đúng MỘT lần thử half-open; thành công → đóng. Metric `ag.apiclient.webhook.deliveries{outcome,error_class}`, `ag.apiclient.webhook.delivery.duration`, `ag.apiclient.webhook.breaker.transitions{to}`; log `apiclient.webhook_breaker_opened` (cảnh báo vận hành = "báo đối tác qua kênh quản trị").
- **Trạng thái:** Admin API `GET /v1/api-clients/{id}/webhooks` (ngắt mạch, cursor đã báo, 10 lần giao gần nhất); đối tác `GET /v1/webhooks` trên cổng. Đóng mạch thủ công `…/reset-breaker`, tắt/bật `…/disable|enable`.
- **Đối soát (R-INT-10):** `GET /v1/feeds/{feed}/checksums` — feed tập dữ liệu: mỗi phiên bản `{record_count, sha256}` = tổng kiểm lúc dựng (SHA-256 ảnh chụp NDJSON, `dataset_version` bất biến); hợp đồng song phương: mỗi ngày nghiệp vụ `{record_count, sha256}` theo `bilateral_day.v1` (SHA-256 các dòng `<business_key>\t<contract_version>\t<payload_sha256>\n` sắp theo khóa nghiệp vụ) trên **sổ đệm** `ingest_staging` của bên gọi — W5.6 chuyển sang sổ song phương + feed cặp (cùng hình dạng phản hồi; bên HTX tính trên bản ghi của mình).
- **Ghi lệch / còn lại:** giao trực tiếp, không qua proxy egress (nếu production bắt buộc proxy, cần chế độ proxy có kiểm địa chỉ ở proxy); NetworkPolicy của `cmd/worker` phải mở egress 443/8443 ra Internet cho job này (chưa sửa chart); thông báo "kênh quản trị" là log + metric + API, chưa có sự kiện outbox/thư; feed lớp `partner`/cặp chưa có chỉ báo (chỉ phân vùng `''`); không có hợp đồng `contracts/integration/envelope` cho thân webhook — lược đồ ở `public.yaml` (`webhooks.feedCursorAdvanced`).

### W5.8 (09/10/2026) — kênh tệp và môi trường thử

- **Kênh tệp (R-INT-11):** `POST /v1/files/{contract}?contract_version=N` trong router Public (`public/files.go`) — kế thừa khóa [+ mTLS], hạn tần suất, sổ đo lường trước byte; cùng scope `ingest:<hợp đồng>:write` với lô. Chỉ hợp đồng có `file` trong `ingest.channels` (mặc định từ chối → 403 `integration.file_channel_not_allowed`). Thân là tệp NDJSON (`application/x-ndjson`) hoặc CSV (`text/csv`, tiêu đề = tên trường, `,`/`;`, BOM). Gói `ingest` CHỈ tách tệp (`ParseFile`; ô CSV đổi sang kiểu JSON của trường khi đúng dạng, sai dạng giữ chuỗi) và chia theo `max_batch` thành phong bì `ingest_batch.v1` (`FileEnvelopes`, `batch_ref` = UUIDv5(mã tệp, k), `sent_at` = lúc nhận tệp) rồi gọi `Service.Submit` của W5.4 — **không có đường kiểm thứ hai**; tệp chỉ bị từ chối cả tệp khi không tách được bản ghi (400 `integration.file_invalid`).
- **Idempotent theo tổng kiểm:** sổ chỉ ghi thêm `pf_apiclient.ingest_file` (pf 000128, idem `file:<client>:<hợp đồng>.v<N>:<sha256>`, RLS tenant đối tác, chỉ `r_apiclient`) ghi TRƯỚC khi gửi lô; thân lô tất định nên tải lại cùng tệp là gọi lặp ở W5.4 và gửi tiếp lô còn thiếu (429 giữa tệp → `incomplete`). Không lưu nội dung tệp (bản ghi ở `ingest_batch.items`). Giới hạn `AG_GATEWAY_INGEST_MAX_FILE_BYTES` (8 MiB, giới hạn thân riêng của route qua `httpx.AllowBody`) và `_MAX_FILE_RECORDS` (10 000).
- **Kết quả:** `GET /v1/files/{id}` gộp trạng thái lô; `GET /v1/files/{id}/results?format=csv|ndjson` (409 khi chưa xong) — số thứ tự bản ghi, `entity_key`, kết cục, mã problem; lô `dead_lettered` → mọi bản ghi rejected với mã của lô. Sổ đo lường: POST `export_request` 0 đơn vị, GET `job_status` (`job_id` = mã tệp) — như lô, chờ W5.9 định đơn vị.
- **Môi trường thử (R-INT-08):** triển khai riêng, DB riêng, dữ liệu `cmd/seed-synthetic`, cờ `AG_PARTNER_SANDBOX=1` ở `api-platform` + `integration-gateway`: phát hành khóa `agks_…` (`domain.GenerateKeyFor`); `app.Authenticator` từ chối khóa sai môi trường (401, log `apiclient.sandbox_key_refused`) nên khóa thử không dùng được ở cổng thật dù lộ; production + cờ → không khởi động (`port.ErrSandboxInProduction`); mọi phản hồi cổng thử có `AG-Environment: sandbox`. Không có nhánh mã riêng — feed, lô, tệp chạy y như thật.
- **Ghi lệch:** không làm URL tải lên ký sẵn (vòng qua sổ đo lường và kiểm đồng bộ — mục 2 chỉ cho URL ký ở chiều xuất) và SFTP; tải xuống feed theo tệp là ảnh chụp của W5.3. Kênh tệp không đòi `Idempotency-Key` (R-51 thỏa bằng khóa tự nhiên SHA-256). Cấp đối tác ở môi trường thử vẫn qua Admin API bốn mắt (chưa tự động trong `cmd/seed-synthetic`). Ingress/Traefik của cổng phải cho thân ≥ giới hạn tệp khi bật.

### W5.9 (09/10/2026) — kiểm thử tải §42

- **Công cụ:** `tools/loadgen` thêm lệnh con `gateway-provision` (cấp N đối tác TỔNG HỢP qua Admin API bốn mắt: khách hàng API → `feed:<tên>:read` → `ingest:<hợp đồng>:write` → gói hạn mức → khóa, tệp TSV 0600) và `gateway` (tải trộn `feed`/`checksums`/`batch`/`file`, mỗi đối tác một nhịp vòng hở; lượt nền và lượt có đối tác ồn chạy xen kẽ nhiều vòng, gộp mẫu; báo cáo JSON + markdown p50/p95/p99 theo lớp `normal`/`noisy` và thao tác, số 429, 429 thiếu `Retry-After`, tỷ lệ lỗi; thoát 1 khi vi phạm). Kiểm thử `TestGatewayNoisyPartnerIsolation_W59` chạy kịch bản trên cổng THẬT trong tiến trình + PostgreSQL cục bộ (mặc định 20 đối tác, bỏ qua dưới `-short`); cách chạy staging và cần ghi gì: [load-drill.md mục 6](../runbooks/load-drill.md#6-kiểm-thử-tải-cổng-tích-hợp-w59-kiến-trúc-v41-42-adr-0033).
- **Lỗi thật lộ ra và đã sửa:** bộ xử lý lô (`ingest.Service.Handle`) giữ giao dịch inbox trong khi port chủ (sổ đệm `Staging`, port W5.6 sau này) mở giao dịch thứ hai trên CÙNG pool; mỗi đối tác một consumer (`Memory.Run`, `Rabbit.Run`) không giới hạn → khi số đối tác có lô đang xử lý ≥ cỡ pool (kiểm thử: 20 đối tác, pool 16; production: `AG_DB_MAX_CONNS` mặc định 20) toàn bộ kết nối bị giữ, cổng kẹt vĩnh viễn (cả xác thực và sổ đo lường). Sửa: `Worker.DB` bọc `limitedDB` — lấy chỗ TRƯỚC khi mượn kết nối, trả khi commit/rollback, tối đa `Service.MaxConcurrent` (mặc định `ingest.DefaultMaxConcurrent` = 4) giao dịch inbox đồng thời mọi đối tác cộng lại; hàng đợi và DLQ vẫn riêng từng đối tác.
- **Số đo cục bộ (máy dev Windows, PostgreSQL dùng chung với các tiến trình khác — chỉ khẳng định cơ chế, không phải số định cỡ):** hạn mức 60 lượt/phút, đối tác ồn 10 lượt/giây, trộn `feed=6,checksums=1,batch=2,file=1` (lô 20 bản ghi, tệp 50 bản ghi).
  - 20 đối tác × 0,5 lượt/giây, 2 × (15 s + 15 s), lượt nền có đối tác ồn dùng đúng hạn mức: đối tác ồn 210/300 lời gọi `429` (mọi lời gọi có `Retry-After`), 0 lỗi khác; đối tác thường 0 lỗi, 0 `429`; p95 đối tác thường **96,6 ms → 85,4 ms** (−11,7 %), p50 44,2 → 41,9 ms, p99 151 → 138 ms — **đạt**.
  - 200 đối tác × 0,1 lượt/giây (20 lượt/giây), 2 × (20 s + 20 s): cấp 200 đối tác qua Admin API mất 3,5 s; không kẹt (sau sửa); đối tác ồn 301–310/400 lời gọi `429`, đối tác thường 0 lỗi, 0 `429`. p95 đối tác thường dao động mạnh giữa các lần chạy: 181 → 98 ms (lượt nền có đối tác ồn ở hạn mức — đạt) nhưng 210 → 635 ms ở hai lần đo liên tiếp với lượt nền vắng đối tác ồn (không đạt). Ở 20 lượt/giây cổng cục bộ (pool 16) đã gần bão hòa nên mọi tải thêm đều đẩy p95; kết luận cách ly ở 200 đối tác cần lượt đo staging.
- **Ghi lệch / còn lại:** lời gọi `429 apiclient.rate_limited` vẫn tốn ba lần chạm DB (xác thực khóa, đọc gói, ghi sổ đo lường — ADR-0023 "mọi lời gọi đã xác thực"); p50 của nó ≈ một nửa lời gọi thật. Nếu lượt staging không đạt vì đường từ chối, phương án (đệm khóa/gói ngắn hạn trong tiến trình, gộp dòng sổ 429) cần ADR-0023 quyết, chưa làm. Lượt đo chính thức 200 đối tác trên staging và số định cỡ production là **Sau** (C-13 tốc độ đối tác thật, G-3 cỡ hạ tầng). Đơn vị đo lường của lô/tệp (W5.8 "chờ W5.9 định đơn vị") vẫn 0 — cần số C-13 để định giá, không đổi ở đây.

## Liên quan

- Kiến trúc v4.1: §6, §37 (R-INT-01), §38, §39, §40 (R-INT-02…11), §41, §42, §43, §44, Phụ lục B mục 15 và 18, R-API-01, R-API-07, R-EVT-05, R-MOD-10.
- Bản đối chiếu: §2.3, X-02, X-03, X-15, X-16, các dòng R-INT.
- Câu hỏi: [cau-hoi-ton-dong.md](../cau-hoi-ton-dong.md) H-3, H-7, H-12.
- [ADR-0008](0008-phan-vung-mang.md), [ADR-0018](0018-su-kien-noi-bo-mkg.md), [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md), [ADR-0022](0022-public-api-ben-thu-ba.md)…[ADR-0025](0025-ho-so-cap-phat-phase-k.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), [ADR-0031](0031-vai-tong-hop-r-agg.md), [ADR-0032](0032-don-vi-hanh-chinh-co-hieu-luc-va-ke-thua.md), [ADR-0034](0034-luong-song-phuong.md), [ADR-0035](0035-cap-phep-tong-hop-theo-xa-nguong-k.md); [docs/design/mkg.md](../design/mkg.md) §1.1, §3.
