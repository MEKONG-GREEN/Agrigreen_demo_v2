# ADR-0034: Luồng song phương — loại luồng thứ tư, chỉ hai bên của giao dịch thấy

**Trạng thái:** Đề xuất — chờ MKG ký (A7) và chờ C-12 (H-2) · **Ngày:** 04/10/2026 (gói W0.2) · **Phụ lục B v4.1:** mục 16 · **Xung đột:** X-04 · **Sửa đổi:** [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md) §2 (thêm loại luồng), `docs/design/mkg.md` §3.1(4), §3.2 · **Thành phần:** `pf/cropcycle` (`pf_cropcycle.collection_*`, hiện tại) → `pf/cooperative` (`pf_coop`, sau W3.3), cổng tích hợp (nạp theo lô, feed của cặp), `mkg/fieldops`, App HTX, Kênh nông hộ · **Liên quan:** [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), [ADR-0031](0031-vai-tong-hop-r-agg.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md), kiến trúc v4.1 §38.2, §38.3, RB-6

## Bối cảnh

- RB-6: mọi số đo phải đối soát chéo giữa hai bên. Số cuộn MKG ghi nhận phải được HTX xác nhận.
- `mkg.md` §3.1(4): **không bao giờ qua van** giá, giá thành, nguồn gốc lô tới thửa/HTX.
- Nhưng `mkg.md` §3.2 có `htx_collection_statement` (dự thảo G3-0) với `htx_ref, plot_ref, collected_on, rolls` đi **qua van công bố** vào tenant chương trình; phản hồi HTX quay về MKG qua `/v1/partner/htx-collection/responses` (ADR-0020). Đây là X-04: nguồn gốc lô tới thửa đi qua kênh "dữ liệu mạng lưới".
- Hiện mọi người dùng của tenant chương trình có vai đọc `pf_cropcycle.collection_*` (RLS chỉ cách ly tenant) thấy bảng kê — gồm cán bộ KN, không phải bên của giao dịch.
- v4.1 §38.2 phân biệt **dữ liệu mạng lưới** (nhiều bên dùng, §3.1 đúng) và **dữ liệu song phương** (bản ghi của một giao dịch giữa MKG và một HTX; nền tảng là nơi trung lập để hai bên cùng nhìn, không bên thứ ba nào thấy).
- BRD SSO v1.1 IV.C.6: Kênh nông hộ xem "khoản thanh toán của hộ". Số tiền là giá; theo §3.1 không qua van. Luồng song phương là cách hợp lệ duy nhất.
- C-12 (H-2): product owner + pháp chế chưa chấp nhận loại luồng này.

## Quyết định

### 1. Loại luồng thứ tư

Bổ sung vào ADR-0020 §2 và §38.1 loại **song phương**, ba ràng buộc cứng:
1. **Chỉ hai bên** của giao dịch thấy bản ghi.
2. **Không tham gia** tổng hợp, chỉ số, view `_pub`, job `r_agg`, van lọc dữ liệu công, sản phẩm dữ liệu.
3. **Chỉ ghi thêm** (RB-1): xác nhận, từ chối, sửa là bản ghi mới.

Mỗi hợp đồng song phương khai `flow: bilateral`, `party_a`, `party_b` trong `contracts/integration/`. Hợp đồng đầu tiên: `collection_schedule`, `roll_count_statement` (thay `htx_collection_statement`), `htx_roll_confirmation`. Hợp đồng `household_payment` chỉ thêm khi C-12 chấp nhận và có hợp đồng riêng với HTX/hộ.

### 2. Mô hình dữ liệu

Sổ chỉ ghi thêm theo mẫu sổ hiện có (chuỗi băm, `payload_sha256`):

| Cột | Ý nghĩa |
|---|---|
| `tenant_id` | Tenant **chủ** của bên HTX (H-1: tenant chương trình theo (a), hoặc tenant HTX theo (b)) |
| `htx_node_id` | Nút của HTX trong cây tổ chức của tenant chủ |
| `counterparty_tenant_id` | Tenant MKG (bên kia) |
| `pair_id` | `(counterparty_tenant_id, htx_org_ref)` — khóa của feed cặp |
| `contract`, `contract_version`, `record_ref`, `revision` | Hợp đồng và khóa nghiệp vụ |
| `payload`, `payload_sha256` | Đúng byte đã kiểm hợp đồng |
| `author_side` | `a` (MKG) \| `b` (HTX) |

Chủ module: tiếp tục `pf/cropcycle` trên bảng `collection_*` hiện có cho tới W3.3; khi `pf/cooperative` có schema `pf_coop`, chuyển bằng `ALTER TABLE … SET SCHEMA` (nguyên tắc 4 của kế hoạch).

### 3. Chính sách RLS `p_pair` (một chính sách, ADR-0030)

```
USING (
  (tenant_id = k_ref.ctx_uuid('app.tenant_id') AND htx_node_id = k_ref.ctx_uuid('app.node_id'))
  OR counterparty_tenant_id = k_ref.ctx_uuid('app.tenant_id')
)
WITH CHECK (
  (author_side = 'b' AND tenant_id = k_ref.ctx_uuid('app.tenant_id') AND htx_node_id = k_ref.ctx_uuid('app.node_id'))
  OR (author_side = 'a' AND counterparty_tenant_id = k_ref.ctx_uuid('app.tenant_id'))
)
```

- **So khớp Nút bằng dấu bằng**, không qua `node_closure`: cán bộ KN cấp xã, tỉnh (Nút tổ tiên của HTX) **không** thấy, dù cùng tenant. Đây là điểm khác `p_scope`.
- Bên MKG chỉ vào qua cổng: bộ xử lý lô và feed cặp của cổng gọi port của module chủ với `app.tenant_id` = tenant MKG (lấy từ khóa API), sau khi kiểm hợp đồng song phương và quan hệ cặp. Không có nhánh chia sẻ `share_scope`.
- Kênh nông hộ (nếu C-12 chấp nhận): thêm một nhánh `household_ref = ctx('app.actor_ref')` **chỉ** trên bảng `household_payment`, không trên bảng số cuộn.

### 4. Luồng

| Bước | Chiều | Cơ chế |
|---|---|---|
| Lịch gom, số cuộn theo lệnh gom | MKG → HTX | `mkg/fieldops` gửi lô `POST /v1/batches/roll_count_statement` (ADR-0033 §3) |
| HTX xem, xác nhận / từ chối có lý do | HTX trên App HTX | Bản ghi `author_side = b` |
| MKG nhận xác nhận | HTX → MKG | Feed của cặp `GET /v1/feeds/bilateral?pair=…` — lọc từng hàng (ngoại lệ được R-INT-05 cho phép) |
| Đối soát | hai chiều | Tổng kiểm hằng ngày theo cặp (số bản ghi + băm), R-INT-10 |

### 5. Không bao giờ vào tổng hợp

- CI: bảng song phương không được có trong `provides.publications` của `module.yaml`, không trong danh sách job `r_agg` (ADR-0031), không trong `contracts/dataproduct`. Kiểm bằng `agmodgraph` (quy tắc mới, W5.6).
- Điểm kiểm soát xuất (ADR-0024) gán lớp `bilateral`; lớp này chỉ được ra qua feed cặp.

### 6. Trong lúc chờ C-12

- Giữ đường hiện tại (van + `/v1/partner/htx-collection/responses`) cho ĐTK 1, **không bật** hợp đồng `htx_collection_statement` ở production.
- Hạn chế đọc `pf_cropcycle.collection_*` ở tầng ứng dụng: chỉ vai HTX có `org_ref` khớp `htx_ref` và vai MKG qua bề mặt đối tác. Ghi rõ trong `mkg.md` §3.2 (W0.3).
- W5.6 làm được mã; **bật production chờ C-12**.

### 7. Nếu C-12 không chấp nhận

- Kênh nông hộ bỏ "khoản thanh toán của hộ" (BRD SSO IV.C.6).
- Số cuộn đối soát ở mức **HTX × ngày**, không có `plot_ref`, qua van công bố như dữ liệu mạng lưới; đối soát theo thửa làm ngoài nền tảng (giấy, ký xác nhận). RB-6 vẫn thỏa ở mức HTX, yếu hơn ở mức thửa.
- ADR này chuyển trạng thái "Không chấp nhận"; W5.6 bỏ.

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Giữ bảng kê qua van công bố (hiện trạng) | Trái §3.1(4); mọi người dùng tenant chương trình thấy nguồn gốc lô tới thửa |
| Cấp phép `share_scope` theo HTX | Cấp phép là quyền đọc dữ liệu của bên chủ; ở đây bản ghi là của **cả hai** bên và bên kia phải ghi được |
| Lưu ở vùng mkg, HTX đăng nhập vùng mkg | Đưa người dùng HTX vào vùng nội bộ MKG; trái phân vùng và R-INT-01 |
| `p_scope` với `node_closure` | Cán bộ KN cấp trên thấy; không còn "chỉ hai bên" |
| Chính sách thứ hai cho bên MKG | Trái "một chính sách mỗi bảng" (ADR-0005, ADR-0030) |

## Hệ quả

- Nền tảng giữ bản ghi có giá (khi có `household_payment`) — lần đầu. Cần đánh giá dữ liệu cá nhân và thương mại trước khi bật (pháp chế, C-12).
- `p_pair` là hình chính sách thứ tư; `v_missing_rls` và cổng `EXPLAIN` chấp nhận tên này.
- Feed cặp là ngoại lệ duy nhất của "không lọc từng hàng" (R-INT-05); khối lượng §38.3: 10.000–13.000 bản ghi/ngày, độ tươi ≤ 15 phút.
- Khi có giao dịch giữa bên thứ ba (cuối 2028), cùng cơ chế dùng lại với cặp khác.

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| W0.3 | `mkg.md` §3.1, §3.2: bảng kê là song phương; hạn chế đọc tạm | — |
| W5.1 | Hợp đồng `collection_schedule`, `roll_count_statement`, `htx_roll_confirmation` (`flow: bilateral`) | — |
| W5.4 | Bộ xử lý lô gọi port module chủ với tenant bên gọi | pf 000102 |
| W5.6 | Sổ song phương, `p_pair`, feed cặp, quy tắc CI không tổng hợp; bật production chờ C-12 | pf 000103, mkg 000025 |
| W5.7 | Tổng kiểm hằng ngày theo cặp | pf 000104 |
| W5.10 | Gỡ consumer `pf.cropcycle.htx_collection_valve` và `/v1/partner/htx-collection/responses` | — |

**W5.1 (08/10/2026):** ba hợp đồng `contracts/integration/ingest/` (`flow: bilateral`, khối `bilateral` có `party_a`, `party_b`, `author_side`, `pair_key: [htx_ref]`, `pair_feed: bilateral`), `status: proposed`, `pending: [A7, C-12]`; `contractcheck` cấm `aggregation` trên hợp đồng song phương. Chi tiết ở [ADR-0033 Hiện thực](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md#hiện-thực).

## Liên quan

- Kiến trúc v4.1: §3 (RB-1, RB-6), §38.1, §38.2, §38.3, R-INT-05, R-INT-10, Phụ lục B mục 16, Phụ lục C C-12.
- Bản đối chiếu: X-04.
- Câu hỏi: [cau-hoi-ton-dong.md](../cau-hoi-ton-dong.md) H-1, H-2.
- [ADR-0005](0005-rls-mot-policy-moi-bang.md), [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md), [ADR-0024](0024-diem-kiem-soat-xuat-du-lieu-dlp.md), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md), [ADR-0031](0031-vai-tong-hop-r-agg.md), [ADR-0033](0033-cong-tich-hop-mkg-nhu-moi-doi-tac.md); [docs/design/mkg.md](../design/mkg.md) §3.
