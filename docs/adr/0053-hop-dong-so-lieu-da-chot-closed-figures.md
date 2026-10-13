# ADR-0053: Một hợp đồng `_pub` có phiên bản cho "số liệu đã chốt" — họ `closed_figures/v1`, port KHTC `KHTCSources`, bên đọc qua thành viên vai `_pub`

**Trạng thái:** Đề xuất — hiện thực ở gói PFPUB (13/10/2026, pf 20261013150108, pf 20261013150110); PO (CEO) đã chốt D8 (mỗi phòng một lịch tuần, KHTC quy đổi), D17 ("đã chốt" = mọi biểu nguồn của tuần đã chốt), D18 (lũy kế của nguồn là chuẩn); người dùng chốt: Trồng trọt giữ vòng đời báo cáo riêng (W11.04 hủy), KHTC tổng hợp số của từng phòng ngay ở G1, D2 (HTX "Chưa xác nhận") · **Ngày:** 13/10/2026 · **Yêu cầu:** R-DB-10, R-DB-02, R-DB-03, R-14, kiến trúc v4.1 §13, §14, §26 · **Nguồn:** BRD KHTC v2.2 BR-DL-02…04, đối chiếu BRD KN 10/2026 Phụ lục D X6, QĐ-6, Q-047, Q-048, review đội (W11.61–W11.69) · **Thành phần:** `migrations/pf/000153_pf_forms_pub_closed_figures.up.sql`, `migrations/pf/000154_pf_extension_pub_closed_figures.up.sql`, `contracts/pubview`, `internal/shared/dbadmin` (`PubReaders`), `internal/platform/extension/port/closedfig.go`, `internal/platform/extension/closedfig`, `internal/platform/forms/port/pub.go` · **Liên quan:** [ADR-0027](0027-bon-tang-module-tep-khai-bao.md), [ADR-0028](0028-vai-dang-nhap-theo-don-vi-trien-khai.md), [ADR-0031](0031-vai-tong-hop-r-agg.md), [contracts/pubview](../../contracts/pubview/README.md)

## Bối cảnh

- KHTC (9 gói W11.61–W11.69; bộ đọc nguồn module là W11.64) phải đọc số liệu **đã chốt** của mọi phòng.
- Hôm nay có hai nguồn:
  1. **Trồng trọt** chạy vòng đời báo cáo `extension/reportcycle` (tuần Thứ Tư – Thứ Ba) — và **giữ lâu dài** (quyết định 13/10/2026: Trồng trọt giữ biểu riêng, W11.04 hủy). W11.10 công bố `pf_extension_pub.v1_consolidation_closed` (pf 000144): kỳ × biểu, lần tổng hợp các xã lúc chốt, cờ biểu/kỳ/khóa kỳ.
  2. **CNTS** chạy kỳ native `cap/forms` (W11.05/05b/42: `pf_forms.period_spec`, `close_snapshot`, `submission_meta`). Nguồn này **chưa có** view `_pub`.
- **Báo giá** có view riêng `pf_price_pub` (W11.76): bảng giá ngày × mặt hàng và số phiếu tuần × xã báo giá. Đây là số liệu khác hạt, nên không vào họ `closed_figures`, nhưng KHTC phải đọc nó qua cùng một port (§7).
- KHTC tổng hợp số của từng phòng vào báo cáo của mình **ngay ở G1**: đường đọc phải dùng được thật, không phải khung.
- Nếu không có một hợp đồng chung, KHTC xây trên nền đang đổi và phải biết riêng từng nguồn.
- KHTC cài trong `pf/extension`, nên giao dịch chạy `SET LOCAL ROLE r_extension` (ADR-0028). Vai `r_extension_pub` hiện chỉ có `r_metrics` và `r_agg` là thành viên. `TestPubViewsDeclaredAndPrivileges_W63` cấm cấp thẳng quyền trên schema/view `_pub` cho vai khác `r_<chủ>_pub`. `ag_migrator` không có CREATEROLE nên migration không cấp được thành viên vai.

## Quyết định

### 1. Họ hợp đồng `closed_figures/v1`: một bộ cột, mỗi nguồn một view

| View | Chủ | Nguồn | Migration |
|---|---|---|---|
| `pf_extension_pub.v1_closed_figures` | `pf/extension` | vòng đời báo cáo (Trồng trọt) — nguồn chính thức, lâu dài. Chiếu lại `v1_consolidation_closed`, nên ngữ nghĩa W11.10 chỉ nằm ở một chỗ | pf 20261013150110 |
| `pf_forms_pub.v1_closed_figures` | `pf/forms` | kỳ native `cap/forms` (profile `kn_cnts` → `chan_nuoi_thuy_san`), lấy từ ảnh chụp đóng băng | pf 20261013150108 |

- **Cột và thứ tự giống hệt nhau** (33 cột):
  - Khóa tra cứu: `tenant_id`, `department_code`, `province_code`, `calendar_id`, `calendar_version`, `period_type`, `period_key`, `week_year`, `period_rule`, `period_from`, `period_to`, `period_label`.
  - Nguồn: `source` (`reportcycle` | `forms`, chỉ để chẩn đoán), `source_module` (`pf/extension` | `pf/forms`), `source_period_id`, `form_code`, `form_version_id`.
  - Cờ: `form_closed`, `period_closed`, `key_closed`, `closed_at`.
  - Số đã chốt: `snapshot_id`, `snapshot_no` (bigint), `data_at`, `provisional`, `units_total`, `units_with_data`, `missing_units`, `rows`, `totals`.
  - Xác nhận và nguồn gốc (§8): `confirmation_status`, `figures_hash`, `status_hash`.
- Hạt: kỳ nguồn × biểu. Danh tính kỳ nguồn (D8) là **(`calendar_id`, `calendar_version`, `period_key` = khóa gốc)**, vd `chan_nuoi_thuy_san:v3:2026-W45`, theo API lịch của gói D8 (`reportcycle/domain`: `Calendar`, `CalendarOf`, `CalendarRef`).
  - `calendar_id` là mã phòng; `calendar_version` (integer) dùng một dãy chung: 1 = v1 (Thứ Hai – Chủ nhật), 2 = v2 mặc định (Thứ Tư – Thứ Ba), 3 = CNTS (Thứ Ba – Thứ Hai), ….
  - View đọc `;cal=<phòng>:v<n>` trong `period_rule`. Thẻ cũ không có `cal=` → (phòng của kỳ, 1 nếu v1-tam, 2 nếu v2), đúng như `CalendarOf`. Thẻ không phải lịch kỳ (tuần làm việc BM-05) → `calendar_version` NULL; quy đổi vẫn theo ngày cuối.
  - `period_rule` mang thẻ đầy đủ (`week_start`, cách đánh số, căn tháng, `cal=`).
  - Khóa dòng: (`calendar_id`, `calendar_version`, `period_type`, `period_key`, `province_code`, `form_code`, `source_period_id`). View **không** quy đổi sang lịch KHTC (§6).
- `rows` dùng một dạng phần tử chung: `{key, unit, unit_kind, parent, region, indicator, nil_report, cells, confirmation}`. `cells` chỉ chứa **ô số** dạng chuỗi thập phân. Với nguồn cũ, `unit` là mã xã và `region` là khu vực. Với kỳ native, `unit` là xã, cơ sở hoặc hộ, và `parent` là mã xã cha.
- `totals` là dòng tổng do nguồn tính: `{key, kind, region, indicator, members, cells}`. Nguồn không tổng hợp (kỳ native) trả `[]`, và bên đọc tự cộng `rows` theo decimal.
- Hợp đồng nằm trong `contracts/pubview/<schema>.closed_figures.v1.json` với trường `family: "closed_figures/v1"`. `pubview.FamilyProblems` kiểm hai điều:
  - mọi thành viên của họ có cột giống hệt (tên, kiểu, nullable, thứ tự);
  - họ có ít nhất hai module chủ.

  `TestPubViewsDeclaredAndPrivileges_W63` so cột của view thật với hợp đồng.
- Không dữ liệu cá nhân (R-14). Không công bố người nộp/rà soát (`submitted_by`, `reviewed_by`, `actor_ref`), mã bài nộp, nguồn tổng hợp (`sources`), hay trường văn bản tự do (ghi chú, kế hoạch công tác, địa điểm). Kỳ native chỉ lấy trường `integer`/`decimal` của phiên bản biểu.

### 2. "Đã chốt" (QĐ-6 mặc định, Q-047, Q-048 — đề xuất của dev)

- **Số liệu = ảnh chụp đã chốt.**
  - Nguồn cũ: lần tổng hợp các xã mới nhất ghi không muộn hơn lần chốt gần nhất chạm biểu (`closed`, `auto_closed`, `reclosed`).
  - Kỳ native: `close_snapshot` mới nhất (mỗi lần chốt sinh một ảnh, ảnh cũ không đổi).
  - Chưa chốt lần nào: cột số liệu là NULL.
- **Mở lại** (cả kỳ hoặc một phần đơn vị): cờ về `false`, nhưng số của lần chốt trước **vẫn trả**. KHTC hiện "Nguồn đã thay đổi". Bài nộp hoặc lần "Cập nhật" khi đang mở là số tạm, không lọt ra. **Chốt lại**: ảnh mới, `snapshot_no` tăng, cờ trở lại `true`.
- **Cờ:**
  - `form_closed`: biểu của kỳ đã chốt và không đơn vị nào của biểu đang mở lại.
  - `period_closed`: kỳ đã chốt và không đơn vị × biểu nào đang mở lại. Kỳ native chỉ có một biểu nên `form_closed = period_closed`.
  - `key_closed`: mọi biểu của mọi kỳ cùng (phòng, tỉnh, loại kỳ, khóa kỳ) **trong view đó** đã chốt (Q-048: "mọi biểu nguồn của tuần đã chốt").
  - Bộ đọc KHTC tính `KeyClosedAll` trên cả họ (§4). Hai cờ chỉ khác nhau khi một phòng có kỳ ở cả hai nguồn cho cùng khóa.
- **Khóa tra cứu là `period_key` đã lưu lúc phát hành**: `report_period_key` ở nguồn cũ, `period_spec.period_key` ở kỳ native. Kỳ không có khóa, kỳ của đợt thu hồi và kỳ đã hủy không có dòng.
- **Q-047 (lũy kế):** KHTC đọc lũy kế **của nguồn**. Cột/trường lũy kế nằm trong `cells`, KHTC không tự tính lại lũy kế.
- **Tỉnh của kỳ native** = tỉnh của Nút phát hành (`owner_ref` → `k_ref.org`), cùng quy tắc với `knActor` của `pf/extension`. Nút chưa đồng bộ vào `k_ref` thì kỳ không có dòng.

### 2a. Quyết định của PO (13/10/2026)

- **D8:** mỗi phòng giữ lịch tuần riêng — CNTS Thứ Ba → Thứ Hai (theo BRD), KHTC Thứ Tư → Thứ Ba (mặc định v2). Hệ thống tự quy đổi số của phòng khác sang tuần KHTC. Hợp đồng vì thế mang danh tính lịch của nguồn và **không giả định một quy tắc tuần chung** (§6). Việc đổi tham số lịch CNTS do gói khác làm; bảng chuyển đổi chạy với mọi thẻ quy tắc.
- **D17:** "đã chốt" = mọi biểu nguồn của tuần đã chốt. Phía nguồn là `key_closed`; phía KHTC là `TargetFigure.KeyClosed` (§6).
- **D18:** lũy kế của nguồn là chuẩn (Q-047).
- **D22** (giá: min/max toàn tỉnh, lấy ngày đã duyệt gần nhất) thuộc `pf_price_pub` (W11.76), ngoài phạm vi ADR này.

### 3. Chính sách phiên bản

- Họ `closed_figures/v<N>` là hợp đồng như port. Hai việc sau được làm bằng `CREATE OR REPLACE VIEW` trong migration của module chủ, **giữ nguyên cột và ý nghĩa**:
  - đổi bảng nội bộ;
  - thêm profile/phòng vào `pf_forms_pub` (biểu thức `CASE profile` → `department_code`).
- Đổi cột, kiểu, thứ tự hay ý nghĩa cờ/số liệu thì phải ra `v<N+1>`: view mới ở **mọi** nguồn cùng lúc (cùng `family`), view cũ giữ tới khi không còn bên đọc. Kiểm thử họ chặn trường hợp một nguồn lệch cột.
- Thêm nguồn mới (vd HCTH, nếu có số liệu kỳ) nghĩa là thêm một view cùng `family` trong `pf_<module>_pub`, thêm module vào `PubReaders` của bên đọc, và thêm một nhánh `UNION ALL` ở bộ đọc.

### 4. KHTC đọc thế nào: thành viên vai `_pub` (bootstrap) + một port đọc trong `pf/extension`

- **Quyền:** `dbadmin.PubReaders` (danh mục đã rà soát) cho `r_extension` làm **thành viên** `r_extension_pub`, `r_forms_pub` và `r_price_pub` (INHERIT).
  - `cmd/migrate bootstrap` cấp thành viên này, cùng chỗ đã cấp `r_metrics`/`r_agg`.
  - `r_extension` chỉ có thêm USAGE trên schema `_pub` và SELECT trên view công bố. Nó không có quyền nào trên `pf_forms.*`: cổng cô lập schema W9.1 đã miễn `*_pub`, và kiểm thử xác nhận đọc `pf_forms.period` trả 42501.
  - Không có ACL thẳng nào mới trên `_pub`.
- **Port:** `extension/port.ClosedFigures`, cài ở `extension/closedfig`, nối qua `Module.ClosedFigures()`.
  - Đọc hai view trong **một** giao dịch chỉ đọc của tenant, vai `r_extension`. RLS bảng gốc áp qua view.
  - Lọc theo cột khóa (phòng, tỉnh, loại kỳ, khóa kỳ) để cờ của từng nguồn vẫn tính trên đủ khóa. Tính `KeyClosedAll` trên `UNION ALL`. Lọc biểu làm sau.
  - KHTC (W11.64) chỉ gọi port này và không nhắc tên view nào.
- **Vì sao không chọn phương án khác:**
  - *Cấp thẳng SELECT view cho `r_extension`*: trái ADR-0031 §2 ("chỉ `r_<chủ>_pub`") và bị kiểm thử W6.3 chặn.
  - *Port đọc của `pf/forms` chạy bằng `r_forms` trên bảng nội bộ*: lặp lại SQL "đã chốt" ở hai chỗ (view và port). Hợp đồng là view, không phải mã Go, và `cap/forms` sẽ phải biết KHTC.
  - *View hợp nhất `UNION ALL` trong một schema `_pub`*: view của `pf_extension_pub` sẽ đọc `pf_forms_pub`, nối hai hợp đồng thành một đối tượng chung lịch sử migration. `r_metrics`/`r_agg` thấy hai lần cùng số liệu, và schema của mỗi chủ không còn chỉ chứa dữ liệu của chính module đó.
  - *Thêm `consumes.publications` vào `module.yaml`*: đổi định dạng manifest, `modgen` và mọi `module.yaml`. Để sau, khi có bên đọc thứ hai ngoài KHTC; `PubReaders` khi đó sinh từ manifest.

### 5. Hai nguồn lâu dài, một hợp đồng

- Trồng trọt **ở lại** `extension/reportcycle` (quyết định 13/10/2026, W11.04 hủy). `pf_extension_pub.v1_closed_figures` là **nguồn chính thức, lâu dài**, không phải cầu chuyển tiếp. Nó chịu cùng mức cam kết phiên bản (§3), cùng kiểm thử cột và cùng nguồn gốc/băm (§8) như nguồn `cap/forms`.
- Hai nguồn khác dạng thật, và hợp đồng giữ khác biệt đó một cách tường minh:
  - `rows` của Trồng trọt: một dòng mỗi xã × chỉ tiêu, ô theo chữ cái cột của mẫu (`F`, `M`…), `region` = khu vực. `totals` = dòng Khu vực/Toàn tỉnh do nguồn tính.
  - `rows` của CNTS: một dòng mỗi đơn vị (xã, cơ sở, hộ), ô theo khóa trường. `totals` = `[]`.
  - Bên đọc cộng `rows` (crosswalk §6 đã làm việc này) và **không rẽ nhánh theo cột `source`**. Mẫu phiếu KHTC (W11.63) tham chiếu `form_code` + khóa ô.
- Phòng mới, hoặc một phòng đổi nguồn, vẫn không đổi hợp đồng. Hai cách:
  - thêm profile vào `CASE` của `pf_forms_pub.v1_closed_figures` (`CREATE OR REPLACE VIEW`);
  - thêm một view cùng `family` cho module nguồn mới (§3).
- `pf_forms.mirror` (ghi song song chốt của vòng đời báo cáo vào `cap/forms`, origin `mirror`) **không** vào `pf_forms_pub`. View chỉ đọc `pf_forms.period` (kỳ native), nên không đếm trùng.

### 6. Bảng chuyển đổi kỳ nguồn → kỳ KHTC (`kn.crosswalk/v1;attribution=source_end`)

- **Đặt sau port đọc của KHTC, là hàm thuần:** `extension/closedfig.Convert`, gọi qua `port.ClosedFigures.Converted(ctx, tenant, q, CrosswalkTarget)`. Đầu vào là các dòng `closed_figures/v1` (lịch nguồn nguyên dạng). Lịch đích là `CrosswalkTarget`: `CalendarID` (mặc định `ke_hoach_tai_chinh`), `RuleTag` (tham số lịch KHTC; mặc định `kn.period_rule/v2`, tuần Thứ Tư – Thứ Ba) và `PeriodType` (tuần mặc định; tháng, quý, năm, ngày cũng được).
  - Không đặt trong view: quy tắc lịch là tham số có phiên bản ở Go (`reportcycle/domain.PeriodRule`). Viết lại bằng SQL sẽ có hai bản quy tắc.
- **Có phiên bản:** `CrosswalkTarget.Version`. Hiện chỉ có `port.CrosswalkV1`, phiên bản lạ trả lỗi. Mỗi `TargetFigure` mang `RuleVersion`. Đổi cách gán = phiên bản mới, bên đọc chọn.
- **Quy tắc v1** (suy từ D18 "lũy kế của nguồn là chuẩn"):
  1. Mỗi kỳ nguồn thuộc **đúng một** kỳ đích: kỳ đích chứa **ngày cuối** của kỳ nguồn (`PeriodOf` của lịch đích). **Không chia tỷ lệ.** Tuần nguồn vắt hai tuần KHTC thuộc tuần chứa ngày cuối, và có cờ `Converted` = khoảng ngày khác kỳ đích.
  2. `Sum`: tổng decimal **chính xác** các ô số của mọi đơn vị × mọi kỳ nguồn quy vào. Kỳ ngày (vd BM-01) vì thế cộng đúng theo tuần KHTC.
  3. `Last`: tổng ô của kỳ nguồn có ngày cuối muộn nhất, tức lũy kế của nguồn tại cuối kỳ đích. **Số tăng của tuần KHTC = hiệu hai lũy kế** ở cuối hai kỳ đích liên tiếp (`closedfig.Increment`).
  4. Ô nào là số phát sinh (dùng `Sum`) và ô nào là lũy kế (dùng `Last`) do mẫu phiếu KHTC (W11.63) quy định. Bảng chuyển đổi trả cả hai.
  5. `Closed` (phía KHTC) = **mọi** kỳ nguồn của biểu quy vào kỳ đích đã chốt và có số. `KeyClosed` = như thế cho **mọi biểu của phòng** (D17). Phòng không có kỳ nguồn nào quy vào kỳ đích → coi là đã chốt, nhưng vẫn gắn cờ `Missing` (quy tắc của bộ đối chứng KHTCQA).
  6. Lịch đích lấy theo D8: `CrosswalkTarget.RuleTag` = `Calendar.Rule.Tag()` của `(*reportcycle/app.Service).CalendarAt(ctx, ke_hoach_tai_chinh, ngày)`; rỗng → quy tắc v2 mặc định, tức lịch (`ke_hoach_tai_chinh`, 2). `TargetCalendarVersion` là số nguyên D8.
- **Kết quả trả cả kỳ nguồn lẫn kỳ đích:**
  - `Sources[]`: `calendar_id`, `calendar_version`, khóa gốc, khoảng ngày, nguồn, mã kỳ, cờ chốt, `Converted`.
  - Kỳ đích: `TargetCalendarID`, `TargetCalendarVersion`, `TargetRule`, `TargetType`, `TargetKey`, `TargetFrom`, `TargetTo`.
  - `RuleVersion`.
- **Bất biến** (kiểm thử `closedfig`): Σ `Sum` trên mọi kỳ đích = Σ ô của mọi kỳ nguồn, đúng tới chữ số decimal cuối, không trôi làm tròn. Mỗi kỳ nguồn nằm trong đúng một kỳ đích, và ngày cuối của nó nằm trong khoảng kỳ đích. Các ca đã kiểm:
  - ranh giới năm: tuần CNTS 29/12/2026 – 04/01/2027 → KHTC `2027-W01`;
  - năm có tuần 53 ở cả hai lịch (quét 12/2026 – 01/2033);
  - tuần vắt hai tuần KHTC;
  - cuối tháng/quý: tháng của KHTC theo ngày cuối tuần KHTC, BR-DL-01;
  - kỳ ngày cộng chính xác;
  - số tăng từ lũy kế;
  - cờ chốt khi một biểu khác của phòng còn mở.

### 7. Port KHTC duy nhất: `extension/port.KHTCSources`

- Gọi `Module.KHTCSources().Periods(ctx, tenant, KHTCQuery{Province, From, To, Departments, Target})`. Kết quả là mọi **kỳ KHTC** giao với `[From, To]` theo lịch đích §6 (mặc định tuần Thứ Tư – Thứ Ba). Mỗi kỳ có:
  - `Departments[]`, mỗi phòng nguồn gồm:
    - `SourceModule`;
    - `Forms[]`: các `TargetFigure` của §6 — số `Sum`/`Last` đã tách theo xác nhận, và `Sources[]` giữ danh tính kỳ nguồn cùng nguồn gốc;
    - `Closed`;
    - `Missing`: phòng được yêu cầu nhưng không kỳ nguồn nào quy vào (vẫn tính là đã chốt — D17/KHTCQA);
    - `Unavailable`: nguồn chưa công bố view;
    - `Fallback`: Báo giá, D22.
  - `Closed` của kỳ = mọi phòng đã chốt (D17).
  - `RuleVersion` và danh tính kỳ đích.
- Ba nguồn được đọc trong **một** giao dịch chỉ đọc của tenant, để ảnh chụp có một mốc nhất quán:
  - **Trồng trọt** — `pf_extension_pub.v1_closed_figures`, tuần Thứ Tư – Thứ Ba. Kỳ cũ v1-tam (tuần Thứ Hai) cũng quy đổi được.
  - **CNTS** — `pf_forms_pub.v1_closed_figures`, lịch riêng Thứ Ba – Thứ Hai (D8).
  - **Báo giá** — `pf_price_pub.v1_board_daily` và `v1_ticket_week` (W11.76). `closedfig.PriceFigures` đưa chúng về dạng kỳ nguồn:

    | Biểu | Kỳ nguồn | Dòng | Ô | Đã chốt khi |
    |---|---|---|---|---|
    | `PRICE-BOARD` | mỗi ngày bảng giá đã duyệt (kỳ ngày) | mặt hàng | `price_min`, `price_max`, `price_avg`, `points_reported` | ngày không đang mở duyệt |
    | `PRICE-TICKETS` | mỗi tuần báo giá (kỳ tuần) | xã báo giá | số phiếu, số ngày theo lịch, số ngày đã duyệt | mọi xã `week_closed` |

- Cách lấy số Báo giá:
  - Giá dùng `Last` = bảng giá **ngày duyệt cuối** của kỳ KHTC, min/max **toàn tỉnh** (D22).
  - Kỳ không có ngày duyệt → lấy bảng của ngày duyệt gần nhất trước kỳ (đọc lùi 62 ngày), `Fallback = true`.
  - Số phiếu dùng `Sum`.
  - View giá có từ W11.76 (pf 000152). Port vẫn dò `to_regclass` mỗi lần đọc: nếu view vắng (môi trường chưa migrate) thì phòng Báo giá `Unavailable` (không lỗi) và kỳ KHTC chưa chốt.
- Quyền: `r_extension` là thành viên `r_extension_pub`, `r_forms_pub`, `r_price_pub` (`dbadmin.PubReaders`, §4).
- Port mức thấp `ClosedFigures` (kỳ nguồn nguyên dạng, `Converted`) giữ lại cho chẩn đoán và kiểm thử. KHTC chỉ dùng `KHTCSources`.
- **Kỳ KHTC (W11.62):** `Module.KHTCSourceCrosswalk()` cài `port.KHTCSourceCrosswalk` bằng chính port này (`closedfig.KHTCCrosswalk`), nối ở `cmd/api-platform/assembly/forms.go` (`kn.UseKHTCSourceCrosswalk`).
  - `Sources(kind, key, from, to, phòng)`: mọi kỳ nguồn (mỗi biểu) quy vào kỳ KHTC theo ngày cuối, tham chiếu (`calendar_id`, `calendar_version`, khóa gốc), mã nguồn `<module, "/" → "_">.<biểu>` viết thường (vd `pf_forms.bm-04`, `pf_extension.pl01`, `pf_price.price-board` — W11.64 đổi từ dạng `pf/forms:BM-04` để khớp ràng buộc cột `khtc_period_source.source`; kỳ không theo lịch tuần ghi `calendar_version` = `-`). Ngày duyệt giá lấy dự phòng (Fallback) không phải kỳ nguồn của kỳ.
  - `States(refs)`: cờ chốt (biểu của kỳ nguồn đã chốt và có số) và lần chốt gần nhất, cùng thứ tự; tham chiếu không còn khớp → chưa chốt. "Đủ dữ liệu" của Kỳ KHTC dùng cờ này.
  - Lịch KHTC = `(*reportcycle/app.Service).CalendarAt(ctx, ke_hoach_tai_chinh, ngày đầu kỳ)` (D8). Tenant lấy từ người gọi trong ctx.

### 8. Nguồn gốc, băm, trạng thái xác nhận — ảnh chụp "Cập nhật số liệu" (W11.64) và D2

- W11.64 **chụp bất biến** kết quả của port khi cán bộ bấm "Cập nhật số liệu", không đọc trực tiếp. Mỗi dòng/kỳ nguồn mang nguồn gốc:
  - `source_module`;
  - (`calendar_id`, `calendar_version`, `period_key` gốc);
  - `closed_at`;
  - `snapshot_no`;
  - `figures_hash`.
- `figures_hash` = sha256 (hex) của **số đã chốt**: dòng lõi **không gồm** `confirmation`, cùng `totals`.
  - Băm đổi khi và chỉ khi số đã chốt đổi. Chốt lại mà số giữ nguyên → băm giữ nguyên. Mở lại mà chưa chốt lại → băm giữ nguyên.
  - W11.64 so băm với ảnh chụp để hiện **"Nguồn đã thay đổi"**. Chỉ cảnh báo, không ghi đè.
- `TargetFigure.FiguresHash` gộp (sha256) danh tính và `figures_hash` của mọi kỳ nguồn quy vào kỳ KHTC. Băm này đổi khi số của bất kỳ kỳ nguồn nào đổi, hoặc khi tập kỳ nguồn đổi.
- **D2 — HTX do TTKN tạo, "Chưa xác nhận":**
  - mỗi phần tử `rows` có `confirmation` (`confirmed` | `unconfirmed`); cột `confirmation_status` = `unconfirmed` nếu có đơn vị chưa xác nhận;
  - nguồn của trạng thái: cổng `cooperative/port.HTXConfirmations` của gói D2. Nút HTX là bí danh của neo HTX **đã nhận quyền** → `confirmed`; HTX tạm hoặc chưa neo → `unconfirmed`;
  - port KHTC nhận cổng qua `extension/port.HTXConfirmations` (cùng chữ ký; gốc lắp ráp nối `kn.UseHTXConfirmations(coop.HTXConfirmations())` khi D2 gộp). Chưa nối → dòng HTX là `unconfirmed` (thận trọng);
  - **quy tắc theo hạt** (dòng của hợp đồng là xã, cơ sở, hộ, mặt hàng, xã báo giá — không phải HTX):
    - dòng có `unit_kind = cooperative` (`unit` = mã Nút HTX) lấy trạng thái từ cổng D2;
    - dòng của đơn vị khác không phải HTX → giữ trạng thái của nguồn. View hiện trả `confirmed`; nguồn nào sau này góp số theo HTX thì phát dòng `unit_kind = cooperative`, và view có thể ghi trạng thái thật bằng `CREATE OR REPLACE VIEW` mà **không đổi cột**;
    - kỳ × biểu: `confirmation_status = unconfirmed` nếu có **ít nhất một** dòng chưa xác nhận;
    - phần chưa xác nhận lộ ra ở `SumUnconfirmed`/`LastUnconfirmed` (cộng theo dòng). `totals` do nguồn tính (dòng Khu vực/Toàn tỉnh của Trồng trọt) không tách được;
  - hiện chưa nguồn nào phát dòng HTX (Trồng trọt gộp theo xã, CNTS theo xã/cơ sở/hộ), nên số hiện tại đều `confirmed` theo đúng quy tắc;
  - số "Chưa xác nhận" vẫn vào báo cáo: `Sum`/`Last` là tổng, kèm `SumConfirmed`/`SumUnconfirmed` và `LastConfirmed`/`LastUnconfirmed` (tổng tách hai phần, decimal chính xác);
  - **trạng thái không vào `figures_hash`**. Nó có băm riêng `status_hash` (và `TargetFigure.StatusHash`). Nhận HTX (unconfirmed → confirmed) chỉ đổi `status_hash`, nên không gây cảnh báo "Nguồn đã thay đổi" giả.
- Báo giá: view giá không có cột băm, nên `PriceFigures` tính `figures_hash`/`status_hash` ở Go theo cùng quy tắc.
- Khi cổng D2 đổi trạng thái một dòng HTX, port tính lại `status_hash` ở Go (sha256 của `key=trạng thái` theo thứ tự dòng). `figures_hash` không đổi.

## Hệ quả

- KHTC có một nền ổn định: một port (`KHTCSources`) cho ba phòng nguồn, một họ cột. Thêm nguồn hay đổi nguồn của phòng không đụng mã KHTC.
- Port giá phụ thuộc hợp đồng `pf_price_pub` (published ở W11.76, pf 000152). Đổi cột ở đó = `v2` (hợp đồng), và `readPrice` phải theo; `TestPubViewsDeclaredAndPrivileges_W63` bắt lệch cột giữa view thật và hợp đồng.
- Thêm vai `r_forms_pub` (bootstrap phải chạy trước `migrate up`; pf 20261013150108 dừng nếu thiếu). `r_metrics`/`r_agg` tự là thành viên, theo mẫu W6.3/W6.4.
- `dbadmin.PubReaders` là điểm rà soát mới. Mỗi mục là một bên đọc chéo module lâu dài và phải ghi vào `consumers` của hợp đồng.
- `pf/extension` đọc `pf_forms_pub` mà không thêm cạnh `depends` (không import mã). Cạnh `pf/extension → pf/forms` vốn đã có.
- `key_closed` của từng view chỉ phản ánh nguồn của nó. Bên đọc khác KHTC muốn cờ toàn phòng phải dùng port hoặc tự AND theo khóa.

## Hiện thực

### PFPUB (13/10/2026)

- pf 20261013150108 `pf_forms_pub` (schema, view, GRANT `r_forms_pub`); pf 20261013150110 `pf_extension_pub.v1_closed_figures` (GRANT `r_extension_pub`).
- `//ag:pubview` trong `forms/port/pub.go` và `extension/port/pub.go`, `modgen` (`pf/forms` sở hữu `pf_forms_pub`).
- Hai hợp đồng `published` cùng `family`, cùng `pubview.FamilyProblems`.
- `dbadmin.PubReaders` (`TestPubReaders_PFPUB`), port `ClosedFigures` + `closedfig.Reader`.
- Kiểm thử tích hợp `tests/kn77` `TestPFPUB_ClosedFigures`:
  - CNTS: chốt → thấy, mở lại → cờ về false và số giữ, chốt lại → ảnh và số mới, `key_closed` theo mọi biểu của tuần;
  - Trồng trọt qua cùng bộ đọc và cùng cột;
  - cách ly tenant;
  - không PII (không tên người, không mã người nộp, không trường văn bản);
  - `r_extension` bị từ chối đọc `pf_forms.period`.
- `TestPubViewsDeclaredAndPrivileges_W63`: 5 module công bố, 14 view.
- Cùng ngày, theo quyết định người dùng:
  - Trồng trọt là nguồn lâu dài (§5).
  - Port `KHTCSources` + `PriceFigures` + `Assemble` (§7); `r_extension` có thêm thành viên `r_price_pub`.
  - Cột `source_module`, `confirmation_status`, `figures_hash`, `status_hash` và `rows[].confirmation` (§8).
  - Kiểm thử đơn vị `closedfig/khtc_test.go`: Báo giá ngày duyệt cuối và Fallback, số phiếu, Missing, Unavailable, tách xác nhận, nhận HTX chỉ đổi băm trạng thái.
  - Kiểm thử tích hợp đọc kỳ KHTC 2026-W37 gồm Trồng trọt, CNTS, Báo giá (view W11.76 đọc thật bằng `r_price_pub`; tenant chưa có bảng giá duyệt → Missing); kiểm băm số không đổi khi mở lại và đổi khi chốt lại với số mới.
- Theo gói D2 (neo HTX): `extension/port.HTXConfirmations`, `Module.UseHTXConfirmations`, `closedfig/htx.go` (quy tắc theo hạt §8), kiểm thử `TestApplyHTX_D2_PFPUB` (HTX đã nhận quyền / HTX tạm / chưa nối cổng, tách tổng, nhận quyền chỉ đổi băm trạng thái). Đã nối ở gốc lắp ráp (`cmd/api-platform/assembly/modules.go`); `extension/port.HTXConfirmations` là bí danh của `cooperative/port.HTXConfirmations`.
- Theo gói D8 (lịch theo phòng — dựng trên nhánh `d8cal`, hai commit D8 nằm dưới commit PFPUB cho tới khi D8 gộp vào dev): `calendar_version` thành integer theo `CalendarOf`; lịch đích qua `CalendarOf`; khóa kỳ nguồn `CalendarRef(id, v):native_key`. Kiểm thử đối chứng `TestCrosswalk_GoldenKHTCQA_PFPUB` chạy `Convert` trên mọi dòng `tests/golden/khtc/crosswalk.csv` (Trồng trọt v1/v2, CNTS Thứ Ba – Thứ Hai, Báo giá kỳ ngày, W53, vắt năm) và kiểm `SpanOfKey` khóa gốc ↔ khoảng ngày.
- D8 (cập nhật cùng ngày): thêm cột `calendar_id`, `calendar_version` vào họ hợp đồng. Bảng chuyển đổi `closedfig.Convert` + `Increment` + `ClosedFigures.Converted`, kiểm thử đơn vị `closedfig/crosswalk_test.go`. Kiểm thử tích hợp đọc quy đổi kỳ CNTS sang tuần KHTC.
