# ADR-0056: Hộ chăn nuôi CNTS v4.0 — danh sách hộ là sổ của KN, họ tên/SĐT ở kho định danh, ổ dịch nhiều hộ qua bảng con chỉ ghi thêm

**Trạng thái:** Chấp nhận — gói CNTS-V4-HOUSEHOLD-ADR (#248). Hiện thực: CNTS-V4-HOUSEHOLD-LIST (#251), CNTS-V4-OUTBREAK-MULTI (#252); phạm vi theo hộ của chiến dịch điều tra (#236) dùng khóa hộ ở đây. **Ngày:** 06/11/2026.
**Căn cứ:** BRD CNTS v4.0 (BRD 2.0, CR-CNTS-001) — FN-10 (luồng H1–H4, BR-03…BR-08, AC-05…AC-09, bảng trường HCN-01…HCN-09), FN-11 (BR-03, AC-04, AC-05; UR-XA-10 bảng trường DB-04, DB-14…DB-16), FN-16 BR-07, FN-18, ma trận quyền FN-10 "Danh sách hộ chăn nuôi (master data)", Mục I.8.1 (che SĐT, NĐ 13/2023/NĐ-CP).
**Liên quan:** [ADR-0055](0055-tach-he-thong-khuyen-nong.md) (Q-KNS-04: KN không ghi dữ liệu HTX), [ADR-0012](0012-duy-nhat-tren-bang-so-phan-vung.md) (tính duy nhất trên sổ phân vùng, cột thêm vào sổ), [ADR-0004](0004-ma-hoa-phong-bi-theo-ho.md) (kho định danh, khóa theo hộ).

## Bối cảnh

CNTS v4.0 thêm **danh sách hộ chăn nuôi** (FN-10): master data theo xã — mã hộ, họ tên chủ hộ/tên trang trại, SĐT, xã + ấp, vật nuôi và số con (nhiều dòng), loại chăn nuôi (nông hộ / trang trại nhỏ / vừa / công nghiệp), vị trí lấy GPS khi check-in tại nhà hộ, trạng thái Đang dùng / Chờ bổ sung vị trí / Vô hiệu hóa, lịch sử "người sửa, thời điểm, giá trị cũ → mới". Không xóa, chỉ vô hiệu hóa (cán bộ tổng hợp Cấp Tỉnh). BM-10 (FN-11) đổi DB-04 thành **chọn một hoặc nhiều hộ** trong danh sách, hoặc nhập hộ chưa có (tạo hộ "Chờ bổ sung vị trí"); thêm DB-14 (loại chăn nuôi từng hộ), DB-15 (số hộ, tự đếm), DB-16 (diện tích thủy sản bị ảnh hưởng, nuôi trồng và khai thác).

Mã hiện có (dev `fcb30d71`):

- `pf_extension.cnts_outbreak` (pf `20261013164309`, gói W11.49) là sổ phiên bản ổ dịch, mỗi phiên bản **một** `household_ref` (ghi chú cũ: "pf/cooperative") và/hoặc `locality`. Ứng dụng không kiểm `household_ref` với nguồn nào; `rule_version = kn.cnts.outbreak/v1`. `cnts_suspect` cùng dạng.
- `pf_extension.cnts_model_household` (pf `20261020161323`, W11.47) là **hộ mô hình** (gắn điểm trình diễn), sổ `chh:<ref>`; chủ hộ ở kho định danh (`person_ref`, `client.Create(PurposeContact, SubjectHousehold)`), hiển thị bằng `Display` (nhãn che bớt, SĐT che `0912•••123`); tọa độ ở bảng thường `cnts_model_point` (QĐ-8: tọa độ không vào sổ không xóa được).
- Kho định danh (`internal/vault/identity`): `Create`, `Display`, `Resolve` (ghi nhật ký), `Lookup` (chỉ mục mù SĐT/CCCD), `Amend` (thay họ tên/SĐT, **không giữ giá trị cũ**; danh mục mục đích hiện chỉ cho `hrm.profile`, `contact.directory`). Mỗi thao tác chỉ nhận mục đích trong `opPurposes` (`internal/vault/identity/domain`).
- Vai `accport.GroupTongHop` là cán bộ tổng hợp Cấp Tỉnh (đã dùng ở lịch trực KSGM).

Câu hỏi phải chốt trước khi #251, #252 viết mã: họ tên/SĐT lưu ở đâu khi BRD đòi lịch sử giá trị cũ → mới; khóa định danh hộ; vô hiệu hóa; vị trí; quan hệ ổ dịch–hộ; dữ liệu ổ dịch cũ một hộ.

## Quyết định

### 1. Hộ chăn nuôi là sổ riêng của KN, tách khỏi hộ mô hình và hộ HTX

Bảng mới `pf_extension.cnts_livestock_household` — sổ chỉ ghi thêm (`shared.apply_ledger`), chuỗi `lhh:<household_ref>`, mỗi dòng là **một phiên bản đủ trường** (cùng mẫu `cnts_model_household`, `cnts_outbreak`). Vai `r_extension`: `SELECT, INSERT`; không `UPDATE`/`DELETE`.

| Cột | Trường BRD | Ghi chú |
|---|---|---|
| `household_ref uuid` | — | Khóa định danh bền (mục 2). |
| `code text` | HCN-01 | Chép vào mọi phiên bản để lọc/hiển thị; không đổi. |
| `event text` | HCN-09 | `created` (thêm từ danh sách, đã check-in), `created_pending` (nhập mới ở BM-10 DB-04, BR-08), `updated` (sửa thông tin), `checked_in` (Chờ bổ sung vị trí → Đang dùng), `relocated` (check-in lại, có lý do — H4), `deactivated` (BR-04). |
| `status text` | HCN-08 | `active` / `pending_location` / `deactivated`; ghi rõ trong phiên bản (không suy ngược) để lọc nhanh. CHECK khớp `event`. |
| `commune_code text` | HCN-04 | Mã ĐVHC cấp xã hiệu lực (FN-01). |
| `hamlet text NULL` | HCN-04 | Ấp, khu phố; ≤ 200 ký tự; ứng dụng quét SĐT/CCCD (`noPII`) như ghi chú hộ mô hình. |
| `person_ref uuid` | HCN-02, HCN-03 | Chủ thể ở kho định danh (mục 3). Họ tên, SĐT **không** ở sổ. |
| `livestock_type text` | HCN-06 | `nong_ho`, `trang_trai_nho`, `trang_trai_vua`, `trang_trai_cong_nghiep`. |
| `species_codes text[]`, `head_counts bigint[]` | HCN-05 | Hai mảng cùng độ dài (CHECK), 1–30 dòng, không trùng loài (ứng dụng), số con nguyên ≥ 0; mã loài theo danh mục `cnts_species` (FN-06) kiểm theo ngày ở ứng dụng. |
| `checkin_ref uuid NULL` | HCN-07 | Lần check-in hiện hành (mục 5); NULL khi `pending_location`. CHECK: `status = 'active'` ⇒ `checkin_ref IS NOT NULL`. |
| `changed_fields text[]` | HCN-09 | Tên trường đổi so với phiên bản trước (dịch vụ tính); rỗng ở bản đầu. |
| `pii_revision_ref uuid NULL` | HCN-09 | Mã bản sửa họ tên/SĐT ở kho định danh (mục 4) khi phiên bản này đổi HCN-02/03. |
| `reason text NULL` | H4, BR-04 | Bắt buộc với `relocated`, `deactivated` (CHECK). |
| `origin_outbreak_ref uuid NULL` | BR-08 | Ổ dịch đã tạo hộ (chỉ `created_pending`). |
| cột sổ chuẩn | — | `id`, `tenant_id`, `stream_key`, `seq`, `event_time`, `recorded_at`, `actor_ref`, `on_behalf_of`, `support_reason`, `idem_key`, `rule_version = kn.cnts.livestock_household/v1`, `prev_hash`, `row_hash`; `PARTITION BY RANGE (recorded_at)`. |

Lý do tách: hộ mô hình (`cnts_model_household`) gắn điểm trình diễn và có vòng đời rút/khôi phục theo dự án; hộ chăn nuôi là master data theo xã, không gắn dự án. Gộp hai thứ vào một sổ buộc mọi bên đọc phải lọc theo loại và trộn hai ma trận quyền. Hộ HTX (`pf/cooperative`) không được ghi (ADR-0055 Q-KNS-04) và không được tham chiếu làm hộ chăn nuôi. Một người có thể vừa là chủ hộ mô hình vừa là chủ hộ chăn nuôi: hai bản ghi dùng chung `person_ref` (kho trả chủ thể cũ khi trùng SĐT), không liên kết thêm.

### 2. Khóa định danh hộ

- **Khóa nội bộ:** `household_ref` UUIDv7, sinh khi tạo. Mọi tham chiếu (bảng con ổ dịch, chiến dịch điều tra #236, báo cáo định kỳ, lớp bản đồ) dùng `household_ref`, không dùng mã hiển thị.
- **Mã hộ HCN-01:** `HCN` + 7 chữ số theo tenant (`HCN0000001`), cấp từ bộ đếm, không dùng lại, không sửa, không mang mã xã (hộ đổi xã thì mã vẫn đúng). Bảng thường `pf_extension.cnts_livestock_household_key (tenant_id, household_ref, code, created_at)`, `PRIMARY KEY (tenant_id, household_ref)`, `UNIQUE (tenant_id, code)` — sổ phân vùng theo `recorded_at` không đặt được `UNIQUE (tenant_id, code)`. Bộ đếm `pf_extension.cnts_livestock_household_counter (tenant_id PK, last bigint)`, cấp bằng `UPDATE … RETURNING` trong cùng giao dịch ghi phiên bản đầu (mẫu `pf_mechanization.code_counter`). Cả hai bảng có RLS tenant; `r_extension`: key `SELECT, INSERT`, counter `SELECT, INSERT, UPDATE`.

### 3. Họ tên, SĐT ở kho định danh

- Tạo hộ: dịch vụ gọi `client.Create(PurposeContact, Identity{Kind: SubjectHousehold, FullName, Phone, AreaCode: commune_code})` **ngoài** giao dịch DB, rồi mở giao dịch ghi sổ. Trùng SĐT → kho trả `ErrDuplicate` kèm chủ thể cũ → dùng chủ thể đó (như hộ mô hình). HCN-02 ≤ 200 ký tự, bắt buộc; HCN-03 không bắt buộc, chỉ chữ số. Giao dịch hỏng sau khi đã tạo chủ thể: chủ thể mồ côi được chấp nhận (cùng cách hộ mô hình); gửi lại cùng SĐT dùng lại chủ thể.
- Danh sách và ô chọn DB-04: `Display` — nhãn che bớt và SĐT che một phần (Mục I.8.1), kèm mã hộ, ấp, xã để phân biệt; tối đa 300 lần gọi kho mỗi trang (như `maxOwnerLabels`). Chi tiết hộ: người có quyền sửa hộ đó xem họ tên đầy đủ qua `Resolve(PurposeFieldSupport)` (kho ghi nhật ký mỗi lần); SĐT vẫn che.
- Sửa HCN-02/03: `Profiles.Amend` trên `person_ref` (mục 4). Không đổi `person_ref` khi sửa (kho không cho hai chủ thể cùng SĐT).
- Cảnh báo trùng (luồng ngoại lệ FN-10): trước khi lưu hộ mới hoặc đổi họ tên/ấp/xã, dịch vụ lấy các hộ `active` cùng `commune_code` + `hamlet`, gọi `Resolve(PurposeContact)` từng chủ thể (kho ghi nhật ký; `registry.dedup` không được `Resolve`) (tối đa 500; quá thì chỉ so 500 hộ ghi gần nhất và ghi rõ trong phản hồi), so họ tên đã chuẩn hóa (bỏ dấu cách thừa, không phân biệt hoa thường). Trùng → `409` kèm mã hộ trùng; người dùng gửi lại với `confirm_distinct = true` thì lưu. Không lưu dấu vân tay họ tên ở KN.

### 4. Lịch sử thay đổi HCN-09

- Trường không phải dữ liệu cá nhân (HCN-04…HCN-08): lịch sử là chuỗi phiên bản của sổ — giá trị cũ là phiên bản `seq − 1`, mới là `seq`, người sửa `actor_ref`, thời điểm `event_time`; `changed_fields` cho biết dòng nào đổi. Chuyển trạng thái (BR-08 `checked_in`, `deactivated`) là một phiên bản nên tự có trong lịch sử.
- Họ tên, SĐT (HCN-02/03): giá trị cũ **ở kho định danh**, không ở KN. #251 mở rộng kho: thêm `extension.contact` vào mục đích của `OpAmend`; `Amend` lưu bản trước (mã hóa bằng khóa dữ liệu của chủ thể, xóa cùng chủ thể khi xóa định danh — RB-3) vào bảng mới `vault.identity_revision` và trả `revision_ref`; thêm `Revisions(ctx, purpose, refs []uuid.UUID) ([]Revision, error)` trả **nhãn che bớt** cũ/mới theo `revision_ref` (ghi nhật ký `revision_read`). KN ghi `revision_ref` vào `pii_revision_ref` của phiên bản cùng lần sửa. Màn lịch sử hiện "Họ tên chủ hộ: Nguyễn V. A. → Nguyễn V. B.", "SĐT: 0912•••123 → 0913•••456". Chủ thể đã xóa định danh → hiện "(đã xóa định danh)".
- Lệch BRD một dòng: HCN-09 "giá trị cũ → mới" của HCN-02/03 hiện ở dạng che bớt, không hiện nguyên văn — theo HCN-02 "dữ liệu cá nhân (NĐ 13/2023)" và Mục I.8.1.

### 5. Vị trí HCN-07: bảng thường, chỉ lấy khi check-in

- Bảng thường `pf_extension.cnts_livestock_household_checkin (id uuid PK, tenant_id, household_ref, location geometry(Point,4326) NOT NULL, gps_accuracy_m numeric(10,2), mocked boolean, captured_at timestamptz, recorded_at, actor_ref, reason text NULL)` — mỗi lần check-in một dòng; ứng dụng chỉ `INSERT`; `DELETE` chỉ khi có yêu cầu xóa dữ liệu cá nhân (vị trí nông hộ là dữ liệu cá nhân — BRD Mục I; QĐ-8 như `cnts_model_point`). Vị trí hiện hành = dòng mà `checkin_ref` của phiên bản hiện hành trỏ tới.
- Không có API nhập tọa độ tay (BR-03). Thêm hộ từ danh sách phải kèm check-in trong cùng yêu cầu, thiếu → `422` (AC-05). Không có GPS → không check-in. Sai số > 50 m → cảnh báo theo ngưỡng "Sai số GPS" (FN-13), vẫn lưu. Vị trí giả lập → `mocked = true`, cảnh báo.
- Đổi vị trí (H4): check-in mới + `reason` → phiên bản `relocated`. Hộ `pending_location`: cán bộ xã phụ trách check-in → phiên bản `checked_in`, trạng thái `active`.

### 6. Vô hiệu hóa, không xóa

- Chỉ `GroupTongHop` Cấp Tỉnh (và Admin chế độ hỗ trợ) được vô hiệu hóa, bắt buộc lý do. Cấp Xã CRU hộ của xã mình phụ trách (AC-06), không vô hiệu hóa; Cấp Tỉnh khác, Ban Giám đốc: chỉ đọc (ma trận quyền FN-10). Nông hộ: không có quyền.
- BRD không có thao tác kích hoạt lại → không làm. Hộ `deactivated` không sửa được nữa.
- Hộ `deactivated` không có trong ô chọn DB-04 (BR-07, AC-07), không tính báo cáo định kỳ và lớp trang trại; vẫn hiện ở ổ dịch, báo cáo đã có (đọc theo `household_ref`, hiện nhãn "Vô hiệu hóa").

### 7. Ổ dịch nhiều hộ: bảng con chỉ ghi thêm

- Bảng mới `pf_extension.cnts_outbreak_household (tenant_id, outbreak_ref, outbreak_seq, household_ref, household_seq, livestock_type, position smallint, recorded_at)`, `PRIMARY KEY (tenant_id, outbreak_ref, outbreak_seq, household_ref)`, RLS tenant; `r_extension`: `SELECT, INSERT` — **không UPDATE/DELETE**. Mỗi phiên bản ổ dịch (v2) ghi đủ tập hộ của phiên bản đó (chụp, không ghi chênh lệch), trong **cùng giao dịch** với dòng `cnts_outbreak`.
- `livestock_type` (DB-14) và `household_seq` chụp theo phiên bản hộ lúc ghi: hộ đổi loại hay bị vô hiệu hóa sau đó không đổi báo cáo cũ (FN-10 BR-06).
- `cnts_outbreak` thêm cột cho phép NULL theo ADR-0012 "cột thêm vào sổ": `affected_households integer` (DB-15 = số dòng bảng con của phiên bản, ≥ 1, dịch vụ đếm, không nhận từ API — AC-04), `aqua_farmed_ha numeric(12,2)`, `aqua_wild_ha numeric(12,2)` (DB-16; ≥ 0, tối đa 2 chữ số thập phân, kiểm cú pháp chuỗi thập phân trước khi parse). DB-16 bắt buộc khi bệnh thuộc nhóm thủy sản: mã bệnh → `attrs.species` của `cnts_disease` → `attrs.group = thuy_san` của `cnts_species` (FN-06; #252 bổ sung danh mục nếu thiếu), kiểm ở ứng dụng (AC-05).
- DB-04 "nhập hộ chưa có": cùng yêu cầu BM-10 mang danh sách hộ mới (họ tên, SĐT tùy chọn, ấp, loại chăn nuôi DB-14 do cán bộ chọn, vật nuôi); kho định danh gọi trước, rồi một giao dịch ghi: hộ `created_pending` (`origin_outbreak_ref`), mã hộ, phiên bản ổ dịch, các dòng bảng con. Hộ chọn được phải `active` hoặc `pending_location` và thuộc xã của ổ dịch.
- Tổng hợp vùng dịch (FN-11 BR-03, FN-18 BR-02) lấy `affected_households`, `aqua_*` của **phiên bản mới nhất**, không cộng các phiên bản.
- `rule_version` của phiên bản mới = `kn.cnts.outbreak/v2`; phiên bản v2 có `household_ref` NULL, ≥ 1 dòng bảng con; `locality` vẫn tùy chọn (ghi chú địa bàn).

### 8. Dữ liệu ổ dịch cũ một hộ: giữ nguyên, đánh dấu cũ

- Không chuyển đổi, không `UPDATE` dòng cũ (sổ chỉ ghi thêm). Phiên bản `rule_version = kn.cnts.outbreak/v1` là **dữ liệu cũ**: API trả `legacy = true`; DB-04 hiện `household_ref`/`locality` như đã ghi; DB-14 trống; DB-15 hiện 1; DB-16 trống. `household_ref` cũ không trỏ tới hộ chăn nuôi và không được tra sang danh sách hộ.
- Cập nhật diễn biến một ổ dịch cũ: người dùng chọn ≥ 1 hộ theo quy tắc v2; phiên bản mới là v2. Phiên bản cũ giữ nguyên trong lịch sử phiên bản.
- `cnts_suspect` giữ nguyên (báo nghi dịch một hộ/địa bàn); xác nhận báo nghi dịch mở BM-10 v2, cán bộ chọn hộ.
- Kiểm chứng của #252: đếm số ổ dịch, số phiên bản v1, tổng `herd_size`/`dead` trước và sau migration bằng nhau; `verify_chain` sạch trên sổ `cnts_outbreak` sau `ADD COLUMN`. Quay lui: gỡ route/màn v2, bảng mới và cột mới giữ (không `DROP`, theo quy tắc migration của repo).

### 9. Bên đọc

- Báo cáo định kỳ (FN-16 BR-07): đếm hộ có `status = active` ở phiên bản hiện hành tại lúc lập báo cáo — số hộ, số trang trại (`trang_trai_*`), số con theo loài — và lưu số vào báo cáo; `pending_location`, `deactivated` không tính (AC-09).
- Bản đồ (FN-18): lớp trang trại = hộ `active` có loại `trang_trai_*`, vị trí từ check-in hiện hành; không lớp nào lấy hộ `pending_location`.
- Chiến dịch điều tra (#236, phạm vi theo hộ): tham chiếu `household_ref` của danh sách này, chỉ chọn hộ `active`.
- Cả ba cùng module `pf/extension`, đọc thẳng bảng; không mở hợp đồng `_pub` cho danh sách hộ (chưa có bên ngoài KN cần).

## Hệ quả

- #251: migration `pf_extension_cnts_livestock_household` (sổ, key, counter, checkin), migration vault `identity_revision` + mục đích `extension.contact` cho `OpAmend` + `Amend` trả `revision_ref` + `Revisions`, dịch vụ/API/màn "Số liệu cấp xã › Danh sách hộ chăn nuôi", lỗi ở `docs/errors/`.
- #252: migration `pf_extension_cnts_outbreak_household` (bảng con + ba cột `cnts_outbreak`), BM-10 v2 (chọn nhiều hộ, nhập hộ mới, DB-14…16, Word), cờ `legacy`, kiểm chứng đếm trước/sau.
- Đọc lịch sử họ tên/SĐT tốn một lần gọi kho mỗi trang lịch sử; danh sách tốn tối đa 300 lần `Display` mỗi trang như hộ mô hình.
- Kiểm trùng theo họ tên gọi `Resolve` nhiều lần (có nhật ký, mục đích `extension.contact`); chấp nhận vì chỉ xảy ra khi thêm hộ / đổi họ tên-ấp-xã và giới hạn trong một ấp.

## Phương án đã loại

- **Lưu họ tên/SĐT ở sổ KN (cột thường hoặc mã hóa tại KN):** trái quy tắc "PII qua vault", không xóa được khi có yêu cầu xóa định danh vì sổ không xóa được.
- **Lưu nhãn che bớt cũ/mới vào `changed_fields` của sổ:** nhãn che bớt vẫn là dữ liệu cá nhân (`pii:"true"`) và nằm mãi trong sổ.
- **Tạo chủ thể kho mới mỗi lần sửa họ tên:** kho chặn hai chủ thể trùng SĐT trong tenant; sửa họ tên mà giữ SĐT sẽ hỏng.
- **Mảng `household_refs uuid[]` trong `cnts_outbreak`:** không chụp được DB-14 theo từng hộ, khó truy vấn ngược "ổ dịch nào có hộ X".
- **Chuyển `household_ref` cũ sang hộ chăn nuôi:** nguồn cũ không kiểm, không có họ tên/loại chăn nuôi để tạo hộ đúng HCN-02…06; tạo hộ giả làm sai báo cáo định kỳ.
- **Dùng lại `cnts_model_household`:** khác vòng đời, khác quyền (mục 1).
