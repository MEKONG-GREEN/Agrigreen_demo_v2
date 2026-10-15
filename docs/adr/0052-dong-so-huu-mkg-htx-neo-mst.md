# ADR-0052: Đồng sở hữu dữ liệu MKG + HTX, neo HTX theo mã số thuế, HTX tạm và nhận quyền

**Trạng thái:** Hiện thực — quyết định CEO D2 (13/10/2026), gói D2 (pf 20261013091454); bổ sung luồng ORGUI 15/10/2026 · **Ngày:** 13/10/2026 · **Trả lời:** H-1 ([cau-hoi-ton-dong.md](../cau-hoi-ton-dong.md)), QĐ-2 ([đối chiếu BRD KN](../kn/doi-chieu-brd-kn-2026-10.md)) · **Sửa đổi:** [ADR-0009](0009-mo-hinh-tenant.md) (ý nghĩa tenant giữ dữ liệu HTX), [ADR-0030](0030-doc-tai-cho-co-cap-phep.md) §7 (H-1 không còn mở), [docs/design/cooperative.md](../design/cooperative.md) D-10, §11 · **Thành phần:** `internal/platform/cooperative/anchor`, `internal/platform/cooperative/member/store/profile.go`, `internal/platform/cooperative/plot/app/sync.go`, `internal/platform/party/{app,handler}` (danh sách HTX), `apps/web-ops/src/app/quan-tri/neo-htx`, `migrations/pf/20261013091454_pf_coop_htx_anchor.up.sql`

## Bối cảnh

- Đến 12/10/2026 mã chạy theo H-1 phương án (a): tenant chương trình khuyến nông là chủ dữ liệu HTX, nông hộ, xã viên, thửa, vụ (W3.1, D-10). ADR-0030 §7 để H-1 mở và nhắc: đổi chủ sau khi có dữ liệu thật phải làm bằng bản ghi bù.
- CEO quyết D2 (13/10/2026):
  1. MKG và HTX (cùng xã viên) **đồng sở hữu** dữ liệu canh tác do HTX nhập: hộ, xã viên, thửa, dữ liệu sản xuất.
  2. Được chia sẻ cho Khuyến nông như **bên thứ ba**. Căn cứ pháp lý là thỏa thuận HTX.
  3. HTX chưa có tài khoản thì KN hoặc MKG được lập **tài khoản tạm** và nhập dữ liệu dưới tài khoản đó. Khi tài khoản thật của HTX kích hoạt, dữ liệu phải tự chuyển sang.
  4. Khóa neo là **mã số thuế (MST)** của HTX.
- Quy tắc bổ sung của người dùng (13/10/2026):
  - HTX tạm do Trung tâm Khuyến nông lập vẫn được ghi.
  - Dữ liệu của HTX tạm có trong mọi báo cáo và tính vào tổng mặc định.
  - HTX tạm mang trạng thái **"Chưa xác nhận"** tới khi HTX thật nhận quyền theo MST. Sau đó trạng thái lật sang "Đã xác nhận", không sao chép dữ liệu.
  - Mặc định cho phép HTX tạm không có MST (dùng mã tạm). MST bắt buộc trước khi nhận quyền.

## Quyết định

### 1. Mô hình sở hữu

- Chủ sở hữu dữ liệu canh tác của một HTX là cặp **(MKG, HTX)**. Cặp này ghi ở **neo HTX** (`pf_coop.htx_anchor`, `co_owners = [mkg, htx]` trên API), không ở `tenant_id`.
- `tenant_id` của các bảng `pf_coop` giữ nguyên. Tenant đang giữ dòng (trước đây gọi "tenant chương trình KN") được định nghĩa lại là **tenant lưu ký** của dữ liệu đồng sở hữu. Tenant này do MKG vận hành trên nền tảng.
- Lý do không đổi `tenant_id`:
  - sổ chỉ ghi thêm (RB-1), nên đổi `tenant_id` nghĩa là ghi bù toàn bộ lịch sử;
  - quyết định D2 không đòi tách dữ liệu vật lý;
  - chuỗi băm, `ledger_idem`, `chain_anchor` giữ nguyên.
- **Khuyến nông là bên thứ ba.**
  - KN đọc qua `share_grant` v2 `intra_platform` với `purpose_code = khuyen-nong` và `basis_ref` = thỏa thuận HTX (`htx_anchor.agreement_ref`).
  - Đường đọc là nhánh `p_share` (W4.2), đã có sẵn. Mã không đổi khi tài khoản KN chuyển sang tenant KN riêng.
  - Trong ĐTK 1, tài khoản KN vẫn ở tenant lưu ký (tách tài khoản là việc của `pf/accounts`, xem "Còn mở"). Vì vậy đường đọc hiện có của KN (`p_tenant` + `p_scope` theo Nút) chạy như cũ. Kiểm thử chứng minh cả mô hình đích: KN ở tenant khác đọc đúng phạm vi cấp phép.
- Logic "chương trình KN là chủ" bị thay:
  - D-10 và §11 của `cooperative.md` trỏ về ADR này;
  - chú thích `TenantID // đơn vị chủ (H-1)` của `cooperativeport.Subject` nghĩa là tenant lưu ký;
  - không có mã nào khác dựa vào giả định (a).

### 2. Neo HTX theo MST

- **Neo** là định danh ổn định của HTX trong `pf/cooperative`, không phải tài khoản. Dữ liệu gắn Nút HTX (`cooperative_ref`, `org_ref`), còn Nút gắn neo qua **bí danh** (`htx_anchor_alias`, chỉ ghi thêm, một Nút thuộc tối đa một neo).
- **Khóa neo** = MST chuẩn hóa, duy nhất trong tenant (`UNIQUE (tenant_id, anchor_key)` và `UNIQUE (tenant_id, mst)`). Cách chuẩn hóa (`anchor/domain.NormalizeMST`):
  - bỏ khoảng trắng và dấu gạch;
  - 10 chữ số → `NNNNNNNNNN`;
  - 13 chữ số → `NNNNNNNNNN-NNN` (chi nhánh, đơn vị phụ thuộc). Hậu tố `000` bị từ chối;
  - còn lại → 422 `cooperative.htx_anchor_mst_malformed`.
- **Chi nhánh là neo riêng**, có khóa 13 số. Chi nhánh có MST riêng và có thể ký thỏa thuận riêng, nên không gộp vào neo đơn vị chính. `RootMST` cho biết đơn vị chính để tra cứu.
- Không kiểm chữ số kiểm tra (giả định, xem `docs/kn/gia-dinh/D2.md`).
- **Không có MST:** neo dùng mã tạm `TMP-<12 hex cuối của mã neo>`. Sự kiện `mst_attached` gắn MST sau. Nhận quyền bắt buộc có MST (422 `cooperative.htx_anchor_mst_required`).
- **Trùng MST** (KN và MKG cùng lập, hoặc MST đã ở hồ sơ HTX khác) → **từ chối**, 409 `cooperative.htx_anchor_mst_taken`, kèm `existing_anchor_id` và `existing_created_by`.
  - Không gộp ngầm: bên lập sau dùng neo đã có.
  - Gộp hai neo cùng một HTX (cả hai đã có dữ liệu) không tự động. Xem "Còn mở".
- MST khác MST trong hồ sơ HTX của Nút → 422 `cooperative.htx_anchor_mst_profile_mismatch`.

### 3. HTX tạm

- HTX tạm là một Nút `kind = cooperative` (đã có hồ sơ, tạo bằng luồng tạo HTX hiện có) cộng với một neo `status = provisional`, `created_by_kind ∈ {kn, mkg}` và `created_by` (người lập).
- API: `POST /v1/htx-anchors` (Admin API, MFA).
  - `created_by = kn` cần `registry_admin`, `platform_admin`, `kn_national_admin` hoặc `kn_province_admin`.
  - `created_by = mkg` cần `platform_admin`. Giả định: MKG vận hành nền tảng.
- Dữ liệu cũ (HTX có trước D2): `POST /v1/htx-anchors/backfill` (có `dry_run`) tạo neo cho mọi HTX chưa có neo.
  - Neo tạo ra có `legacy_backfilled`, `created_by = kn`, trạng thái tạm.
  - Khóa neo là MST hồ sơ nếu đúng dạng, còn lại là mã tạm.
  - Không đổi dòng dữ liệu nào: đây là toàn bộ việc "chuyển" dữ liệu hiện có sang mô hình đồng sở hữu.

### 4. Nhận quyền

`POST /v1/htx-anchors/{id}/claim` nhận `{account_ref, verified_mst, verification_ref, target_org_ref?, dry_run}` và cần `registry_admin` hoặc `platform_admin`.

- **Điều kiện:**
  - neo có MST;
  - `verified_mst` chuẩn hóa bằng MST của neo;
  - `account_ref` là tài khoản HTX đã kích hoạt;
  - `verification_ref` là chứng cứ xác minh MST.
- **Nút đích:**
  - Mặc định là Nút tạm. Đây là trường hợp thường: tài khoản HTX thật được kích hoạt trên chính Nút KN/MKG đã lập.
  - Nếu Nút đích khác (HTX đã tự đăng ký Nút riêng), Nút đích phải có hồ sơ HTX và không thuộc neo khác. Nút đích có dữ liệu → **báo cáo xung đột** và từ chối (409 `cooperative.htx_anchor_claim_conflict`, `conflict_report`).
  - Nút đích trống → nhận quyền. Nút đích thành bí danh và Nút hiện hành của neo.
- **Một lần mỗi neo, lũy đẳng:**
  - cùng `Idempotency-Key`, hoặc cùng tài khoản + Nút → phát lại kết quả đã ghi (`replayed = true`);
  - khác → 409 `cooperative.htx_anchor_already_claimed`;
  - khóa chính `htx_anchor_claim (tenant_id, anchor_id)` chặn cả hai yêu cầu song song (kiểm thử bốn luồng).
- **Nguyên tử:** một giao dịch ghi dòng sổ `claimed`, khóa nhận quyền, bí danh và bảng chiếu. Lỗi ở bất kỳ bước nào → lùi toàn bộ (kiểm thử chèn lỗi sau khi ghi).
- **Chạy thử:** `dry_run = true` chạy cùng kiểm tra trong giao dịch chỉ đọc và trả báo cáo, không ghi gì.
- **Vết kiểm toán:** báo cáo (`HTXClaimReport`) được ghi nguyên vào `details` của dòng sổ `claimed` (chuỗi băm `anchor:<id>`). Báo cáo gồm:
  - bên trước (`provisional`, `created_by`, Nút) và bên sau (`htx`, `account_ref`, Nút);
  - số dòng "chuyển chủ" theo Nút (thửa, xã viên, vụ, nhật ký, diện tích vụ);
  - bí danh thêm;
  - xung đột;
  - trạng thái xác nhận trước và sau;
  - `data_copied = false`.
- **Không UPDATE lịch sử sổ.** Chỉ bảng chiếu `htx_anchor` được cập nhật (R-21).
- Sổ neo ghi các hành động `provisional_created`, `legacy_backfilled`, `mst_attached`, `claimed`. Tên này ở mức sổ. Không phát sự kiện outbox mới trong gói này.

### 5. Trạng thái xác nhận ("Chưa xác nhận")

- Một Nút HTX **đã xác nhận** khi Nút đó là bí danh của một neo `claimed`. Mọi Nút khác (neo tạm, hoặc chưa có neo) là **"Chưa xác nhận"**.
- Nhận quyền lật trạng thái của mọi Nút của neo, kể cả Nút tạm. Không dòng dữ liệu nào bị sao chép hay ghi lại.
- Đường đọc mang trạng thái:
  - hồ sơ HTX (`partyport.CoopProfile.Confirmed`, tính trong cùng câu SQL của `pf_coop.cooperative`);
  - danh sách và chi tiết HTX `GET /v1/cooperatives[/{ref}]`;
  - danh sách KN `GET /v1/kn/cooperatives`.
  Các đường này trả `confirmation_status` (`confirmed`/`unconfirmed`), `confirmation_label` ("Đã xác nhận"/"Chưa xác nhận") và lọc được bằng `?confirmation=`.
- **Mặc định gồm cả HTX tạm** và tính vào tổng. Chỉ lọc khi người dùng yêu cầu.
- Cổng `cooperativeport.HTXConfirmations` (module `pf/cooperative`) cho các đường đọc cấp báo cáo của module khác: tổng hợp `cap/forms`, kỳ báo cáo, view `_pub`. Xem "Còn mở" về phần nối.

### 6. RLS

- Bảng mới (`htx_anchor_event` là sổ; `htx_anchor`, `htx_anchor_alias`, `htx_anchor_claim`) chỉ có `p_tenant`.
  - Không `p_scope`, vì chỉ quản trị dùng.
  - Không `p_share`, vì không thuộc `shares`.
  - `r_cooperative` có SELECT, INSERT, và thêm UPDATE trên bảng chiếu `htx_anchor`. Bí danh và khóa nhận quyền chỉ ghi thêm.
- **Không đổi chính sách** của bảng dữ liệu. Bất biến `v_missing_rls` (`p_tenant` PERMISSIVE, `p_scope` RESTRICTIVE, `p_share` FOR SELECT) giữ nguyên.
- Nhận quyền không đổi `tenant_id` hay cột Nút của dòng dữ liệu. Vì vậy:
  - HTX đọc Nút của mình qua `p_scope`;
  - HTX khác cùng tenant không thấy;
  - MKG (vận hành tenant lưu ký, không gắn Nút) thấy cả tenant;
  - KN ở tenant khác đọc đúng phạm vi cấp phép qua `p_share`.
  Tất cả giống hệt trước và sau nhận quyền (`migrations.TestHTXAnchorRLSAfterClaim_D2`).
- Trường hợp Nút đích khác Nút tạm: dòng cũ vẫn gắn Nút tạm. HTX thật thấy chúng khi Nút tạm nằm trong cây Nút của mình. Thao tác `POST /v1/orgs/{ref}/move` (sổ `org_event`) đưa Nút tạm về dưới Nút HTX thật. Trường hợp này hiếm vì Nút đích phải trống.

### 7. Đồng bộ ngoại tuyến

- Bản ghi tạo trên máy dưới Nút tạm trước khi nhận quyền vẫn đồng bộ được sau đó:
  - Nút tạm là bí danh của neo;
  - sink thửa (`plot/app/sync.go`) phân giải `cooperative_ref` qua `Anchors.CurrentOrg` về Nút hiện hành;
  - Nút không thuộc neo giữ nguyên.
- Trường hợp thường (nhận quyền trên chính Nút tạm) không đổi Nút nào nên không cần ánh xạ.
- Quyền ghi của KN sau nhận quyền không đổi trong gói này. Giả định: KN tiếp tục nhập thay theo thỏa thuận.

### 8. KN được và không được làm gì

| KN được | KN không được |
|---|---|
| Lập HTX tạm (`created_by = kn`), nhập dữ liệu dưới HTX tạm | Lập HTX tạm "do MKG" |
| Gắn MST cho neo mã tạm (quản trị KN tỉnh/TW) | Nhận quyền thay HTX (chỉ `registry_admin`/`platform_admin` sau khi xác minh MST) |
| Đọc dữ liệu HTX qua cấp phép có `basis_ref` = thỏa thuận HTX (đích), qua phạm vi Nút (ĐTK 1) | Ghi/sửa/xóa dòng chia sẻ khi đọc qua `p_share` (T6e) |
| Xem danh sách neo, lọc "Chưa xác nhận" | Đọc sổ neo của tenant khác |

## Phương án đã cân nhắc

| Phương án | Lý do không chọn |
|---|---|
| Chuyển `tenant_id` sang tenant của từng HTX (H-1 b nguyên bản) | Ghi bù toàn bộ sổ, đổi chuỗi băm. D2 nói "đồng sở hữu", không nói "HTX là tenant" |
| Neo theo tài khoản (sub) | Tài khoản đổi, nhiều người, khóa. D2 nói khóa là MST |
| Gộp tự động khi trùng MST | Hai HTX tạm cùng MST có thể là nhập nhầm. Gộp cần người quyết. Ưu tiên báo lỗi kèm neo đã có |
| Sao chép dữ liệu sang Nút HTX thật khi nhận quyền | Trái "không sao chép" của quy tắc 13/10 và RB-1. Hai bản lệch nhau |
| Loại HTX tạm khỏi tổng báo cáo | Trái quy tắc 13/10: chỉ gắn nhãn, mặc định tính vào tổng |

## Hệ quả

- H-1 và QĐ-2 đã có câu trả lời. W3.3–W4.2 giữ nguyên mã. Bật production của `p_share` cho KN chờ thỏa thuận HTX mẫu (H-15) và việc tách tài khoản KN sang tenant riêng.
- Thêm một migration ở `pf_coop`, bốn bảng.
- Danh sách HTX có thêm hai trường bắt buộc trong OpenAPI (`confirmation_status`, `confirmation_label`).

## Hiện thực

| Gói | Việc | Migration |
|---|---|---|
| D2 (13/10/2026) | Neo, HTX tạm, gắn MST, nhận quyền (lũy đẳng, nguyên tử, chạy thử, kiểm toán, xung đột), backfill, trạng thái xác nhận trên hồ sơ và danh sách HTX (lọc `?confirmation=`), phân giải bí danh ở sink thửa, cổng `HTXConfirmations`, màn web-ops `/quan-tri/neo-htx`. Kiểm thử: `anchor/domain` (chuẩn hóa, chi nhánh), `anchor/app` (trùng MST KN/MKG, sai dạng, chi nhánh, gắn MST, lũy đẳng, song song, lùi khi lỗi, xung đột, bí danh, xác nhận, backfill, chuỗi băm), `migrations` (RLS trước/sau nhận quyền), `plot/handler` (bản ghi ngoại tuyến), `party/handler` (nhãn + lọc) | pf 20261013091454 |

## Còn mở

- Nối `HTXConfirmations` vào tổng hợp `cap/forms`, kỳ báo cáo (`reportcycle`, gói D8) và view `_pub` (gói PFPUB). Gói này không sửa các phần đó để tránh xung đột với gói đang chạy song song. Cổng và nhãn đã sẵn; mặc định tính vào tổng nên số liệu hiện tại đã đúng, chỉ thiếu nhãn.
- Tự kích hoạt nhận quyền khi `pf/accounts` kích hoạt tài khoản HTX. Hiện quản trị gọi API sau khi xác minh MST.
- Gộp hai neo cùng một HTX khi cả hai đã có dữ liệu.
- Tách tài khoản KN sang tenant KN riêng để KN đọc chỉ qua cấp phép. Cấp phép `share_grant` mỗi neo (hoặc theo Nút tỉnh) với `basis_ref` = `agreement_ref` cần luồng cấp hàng loạt (W4.1).

## Bổ sung luồng tạo ORGUI (15/10/2026)

`POST /v1/orgs` không tạo HTX mới; nhập cây cũng loại dòng HTX mới. Admin nền tảng
(`platform_admin`/`registry_admin`) dùng `POST /v1/htx-anchors` với `new_cooperative`
để tạo Nút và hồ sơ qua `party/port.CooperativeRegistrar`, rồi lập Neo. `org_ref` vẫn dùng
cho hồ sơ đã có. MST của hồ sơ mới lấy từ MST đã chuẩn hóa của yêu cầu Neo; người đại diện
chuyển thẳng vault. Mất phản hồi/lỗi giữa bước gửi lại cùng khóa để hoàn tất, không có giao dịch
phân tán nguyên tử. Chứng minh: ORG-NEO-001/002/003 và [nghiệm thu tổ chức](../uat/to-chuc-acceptance.md).
Bổ sung này thay đường HTTP tạo mới theo CODEX-G1, không thay quyết định CEO về sở hữu/nhận quyền.
Đường KN cũ nhập HTX còn phải xử lý theo đề xuất tác động tại ADR-0055 trước khi tuyên bố KN độc lập.
