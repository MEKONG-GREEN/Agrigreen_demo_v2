# ADR-0020: Liên thông nền tảng ↔ MKG khác tenant bằng cấp quyền chia sẻ

**Trạng thái:** Chấp nhận · Sửa đổi bởi [ADR-0030](0030-doc-tai-cho-co-cap-phep.md) (đề xuất) · **Ngày:** 30/09/2026 · **Quyết định của:** chủ sản phẩm, 29–30/09/2026 (trong trao đổi) · **Đóng:** RB-2, Q-MKG-12, Q3-08 · **Thành phần:** `pf/party` (sổ cấp quyền, migration pf 000042), bề mặt đối tác `/v1/partner/*` (`pf/party`, `pf/geo`, `pf/cropcycle`), consumer van `pf.geo.hub_valve`, `pf.cropcycle.htx_collection_valve`, `pf.mrv.collection_evidence`, `mkg/fieldops` (adapter nền tảng)

## Quyết định của chủ sản phẩm

> "Hệ thống nội bộ (ERP, TMS) của MKG sẽ không dùng chung tenant với platform, chỉ liên thông các dữ liệu cần thiết từ platform qua internal system."

Hệ quả:
- MKG là **tenant riêng**, khác tenant chương trình khuyến nông (ADR-0009).
- Nền tảng → MKG: **chỉ** các loại dữ liệu cần thiết, đi qua đường hệ thống nội bộ (tài khoản dịch vụ của MKG gọi Application API).
- MKG → nền tảng: **vẫn chỉ** qua van công bố (ADR-0018, mkg.md §3).

Hai phương án của câu hỏi RB-2 trước đây là (a) cấp quyền chia sẻ tường minh giữa hai tenant, (b) tài khoản dịch vụ MKG có vai chỉ đọc bên trong tenant chương trình. Phương án (b) đưa MKG vào tenant chương trình, trái với quyết định trên. Chọn (a).

## Bối cảnh

- Thửa, HTX, nông hộ, vụ thuộc tenant chương trình (ADR-0009). RLS chỉ cách ly tenant (R-33), vì vậy token tenant MKG gọi Application API thường thấy rỗng.
- G3-2 đã phải dùng một tenant cho cả hai vùng trong kịch bản E2E. Bản ghi van (hub, lịch gom, bảng kê, bằng chứng MRV) nằm ở tenant của phong bì van. Hậu quả: quản lý HTX (tenant chương trình) không thấy bảng kê của MKG nếu hai bên khác tenant.
- Không được có vai `BYPASSRLS` cho mã ứng dụng (R-30, R-31). Không kết nối DB chéo cụm, không import chéo vùng (R-03, R-05).

## Quyết định

### 1. Cấp quyền chia sẻ là một bản ghi tường minh, kiểm toán được

Module chủ là **`pf/party`**, vì:
- tenant = tổ chức sở hữu dữ liệu, là bên tham gia (ADR-0009); cấp quyền là quan hệ giữa hai bên tham gia;
- `pf/party` đã giữ đồng ý theo mục đích (`pf_party.consent`), cùng loại "cho phép dùng dữ liệu cho mục đích";
- `pf/party` sở hữu danh bạ HTX, loại dữ liệu đầu tiên được chia sẻ;
- `pf/accounts` chỉ quản tài khoản Keycloak, không giữ quan hệ dữ liệu giữa tổ chức.

Mỗi cấp quyền có:

| Trường | Ý nghĩa |
|---|---|
| bên cấp | tenant chương trình (`tenant_id` của sổ) |
| bên nhận | tenant MKG (`grantee_tenant_id`), phải khác bên cấp |
| phạm vi | danh sách loại dữ liệu (`kinds`), xem mục 2 |
| mục đích | văn bản 10–500 ký tự |
| hạn | `valid_from` – `valid_to`, tối đa 5 năm; gia hạn bằng cấp quyền mới |
| trạng thái | `proposed` → `active` → `revoked` |

Luồng duyệt:
- `platform_admin` của **bên cấp** đề xuất (`POST /v1/share-grants`, Admin API, MFA).
- Một `platform_admin` **khác** duyệt (`POST /v1/share-grants/{id}/approve`). R-56 kiểm ở `app/` **và** bằng CHECK DB trên sổ và bảng hiện hành.
- Bên cấp thu hồi bất kỳ lúc nào, bắt buộc có lý do (`POST /v1/share-grants/{id}/revoke`). Thu hồi có hiệu lực ngay với lời gọi kế tiếp.

Bảng (migration pf 000042):

| Bảng | Tenant | Nội dung |
|---|---|---|
| `pf_party.share_grant_event` | bên cấp | sổ chỉ ghi thêm, chuỗi băm |
| `pf_party.share_grant` | bên cấp | bảng chiếu hiện hành |
| `pf_party.share_grant_inbound` | bên nhận | bảng chiếu cấp quyền đã duyệt / đã thu hồi |
| `pf_party.partner_read_log` | bên cấp | sổ mọi lần đọc |

`share_grant_inbound` do `app/` của `pf/party` ghi **trong cùng giao dịch** với sổ khi duyệt / thu hồi, bằng `tenant.Within` (đổi `app.tenant_id` trong giao dịch). RLS vẫn áp đầy đủ; không có vai bỏ qua RLS. Nhờ bảng này, bên nhận và consumer van (chỉ biết tenant phong bì) tra được cấp quyền của chính mình.

### 2. Loại dữ liệu

| Loại | Chiều | Dữ liệu | Không bao giờ có |
|---|---|---|---|
| `cooperative_directory` | nền tảng → MKG | HTX: mã, tên, xã, trạng thái, vị trí hồ sơ | người đại diện, mã số thuế, nông hộ, SĐT |
| `plot_directory` | nền tảng → MKG | thửa: hình học, diện tích, xã, HTX, trạng thái, revision | nông hộ, tên thửa, người tạo |
| `season_harvest_dates` | nền tảng → MKG | vụ: mã vụ/năm, ngày gieo, ngày thu hoạch dự kiến, ngày mở thu hoạch, trạng thái (proto FM-01/02) | nông hộ, sản lượng, người mở vụ |
| `htx_collection_responses` | nền tảng → MKG | phản hồi của HTX với bảng kê do chính tenant MKG gửi | người phản hồi |
| `valve_hub` | MKG → nền tảng | nền tảng nhận van `hub_facility`, `hub_capacity` vào tenant bên cấp | — |
| `valve_collection` | MKG → nền tảng | nhận `collection_schedule`, `htx_collection_statement` | — |
| `valve_mrv_evidence` | MKG → nền tảng | nhận `mrv_collection_evidence` | — |

Thêm loại mới cần ba việc: sửa ADR này, thêm vào `party/port.ShareKinds`, và migration mới cho CHECK.

### 3. Bề mặt đối tác (nền tảng → MKG)

Nhóm route `/v1/partner/*` trên Application API, chỉ cho vai `mkg_sync` (client `agrigreen-mkg-sync`, tài khoản dịch vụ **thuộc tenant MKG**):

| Route | Loại |
|---|---|
| `GET /v1/partner/grants` | cấp quyền đang hiệu lực của tenant người gọi |
| `GET /v1/partner/cooperatives`, `/{ref}` | `cooperative_directory` |
| `GET /v1/partner/plots`, `/{ref}` | `plot_directory` |
| `GET /v1/partner/season-harvest-dates` | `season_harvest_dates` |
| `GET /v1/partner/htx-collection/responses` | `htx_collection_responses`; `GET /v1/cropcycle/htx-collection/responses` giữ làm bí danh, cùng kiểm |

Mỗi lời gọi đi theo thứ tự:
1. Module chủ dữ liệu gọi `partyport.PartnerAccess.Authorize(kind, grantor)`. Điều kiện: có vai `mkg_sync`; bên nhận = tenant token (R-16); có cấp quyền `active`, trong hạn, có loại `kind`, **ở cả bảng chiếu bên nhận lẫn bảng hiện hành bên cấp**.
2. Không đạt → 403 `party.share_grant_missing`. Có nhiều bên cấp mà không truyền `grantor` → 422 `party.share_grantor_ambiguous`.
3. Đạt → đọc bằng pool vai của chính module với `tenant.WithTenantTx(…, grantor, …)`. Tenant được đặt tường minh **sau** khi kiểm cấp quyền; không có vai BYPASSRLS. Chỉ trả trường tối thiểu ở mục 2.
4. `RecordRead` ghi một dòng `partner_read_log` (tenant bên cấp) **trước** khi trả dữ liệu. Không ghi được → 503 `party.partner_audit_unavailable`, không trả dữ liệu.

Bên cấp xem sổ đọc ở `GET /v1/share-grants/{id}/reads`.

Tham số `grantor` **không** phải tenant của người gọi. Tenant người gọi vẫn luôn lấy từ token (R-16); `grantor` chỉ chọn bên cấp và luôn được kiểm cấp quyền.

### 4. Van (MKG → nền tảng) khi khác tenant

Consumer của nền tảng không ghi vào tenant phong bì (MKG) nữa:
- Consumer gọi `partyport.ValveTargets(ValveGrants, tenant phong bì, valve_*)` để lấy mọi bên cấp đang có cấp quyền loại đó cho tenant phong bì, tại lúc consumer nhận (không theo `occurred_at` — sự kiện hiện trường có thể ghi lùi giờ).
- Với mỗi bên cấp, consumer ghi trong cùng giao dịch inbox, đổi ngữ cảnh bằng `tenant.Within`.
- `pf/geo` ghi `HubSink` với tenant bên cấp.
- Mọi lớp idempotent giữ nguyên, tính theo từng tenant bên cấp: sổ nhận theo (hợp đồng, ref, revision), `HubSink` theo phiên bản, MRV theo (evidence_ref, revision).
- `pf_cropcycle.collection_*` giữ `source_tenant_id` = tenant MKG. Luồng phản hồi chỉ trả phản hồi của bảng kê do chính tenant người gọi gửi.
- Sự kiện `pf.cropcycle.collection_*.received` phát ở tenant bên cấp, nên `pf/notify` báo đúng quản lý HTX.

Không có cấp quyền đang hiệu lực → lỗi `party.valve_share_grant_missing` → giao lại → DLQ (R-62). Sửa bằng cách duyệt cấp quyền rồi chuyển lại từ DLQ; xử lý là idempotent.

Nhiều bên cấp cùng cấp một loại `valve_*` → sự kiện được ghi vào **mọi** bên cấp đó. ĐTK 1 chỉ có một tenant chương trình. Với `valve_collection`, tenant chương trình nên chỉ cấp khi HTX của các bảng kê thuộc tenant đó. Lọc theo HTX trong cấp quyền là việc mở (xem Hệ quả).

### 5. Phía MKG

`mkg/fieldops` gọi bề mặt đối tác (`adapter.PlatformHTTP`):
- `PlatformDirectory` → `/v1/partner/cooperatives/{ref}`, `/v1/partner/plots/{ref}`;
- `HTXResponseFeed` → `/v1/partner/htx-collection/responses`;
- `HarvestCalendar` (cổng mới, thêm vào) → `/v1/partner/season-harvest-dates`.

Cấu hình `AG_PARTNER_GRANTOR_TENANT` = tenant chương trình. Để rỗng thì nền tảng tự chọn khi chỉ có một bên cấp; hai bên cấp thì phải đặt.

Nền tảng trả 403/404 → `ErrDirectoryNotFound` (lệnh gom bị từ chối `fieldops.htx_not_found` / `plot_not_found`). Với luồng phản hồi, 403/404 → Unavailable: job không tiến con trỏ.

Không có kết nối DB hay import vùng nền tảng (R-03).

## Hệ quả

- Kịch bản `TestG3FieldToCustomer` chạy **hai tenant** đầu–cuối:
  - cấp quyền với người đề xuất ≠ người duyệt;
  - trước khi duyệt → 403;
  - van ghi vào tenant chương trình, tenant MKG không có dòng pf;
  - sổ đọc có đủ bốn loại;
  - thu hồi → bề mặt đối tác 403, job kéo phản hồi lỗi, lệnh gom mới bị từ chối, van mới vào DLQ.
- `ADR-0009` giữ nguyên nguyên tắc tenant = tổ chức sở hữu dữ liệu. ĐTK 1 nay có **hai** tenant: chương trình và MKG.
- `tenant.Within` là cơ chế **duy nhất** được đổi tenant trong một giao dịch. Chỉ dùng sau một kiểm cấp quyền ở `app/`: `pf/party` khi duyệt / thu hồi / kiểm, và consumer van sau `ValveTargets`.
- Việc mở:
  - lọc cấp quyền theo HTX hoặc xã (khi ĐTK 2 có nhiều tenant chương trình);
  - quy tắc FM-01/02 ở fieldops (không cuộn trước ngày gặt): cổng `HarvestCalendar` đã có, quy tắc chưa dùng;
  - người đề xuất / phê duyệt thật của hợp đồng van (Q-MKG-08).
