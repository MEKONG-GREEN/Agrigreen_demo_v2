# Thiết kế ranh giới `pf/cooperative` (`dom/cooperative`, schema `pf_coop`)

**Gói:** W3.1 ([kế hoạch v4.1](../kien-truc-v4.1-ke-hoach.md)). Bản này là đầu vào cho W3.2 (module Go), W3.3 (chuyển schema, pf 000094–000096), W3.4 (RLS theo Nút, pf 000097) và W3.5 (hợp nhất dữ liệu UAT).
**Ngày:** 06/10/2026 · **Trạng thái:** đề xuất. Giả định chủ dữ liệu theo phương án (a) của H-1 (mục 11).
**Đọc cùng:**
- [bản đối chiếu](../kien-truc-v4.1-doi-chieu.md) §2.2, §2.3, X-01, DB-07;
- [ADR-0027](../adr/0027-bon-tang-module-tep-khai-bao.md) (bảng ánh xạ tên, `kind`);
- [ADR-0028](../adr/0028-vai-dang-nhap-theo-don-vi-trien-khai.md) (`r_<module>`);
- [ADR-0029](../adr/0029-k-ref-ban-chieu-loi-moi-cum.md) (`k_ref`);
- [ADR-0030](../adr/0030-doc-tai-cho-co-cap-phep.md) (`shares`, `p_scope`, H-1 §7);
- [ADR-0012](../adr/0012-duy-nhat-tren-bang-so-phan-vung.md) (sổ, `chain_head`);
- kiến trúc v4.1 §17.2, §17.3, §27.1;
- [docs/dev/modules.md](../dev/modules.md).

Số liệu của bản này lấy từ mã ở cab0280: `migrations/pf/*.up.sql`, `internal/platform/{party,geo,cropcycle}`, `cmd/*/assembly`.

---

## 1. Tóm tắt quyết định

| # | Quyết định | Lý do ngắn |
|---|---|---|
| D-1 | Module mới `pf/cooperative`, `aka: [dom/cooperative]`, `kind: domain`, schema **`pf_coop`**, vai **`r_coop`** | Tên theo kế hoạch W3.3 và ADR-0030. Schema và vai lệch quy ước `pf_<module>`/`r_<module>`, nên W3.2 phải sửa `modreg.DBRole` (mục 7.2) |
| D-2 | Chuyển bằng `ALTER … SET SCHEMA`. Không sao chép, không `UPDATE`. **Không đổi tên bảng** trong W3.3 | Giữ OID, ACL, chính sách, chỉ mục, trigger và chuỗi băm. Tên logic của v4.1 (`season`, `diary_entry`) ánh xạ ở `shares` (mục 7.3) |
| D-3 | **Giữ nguyên tên sổ** (tham số `TG_ARGV[0]` của `trg_chain`, vd `'pf_geo.plot_boundary'`). Không chèn tên mới vào `shared.chain_head` | Tên sổ là định danh logic, không phải vị trí. Đổi tên làm hỏng `migrate verify-chains`, `ledger_idem` và `chain_anchor` (mục 8.3). Lệch chữ của dòng W3.3 trong kế hoạch, ghi lý do ở mục 8.3 |
| D-4 | Chuyển cùng vụ cả **SOP** (`sop_template`, `sop_version`, `variety_sop_binding`) và **`harvest_declaration`, `yield_summary`**. `pf_cropcycle` chỉ còn van gom HTX (`collection_*`) | `yield_summary` (tiến độ SOP) được dựng lại trong **cùng giao dịch** với duyệt nhật ký (R-21). Để SOP ở schema khác nghĩa là cấp quyền chéo, hoặc tạo chu trình `cooperative ↔ cropcycle` |
| D-5 | Chuyển thêm `pf_geo.boundary_conflict` và `pf_party.kn_profile_event` | Hai bảng chỉ có chủ thể là thửa hoặc HTX/hộ. `kn_profile_event.subject_kind ∈ {cooperative, household}` |
| D-6 | `pf_party.org` (cây Nút), `org_event`, `cooperative_code_seq` **ở lại** `pf_party`. Mã HTX vẫn là `org.code` | HTX là một Nút của `k/orgtree`. `pf_coop.cooperative` là hồ sơ nghiệp vụ gắn với Nút đó qua `org_ref` |
| D-7 | Giữ hai FK chéo schema `pf_coop.{cooperative,household_membership} → pf_party.org`. Ghi thành ngoại lệ có tên | Toàn vẹn tham chiếu do chủ bảng (`ag_migrator`) kiểm, không cần quyền chéo cho `r_coop`. Nút không bao giờ bị xóa, chỉ vô hiệu hóa |
| D-8 | Giữ tên sự kiện v1 (`pf.party.household.*`, `pf.geo.plot*.*`, `pf.cropcycle.{season,farm_log,harvest,coop_season_area}.*`). Đổi chủ khai `//ag:event`, không đổi hợp đồng | Bên tiêu thụ hiện có (`mrv` khai lại tên sự kiện, `metrics`, `notify`, `mechanization`) không phải sửa. Tên `pf.coop.*` chỉ dùng cho v2 nếu cần |
| D-9 | Không đổi đường dẫn HTTP, kể cả `/v1/partner/{cooperatives,plots,season-harvest-dates}`. Handler chuyển sang `pf/cooperative` | `mkg/fieldops` gọi qua HTTP (`adapter/directory.go`), nên không bị ảnh hưởng. W5.x (ADR-0033) thay `/v1/partner/*` sau |
| D-10 | Phương án H-1 (a): chủ là tenant chương trình. Phạm vi trong đơn vị dùng `p_scope` theo Nút = `cooperative_ref` | ADR-0030 §7. Mục 11 nêu những gì đổi nếu chọn (b) |

---

## 2. Kiểm kê bảng chuyển sang `pf_coop`

Ký hiệu loại:
- **Sổ**: `apply_ledger`, có phân vùng tháng, `trg_chain` + `trg_immutable`;
- **Chiếu**: bảng chiếu R-21, có `apply_tenant_rls`;
- **QT**: bảng quy trình có trigger bảo vệ;
- **HT**: bảng hiện tại.

Mọi bảng hiện có chính sách `p_tenant` (`shared.apply_tenant_rls`, `FORCE RLS`). Bảng phân vùng có thêm RLS riêng trên từng phân vùng (`shared.secure_partition`).

### 2.1 Từ `pf_party` (module `pf/party`)

| Bảng | Migration | Loại | `TG_ARGV[0]` | FK | Đối tượng kèm | Ghi |
|---|---|---|---|---|---|---|
| `cooperative` | 000003, 000032 (`head_office`) | HT | — | `(tenant_id, org_ref) → pf_party.org` **(chéo, D-7)** | chỉ mục `cooperative_tax_code`, `cooperative_location` (gist) | `party/store/org.go` chèn trong `CreateOrg` (cùng giao dịch với `org`, mục 8.6) |
| `household` | 000003 | HT | — | — | `household_commune` | Chỉ có `household_ref` trỏ vault (R-41) |
| `household_membership` | 000003, 000017 | HT | — | `→ household` (cùng chuyển); `(tenant_id, org_ref) → pf_party.org` **(chéo)** | `household_membership_open` (duy nhất một), `_org`, `_org_open` | `party/store/household.go` có `UPDATE … SET until_date` |
| `kn_profile_event` | 000078 | Sổ | `'pf_party.kn_profile_event'` | — | phân vùng `kn_profile_event_default`, `_YYYYMM` | D-5. Migration 000078 thuộc `pf/mechanization`, có ngoại lệ `schema` riêng (W7.1) |

**Ở lại `pf_party`:** `org`, `org_event`, `consent*`, `department*`, `region*`, `share_grant*`, `partner_read_log`, `module_enablement_*`, `session_context`… cùng `cooperative_code_seq`.

### 2.2 Từ `pf_geo` (module `pf/geo`)

| Bảng | Migration | Loại | `TG_ARGV[0]` | Đối tượng kèm | Ghi |
|---|---|---|---|---|---|
| `plot` | 000004, 000079 (thuộc tính KN), 000084 | HT (`trg_no_delete`, `trg_no_truncate`) | — | `plot_code_seq`, `plot_sync_seq` (**chuỗi số độc lập, không OWNED BY**: `DEFAULT nextval('pf_geo.plot_sync_seq')`); chỉ mục `plot_geom`, `plot_commune`, `plot_cooperative`, `plot_sync`, `plot_kn_list` | Hình học là dữ liệu cá nhân gián tiếp (v4.1 §28) |
| `plot_boundary` | 000004, 000079, 000084 | Sổ | `'pf_geo.plot_boundary'` | phân vùng | `geo/app/metrics.go` có tên bảng cứng |
| `plot_event` | 000004, 000018, 000079 | Sổ | `'pf_geo.plot_event'` | phân vùng | |
| `boundary_conflict` | 000004 | QT | — | hàm `pf_geo.fn_boundary_conflict_guard()` | D-5 |
| `cooperative_boundary` | 000022 | Sổ | `'pf_geo.cooperative_boundary'` | phân vùng | |
| `cooperative_area` | 000022 | HT (`trg_no_delete`) | — | — | Nguồn của `CoopCentroids` |

**Ở lại `pf_geo` (`cap/geo`):** `admin_boundary`, `facility*`, `hub_view`, `capacity_latest`, `road*`, `gps_fix` và hàm `fn_admin_boundary_guard`.

### 2.3 Từ `pf_cropcycle` (module `pf/cropcycle`)

| Bảng | Migration | Loại | `TG_ARGV[0]` | Đối tượng kèm |
|---|---|---|---|---|
| `sop_template`, `sop_version`, `variety_sop_binding` | 000020 | QT | — | `sop_sync_seq`; ba hàm `pf_cropcycle.fn_sop_template_guard`, `fn_sop_version_guard`, `fn_variety_binding_guard` (D-4) |
| `season_event` | 000021 | Sổ | `'pf_cropcycle.season_event'` | phân vùng |
| `farm_log_batch` | 000021 | Sổ | `'pf_cropcycle.farm_log_batch'` | phân vùng |
| `farm_log` | 000021 | Sổ | `'pf_cropcycle.farm_log'` | phân vùng |
| `farm_log_review` | 000021 | Sổ | `'pf_cropcycle.farm_log_review'` | phân vùng |
| `harvest_declaration` | 000021 | Sổ | `'pf_cropcycle.harvest_declaration'` | phân vùng (D-4) |
| `season_current` | 000021 | Chiếu | — | `season_sync_seq` |
| `farm_log_current` | 000021 | Chiếu | — | `farm_log_sync_seq` |
| `coop_season_area` | 000021 | Chiếu | — | — |
| `yield_summary` | 000021 | Chiếu | — | — (D-4) |

**Ở lại `pf_cropcycle`:** `collection_valve_receipt` (sổ), `collection_schedule_view`, `collection_statement_view`, `collection_response` (sổ), `collection_sync_seq`, `collection_response_feed_seq`. Đây là van MKG ↔ HTX (pf 000040). ADR-0027 ghi "van còn lại đến G2". Sau G2, ADR-0033 (cổng tích hợp) quyết định nơi đặt.

**Không chuyển** (đúng chủ theo §2.2 bản đối chiếu):
- `pf_mechanization.season_area_event`, `season_area`: bản chiếu của CGH, nhận qua sự kiện `coop_season_area.changed`;
- `pf_mechanization.coop_machine_count*`: thuộc `dom/mechanization`;
- `pf_accounts.farmer_link`: liên kết tài khoản, chỉ giữ `household_ref` dạng UUID thuần, không FK.

### 2.4 Tổng hợp đối tượng phải chuyển

| Loại | Số | Danh sách |
|---|---|---|
| Bảng cha | 22 | 4 (`pf_party`) + 6 (`pf_geo`) + 12 (`pf_cropcycle`) |
| Bảng sổ (có phân vùng) | 9 | `kn_profile_event`, `plot_boundary`, `plot_event`, `cooperative_boundary`, `season_event`, `farm_log_batch`, `farm_log`, `farm_log_review`, `harvest_declaration` |
| Phân vùng | ~9 × 26 | `<sổ>_default` + `<sổ>_YYYYMM`. `create_monthly_partitions` tạo trước 24 tháng; job `maintain` tạo thêm. **`ALTER TABLE <cha> SET SCHEMA` không chuyển phân vùng** (mục 8.2) |
| Chuỗi số độc lập | 5 | `plot_code_seq`, `plot_sync_seq`, `sop_sync_seq`, `season_sync_seq`, `farm_log_sync_seq` |
| Hàm trigger trong schema module | 4 | `pf_geo.fn_boundary_conflict_guard`, `pf_cropcycle.fn_sop_template_guard`, `fn_sop_version_guard`, `fn_variety_binding_guard` |
| View, hàm SQL tham chiếu bảng | 0 | Không view hay hàm SQL nào khác nhắc các bảng này. `shared.fn_chain` chỉ dùng `TG_ARGV` |
| FK chéo schema sau khi chuyển | 2 | `cooperative.org_ref`, `household_membership.org_ref` → `pf_party.org` (D-7) |
| FK từ module khác vào bảng chuyển | 0 | `farmer_link`, `season_area`, `coop_machine_count` chỉ giữ UUID |

**Quyền hiện có trên các bảng chuyển:**
- vai đăng nhập cũ `party_app`, `geo_app`, `cropcycle_app` (pf 000003, 000004, 000020, 000021, 000022, 000078);
- `r_party`, `r_geo`, `r_cropcycle`, do pf 000090 chép từ vai cũ;
- W2.6 (pf 000119) bỏ quyền bảng của `<module>_app`.

ACL đi theo bảng khi `SET SCHEMA`, nên W3.3 phải `REVOKE` của `r_party`/`r_geo`/`r_cropcycle` và `GRANT` cho `r_coop` (mục 8.4). Không có quyền nào trên các bảng này cấp cho `ag_outbox_publisher`, `r_agg` hay vai giám sát.

**SQL trong mã Go nhắc tên bảng:**
- **Chỉ trong module chủ**: `party/store/{org,household,kn_collect,readmodel,sharing,wiring}.go`, `geo/store/{plot,coop,kn,conflict_detail}.go`, `geo/app/metrics.go`, `cropcycle/store/{season,log,harvest,…}.go`, `cropcycle/app/metrics.go`.
- Ngoài module chủ: không có trong mã chạy thật.
- Kiểm thử có SQL thẳng: `cmd/api-public/integration_test.go` (`pf_geo.plot`, `pf_cropcycle.season_current`), `tests/e2e/g5_scenario_test.go`, `tools/migratecheck/main_test.go`. W3.3 phải sửa cả các tệp này.

---

## 3. Ranh giới module sau W3.3

| Module | `kind` | Giữ | Mất cho `pf/cooperative` |
|---|---|---|---|
| `pf/party` | kernel (`k/orgtree` + `k/access`) | Cây Nút, phòng ban, khu vực, đồng ý, cấp phép, `Orgs`, `ScopeResolver`, `PartnerAccess` (đến W5.x) | Hồ sơ HTX, hộ, xã viên, sổ hồ sơ KN. Màn KN FN-XA-03.03 (`/v1/kn/cooperatives*`, `/v1/kn/households*`). `/v1/partner/cooperatives*`. Port `Households`, `CooperativeStats`, `CooperativeSites`, `SeasonActivity`, `CoopMachineCounts` |
| `pf/geo` | capability (`cap/geo`) | Lớp bản đồ, ranh giới hành chính, kho trung chuyển, đường, `GpsLog` | Thửa, ranh giới thửa, xung đột, ranh giới và diện tích HTX. `/v1/kn/plots` (FN-XA-03.05). `/v1/partner/plots*`. Port `Plots`, `PlotFacts`, `PlotStats`, `PlotHouseholds`, `CoopCentroids`, `FarmerIdentity` |
| `pf/cropcycle` | domain (van) | `collection_*` (van MKG ↔ HTX), port collection | Vụ, nhật ký, khai báo sản lượng, SOP. `/v1/partner/season-harvest-dates`. Port `Stats`, `SupplyStats`, `ReportData`. Đổi `aka` thành rỗng (hoặc `dom/cooperative`/van) ở W3.3 |
| **`pf/cooperative`** | domain (`dom/cooperative`) | Toàn bộ mục 2 | — |

**Hướng phụ thuộc** (đi xuống theo R-MOD-05, không chu trình):

```
pf/cooperative ──► pf/party (Orgs, ScopeResolver, PartnerAccess)   kernel
               ──► pf/kref  (SQL đọc k_ref.*; không port)           kernel
               ──► pf/geo   (GpsLog, AdminBoundary nếu cần)         capability
               ──► pf/masterdata, pf/sync, pf/metrics, pf/metering capability
               ──► vault/identity (FarmerIdentity)
pf/mechanization, pf/reports, pf/extension, pf/accounts, pf/notify, pf/cropcycle(van) ──► pf/cooperative
```

**Đảo phụ thuộc đi lên.** Hôm nay `party` và `geo` gọi ngược dữ liệu HTX/thửa. Sau W3.3, module tầng dưới **khai interface trong `port/` của chính nó**, `pf/cooperative` hiện thực, assembly nối. Đây là mẫu đã dùng cho `OrgImpactSource`, `CoopMarkers`, `SeasonActivity`.

| Bên cần (tầng dưới) | Interface (khai ở bên cần) | Hiện thực |
|---|---|---|
| `pf/party`: tác động khi di chuyển hoặc vô hiệu hóa Nút (`kn_tree.go` đếm HTX, thửa) | `partyport.OrgImpactSource` (đã có; `ImpactCooperatives`, `ImpactPlots`) | `pf/cooperative` |
| `pf/geo`: lớp HTX trên bản đồ | `geoport.CoopMarkers` (đã có) | chuyển từ `party/store/wiring.go` sang `pf/cooperative` |
| `pf/geo`: `GpsLog.RecordFix` tính `DistanceToPlotM` (hôm nay đọc `pf_geo.plot`) | **mới** `geoport.PlotGeometry{ Centroid/Distance(ctx, plotRef, lon, lat) }` | `pf/cooperative` |

Cách này gỡ:
- các ngoại lệ `tier` `pf/party → pf/geo | pf/masterdata | pf/sync` (W3.3);
- phần lớn ngoại lệ `cycle` có chủ W3.2/W3.3 trong `tools/archlint/modgraph_exceptions.yaml`: 15 mục W3.2 và 12 mục W3.3.

W3.2 và W3.3 phải xóa các mục đã gỡ.

---

## 4. Port mới `internal/platform/cooperative/port` (`cooperativeport`)

Chữ ký suy từ những gì bên tiêu thụ gọi hôm nay. Mục 5 ánh xạ từng lời gọi. DTO giữ nguyên trường của DTO cũ để W3.2 chuyển bằng đổi import, không đổi ngữ nghĩa. Tiền tệ, khối lượng, diện tích dùng `decimal`/`mass.Kg`.

Plan W3.1 liệt kê năm port. Bản này thêm **`Cooperatives`** thứ sáu, vì ba bên tiêu thụ (`extension`, `simulation`, `geo`) đọc dữ liệu cấp HTX không thuộc hộ, thửa hay vụ. Ghép vào `Households` sẽ làm port đó mơ hồ.

```go
package cooperativeport

// Cooperatives: hồ sơ HTX (Nút kind=cooperative của pf/party + pf_coop.cooperative + cooperative_area).
type Cooperatives interface {
	// thay partyport.CooperativeStats.CooperativesByUnit (extension unified.go)
	ByUnit(ctx context.Context, units []string) ([]UnitCooperatives, error)
	// thay partyport.CooperativeSites.ActiveCooperatives (assembly SupplySource → simulation)
	Active(ctx context.Context) ([]CooperativeSite, error)
	// thay geoport.CoopCentroids.CoopCentroids (tâm cooperative_area đang hiệu lực); coops rỗng = tất cả
	Centroids(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]CoopCentroid, error)
}

// Households: nông hộ (chỉ household_ref của vault, R-41) và xã viên theo thời gian.
type Households interface {
	// thay partyport.Households.Membership (accounts farmer.go; nội bộ geo service.go:266)
	Membership(ctx context.Context, ref uuid.UUID) (HouseholdMembership, error) // không có → ErrHouseholdNotFound
	// thay geoport.PlotHouseholds.HouseholdsInCommunes (assembly AreaFarmers → notify)
	InCommunes(ctx context.Context, communes []string) ([]uuid.UUID, error)
}

// Plots: thửa và hình học (dữ liệu cá nhân gián tiếp — không trả geometry qua port).
type Plots interface {
	Get(ctx context.Context, ref uuid.UUID) (Plot, error)                                   // geoport.Plots.Get
	DistanceM(ctx context.Context, ref uuid.UUID, lon, lat decimal.Decimal) (decimal.Decimal, error) // geoport.Plots.DistanceM
	Facts(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]Plot, error)                 // geoport.PlotFacts.PlotFacts
	DigitizedByCommune(ctx context.Context, communes []string) ([]PlotStat, error)           // geoport.PlotStats
	DigitizedByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]PlotStat, error)
}

// Seasons: vụ, khai báo sản lượng, diện tích vụ theo HTX, số liệu báo cáo.
type Seasons interface {
	ActivityByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]CoopActivity, error) // ccport.Stats
	Areas(ctx context.Context, coop uuid.UUID, seasonCode string, year int) ([]CoopSeasonArea, error)  // ccport.Stats.SeasonAreas
	Supply(ctx context.Context, year int) ([]CoopSeasonSupply, error)                                  // ccport.SupplyStats.SeasonSupply
	Production(ctx context.Context, q ProductionQuery) (ProductionData, error)                         // ccport.ReportData.Production
	HarvestDates(ctx context.Context, plotRefs []uuid.UUID) ([]SeasonHarvestDate, error)              // mới: thay đọc chéo của van collection
}

// Diary: nhật ký canh tác (sổ farm_log + farm_log_current), bằng chứng MRV (ADR-0011).
// Hôm nay chưa bên nào gọi đồng bộ (mrv dùng sự kiện); port phục vụ cap/evidence (W6.5) và đọc chia sẻ (W4.2).
type Diary interface {
	Get(ctx context.Context, ref uuid.UUID) (DiaryEntry, error)                  // phiên bản hiện hành của log_ref
	BySeason(ctx context.Context, season uuid.UUID, after uuid.UUID, limit int) ([]DiaryEntry, error)
}

// Resolve: bộ ba (subject_module, subject_type, subject_id) của R-DB-07 / cap/evidence (W6.5).
type Resolve interface {
	Resolve(ctx context.Context, t SubjectType, id uuid.UUID) (Subject, error) // không có hoặc ngoài phạm vi → ErrSubjectNotFound
}

type SubjectType string // "cooperative" | "household" | "plot" | "season" | "diary_entry" | "harvest"

type Subject struct {
	Type          SubjectType
	ID            uuid.UUID
	TenantID      uuid.UUID // đơn vị chủ (H-1)
	NodeRef       uuid.UUID // Nút phạm vi = cooperative_ref; uuid.Nil khi thửa nháp chưa gán HTX
	CommuneCode   string
	HouseholdRef  uuid.UUID // uuid.Nil nếu không gắn hộ
	Status        string
	ViaShareGrant bool      // W4.2
}

// Consumer-defined (cooperative cần, module khác hiện thực):
type MachineCounts interface { // thay partyport.CoopMachineCounts; pf/mechanization hiện thực (FN-XA-03.03)
	CoopMachineTotal(ctx context.Context, cooperativeRef uuid.UUID) (int, error)
}
type FarmerHouseholds interface { // thay geoport/ccport.FarmerHouseholds; assembly nối qua pf/accounts
	HouseholdsOf(ctx context.Context, actorRef uuid.UUID) ([]uuid.UUID, error)
}
```

**DTO chuyển nguyên trạng:**
- `HouseholdMembership`, `UnitCooperatives`, `CooperativeSite`, `CoopCentroid` (party, geo);
- `Plot`, `PlotStat` (geo);
- `CoopActivity`, `CoopSeasonArea`, `CoopSeasonSupply`, `ProductionQuery`, `ProductionData`, `ProductionFigures` (cropcycle).

**DTO mới:** `DiaryEntry` lấy cột của `farm_log_current`, không có GPS thô. `SeasonHarvestDate` lấy từ `cropcycle/app/partner.go`.

**Hằng số:**
- vai: `RoleCoopManager`, `RoleFarmer`, `RolePlotEditor`, `RolePlotViewer`, `RoleSOPAdmin`;
- loại chia sẻ `Kind*`: chuyển dần, nhưng ba hằng `KindCooperativeDirectory`, `KindPlotDirectory`, `KindSeasonHarvestDates` vẫn ở `partyport` đến W5.x;
- sự kiện và thực thể đồng bộ (`EntityPlot`, `EntityPlotBoundary`, `EntitySeason`, `EntityFarmLog`, `EntitySeasonOpen`, `EntityLogBatch`, `EntityHarvest`).

**Port cũ trong W3.2.**
- `partyport.Households`, `CooperativeStats`, `CooperativeSites`, `geoport.Plots`, `PlotFacts`, `PlotStats`, `PlotHouseholds`, `CoopCentroids`, `ccport.Stats`, `SupplyStats`, `ReportData` còn tồn tại, đánh dấu `// Deprecated: dùng cooperativeport.X (W3.2)`.
- `pf/cooperative` hiện thực các port mới bằng cách gọi port cũ (store tạm). Bên tiêu thụ đổi import trong W3.2.
- W3.3 chuyển store vào `pf/cooperative`, rồi xóa port cũ (`version` MAJOR của `party`, `geo`, `cropcycle` tăng lên 2.0.0, R-MOD-06).

---

## 5. Ánh xạ port cũ → mới theo bên tiêu thụ

| Bên tiêu thụ | Tệp | Hôm nay | Sau W3.2 |
|---|---|---|---|
| `pf/accounts` | `app/farmer.go:94-107, :504` | `partyport.Households.Membership`, `ErrHouseholdNotFound` | `cooperativeport.Households.Membership`. `Orgs.Get` + `OrgCooperative` giữ ở party |
| `pf/extension` | `app/unified.go:126, :140` | `partyport.CooperativeStats.CooperativesByUnit`, `geoport.PlotStats.DigitizedByCommune` | `Cooperatives.ByUnit`, `Plots.DigitizedByCommune` |
| `pf/extension` | `app/gpslog.go` | `geoport.GpsLog.RecordFix` | Giữ (`cap/geo`) |
| `pf/extension` (KN P7) | — | P7 của extension chỉ đồng bộ báo cáo. Màn "hồ sơ HTX + máy + thửa" nằm ở party (FN-XA-03.03), mechanization (FN-XA-03.04), geo (FN-XA-03.05) | FN-XA-03.03 và 03.05 chuyển **vào** `pf/cooperative` (handler `/v1/kn/cooperatives*`, `/v1/kn/households*`, `/v1/kn/plots`). 03.04 ở lại mechanization, cooperative đọc tổng máy qua `MachineCounts` |
| `pf/mechanization` | `app/areasync.go` | sự kiện `ccport.EventCoopSeasonAreaChanged`, `CoopSeasonAreaChangedV1` | `cooperativeport.EventCoopSeasonAreaChanged` (tên `pf.cropcycle.coop_season_area.changed` giữ, D-8) |
| `pf/mechanization` | `app/machine_count.go:408`, `module.go:61` | hiện thực `partyport.CoopMachineCounts` | hiện thực `cooperativeport.MachineCounts` |
| `pf/mrv` | `app/g3.go`, `port/g3.go` | Không import. Khai lại tên sự kiện farm_log/season và tự giải mã | Không đổi (D-8). W6.5 dùng `Diary` + `Resolve` cho bằng chứng |
| `pf/reports` | `app/build_htx.go:76, :105`, `service.go:106`, `module.go:59` | `ccport.ReportData.Production`, `ccport.RoleCoopManager` | `Seasons.Production`, `cooperativeport.RoleCoopManager` |
| `pf/notify` | `app/consume.go`, `domain/{templates,cropcycle}.go`, `module.go:143-146` | Kiểu sự kiện của `partyport` (`HouseholdRegisteredV1`, `HouseholdMembershipChangedV1`, `Membership*`, `HouseholdSourceHTXApp`) và `ccport` (`FarmLogRecordedV1`, `FarmLogReviewedV1`, `SeasonOpenedV1`, `LogNeedsSupplement`) | Kiểu của `cooperativeport`. Sự kiện `collection_*` giữ `ccport` |
| `pf/metrics` | `app/consumer.go:33-36` | Kiểu sự kiện geo/cc. `metricsport.Source` từ `cc.MetricsSource()`, `g.MetricsSource()` | Kiểu `cooperativeport`. `coop.MetricsSource()`. Tên bảng trong `geo/app/metrics.go:89-90`, `cropcycle/app/metrics.go:61,108` đổi sang `pf_coop.*` ở W3.3 |
| `pf/simulation` | qua assembly `SupplySource` | `SupplyStats.SeasonSupply`, `CooperativeSites.ActiveCooperatives`, `CoopCentroids` | `Seasons.Supply`, `Cooperatives.Active`, `Cooperatives.Centroids` |
| `pf/notify` (khu vực) | assembly `AreaFarmers`, `cmd/worker/notify.go` | `geoport.PlotHouseholds.HouseholdsInCommunes` | `Households.InCommunes` |
| `pf/party` | `app/readmodel.go:98`, `kn_collect.go:525`, `kn_tree.go:343`, `app/wiring.go:100` | `geoport.PlotStats`, tiêu thụ `SeasonActivity`, `CoopMachineCounts`, hiện thực `geoport.CoopMarkers` | `kn_collect`, `readmodel` (danh bạ HTX), `CoopMarkers` chuyển vào cooperative. `kn_tree` dùng `OrgImpactSource` do cooperative hiện thực. Party **không** import `cooperativeport` (kernel) |
| `pf/geo` | `app/service.go:247, :266`, `coop.go:84`, `importer.go:197` | `partyport.Households.Membership`, `Orgs.Get`, `OrgCodes.ByCode` | Mã thửa chuyển vào cooperative, gọi `Households` nội bộ. `Orgs`/`OrgCodes` gọi party như cũ |
| `pf/cropcycle` (van còn lại) | `app/collection.go:333`, `dataproduct.go:121` | `geoport.Plots.Get/DistanceM`, `geoport.PlotFacts` | `Plots.Get/DistanceM`, `Plots.Facts` |
| `cmd/api-platform/assembly` | `modules.go`, `wiring.go`, `consumers.go` | Nối như trên | Thêm `coop := cooperative.New(…)`. Đổi mọi `pty.Households()`, `g.PlotStats()`, `cc.Stats()`… sang `coop.*`. Consumer `GpsLogConsumer` của cc chuyển sang coop |
| `cmd/api-public/assembly` | `assembly.go` | `geo.New`, `cropcycle.New`, `cc.UseCollectionVolume(g.PlotFacts(), …)` | Thêm `cooperative.New` (sản phẩm dữ liệu thửa/vụ đi theo dữ liệu). `cc.UseCollectionVolume(coop.Plots(), …)`. `grantlookup` giữ |
| `cmd/worker` | `main.go`, `reports.go`, `notify.go` | `cc` vào reports, `geo.New(...).PlotHouseholds()` | `coop.Seasons()` vào reports, `coop.Households()` vào notify. Consumer inbox của cooperative chạy dưới `module: cooperative.DBModule` |
| `/v1/partner/cooperatives*` | `party/handler/sharing.go:38-39` | party | cooperative (cùng đường dẫn, cùng `KindCooperativeDirectory`, cùng `partner_read_log` qua `PartnerAccess.RecordRead`) |
| `/v1/partner/plots*` | `geo/handler/http.go:27-28` | geo | cooperative |
| `/v1/partner/season-harvest-dates` | `cropcycle/handler/collection.go:25` | cropcycle | cooperative |
| `/v1/partner/htx-collection/responses` | `cropcycle/handler/collection.go:23` | cropcycle | giữ (van) |
| `internal/mkg/*` | `fieldops/adapter/{directory,htxfeed}.go` | HTTP `/v1/partner/*` | **Không đổi** (D-9; R-03 cấm import platform) |
| `pf/sync` | `syncport.Source`/`Sink` mỗi module | geo, cc, party đăng ký thực thể | cooperative đăng ký `plot`, `plot_boundary`, `season*`, `farm_log`, `log_batch`, `harvest`, hộ. Tên thực thể giữ nguyên để field-app/htx-app không phải đổi |

---

## 6. Sự kiện, đồng bộ, hợp đồng

- **Sự kiện** (D-8). `//ag:event` của 15 sự kiện chuyển sang `internal/platform/cooperative/port`:
  - 2 `pf.party.household.*`;
  - 5 `pf.geo.{plot,plot_boundary,cooperative_boundary}.*`;
  - 8 `pf.cropcycle.{season,farm_log,harvest,coop_season_area,sop}.*`.

  `contracts/events/*.json` giữ nguyên. `modgen` sinh lại `provides.events` của bốn module. `contractcheck` không ràng buộc tiền tố với chủ (đã kiểm `tools/contractcheck`).
- **Hộp thư outbox.** Sự kiện được ghi vào outbox trong giao dịch của `r_coop`. Phải kiểm quyền `INSERT shared.outbox` của `r_coop`: W2.3 cấp theo `r_*`. Thêm `r_coop` vào danh sách trong `dbadmin`.
- **Đồng bộ ngoại tuyến.** Con trỏ `sync_seq` đi theo chuỗi số (giữ giá trị, D-2), nên client không phải đồng bộ lại toàn bộ.
- **OpenAPI.** Không đổi đường dẫn hay lược đồ. Chỉ đổi `x-owner`/tag nếu tệp có, rồi `npm run gen:api` để chắc chắn không lệch.

---

## 7. `module.yaml` và vai

### 7.1 Phác thảo

```yaml
# internal/platform/cooperative/module.yaml — trường SINH do modgen điền ở W3.2
name: pf/cooperative
aka: [dom/cooperative]
kind: domain
zones: [pf]
version: 1.0.0
depends:                       # sinh từ import (W3.2 → W3.3)
  pf/party: ">=1.0 <3"         # Orgs, OrgCodes, ScopeResolver, PartnerAccess (đến W5.x), OrgImpactSource
  pf/geo: ">=1.0 <3"           # GpsLog, PlotGeometry/CoopMarkers (hiện thực)
  pf/masterdata: ">=1.0 <2"    # ĐVHC, giống lúa
  pf/sync: ">=1.0 <2"
  pf/metrics: ">=1.0 <2"
  pf/metering: ">=1.0 <2"      # đếm đọc /v1/partner/*
  pf/apiclient: ">=1.0 <2"     # sản phẩm dữ liệu (nếu thửa/vụ có)
  vault/identity: ">=1.0 <2"   # FarmerIdentity (KN vẽ thửa)
provides:
  ports: [Cooperatives, Households, Plots, Seasons, Diary, Resolve]   # + MachineCounts, FarmerHouseholds (consumer-defined)
  events: [ …15 sự kiện mục 6… ]
permissions: [coop_manager, farmer, plot_editor, plot_viewer, sop_admin]   # sinh từ Role* trong port/
shares: [household, plot, season, diary_entry]   # tay; nghĩa ở 7.3, có hiệu lực từ W4.2
owns_schema: [pf_coop]         # sinh: chỉ module này nhắc pf_coop + 000094 CREATE SCHEMA
enable: per_tenant             # mặc định bật (W1.4); KN/HTX/CGH cần, MKG/SIM không
```

- `permissions` chỉ gồm vai **module khai và kiểm**. Vai KN (`kn_officer`, `kn_province_admin`, `kn_national_admin`) và `registry_admin` vẫn do `pf/party` khai, cooperative kiểm qua hằng của `partyport`. `coop_manager` hôm nay được party, geo, cropcycle cùng khai. Sau W3.3 chỉ cooperative khai, trừ khi party vẫn cần nó cho cây Nút (kiểm ở W3.3 bằng `modgen`).
- `enable: per_tenant`:
  - `modreg.RoleBlocked` không chặn `coop_manager` khi module tắt nếu còn module lõi khai cùng vai;
  - W3.3 kiểm lại `accounts.role_module_not_enabled` khi `coop_manager` chỉ còn cooperative khai;
  - ứng dụng `app/{KN,HTX,CGH}/app.yaml` thêm `pf/cooperative` (W1.3 kiểm ghép).

### 7.2 Vai `r_coop`

- `modreg.DBRole("pf/cooperative")` hôm nay trả `r_cooperative`. W3.2 thêm bảng ghi đè một mục `{"pf/cooperative": "r_coop"}` trong `internal/shared/modreg/assembly.go`. Kiểm thử bảng đảm bảo không trùng vai.
- Phương án thay thế: đặt tên module là `pf/coop`. Bị loại vì Phụ lục D dùng `pf/cooperative`.
- `dbadmin.groupOnlyModules["pf/cooperative"] = {migrations.PF}`. Module không có vai cũ `cooperative_app`, nên `migrate bootstrap` tạo `r_coop` NOLOGIN và cấp membership cho `app_platform`, `app_worker`, `app_public`, `app_sync` theo `modreg/assembly_gen.go`.
- **Phải chạy bootstrap trước pf 000094**: `ag_migrator` không có CREATEROLE. Migration lỗi rõ ràng nếu thiếu vai, theo mẫu pf 000090.
- `r_coop` được:
  - `USAGE` trên `pf_coop`, `k_ref` (R-DB-05);
  - `SELECT` các quan hệ `k_ref`, `EXECUTE k_ref.ctx_*`;
  - quyền bảng như mục 8.4;
  - `USAGE` chuỗi số;
  - quyền `shared.*` như các `r_*` khác: outbox, inbox, idempotency, `fn_chain` qua trigger `SECURITY DEFINER`.
- **Không** có quyền nào trên `pf_party`, `pf_geo`, `pf_cropcycle`.

### 7.3 `shares` — tên logic → bảng

| `shares` | Bảng mang nhánh `OR EXISTS share_scope` (W4.2) | `module_table` trong `k_ref.share_scope` | Cột Nút |
|---|---|---|---|
| `household` | `pf_coop.household`, `household_membership` | `pf_coop.household` | Qua xã viên đang mở (mục 9) |
| `plot` | `pf_coop.plot`. `plot_boundary` chỉ khi ADR-0030 cho đọc lịch sử hình học | `pf_coop.plot` | `cooperative_ref` |
| `season` | `pf_coop.season_current`, `coop_season_area`, `yield_summary` | `pf_coop.season` | `cooperative_ref` |
| `diary_entry` | `pf_coop.farm_log_current` | `pf_coop.diary_entry` | `cooperative_ref` |

- Sổ (`*_event`, `farm_log`, …) **không** có nhánh chia sẻ: đọc chéo đơn vị chỉ đọc bảng chiếu.
- `cooperative` (hồ sơ HTX) không nằm trong `shares` của v4.1. Danh bạ HTX cho CGH/MKG vẫn đi `/v1/partner/cooperatives` đến W5.x.
- `modreg` hiện chỉ kiểm `shares` là danh sách chuỗi. W4.1 quyết định có thêm ánh xạ tên logic → bảng vào `module.yaml` hay giữ trong mã của cooperative (`port.ShareTables`).

---

## 8. W3.3 — kế hoạch migration pf 000094–000096

### 8.1 Tệp

Tên tệp gắn module theo quy tắc `agmodgraph` (`<số>_<cụm>_<module>_…`). Ba tệp đều thuộc `pf/cooperative`.

| Tệp | Nội dung |
|---|---|
| `000094_pf_cooperative_schema.up.sql` | Kiểm `r_coop` tồn tại (không thì `RAISE`, như pf 000090). `CREATE SCHEMA pf_coop`, `REVOKE ALL … FROM PUBLIC`, `GRANT USAGE ON SCHEMA pf_coop TO r_coop`. Quyền `k_ref` cho `r_coop` (R-DB-05, như pf 000092 mục cấp cho `r_*`). `COMMENT ON SCHEMA` |
| `000095_pf_cooperative_move.up.sql` | Chuyển đối tượng (8.2). `REVOKE` của `r_party`/`r_geo`/`r_cropcycle` (+ `party_app`/`geo_app`/`cropcycle_app` nếu W2.6 chưa bỏ). `GRANT` cho `r_coop` (8.4) |
| `000096_pf_cooperative_verify.up.sql` | Khối `DO` chỉ kiểm, không sửa. `RAISE` nếu một trong các điều kiện sai: còn bảng hoặc phân vùng trong danh sách 2.4 ở schema cũ; `trg_chain` của 9 sổ đổi tham số; `shared.v_missing_immutability`, `v_missing_rls` không rỗng; `r_party`/`r_geo`/`r_cropcycle` còn quyền trên `pf_coop` |

`migratecheck`:
- không có `UPDATE`/`DELETE`/`TRUNCATE` mức câu lệnh;
- `SET SCHEMA` là DDL, đi được;
- khối `DO` dùng `EXECUTE format(…)` cho vòng lặp phân vùng.

`agmodgraph` quy tắc `schema`: migration của module A không được sửa đối tượng trong schema của module B. `ALTER TABLE pf_party.x SET SCHEMA pf_coop` vi phạm chữ của quy tắc. W3.3 sửa analyzer, cho phép đúng một dạng: `ALTER {TABLE|SEQUENCE|FUNCTION} <schema khác>.<x> SET SCHEMA <schema của mình>` cùng `REVOKE … ON <schema khác>.<x>` trong cùng tệp. Không thêm ngoại lệ, vì tệp ngoại lệ chỉ được co lại. Analyzer không thấy SQL động trong `DO`, nên vòng lặp phân vùng dùng tên lấy từ `pg_inherits`. Kiểm ở 000096.

### 8.2 Thứ tự trong 000095 (một giao dịch)

```sql
-- 0. Khóa theo thứ tự cố định (tránh deadlock với job maintain/anchor đang chạy)
LOCK TABLE pf_party.cooperative, pf_party.household, pf_party.household_membership,
           pf_party.kn_profile_event, pf_geo.plot, pf_geo.plot_boundary, pf_geo.plot_event,
           pf_geo.boundary_conflict, pf_geo.cooperative_boundary, pf_geo.cooperative_area,
           pf_cropcycle.sop_template, pf_cropcycle.sop_version, pf_cropcycle.variety_sop_binding,
           pf_cropcycle.season_event, pf_cropcycle.farm_log_batch, pf_cropcycle.farm_log,
           pf_cropcycle.farm_log_review, pf_cropcycle.harvest_declaration, pf_cropcycle.season_current,
           pf_cropcycle.farm_log_current, pf_cropcycle.coop_season_area, pf_cropcycle.yield_summary
  IN ACCESS EXCLUSIVE MODE;

-- 1. Chuỗi số độc lập (DEFAULT nextval(regclass) bám OID nên tự theo)
ALTER SEQUENCE pf_geo.plot_code_seq       SET SCHEMA pf_coop;
ALTER SEQUENCE pf_geo.plot_sync_seq       SET SCHEMA pf_coop;
ALTER SEQUENCE pf_cropcycle.sop_sync_seq      SET SCHEMA pf_coop;
ALTER SEQUENCE pf_cropcycle.season_sync_seq   SET SCHEMA pf_coop;
ALTER SEQUENCE pf_cropcycle.farm_log_sync_seq SET SCHEMA pf_coop;

-- 2. Hàm trigger của schema module (trigger bám OID hàm, không cần tạo lại)
ALTER FUNCTION pf_geo.fn_boundary_conflict_guard()   SET SCHEMA pf_coop;
ALTER FUNCTION pf_cropcycle.fn_sop_template_guard()  SET SCHEMA pf_coop;
ALTER FUNCTION pf_cropcycle.fn_sop_version_guard()   SET SCHEMA pf_coop;
ALTER FUNCTION pf_cropcycle.fn_variety_binding_guard() SET SCHEMA pf_coop;

-- 3. Bảng cha theo thứ tự phụ thuộc FK (cha trước con không bắt buộc với SET SCHEMA,
--    giữ thứ tự để dễ đọc): party → geo → cropcycle
ALTER TABLE pf_party.cooperative           SET SCHEMA pf_coop;
ALTER TABLE pf_party.household             SET SCHEMA pf_coop;
ALTER TABLE pf_party.household_membership  SET SCHEMA pf_coop;
ALTER TABLE pf_party.kn_profile_event      SET SCHEMA pf_coop;
-- … 18 bảng còn lại của mục 2.2, 2.3

-- 4. Phân vùng: SET SCHEMA của bảng cha KHÔNG chuyển phân vùng (mỗi phân vùng là một quan hệ riêng)
DO $$
DECLARE r record;
BEGIN
  FOR r IN
    SELECT format('%I.%I', n.nspname, c.relname) AS part
      FROM pg_inherits i
      JOIN pg_class c ON c.oid = i.inhrelid
      JOIN pg_namespace n ON n.oid = c.relnamespace
      JOIN pg_class p ON p.oid = i.inhparent
      JOIN pg_namespace pn ON pn.oid = p.relnamespace
     WHERE pn.nspname = 'pf_coop' AND n.nspname <> 'pf_coop'
  LOOP
    EXECUTE format('ALTER TABLE %s SET SCHEMA pf_coop', r.part);
  END LOOP;
END $$;
```

**Không cần làm lại:**
- chính sách `p_tenant` đi theo bảng;
- chỉ mục, ràng buộc, trigger (`trg_chain`, `trg_immutable*`, `trg_no_delete`, guard) đi theo bảng. `trg_immutable_trunc` của từng phân vùng đi theo phân vùng;
- `FORCE RLS` giữ.

`shared.create_monthly_partitions` lấy schema của bảng cha, nên phân vùng mới do job `maintain` tạo sẽ nằm ở `pf_coop`.

**Đã thử trên PostgreSQL cục bộ (06/10/2026, giao dịch ROLLBACK, schema tạm):**
1. Sau `ALTER TABLE <cha> SET SCHEMA`, bảng cha sang schema mới. Phân vùng `_default` và `_YYYYMM` **ở lại schema cũ**. `INSERT` qua bảng cha vẫn định tuyến đúng, nên lỗi không lộ ngay.
2. Sau `ALTER SEQUENCE … SET SCHEMA`, biểu thức `DEFAULT` tự hiển thị `nextval('<schema mới>.s'::regclass)`. `INSERT` lấy số tiếp.

Khi diễn tập W3.3 còn phải đo thời gian khóa với số phân vùng thật.

### 8.3 Chuỗi băm: giữ tên sổ (D-3)

`shared.fn_chain` dùng `TG_ARGV[0]` làm khóa của ba nơi:
- `shared.chain_head (ledger, tenant_id, stream_key)`;
- `shared.ledger_idem (ledger, tenant_id, idem_key)`;
- `shared.chain_anchor`.

`opsjobs.VerifyAllChains` (`migrate verify-chains`) đọc `chain_head`, ánh xạ `ledger → bảng` qua `pg_trigger.tgargs` của `trg_chain` (`opsjobs.LedgerTables`). Nó báo **lỗi** cho mọi dòng `chain_head` không tìm được bảng.

| | (A) Giữ `TG_ARGV` cũ — **chọn** | (B) Đổi sang `'pf_coop.<bảng>'` |
|---|---|---|
| Thao tác | Không gì | Trong cùng giao dịch: `DROP TRIGGER trg_chain` + `CREATE TRIGGER … fn_chain('pf_coop.x')` trên bảng cha. `INSERT INTO shared.chain_head SELECT 'pf_coop.x', tenant_id, stream_key, last_seq, last_hash FROM shared.chain_head WHERE ledger = 'pf_geo.x'`. `INSERT … ledger_idem` tương tự (cửa sổ 30 ngày, W2.1). **Không UPDATE, không DELETE** dòng cũ |
| `verify-chains` | Xanh, không đổi mã | Dòng cũ còn lại, không còn trigger mang tên cũ → báo "không tìm thấy bảng sổ" → P1 giả. Cần bảng bí danh `shared.ledger_alias(old, new)` (INSERT) và sửa `VerifyAllChains`, `anchor` |
| Chống trùng | Liên tục | Đúng chỉ khi chép đủ `ledger_idem` trong cùng giao dịch |
| Mỏ neo ngày (`chain_anchor`) | Liên tục | Đứt tên: báo cáo neo phải nối hai tên |
| Rủi ro | Tên sổ "nói dối" vị trí | Cao, nhiều mã `shared/opsjobs` phải sửa |

Chọn (A). Tên sổ được coi là **định danh logic bất biến**, như tên sự kiện (D-8). Ghi chú trong `COMMENT ON TABLE pf_coop.<sổ>`, kèm một dòng trong `docs/runbooks/verify-chains.md`. Kế hoạch W3.3 ghi "thêm tên mới trong `chain_head` bằng INSERT". Đó là cách làm của (B), và chỉ cần nếu ai đó bác (A). Khi đó W3.3 phải làm cả bảng bí danh và sửa `opsjobs` như trên.

### 8.4 Quyền sau 000095

| Đối tượng | `r_coop` |
|---|---|
| 9 sổ | `SELECT, INSERT` (`REVOKE UPDATE, DELETE, TRUNCATE`) |
| `plot`, `boundary_conflict`, `cooperative_area`, `cooperative`, `household`, `household_membership`, `sop_*`, `variety_sop_binding` | `SELECT, INSERT, UPDATE` (không DELETE) |
| `season_current`, `farm_log_current`, `coop_season_area`, `yield_summary` | `SELECT, INSERT, UPDATE, DELETE` (dựng lại từ sổ, R-22, như pf 000021) |
| 5 chuỗi số | `USAGE` |

`REVOKE ALL` trên 22 bảng và 5 chuỗi số khỏi:
- `r_party`, `r_geo`, `r_cropcycle`;
- `party_app`, `geo_app`, `cropcycle_app`, nếu W2.6 chưa bỏ.

Ma trận quyền mong đợi được kiểm bằng `migrations/module_roles_integration_test.go` (mở rộng ở W3.3).

### 8.5 Triển khai, đường lui, diễn tập

- **Không tương thích ngược ở mức SQL.** Mã cũ viết `pf_geo.plot`, nên sau 000095 nhị phân cũ hỏng. Chạy trong **cửa sổ bảo trì** (H-17): dừng `api-platform`, `api-public`, `worker`, `sync` → `migrate bootstrap` → `migrate up` (000094–000096) → `migrate verify-chains` → khởi động nhị phân mới.
- Không dùng view tương thích ở schema cũ, vì hai lý do:
  - view trên sổ phá mẫu `INSERT` qua trigger;
  - view vi phạm "mỗi schema một chủ".
- **Đường lui.** Repo không có `.down.sql` (R-25). Đường lui là migration tiến `000096b` (đánh số khi cần) làm ngược `SET SCHEMA` cùng `GRANT`/`REVOKE`. Viết sẵn trong thư mục diễn tập, không đưa vào `migrations/` trừ khi phải dùng. Vì không đổi dữ liệu, không đổi `chain_head` (D-3) và không đổi OID, lui là thao tác catalog.
- **Diễn tập trên bản sao UAT:**
  1. `pg_dump` UAT → cụm tạm;
  2. ghi đếm dòng và `sum(hashtext(row_hash::text))` theo bảng, `chain_head` theo sổ;
  3. chạy 000094–000096 và đo thời gian khóa;
  4. `migrate verify-chains` phải xanh, số chuỗi bằng trước;
  5. so lại đếm/tổng kiểm từ bước 2;
  6. chạy bộ e2e G1 trên nhị phân mới;
  7. chạy đường lui và lặp lại bước 4–5.

  Kết quả ghi vào `docs/runbooks/` (W3.3).

### 8.6 Mã Go trong W3.3

- Chuyển `party/store/{household,kn_collect}.go` và phần HTX của `org.go`, `readmodel.go`, `sharing.go` (`CooperativeEntries`), `wiring.go`.
- Chuyển `geo/store/{plot,coop,kn,conflict_detail}.go` và `cropcycle/store/*` (trừ collection) vào `internal/platform/cooperative/store`, với `pf_coop.*`.
- **Tạo HTX** (`party/store/org.go`: `org` + `cooperative` cùng giao dịch). Giao dịch không được chạy qua hai vai. Luồng mới:
  1. `pf/cooperative` nhận yêu cầu tạo HTX;
  2. gọi `partyport.Orgs.Create(kind=cooperative, idemKey)` (thêm vào port; giao dịch party, sinh `org.created`);
  3. ghi `pf_coop.cooperative` trong giao dịch của mình, cùng `idemKey`.

  Nếu bước 3 lỗi, gọi lại bằng cùng khóa là lũy đẳng. Job đối soát báo các `org` kind cooperative mà không có hồ sơ. FK `cooperative → org` bảo đảm không có hồ sơ mồ côi.
- **Đọc tên HTX.** `pf_coop` đọc mã, loại, ĐVHC, trạng thái từ `k_ref.org` (đã có quyền). Tên hiển thị **không** có trong `k_ref.org`. Có hai đường:
  - (i) gọi `partyport.Orgs` theo lô;
  - (ii) thêm cột `name` vào `k_ref.org`. Đây là migration của `pf/kref`, số do kế hoạch cấp; worker `pf.kref` chép tên.

  Đề xuất (ii), vì tên tổ chức không phải dữ liệu cá nhân và danh bạ cần sắp, lọc theo tên (`org_cooperative_name`). Hỏi lại khi giao W3.3 vì cần số migration ngoài 000094–000096.
- Sửa test fixture có SQL thẳng (mục 2.4) và tên bảng trong `*/app/metrics.go`.

---

## 9. W3.4 — RLS `p_scope` theo Nút (pf 000097)

Thay `p_tenant` bằng **một** chính sách `p_scope` mỗi bảng (R-DB-08, ADR-0005), theo mẫu §17.2. Biến ngữ cảnh đã có từ W2.4. Nút = HTX (`cooperative_ref`) vì HTX là Nút `kind=cooperative` trong `k_ref.org`.

```sql
-- Mẫu cho bảng có cooperative_ref (plot, season_current, farm_log_current, yield_summary,
-- coop_season_area, harvest_declaration, cooperative_boundary, cooperative_area; cooperative dùng org_ref)
CREATE POLICY p_scope ON pf_coop.plot
  USING (
        tenant_id = k_ref.ctx_uuid('app.tenant_id')
    AND (   k_ref.ctx_uuid('app.node_id') IS NULL                      -- consumer/job/Admin: chỉ lọc tenant (giữ kiểm ở app)
         OR EXISTS (SELECT 1 FROM k_ref.node_closure c
                     WHERE c.tenant_id  = plot.tenant_id
                       AND c.ancestor   = k_ref.ctx_uuid('app.node_id')
                       AND c.descendant = plot.cooperative_ref)
         OR plot.commune_code = ANY (k_ref.ctx_text('app.xa_ids')::text[]))  -- thửa nháp (cooperative_ref NULL), cán bộ xã
  )
  WITH CHECK (tenant_id = k_ref.ctx_uuid('app.tenant_id'));
```

| Bảng | Cột Nút | Ghi |
|---|---|---|
| `cooperative` | `org_ref` | |
| `household` | **không có cột Nút**. Phạm vi qua `EXISTS (household_membership m JOIN k_ref.node_closure c …)` hoặc `commune_code = ANY(xa_ids)` | Bán nối, không gọi hàm. EXPLAIN phải cho `hashed SubPlan` hoặc `Hash Semi Join` với `loops=1`. Nếu không đạt, không dùng được cột sinh, vì cột sinh không tham chiếu bảng khác. Phương án còn lại là cột `node_ref` thường, do mã ghi khi đổi xã viên. Cột mới thêm bằng `ADD COLUMN`; dòng cũ được điền qua sự kiện bù của ứng dụng, không `UPDATE` trong migration. Quyết định ở W3.4 |
| `household_membership` | `org_ref` | |
| `plot`, `plot_boundary`, `plot_event`, `boundary_conflict` | `cooperative_ref`. Thửa nháp có `NULL` → dùng nhánh `xa_ids`. `boundary_conflict` dùng `owner_ref` | `plot_boundary`, `plot_event` có sẵn `cooperative_ref` (NULL được) và `commune_code` (ảnh chụp lúc ghi, pf 000004) |
| `season_event`, `farm_log*`, `harvest_declaration`, `season_current`, `yield_summary`, `coop_season_area` | `cooperative_ref` | |
| `sop_template`, `sop_version`, `variety_sop_binding` | — | Danh mục cấp tenant: giữ `p_tenant` viết lại bằng `ctx_uuid` |
| `kn_profile_event` | `subject_ref` khi `subject_kind = 'cooperative'`; hộ → nhánh `commune_code` | |

- **Vì sao có nhánh `node_id IS NULL`.** Consumer, job, worker `pf.kref` và Admin không gắn Nút (W2.4: "Có KN mà không gắn Nút (Admin) → rỗng"). Nếu không có nhánh này, chính sách §17.2 trả rỗng cho họ. Nhánh này không làm yếu hơn hiện trạng: hôm nay chỉ có `p_tenant`, phạm vi kiểm ở ứng dụng, và W3.4 **giữ** kiểm ở ứng dụng.
- **Rủi ro.** Một yêu cầu người dùng KN mất `app.node_id` sẽ thấy cả tenant. Có hai biện pháp:
  1. kiểm thử W3.4: mọi Principal có KN hoặc HTX phải đặt `app.node_id`;
  2. `db.ForModule` từ chối giao dịch người dùng có vai HTX/KN xã mà `node_id` rỗng. Hỏi lại ở W3.4.
- `shared.v_missing_rls` chấp nhận chính sách tên `p_scope` (hôm nay chỉ `p_tenant`).
- **Cổng CI `EXPLAIN`** (DB-14): với mỗi bảng `pf_coop`, `EXPLAIN (FORMAT JSON) SELECT * FROM t` dưới `r_coop` có ngữ cảnh mẫu. Cổng hỏng nếu có SubPlan không `hashed` hoặc có lời gọi hàm không inline.
- Phân vùng: `shared.secure_partition` gọi `apply_tenant_rls` nên phân vùng giữ `p_tenant`. Truy cập thẳng phân vùng chỉ có `ag_migrator`; `r_coop` không có quyền phân vùng. W3.4 kiểm rằng không vai ứng dụng nào có quyền trên phân vùng.

---

## 10. W3.5 — hợp nhất dữ liệu UAT

Bản này mở đường cho W3.5 như sau:
- mọi thực thể có **một** bảng đích trong `pf_coop`;
- khóa so khớp:
  - HTX: `cooperative.tax_code` (duy nhất trong tenant), rồi `org.code`;
  - hộ: `household_ref` của vault (sau khi nạp danh tính) + `commune_code`;
  - thửa: hình học (`ST_Equals`/tỉ lệ chồng ≥ ngưỡng của `boundary_conflict`) + `cooperative_ref`;
- nạp **qua port và sổ** của `pf/cooperative` (sự kiện `registered`, `plot.created` …), không `INSERT` thẳng bảng chiếu. Như vậy chuỗi băm và outbox đúng (R-INT-10);
- tổng kiểm: đếm theo (xã, HTX) trước và sau, so với ba nguồn UAT.

W3.5 vẫn **chặn** chờ dữ liệu UAT (C-9).

---

## 11. Giả định chủ dữ liệu (X-01, H-1)

**Giả định của bản này:** phương án **(a)**, theo ADR-0009 và ADR-0030 §7.
- `tenant_id` của mọi bảng `pf_coop` là tenant chương trình khuyến nông.
- HTX là một Nút trong cây của tenant đó.
- App HTX và KN đọc cùng tenant; phạm vi được giới hạn bằng `p_scope` theo Nút (mục 9).
- CGH và đơn vị khác đọc qua nhánh chia sẻ (W4.2).

**Nếu H-1 trả lời (b)** (mỗi HTX hoặc đơn vị vận hành App HTX là đơn vị chủ):

| Thành phần | Đổi |
|---|---|
| Schema, bảng, port, `module.yaml` của bản này | **Không đổi** |
| `tenant_id` của bản ghi `pf_coop` | Tenant của HTX. Dữ liệu có trước ngày chuyển phải **chuyển chủ bằng bản ghi bù**: ghi mới vào sổ của tenant HTX với `supersedes`/`correction_reason`, đóng chuỗi ở tenant cũ. Sổ chỉ ghi thêm (RB-1), không `UPDATE tenant_id` |
| `p_scope` | Nhánh Nút vẫn chạy (Nút trong tenant HTX). KN đọc qua nhánh `share_scope` với `purpose = khuyen-nong` |
| `share_grant` | Mỗi HTX × KN (và × CGH) một cấp phép `intra_platform` với `basis_ref` = thỏa thuận mẫu (H-15). Cần luồng cấp phép hàng loạt (W4.1) |
| FK `cooperative.org_ref → pf_party.org` | Vẫn đúng: `org` của HTX nằm trong tenant HTX |
| Tạo HTX (mục 8.6) | Thêm bước tạo tenant/đơn vị HTX (`pf/accounts`) trước `Orgs.Create` |
| W3.5 hợp nhất | Nạp vào nhiều tenant, khóa so khớp như cũ |
| Hiệu năng | `share_scope` lớn hơn (≈ số HTX × đơn vị xem). Phép thử T9/T10 (W4.2) phải chạy ở cỡ này |

Đổi từ (a) sang (b) **trước** 15/12/2026 (chưa có dữ liệu thật) chỉ cần nạp lại dữ liệu UAT vào tenant đúng. Đổi **sau** ngày đó cần quy trình chuyển chủ có bản ghi bù như trên, ước lượng thêm L. Vì vậy H-1 vẫn phải có câu trả lời trước W3.3.

---

## 12. Rủi ro

| # | Rủi ro | Mức | Giảm |
|---|---|---|---|
| R-1 | Phân vùng sổ ở lại schema cũ sau `SET SCHEMA` bảng cha → `agmodgraph`/`v_missing_*` lệch, job `maintain` tạo phân vùng mới ở `pf_coop`, cũ ở `pf_geo` | Cao | Vòng lặp `pg_inherits` (8.2) + kiểm 000096 |
| R-2 | Nhị phân cũ chạy sau 000095 (SQL `pf_geo.*`) | Cao | Cửa sổ bảo trì (H-17), thứ tự triển khai 8.5, `readyz` kiểm phiên bản migration |
| R-3 | Đổi tên sổ (nếu chọn 8.3-B) → `verify-chains` báo P1 giả, mất chống trùng | Cao | Chọn (A) |
| R-4 | Tạo HTX hai bước (8.6) để lại Nút không hồ sơ | Trung bình | Khóa lũy đẳng + job đối soát + màn KN hiển thị "HTX thiếu hồ sơ" |
| R-5 | `p_scope` nhánh `node_id IS NULL` mở rộng nếu Principal người dùng mất Nút | Trung bình | Kiểm thử + từ chối ở `db.ForModule` (mục 9) |
| R-6 | `household` không có cột Nút → SubPlan theo hàng (DB-14) | Trung bình | Cổng `EXPLAIN`; phương án cột thường ở W3.4 |
| R-7 | Song song với W2.6 (pf 000119 đổi quyền của mọi module) | Trung bình | W3.3 sau W2.6 (đường găng). 000095 `REVOKE` cả vai cũ, idempotent |
| R-8 | Chu trình mới `cooperative ↔ cropcycle` nếu SOP ở lại cropcycle | Trung bình | D-4 |
| R-9 | `coop_manager` chỉ còn module `per_tenant` khai → bị chặn gán vai khi tắt module | Thấp | 7.1, kiểm ở W3.3 |
| R-10 | Xung đột tài liệu với gói song song (`cau-hoi-ton-dong.md` §H, `roadmap.md`) | Thấp | Một dòng mỗi tệp |

---

## 13. Trình tự

1. **W3.1** (bản này).
2. **W3.2** (M, không đổi DB):
   - khung `internal/platform/cooperative` (`module.go`, `module.yaml`, `port/` mục 4, `app/` mặt tiền gọi port cũ);
   - `modreg.DBRole` ghi đè `r_coop`, `dbadmin.groupOnlyModules`, `modgen`;
   - đổi bên tiêu thụ theo mục 5;
   - đánh dấu port cũ `Deprecated`;
   - xóa ngoại lệ `agmodgraph` có chủ W3.2.
3. **W2.6** (song song, pf 000119).
4. **W3.3** (L): 000094–000096; chuyển store, handler, consumer, sync, metrics vào cooperative; xóa port cũ (party, geo, cropcycle `2.0.0`); sửa `agmodgraph` (8.1); diễn tập UAT; xóa ngoại lệ W3.3.
5. **W3.4** (M): pf 000097, cổng `EXPLAIN`.
6. **W4.2**: nhánh `share_scope` trên bảng `shares`. **W6.5**: `Resolve`. **W3.5**: khi có dữ liệu UAT.

Mốc: W3.3 + W3.4 xong **trước 15/12/2026**. H-1 phải được trả lời trước khi chạy W3.3 trên production.

---

## 14. Kế hoạch kiểm thử

| Gói | Kiểm |
|---|---|
| W3.2 | Đơn vị: mặt tiền trả đúng DTO cũ (bảng so sánh port cũ ↔ mới trên cùng dữ liệu). Tích hợp: bộ hiện có của `accounts`, `extension`, `reports`, `mechanization`, `notify`, `simulation`, assembly xanh, không đổi kỳ vọng. `modgen -check`, `archlint` (ngoại lệ W3.2 đã gỡ) |
| W3.3 | (1) `migrations` tích hợp: sau 000096, 22 bảng + phân vùng + 5 chuỗi số + 4 hàm ở `pf_coop`; không đối tượng nào còn ở schema cũ (2.4). (2) `module_roles_integration_test`: `r_coop` đủ quyền 8.4; `r_party`, `r_geo`, `r_cropcycle` **không** chạm được `pf_coop` (`permission denied`). (3) Chuỗi: tạo dữ liệu sổ trước 000095, ghi tiếp sau; `verify_chain` liền mạch (seq tiếp, `prev_hash` nối); `idem_key` trùng trước/sau vẫn bị chặn. (4) `migrate verify-chains`, `anchor` xanh. (5) `sync_seq` tiếp tục tăng. (6) e2e G1 (`tests/e2e`), `cmd/api-public/integration_test.go` với fixture `pf_coop.*`. (7) Luồng tạo HTX hai bước: lỗi bước 3 → gọi lại → một hồ sơ. (8) Diễn tập UAT (8.5) |
| W3.4 | T-scope: cán bộ xã A không thấy thửa xã B; Cấp Tỉnh thấy mọi xã trong tỉnh; HTX chỉ thấy Nút của mình; consumer (không Nút) thấy cả tenant; `WITH CHECK` chặn ghi tenant khác. Cổng `EXPLAIN` (DB-14) trên mọi bảng `pf_coop`. `v_missing_rls` rỗng. Kiểm thử song song kết nối tái sử dụng (DB-13) |
| W4.2 | T6a–T6e, T9–T10 trên bảng `shares` (7.3) |

---

## 15. Câu hỏi phát sinh

- **H-17** (mới, [cau-hoi-ton-dong.md](../cau-hoi-ton-dong.md#h-kiến-trúc-v41)): cửa sổ bảo trì cho W3.3 và bản sao dữ liệu UAT để diễn tập. Ước lượng ≤ 15 phút dừng ghi trên `api-platform`/`api-public`/`worker`/`sync`; cần trước 15/12/2026.
- **Nội bộ, quyết khi giao W3.3** (không cần MKG):
  - cột `name` trong `k_ref.org` (8.6, cần số migration của `pf/kref`);
  - giữ hay bỏ nhánh `node_id IS NULL` cho người dùng (mục 9).

---

## 16. Ghi chú hiện thực W3.2

**Ngày:** 08/10/2026, dựng lại trên `origin/main` aabe11c (sau rà soát đảo phụ thuộc R-MOD-05: port nguồn của bên tiêu thụ tầng dưới chuyển về port của bên tiêu thụ — `partyport.PlotStats`, `reports/port/cropcyclesrc`…; `agmodgraph` bỏ qua import chỉ dùng hợp đồng dữ liệu; ngoại lệ còn 22). Phần dưới ghi chỗ hiện thực **lệch** bản thiết kế và lý do. W3.3 đọc mục này trước mục 8.

### 16.1 `pf/cooperative` là đỉnh lá trong W3.2

Mục 4 và 7.1 cho `pf/cooperative` gọi port cũ trong module (`depends: pf/party, pf/geo, …`). W3.2 không làm vậy:
- `pf/cropcycle` (vụ và van collection) đổi sang `cooperativeport.Plots` (mục 5). Nếu mặt tiền import `cropcycle/port` để đọc vụ (`Stats`, `SupplyStats`, `cropcyclesrc.ReportData`) thì `cropcycle ↔ cooperative` thành chu trình — cạnh `cycle` mới, tệp ngoại lệ chỉ được co lại;
- vì vậy `cooperativeport` có DTO riêng (cùng trường DTO cũ), `module.yaml` có `depends: {}`, và `store.Legacy` (store tạm) giữ nguồn kiểu `cooperativeport`. Gốc lắp ráp dựng nguồn từ port cũ bằng bộ chuyển `cmd/internal/coopwire` (đổi DTO, đổi `ErrPlotNotFound`/`ErrHouseholdNotFound` sang lỗi của `cooperativeport`);
- `api-platform`: `coop` dựng sau `party`, `geo`; `coop.UseSeasons(…)` sau `cropcycle`. `worker` (notify) và `api-public` dựng `coop` với đúng nguồn cần.

**Đồ thị sau rà soát.** Thành phần liên thông mạnh pf chỉ còn `{accounts, party, sync}` (4 cạnh `cycle`, chủ W3.3/W7.2). Không module nào trong đó phụ thuộc `pf/cooperative` hay bên tiêu thụ của nó. Hệ quả cho **W3.3**: khi vụ chuyển vào `pf/cooperative` (store `pf_coop`), module được import `partyport`, `geoport`, `masterdata`, `sync`, `metrics`… mà không sinh chu trình (miễn `party`/`sync`/`accounts` không quay lại import `cooperativeport` — chúng dùng interface tự khai, gốc lắp ráp nối). Lúc đó gộp `cmd/internal/coopwire` vào `cooperative/store` rồi xóa.

### 16.2 Bên tiêu thụ đã đổi và chưa đổi

| Bên tiêu thụ | W3.2 |
|---|---|
| `pf/mechanization` | Đổi: `MachineCounts()` trả `cooperativeport.MachineCounts` (party nhận qua `partyport.CoopMachineCounts`, cùng chữ ký). Sự kiện `pf.cropcycle.coop_season_area.changed` **giữ** `ccport` — từ aabe11c import chỉ dùng kiểu/hằng sự kiện là `consumes.events`, không tạo `depends`, nên không cần khai lại tên ở `cooperativeport` (đã bỏ; lệch mục 5) |
| `pf/extension` | Đổi: `Deps.Coops cooperativeport.Cooperatives`, `Deps.Plots cooperativeport.Plots` (thay `partyport.CooperativeStats`, `partyport.PlotStats`). `GpsLog` giữ `geoport` |
| `pf/cropcycle` | Đổi: `Deps.Plots`, `UseCollectionVolume` nhận `cooperativeport.Plots` (vụ và van collection). `GpsLog`, `FarmerHouseholds` giữ |
| Gốc lắp ráp `api-platform` | `SupplySource{Seasons, Coops}` (simulation), `AreaFarmers{Households}` (notify), `SeasonActivity{Seasons}` (party) đọc `coop.*` |
| `cmd/worker` (notify), `cmd/api-public` | `coop.Households()`, `coop.Plots()` |
| `pf/accounts` (kernel), `pf/reports`, `pf/notify`, `pf/metrics`, `pf/party` (tầng dưới) | **Không import `cooperativeport`** (cạnh `tier` mới, R-MOD-05). Mẫu đúng — đã áp ở aabe11c — là interface do bên tiêu thụ khai (`partyport.PlotStats`, `reports/port/cropcyclesrc.ReportData`…); gốc lắp ráp nối nguồn. W3.3 nối các interface đó vào `pf/cooperative` thay vì geo/cropcycle |
| `pf/geo` (nội bộ) | Chưa đổi (W3.3: mã thửa, `CoopMarkers` chuyển vào cooperative) |
| `pf/mrv` | Không đổi (D-8) |
| `/v1/partner/{cooperatives,plots,season-harvest-dates}` | **Chưa chuyển handler** — cần store; chuyển cùng store ở W3.3. Đường dẫn không đổi (D-9) |
| `pf/sync` | Chưa đổi: thực thể đồng bộ vẫn do geo/cropcycle/party đăng ký đến W3.3 |

### 16.3 Cổng

- `Seasons.HarvestDates`, `Diary`: port cũ không có nguồn (ngày thu hoạch chỉ có ở bề mặt đối tác có cấp quyền; nhật ký chưa có cổng đọc) → `ErrUnavailable` đến W3.3.
- `Resolve`: phân giải `cooperative` (HTX đang hoạt động), `household`, `plot`, `diary_entry`; `season`, `harvest` → `ErrSubjectTypeUnsupported` đến W3.3. Thêm `ErrSubjectTypeUnsupported`, `ErrUnavailable`, `ErrDiaryEntryNotFound`, `SubjectModule = "pf/cooperative"`.
- `Diary.BySeason`: `limit ≤ 0 → 100`, `> 500 → 500`.
- Danh sách lọc: `nil` = tất cả, rỗng = không gì (mặt tiền trả rỗng, không gọi nguồn) — như port cũ.
- Sự kiện: `cooperativeport` chưa khai tên sự kiện; `//ag:event` ở lại port cũ đến W3.3 (D-8).
- Port cũ do module chủ khai (`partyport.Households`, `CooperativeStats`, `CooperativeSites`, `geoport.Plots`, `PlotFacts`, `PlotHouseholds`, `CoopCentroids`, `ccport.Stats`, `SupplyStats`) có ghi chú "Thay thế (W3.2)", **không** ghi `// Deprecated:` (staticcheck sẽ chặn chỗ dùng hợp lệ còn lại). `partyport.PlotStats`, `cropcyclesrc.ReportData` là interface của bên tiêu thụ, giữ lâu dài.
- `porttest.RunContract(t, withTenant, mk)`: kiểm thử truyền `tenant.WithID` (gói `porttest` không phải `_test.go`, archlint `agtenantgate`).

### 16.4 Vai, `module.yaml`, ứng dụng

- `modreg.DBRole` ghi đè `r_coop` và `dbadmin.groupOnlyModules` (mục 7.2) **chưa làm**: module chưa có pool. `dbadmin.ModuleGroups` bỏ qua module không có vai cũ `<module>_app` nên không sinh `r_cooperative`. TODO W3.3 (cùng `migrate bootstrap` trước pf 000094).
- `module.yaml`: `depends: {}`, `owns_schema: []`, `provides.events: []`, `consumes.events: []`, `permissions: []` — **chưa khai hằng `Role*`** trong `cooperativeport`: module chưa kiểm vai, và `modreg.RoleBlocked` coi một module per_tenant đang bật cùng khai vai là "không chặn" (tắt `pf/geo` sẽ không còn chặn `plot_editor`, `TestPerTenantAndRoleBlocked`). W3.3 thêm khi chuyển handler.
- `app/{KN,HTX,CGH,PF,SIM,NH}/app.yaml` thêm `pf/cooperative` (các ứng dụng có đơn vị ghép module: `api-platform`, `worker`, `api-public`).

### 16.5 Ngoại lệ `agmodgraph`

Rà soát aabe11c đã gỡ mọi mục chủ W3.2 (kể cả `geo → waterway`: `geoport.Layer` do geo khai). W3.2 không thêm, không bớt mục nào; tệp còn 22 mục (4 `cycle`, 1 `tier` trong `{accounts, party, sync}`, 17 `schema`). Mục 3 ("W3.2 gỡ 15 mục") đã do rà soát làm.
