# ADR-0030: Chia sẻ trong vùng nền tảng là đọc tại chỗ có cấp phép, qua RLS `share_scope`

**Trạng thái:** Đề xuất — chờ MKG ký (A7) · **Ngày:** 04/10/2026 (gói W0.1) · **Phụ lục B v4.1:** mục 9 · **Xung đột:** X-02, X-11 (và X-01 / H-1 còn mở) · **Sửa đổi:** [ADR-0005](0005-rls-mot-policy-moi-bang.md), [ADR-0009](0009-mo-hinh-tenant.md), [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md) · **Thành phần:** `pf/party` (sổ cấp phép), `pf/kref` (`share_scope`), các bảng khai `shares` của `pf_coop`, `pf_mechanization`, middleware nhật ký đọc · **Liên quan:** [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0029](0029-k-ref-ban-chieu-loi-moi-cum.md), kiến trúc v4.1 §17.2, §17.3, §37

## Bối cảnh

Hiện trạng:
- **ADR-0005:** mỗi bảng đúng một chính sách `p_tenant`, chỉ cách ly tenant. Phạm vi trong tenant kiểm ở ứng dụng (R-33).
- **ADR-0009:** tenant = tổ chức sở hữu dữ liệu. Ứng dụng khác đọc dữ liệu dùng chung qua `port/` hoặc API, "không chồng policy".
- **ADR-0020:** chia sẻ giữa tenant là sổ cấp phép tường minh (`pf_party.share_grant_event`, bốn mắt bằng CHECK, thu hồi có lý do, `partner_read_log`). Cách đọc là **endpoint riêng** `/v1/partner/*` cho tài khoản `mkg_sync`, đọc bằng `tenant.WithTenantTx(grantor)`. Phạm vi là danh sách loại có tên (`kinds`). Cấp phép hiện **chỉ** dùng cho MKG và bên thứ ba.

Kiến trúc v4.1 tách hai việc mà ADR-0020 đang trộn (bản đối chiếu §2.3):
- (a) chia sẻ **trong vùng nền tảng** giữa các đơn vị tham gia (KN đọc HTX, CGH đọc máy): **đọc tại chỗ**, cùng endpoint của module, RLS có nhánh `EXISTS k_ref.share_scope` (§17.3);
- (b) MKG và đối tác: **cổng tích hợp**, tập dữ liệu dựng sẵn, quyền ở mức tập (§37, R-INT-05).

v4.1 cũng thay "chia sẻ bằng sao chép" của SAD định danh 6.3. Repo **chưa từng** sao chép dữ liệu giữa đơn vị, nên không có gì phải gỡ.

## Quyết định

### 1. Đọc tại chỗ, không sao chép

Đơn vị xem đọc dữ liệu của đơn vị chủ **tại bảng của module chủ**, qua cùng port/endpoint mà đơn vị chủ dùng. Không bản sao, không endpoint riêng cho chia sẻ trong vùng nền tảng.

### 2. Hình chính sách RLS (sửa ADR-0005)

Vẫn **một chính sách mỗi bảng**. Có ba hình, chọn theo bảng:

| Hình | Khi nào | Điều kiện `USING` |
|---|---|---|
| `p_tenant` | Bảng không gắn Nút, không khai `shares` | `tenant_id = k_ref.ctx_uuid('app.tenant_id')` (hiện tại dùng `shared.current_tenant()`, tương đương) |
| `p_scope` (phạm vi Nút) | Bảng có `node_id` | `tenant_id = ctx AND EXISTS (SELECT 1 FROM k_ref.node_closure c WHERE c.ancestor = ctx('app.node_id') AND c.descendant = <bảng>.node_id)` |
| `p_scope` + nhánh chia sẻ | Bảng khai trong `shares` của `module.yaml` | `(<điều kiện của chính đơn vị>) OR EXISTS (SELECT 1 FROM k_ref.share_scope s WHERE s.viewer_tenant = ctx('app.tenant_id') AND s.purpose = ctx_text('app.purpose') AND s.module_table = '<schema.bảng>' AND s.owner_tenant = <bảng>.tenant_id AND s.node_id = <bảng>.node_id)` |

Quy tắc (R-DB-08):
- `ENABLE` + `FORCE` RLS như cũ.
- Đọc biến ngữ cảnh **chỉ** qua `k_ref.ctx_uuid()`, `k_ref.ctx_text()`. Cấm `current_setting(...)::uuid` trực tiếp.
- Điều kiện viết thẳng vào chính sách, **không** gọi hàm có truy vấn con (DB-14). CI chạy `EXPLAIN`, cấm SubPlan gọi hàm theo hàng.
- Nhánh chia sẻ **chỉ** có trên bảng khai `shares`. Danh sách `shares` là hợp đồng của module chủ (ADR-0027).
- `WITH CHECK` **không bao giờ** có nhánh chia sẻ: cấp phép là quyền đọc, không phải quyền ghi. `WITH CHECK` giữ `tenant_id = ctx`.
- Kiểm ở tầng ứng dụng (`authz.Principal`, KN P1) **giữ nguyên**: hai lớp.
- `v_missing_rls` chấp nhận tên `p_scope` (W3.4).

### 3. Cấp phép v2 (giữ sổ của ADR-0020)

Giữ nguyên: sổ `share_grant_event` chuỗi băm, bảng chiếu `share_grant`, CHECK người duyệt ≠ người đề xuất (chặt hơn v4.1), thu hồi có lý do, hạn tối đa 5 năm.

Thêm (W4.1, cột NULL được để bản ghi cũ vẫn hợp lệ):

| Trường | Ý nghĩa |
|---|---|
| `grant_class` | `intra_platform` (đọc tại chỗ, ADR này) \| `partner` (MKG, bên thứ ba — đường của ADR-0020, sau là cổng tích hợp) |
| `module_table` | Bảng được chia sẻ; phải có trong `shares` của module chủ |
| `scope_node_ref` | Nút phạm vi bên đơn vị chủ (vd. một tỉnh) |
| `purpose_code` | Mục đích có mã (vd. `khuyen-nong`); khớp `app.purpose` của phiên |
| `basis_ref` | Căn cứ: thỏa thuận chia sẻ (C-10 / H-15) |

Quản trị viên **đơn vị chủ** đề xuất; một quản trị viên khác duyệt. Cấp phép xuất hiện trong rà soát quyền định kỳ (R-SEC-04).

Worker `k/ref` dựng `k_ref.share_scope` = cấp phép `intra_platform` đang hiệu lực × các Nút hậu duệ của `scope_node_ref` ([ADR-0029](0029-k-ref-ban-chieu-loi-moi-cum.md)). Trễ ≤ 5 s; thu hồi tăng epoch ngữ cảnh của tài khoản thuộc đơn vị xem.

### 4. Nhật ký đọc

- Port của module chủ trả cờ `via_share_grant` cho bản ghi đến từ nhánh chia sẻ (bản ghi có `tenant_id` ≠ tenant phiên).
- Middleware ghi **một sự kiện gộp mỗi yêu cầu** (đơn vị xem, đơn vị chủ, bảng, mục đích, số bản ghi, cấp phép).
- Giữ nguyên tắc **đóng khi lỗi** của ADR-0020: không ghi được nhật ký → 503, không trả dữ liệu đọc qua cấp phép.
- Chỉ số "đọc qua cấp phép theo đơn vị".

### 5. Tách ADR-0020

| Phần của ADR-0020 | Sau ADR này |
|---|---|
| Sổ cấp phép, bốn mắt, thu hồi, hạn, `partner_read_log` | **Giữ**, mở rộng thành v2 (mục 3) |
| Chia sẻ giữa đơn vị **trong vùng nền tảng** | **Đọc tại chỗ** (ADR này) |
| Bề mặt `/v1/partner/*` cho MKG, van RabbitMQ, `tenant.Within` ở consumer van | **Giữ đến G2**, rồi chuyển sang **cổng tích hợp** — ADR-0033 (gói W0.2, câu hỏi H-3) |
| `kinds` có tên | Giữ cho `grant_class = partner`; `intra_platform` dùng `module_table` × Nút |

### 6. Sửa ADR-0009

- "Tenant" trong repo = "đơn vị tham gia / đơn vị chủ dữ liệu" trong v4.1. Nguyên tắc "tenant = tổ chức sở hữu dữ liệu" **giữ nguyên**.
- Bỏ câu "ứng dụng khác đọc qua `port/` hoặc API, không chồng policy" theo nghĩa cấm RLS chia sẻ: nhánh chia sẻ nằm **trong** chính sách duy nhất của bảng, không phải chính sách chồng.
- "Phạm vi theo cây là phân quyền tầng ứng dụng" → phạm vi theo cây kiểm ở **cả** ứng dụng **và** RLS (`p_scope`) cho bảng có `node_id`.

### 7. Câu hỏi mở H-1 (X-01): ai là đơn vị chủ dữ liệu HTX, nông hộ, thửa?

ADR này **không** trả lời H-1. Cơ chế chạy được với cả hai câu trả lời; chỉ dữ liệu cấp phép khác nhau:

| | (a) Chương trình khuyến nông là chủ (hiện trạng ADR-0009) | (b) Mỗi HTX / đơn vị vận hành App HTX là chủ |
|---|---|---|
| `tenant_id` của `pf_coop.*` | Tenant chương trình | Tenant của HTX |
| KN đọc | Chính đơn vị, phạm vi Nút qua `p_scope` (`node_closure`) | Qua nhánh chia sẻ: cấp phép `intra_platform`, `purpose_code = khuyen-nong`, `basis_ref` = thỏa thuận mẫu (H-15) |
| App HTX đọc | Cùng tenant, phạm vi Nút = HTX đó | Chính đơn vị |
| CGH, đơn vị khác đọc | Nhánh chia sẻ | Nhánh chia sẻ |
| Số cấp phép | Ít | Một (hoặc vài) cấp phép mỗi HTX × đơn vị xem |

Rủi ro duy nhất nằm ở **đổi câu trả lời sau khi có dữ liệu thật**: chuyển từ (a) sang (b) nghĩa là đổi `tenant_id` của bản ghi sổ, mà sổ chỉ ghi thêm (RB-1). Việc đó cần một quy trình chuyển chủ có bản ghi bù, không phải `UPDATE`. Vì vậy:
- H-1 nên được trả lời **trước 15/12/2026** (KN nhận dữ liệu thật, đường găng W3.3–W3.4);
- cho đến khi có câu trả lời, ĐTK 1 chạy theo (a), W3.1 ghi giả định này.

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Sao chép dữ liệu sang đơn vị xem (SAD định danh 6.3) | Nhiều bản, lệch dữ liệu, xóa PII khó (RB-3), thu hồi phải xóa bản sao. v4.1 đã thay |
| Endpoint riêng cho mỗi loại chia sẻ (mở rộng `/v1/partner/*` cho trong vùng) | Nhân đôi handler, kiểm quyền rải từng module, không có lớp DB |
| Chính sách thứ hai (chồng) cho chia sẻ | PostgreSQL OR các chính sách PERMISSIVE: khó rà soát, trái "một chính sách mỗi bảng" |
| Hàm `in_subtree()` / `in_node_scope()` trong chính sách | Không inline, gọi theo hàng (DB-14) |
| Giữ phạm vi chỉ ở ứng dụng (ADR-0005, ADR-0009 nguyên trạng) | Một lỗi mã là lộ dữ liệu xã khác. v4.1 yêu cầu lớp DB (X-11) |

## Hệ quả

- Thu hồi qua nhánh chia sẻ có hiệu lực sau tối đa ≤ 5 s (p99) thay vì ngay lập tức như `/v1/partner/*`. Bù bằng epoch ngữ cảnh. `/v1/partner/*` giữ nguyên độ chặt cho đến khi gỡ.
- `share_scope` tăng theo số Nút × số cấp phép (vài trăm dòng mỗi cấp phép tỉnh). Bài đo dashboard Cấp Tỉnh ở khối lượng ĐTK 2 có và không có nhánh chia sẻ để đặt ngưỡng vận hành.
- Mọi kiểm thử RLS hiện có giữ nguyên; thêm T6a–T6e, T9–T10 (v4.1), và thu hồi + kết nối tái sử dụng qua PgBouncer.
- ADR-0005, ADR-0009, ADR-0020 giữ thân bài; dòng trạng thái ghi "Sửa đổi bởi ADR-0030". Khi ADR này được ký, ba ADR đó thêm mục "Cập nhật" trỏ về đây.
- Sự cố P1 mới trong runbook: "đọc chéo không cấp phép".

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| W2.4 | Biến `app.node_id`, `app.purpose` trong `WithContextTx` | trong pf 000091 |
| W2.5 | `k_ref` với `share_grant_active`, `share_scope` rỗng | pf 000092, mkg 000022 (`ctx_*` ở pf 000091 của W2.4) |
| W3.1 | Thiết kế `pf/cooperative`, `shares: [household, plot, season, diary_entry]`, ghi giả định H-1 — xong 06/10/2026: [docs/design/cooperative.md](../design/cooperative.md) (giả định (a) và phần đổi nếu (b) ở §11, ánh xạ `shares` → bảng ở §7.3, `p_scope` ở §9) | — |
| W3.2 | Module `pf/cooperative` khai `shares: [household, plot, season, diary_entry]` (chưa có hiệu lực tới W4.2), port `Resolve` (bộ ba R-DB-07; W3.2 phân giải `cooperative`, `household`, `plot`, `diary_entry`) — xong 08/10/2026 | — |
| W3.3 | Schema `pf_coop` — xong 09/10/2026: 22 bảng của `shares` (household, plot, season, diary_entry) ở một schema, một vai `r_cooperative`; `Resolve` phân giải đủ sáu loại chủ thể (thêm `season`, `harvest`); tên sổ giữ nguyên; giả định H-1 (a) không đổi ([ghi chú](../design/cooperative.md#17-ghi-chú-hiện-thực-w33)) | pf 000131–000133 |
| W3.4 | `p_scope` theo Nút — xong 09/10/2026: `p_scope` **RESTRICTIVE** trên 19 bảng `pf_coop`, `p_tenant` giữ nguyên là chính sách PERMISSIVE duy nhất (hàng = tenant AND phạm vi, phạm vi không nới ra tenant khác — `v_missing_rls` đòi bất biến này); nhánh Nút (`EXISTS k_ref.node_closure`, hashed SubPlan + chỉ mục), xã phụ trách, không Nút, dòng chưa gắn Nút, tenant chưa có `k_ref`; `WITH CHECK` = phạm vi (trừ `household`). Hệ quả cho W4.2: nhánh `OR EXISTS share_scope` đọc khác tenant không thêm được bằng chính sách PERMISSIVE mới khi `p_tenant` vẫn là chính sách cho phép duy nhất — W4.2 phải đổi bất biến của `v_missing_rls` (shared) hoặc đặt nhánh chia sẻ trong cùng tenant ([ghi chú](../design/cooperative.md#18-ghi-chú-hiện-thực-w34)) | pf 000134, shared 000012 |
| W4.1 | Cấp phép v2, luồng đơn vị chủ, rà soát định kỳ, epoch — xong mã 08/10/2026 (bật production chờ C-10/H-1). Lệch so với mục 3: (1) năm trường v2 nằm ở **sổ phụ** `pf_party.share_grant_terms` (một dòng mỗi cấp phép, ghi cùng giao dịch với `proposed`) và bảng chiếu `share_grant`, **không** ở sổ `share_grant_event` — chuỗi băm tính trên `to_jsonb` toàn dòng nên thêm cột (kể cả NULL) làm `verify_chain` của mọi dòng cũ báo gãy; (2) `module_table` lưu tên logic `<module>:<share>` (vd `pf/cooperative:household`), kiểm theo `modreg.Shared` (danh mục sinh mang `shares`); W4.2 ánh xạ sang bảng vật lý `pf_coop.household` của `share_scope`; (3) `intra_platform` không có `kinds`, không ghi `share_grant_inbound` (đường partner); (4) rà soát định kỳ: sổ `share_grant_review` (`retain` \| `revoke`), chu kỳ 90 ngày (chờ C-10), API `GET /v1/share-grant-reviews/due`, `GET\|POST /v1/share-grants/{id}/reviews`; (5) epoch ngữ cảnh: chưa có cơ chế nào trong repo — thêm bộ đếm tối thiểu `pf_party.context_epoch` theo tenant đơn vị xem, tăng cùng giao dịch khi thu hồi cấp phép đang hiệu lực, sự kiện `pf.party.context_epoch.bumped`, port `ContextEpochs`; W7.2 hợp nhất với epoch ở Valkey định danh; (6) cột v2 của `k_ref.share_grant_active` chuyển W4.2 (agmodgraph: một migration không sửa schema của hai module; sự kiện đã mang đủ trường) | pf 000120 |
| W4.2 | Worker dựng `share_scope`; nhánh chia sẻ trên `pf_coop`, `pf_mechanization.machine`; `via_share_grant`; nhật ký gộp; T6a–T6e, T9–T10 — xong mã 09/10/2026, **bật production chờ C-10/H-1** (thỏa thuận chia sẻ mẫu, câu trả lời chủ dữ liệu). Hình chính sách lệch mục 2: xem "W4.2 — hình chính sách" dưới bảng | pf 000136, shared 000014 |
| W5.x | Cổng tích hợp thay `/v1/partner/*` (ADR-0033) | — |

### W4.2 — hình chính sách (lệch mục 2)

Mục 2 viết nhánh chia sẻ là `OR EXISTS share_scope` **trong** chính sách duy nhất. Sau W3.4 điều đó không làm được mà vẫn giữ các bất biến đã chốt:

- `p_tenant` là `FOR ALL` (một `USING` cho SELECT, UPDATE, DELETE). Thêm nhánh chia sẻ vào `USING` của nó là cho đơn vị xem **UPDATE/DELETE** dòng của đơn vị chủ — trái "cấp phép là quyền đọc". `WITH CHECK` không có nhánh vẫn không chặn được UPDATE giữ nguyên `tenant_id`.
- `p_scope` (W3.4) là RESTRICTIVE: chỉ thu hẹp, không thể mở ra tenant khác; `migratecheck` cấm `DROP POLICY p_tenant`.

Quyết định hiện thực:

1. **`p_share`** — chính sách PERMISSIVE thứ hai, **`FOR SELECT`**, `TO PUBLIC`, **không `WITH CHECK`**, `USING` = điều kiện `EXISTS k_ref.share_scope` của §17.3 (viết thẳng, hashed SubPlan một lần). PostgreSQL OR các chính sách PERMISSIVE áp cho lệnh: SELECT thấy `p_tenant OR p_share`; INSERT/UPDATE/DELETE chỉ có `p_tenant` → đơn vị xem không ghi, không sửa, không xóa được dòng chia sẻ (T6e).
2. **Cổng giữ "khó rà soát" của phương án bị loại ("chính sách thứ hai chồng") ở mức hẹp nhất:** `shared.v_missing_rls` (shared 000014) chỉ nhận ba tên `p_tenant` (PERMISSIVE), `p_scope` (RESTRICTIVE), `p_share` — và `p_share` phải đúng hình: PERMISSIVE, `polcmd = SELECT`, vai PUBLIC, không `WITH CHECK`, phụ thuộc (`pg_depend`) `k_ref.share_scope`. Chính sách khác, hay `p_share` sai một điều → báo. Tập bảng có `p_share` đối chiếu `shares` của `modreg` bằng kiểm thử (`migrations/share_rls_integration_test.go`). Thêm `p_share` chỉ qua `shared.apply_share_rls` (như `apply_tenant_rls`).
3. **`p_scope` và dòng chia sẻ:** `apply_share_rls` nới `USING` của `p_scope` thành `T.tenant_id <> shared.current_tenant() OR (<phạm vi W3.4>)`. Lý do: Nút của người xem (`app.node_id`) thuộc cây của **đơn vị xem**; dòng chia sẻ gắn Nút của cây **đơn vị chủ** — hai cây không so được, nên phạm vi Nút của dòng chia sẻ là Nút phạm vi của cấp phép (`share_scope.node_id` = hậu duệ của `scope_node_ref`), đã nằm trong `p_share`. An toàn vì `p_scope` vẫn RESTRICTIVE và dòng khác tenant chỉ qua được `p_share` (SELECT); `WITH CHECK` của `p_scope` không đổi. Thu hẹp dòng chia sẻ theo người xem trong đơn vị xem (vd cán bộ xã của KN) là việc của tầng ứng dụng (`authz.Principal`, phạm vi KN) — như ADR này giữ hai lớp.
4. **`share_scope`** thêm `grant_id` (thu hồi/dựng lại theo cấp phép, hai cấp phép phủ cùng Nút không xóa dòng của nhau) và `valid_from`/`valid_to` (hết hạn đóng ngay trong chính sách, không chờ sự kiện). Chính sách của chính `share_scope`: đơn vị xem và đơn vị chủ đọc, chỉ đơn vị chủ ghi (worker chạy trong tenant bên cấp).
5. **Khóa bảng:** `module_table` của cấp phép là tên logic `<module>:<share>`; worker ánh xạ sang khóa `<schema chính>.<share>` (`modreg.ShareTable`: `pf_coop.household`, `pf_coop.plot`, `pf_coop.season`, `pf_coop.diary_entry`, `pf_mechanization.machine`). Một khóa phủ nhiều bảng vật lý (thiết kế cooperative §7.3). `pf/mechanization` khai `shares: [machine]`.
6. **Thu hồi + kết nối tái sử dụng:** worker xóa dòng `share_scope` của cấp phép trong cùng giao dịch áp sự kiện `revoked`; biến `app.*` là `set_config(…, true)` (SET LOCAL) nên giao dịch kế tiếp trên **cùng** kết nối PgBouncer không còn thấy dữ liệu; kết nối từng đặt biến mà giao dịch mới không đặt → 0 dòng, không lỗi ép kiểu (DB-13, T6d). Khoảng hở là độ trễ worker (≤ 5 s p99, mục Hệ quả); epoch ngữ cảnh (W4.1) cho phiên làm mới — W7.2 dùng nó.
7. **Nhật ký đọc (mục 4):** middleware `internal/shared/shareread` (pf/party cài làm `RequestMiddleware`): `X-Purpose` đúng dạng mã mục đích → `app.purpose`; port của module chủ đặt `ViaShareGrant` và cộng vào bộ gom; cuối yêu cầu ghi **một** sự kiện `pf.party.share_read.recorded` (tenant phong bì = đơn vị xem) — lỗi → 503 `share.read_log_unavailable`, phản hồi đã giữ bị bỏ. Chỉ đệm phản hồi khi lúc handler bắt đầu ghi đã có bản ghi qua cấp phép (tải tệp có `X-Purpose` của `pf/sync` không bị đệm). Chỉ số `ag.share.reads` (theo `viewer_tenant`, `module_table`). Port đã gắn cờ: `cooperativeport.Plots.Get/Facts`, `Households.Membership`, `Diary.Get/BySeason`, danh sách máy của `pf/mechanization`.
8. **Chi phí đã biết:** hai chính sách PERMISSIVE kết hợp bằng OR nên đọc **của chính đơn vị** trên 8 bảng này không còn dùng điều kiện `tenant_id = …` làm điều kiện chỉ mục duy nhất (dạng `OR`); T9 (40 000 thửa, 20 cấp phép) đếm 20 000 dòng chia sẻ ≈ 0,1 s, hashed SubPlan một vòng. Bài đo dashboard Cấp Tỉnh ở khối lượng ĐTK 2 có/không nhánh chia sẻ (mục Hệ quả) vẫn phải chạy trước khi bật production.
9. **Cụm mkg:** `k_ref.share_scope` của mkg 000022 giữ hình cũ (không có worker, không bảng `shares` ở cụm mkg).

## Liên quan

- Kiến trúc v4.1: §5.1 (○ = đọc qua cấp phép), §7.1 (`shares`), §17.2, §17.3 (R-DB-08), §37, Phụ lục B mục 9, Phụ lục C C-10, Phụ lục D (SAD 6.2, 6.3).
- Bản đối chiếu: §2.2, §2.3, X-01, X-02, X-11.
- Câu hỏi: [cau-hoi-ton-dong.md](../cau-hoi-ton-dong.md) H-1, H-3, H-15.
- [ADR-0005](0005-rls-mot-policy-moi-bang.md), [ADR-0009](0009-mo-hinh-tenant.md), [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md), [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0029](0029-k-ref-ban-chieu-loi-moi-cum.md); ADR-0033, ADR-0035 (gói W0.2).
