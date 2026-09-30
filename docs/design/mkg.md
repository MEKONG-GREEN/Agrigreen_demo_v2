# Thiết kế chi tiết vùng nội bộ Mekong Green (`internal/mkg/*`)

**Cổng:** G2 (31/03/2027) — "Thiết kế chi tiết `mkg/*` chứng minh tuân thủ bảy ràng buộc" (tài liệu kiến trúc v3.0, Phụ lục A)
**Ngày:** 29/09/2026 · **Trạng thái:** đề xuất; `mkg/hub` và van `hub_facility`/`hub_capacity` đã hiện thực ở đợt 1 G2
**Đọc cùng:** tài liệu kiến trúc v3.0 (mục 3, 5, 6, 8, 9, 12, 13, 16), [ADR-0003](../adr/0003-event-sourcing-muc-b.md) (mức B), [ADR-0005](../adr/0005-rls-mot-policy-moi-bang.md), [ADR-0009](../adr/0009-mo-hinh-tenant.md), [ADR-0012](../adr/0012-duy-nhat-tren-bang-so-phan-vung.md), [g1-conventions](../dev/g1-conventions.md), [g2-conventions](../dev/g2-conventions.md), [contracts/publication/README.md](../../contracts/publication/README.md).
**Đặc tả nghiệp vụ tham chiếu:** prototype `mekong-green/src/erp/warehouse/service.ts` (lô là nguồn sự thật duy nhất, `stock_movements` bất biến, xuất theo rủi ro xuống cấp — không FEFO), cùng kiểm thử `tests/arch-review-2026-09-24.test.ts` (R3/A03). Mã story kho ghi tiền tố `WH` (BRD AgriGreen v1.1 — Warehouse & Inventory, FN-01..39).

---

## 1. Vùng mkg trong hệ thống

| Thuộc tính | Giá trị |
|---|---|
| Chủ dữ liệu | Mekong Green (MKG) — một pháp nhân, **một tenant** (§2) |
| Đơn vị triển khai | `cmd/api-internal` (API nội bộ MKG: bề mặt ứng dụng + bề mặt quản trị MFA), `cmd/worker-internal` (khung từ G3-0: consumer vhost `mkg.internal`, ADR-0018), `cmd/ingest` + `cmd/edge-agent` (G3, measurement; hợp đồng dây đóng băng ở G3-0, ADR-0019) |
| Cụm DB | `ag_mkg` — **database và tài khoản riêng** với `ag_pf` (R-05, mục 5). ĐTK 1 có thể chung instance |
| Schema | `mkg_<module>`; chuỗi thời gian cân thô ở cụm `ts` (`ag_ts`, `ts_weigh.reading_batch` + `ts_weigh.raw_reading` — đã có từ G3-0, ADR-0017) |
| Mã nguồn | `internal/mkg/<module>/{port,domain,app,store,handler}` + `module.go` (mẫu `internal/platform/party`) |
| Gốc lắp ráp | **Chỉ** `cmd/api-internal/assembly` và `cmd/worker-internal/assembly` (cùng `cmd/ingest`, `cmd/edge-agent`). `tools/archlint` hỏng build nếu `cmd/api-platform` import `internal/mkg/*` hoặc `cmd/api-internal`, `cmd/ingest`, `cmd/edge-agent`, `cmd/worker-internal` import `internal/platform/*` (R-03 ở gốc lắp ráp); `cmd/edge-agent` chỉ thư viện chuẩn + `shared/{clock,ids,mass}` + `edgewire` |

### 1.1 Ba cửa giữa hai vùng (mục 5) — cách hiện thực

| Cửa | Chiều | Hiện thực | Trạng thái |
|---|---|---|---|
| **Van công bố** | MKG → nền tảng | Hợp đồng `contracts/publication/<tên>.json` (có người đề xuất ≠ người phê duyệt) → struct `//ag:publication` trong `mkg/<module>/port` → `Contract.Validate` lúc chạy → sổ `mkg_<module>.valve_export` + `shared.outbox` cụm mkg **cùng giao dịch** → `outbox-publisher` (đã quét mọi cụm, vai `ag_outbox_publisher`) → RabbitMQ → consumer pf (đợt 2: `pf/geo` `HubSink`) | `hub_facility`, `hub_capacity` v1 — G2 đợt 1 |
| **Tiêu thụ không đặc quyền** | nền tảng → MKG | Worker mkg gọi **Application API công khai** của nền tảng bằng tài khoản dịch vụ thuộc tenant MKG (OIDC client credentials). Không kết nối DB mkg → pf | G3 (fieldops cần thửa/HTX) |
| **Van lọc dữ liệu công** | nền tảng → MKG (dữ liệu công) | Chỉ tổng hợp qua ngưỡng gom nhóm k ≥ `[KÝ]` (C-3) | Chờ C-3 |

**Không có** import mã xuyên vùng (R-03, `agboundary`), **không có** kết nối DB xuyên cụm (R-05: vai `hub_app` chỉ CONNECT `ag_mkg`), **không có** join xuyên schema (R-06).

---

## 2. Tenancy — tài khoản MKG

- `tenant_id` = MKG (ADR-0009: tổ chức sở hữu dữ liệu). Mọi bảng `mkg_*` có `tenant_id`, `ENABLE + FORCE RLS`, đúng một policy `p_tenant` qua `shared.apply_tenant_rls` (RB-2, R-31, R-33). Tenant lấy từ token (R-16); mọi truy cập qua `tenant.WithTenantTx` (R-30).
- Người dùng MKG là tài khoản trong realm Keycloak chung, claim `tenant_id` = tenant MKG; vai nghiệp vụ `hub_*`, `field_*`… chỉ cấp cho người thuộc tenant MKG. Token của tenant nền tảng (chương trình khuyến nông) vào API nội bộ MKG thấy **rỗng** (RLS) — kiểm thử `TestTenantIsolation`.
- HTX, nông hộ, khách hàng là **đối tác được tham chiếu** (`htx_ref`, `plot_ref`, `customer_ref`, `household_ref`), không phải tenant của vùng mkg. Dữ liệu cá nhân của họ chỉ ở vault (R-04, RB-3).
- ĐTK 2 nhiều tenant: vùng mkg vẫn một tenant (MKG là một bên). Nếu MKG có công ty con cần cách ly sổ → mỗi công ty con một tenant, **không đổi mô hình** (R-01).
- Phạm vi trong tenant (hub nào người vận hành được thao tác) là phân quyền tầng ứng dụng, **chưa có ở G2** (mọi `hub_operator` thấy mọi hub của MKG) — Q-MKG-03.

---

## 3. Van công bố — dữ liệu nào được qua và vì sao

### 3.1 Nguyên tắc

1. **Mặc định từ chối** (mục 5). Chỉ trường có trong hợp đồng đã phê duyệt đi qua; kiểm ở 2 lớp: build (`tools/contractcheck`) và chạy (`contracts/publication.Contract.Validate`, cả object lồng).
2. **Mọi sự kiện của vùng mkg lên vhost nền tảng là công bố.** Exchange `ag.events` của vhost nền tảng dùng chung; consumer pf bind được mọi routing key. Vì vậy `contractcheck` bắt: struct `//ag:event` trong `internal/mkg/**` phải kèm `//ag:publication`, và mọi `outbox.Event.Payload` (kind `platform`) trong `internal/mkg/**` phải chứng minh được là hợp đồng van. Sự kiện **nội bộ** giữa các module mkg (G3, Q-MKG-01 đã chốt — ADR-0018) mang `//ag:internal-event`, schema ở `contracts/events-internal/`, chỉ phát với `outbox.KindInternal` lên vhost riêng `mkg.internal` mà tài khoản nền tảng không có quyền; `contractcheck` bắt payload nội bộ không bao giờ đi kind `platform`. Mặc định vẫn là `port/` đồng bộ trong cùng tiến trình.
3. **Sổ xuất là bằng chứng** (RB-1): `mkg_<module>.valve_export` lưu đúng byte đã kiểm, `payload_sha256`, `contract`, `contract_version`, `event_type`, `schema_version`, `subject_ref`, `actor_ref`; `id` = `event_id` trên outbox. Outbox **không** phải sổ (R-60).
4. **Không bao giờ qua van:** dữ liệu cá nhân (RB-3, R-14), giá mua/bán, giá thành, biên lợi nhuận, `yield_kg_per_roll` (chỉ số biên lợi nhuận, mục 13.3), danh tính và điều khoản khách hàng, hồ sơ tranh chấp, số đo cân thô, tồn **theo lô** và nguồn gốc lô tới thửa/HTX (kết hợp với khối lượng lộ thông tin thương mại), bút toán.

### 3.2 Hợp đồng hiện có và ứng viên

| Hợp đồng | Chủ | Trường | Vì sao được qua | Người nhận | Trạng thái |
|---|---|---|---|---|---|
| `hub_facility` v1 | mkg/hub | `hub_ref, code, name, kind, location{lat,lng}, boundary?, province_code, status, capacity_tons, unit, effective_from` | Nền tảng (GIS, mô phỏng chọn hub, điều phối gom) cần vị trí, loại, trạng thái, sức chứa danh định của cơ sở **đã phê duyệt**. Là dữ liệu hạ tầng, không cá nhân, không giá | pf/geo (mô hình đọc hub, đợt 2) | **Đã có** — phát khi duyệt phiên bản dữ liệu chủ |
| `hub_capacity` v1 | mkg/hub | `hub_ref, as_of, capacity_tons, used_tons, available_tons, unit` | Điều phối gom rơm cần sức chứa còn trống. Chỉ con số tổng của cơ sở — không lô, không nguồn gốc | pf/geo | **Đã có** — phát khi duyệt và khi chụp (`POST …/capacity-snapshots`) |
| `collection_schedule` v1 (**dự thảo G3-0**) | mkg/fieldops | `schedule_ref, htx_ref, commune_code, window_from, window_to, status, revision` | HTX/cán bộ KN cần biết lịch gom để không đốt rơm (mục tiêu chương trình) | pf/cropcycle, notify | Tệp hợp đồng + schema sự kiện + struct `fieldops/port` đã có; người phê duyệt là vai giữ chỗ — cần MKG phê duyệt |
| `htx_collection_statement` v1 (**dự thảo G3-0**) | mkg/fieldops | `statement_ref, htx_ref, plot_ref, season_ref?, collected_on, rolls, revision` | Đối soát chéo chiều mua (mục 13.3, `htx_confirmation_ref`): HTX xác nhận số cuộn trên app HTX. Không có giá, không khối lượng cân | pf/cropcycle (HTX xác nhận) → quay về MKG qua API công khai | Dự thảo đã có — cần chốt với khách (C-4: trả theo cuộn hay theo tấn) |
| `mrv_collection_evidence` v1 (**dự thảo G3-0**) | mkg/measurement | `evidence_ref, plot_ref, season_ref?, collected_on, collected_kg_bucket, bucket_rule_version, revision` | MRV cần lượng rơm được thu (tránh đốt) theo thửa; khối lượng làm tròn theo bậc để không lộ sản lượng thương mại | pf/mrv | Dự thảo đã có — chờ C-1 (danh mục trường MRV, phương pháp tCO2e) |

Hợp đồng mới: PR riêng, `proposed_by` ≠ `approved_by` (R-56), đi kèm schema sự kiện `mkg.<module>.<thực thể>.published v<N>`.

### 3.3 Luồng van (hiện thực ở `mkg/hub`)

```
app (giao dịch nghiệp vụ, tenant MKG)
  ├─ ghi sổ nghiệp vụ (vd facility_decision + facility_current)
  ├─ payload := port.HubCapacityPublishedV1{…}         // struct //ag:publication + //ag:event
  └─ export(pub port.Publication):
       c   := publication.Get(pub.PublicationName())  // hợp đồng nhúng; thiếu/chưa duyệt → từ chối
       raw := json.Marshal(pub); c.Validate(raw)      // khóa lạ, sai kiểu, thiếu required → từ chối, rollback
       INSERT mkg_hub.valve_export (id, payload = raw, payload_sha256, contract, …)   // sổ, chuỗi băm
       outbox.Enqueue(Event{ID: id, Type: c.Event, Payload: pub})                      // cùng giao dịch
outbox-publisher (cụm mkg, vai ag_outbox_publisher) → RabbitMQ (publisher confirm) → consumer pf (inbox, R-61)
```

---

## 4. Module `mkg/hub` — ĐÃ HIỆN THỰC (G2 đợt 1)

### 4.1 Trách nhiệm

Hub sơ chế, kho, bãi tập kết của MKG: dữ liệu chủ có phiên bản và phê duyệt; nhận lô có N nguồn kèm tỷ lệ; mẻ biến đổi (sấy, nén, phân loại) ghi thành phần đầu vào **tại thời điểm mẻ chạy**; dịch chuyển tồn bất biến; tồn theo lô; xuất theo rủi ro xuống cấp; van `hub_facility`, `hub_capacity` (mục 16 tài liệu kiến trúc).

### 4.2 Aggregate, sổ, bảng chiếu (migration mkg 000002, 000003 — schema `mkg_hub`, vai `hub_app`)

| Bảng | Loại | `stream_key` | Nội dung / bất biến |
|---|---|---|---|
| `facility_version` | sổ | `facility:<id>` (seq = số phiên bản) | Một phiên bản đề xuất: `code, name, kind ∈ {hub, warehouse, yard}, parent_facility_id, location Point 4326, boundary MultiPolygon 4326?, province_code, status ∈ {active, inactive}, capacity_kg numeric(14,3), effective_from`. Từ phiên bản 2: `supersedes_id` = phiên bản hiện hành + `correction_reason` = lý do thay đổi ≥ 10 ký tự |
| `facility_decision` | sổ | `facility:<id>` | `approved/rejected`, `reason` (bắt buộc khi từ chối), `proposer_ref`, `actor_ref` = người duyệt. **CHECK `actor_ref <> proposer_ref`** — lớp chặn tự duyệt thứ hai sau `app/` (R-56) |
| `facility_current` | bảng chiếu | — | Phiên bản đã duyệt mới nhất; `UNIQUE (tenant_id, code)` |
| `lot` | sổ | `facility:<facility_id>` | `code, product ∈ {straw_bale, straw_loose, dried_straw, compressed_bale}, origin ∈ {receipt, transform}, batch_id, initial_kg, moisture_pct, sources jsonb`. **CHECK `mkg_hub.sources_valid(sources)`**: mảng ≥ 1, mỗi phần tử có `kind, ref, ratio`, `ratio` chuỗi decimal ≤ 6 chữ số > 0, **Σ ratio = 1 chính xác** (numeric) |
| `batch` | sổ | `facility:<id>` | `kind ∈ {drying, compression, sorting}`, `inputs` (lô, kg, độ ẩm, dạng hàng, **ảnh chụp nguồn của lô vào**), `outputs`, `input_kg, output_kg, loss_kg`; **CHECK `loss_kg = input_kg − output_kg`, `loss_kg ≥ 0`** |
| `dispatch` | sổ | `facility:<id>` | `mass_kg, destination_ref, allocations` (lô, kg, điểm rủi ro), `strategy ∈ {risk_suggested, manual}`, `rule_version` = phiên bản quy tắc rủi ro |
| `stock_movement` | sổ | `lot:<lot_id>` | `kind ∈ {receive, transform_in, transform_out, dispatch, correction}`, `qty_kg` có dấu, `balance_after_kg ≥ 0`, `ref_type/ref_id`. CHECK dấu theo loại; `kind = correction ⇔ supersedes_id IS NOT NULL` |
| `lot_balance` | bảng chiếu | — | Tồn còn lại theo lô, `status ∈ {in_stock, depleted}`, CHECK `depleted ⇔ remaining = 0`. **Không có cột "tồn cơ sở"**: tồn cơ sở = Σ `remaining_kg` (lô là nguồn sự thật duy nhất — prototype A03/R3) |
| `valve_export` | sổ | `valve:<contract>:<hub_ref>` | Bản ghi đã xuất qua van (§3.3) |

Mọi sổ: mẫu mục 8.1 + `shared.apply_ledger` (trigger chuỗi băm, chặn UPDATE/DELETE/TRUNCATE kể cả owner, RLS, phân vùng tháng); `GRANT SELECT, INSERT` + `REVOKE UPDATE, DELETE, TRUNCATE` cho `hub_app`. Bảng chiếu cập nhật cùng giao dịch (R-21), dựng lại bằng `POST /v1/hub/projections/rebuild` (R-22, vai `hub_approver`).

### 4.3 Bất biến nghiệp vụ

| Mã | Bất biến | Cưỡng chế |
|---|---|---|
| H-1 | Lô có ≥ 1 nguồn; tỷ lệ tồn tại kể cả N = 1; Σ = 1 **chính xác**, 6 chữ số | `domain.BuildSources` (chia theo khối lượng bằng phương pháp dư lớn nhất, tất định) + CHECK DB `sources_valid` |
| H-2 | Mẻ: hao hụt = vào − ra ≥ 0; lô ra có nguồn `kind = lot` là các lô vào, tỷ lệ theo khối lượng; số tầng biến đổi là dữ liệu | `domain.Totals`, `domain.OutputSources` + CHECK DB |
| H-3 | Không lô thì không hàng: xuất/lấy vào mẻ/bù phải đủ **theo lô**, kiểm lại trong giao dịch trên lô đã khóa (`FOR UPDATE` theo thứ tự `lot_id`) | `domain.Apply`, `domain.Allocate`, CHECK `balance_after_kg ≥ 0`, `remaining_kg ≥ 0` |
| H-4 | Xuất theo **rủi ro xuống cấp**, không FEFO: `điểm = min(1, tuổi/90 ngày)×40 + (ẩm > 18% ? min(1,(ẩm−18)/10)×35 : 0) + min(1, cảnh báo/10)×25` (hệ số của prototype; G2 chưa có cảnh báo môi trường → 0). Hòa → nhận sớm trước → id | `domain.RiskV1` (G2: `hub.risk.v1`); **G3:** `hub.risk.v2#<n>` có phiên bản + cảnh báo môi trường (§4.7), không cảm biến → cùng điểm v1 |
| H-5 | Sổ dịch chuyển bất biến; sửa sai = dòng `correction` ngược dấu, `supersedes_id` + lý do ≥ 10 ký tự; mỗi dòng bù một lần; bù không được làm tồn âm. Chỉ `receive`/`dispatch` bù trực tiếp | `domain.Compensate` + trigger `fn_immutable` + CHECK |
| H-6 | Dữ liệu chủ có phiên bản; không xóa cứng (ngừng dùng = phiên bản `inactive`); một phiên bản chờ duyệt mỗi cơ sở; người duyệt ≠ người đề xuất; không đổi `kind` | `app.Propose/Decide` + CHECK `actor_ref <> proposer_ref` |
| H-7 | Cơ sở ngừng dùng không nhận hàng, không chạy mẻ; vẫn xuất được (rút hàng) | `activeFacility` |
| H-8 | Vượt sức chứa danh định vẫn ghi nhận (hàng đã về, không được mất bản ghi) và báo `over_capacity = true`; `available_tons = max(0, sức chứa − tồn)` | `app.Receive`, `domain.Snapshot` |

### 4.4 API (API nội bộ MKG — `contracts/openapi/mkg-internal.yaml`)

| Bề mặt | Route | Vai |
|---|---|---|
| Quản trị (MFA) | `POST /v1/hub/facilities`, `POST /v1/hub/facilities/{id}/versions`, `GET …/versions` | `hub_operator` |
| Quản trị (MFA) | `POST /v1/hub/facilities/{id}/versions/{n}/decision`, `POST /v1/hub/projections/rebuild` | `hub_approver` |
| Ứng dụng | `GET /v1/hub/facilities`, `GET …/{id}`, `GET …/{id}/inventory`, `GET …/{id}/dispatch-suggestion?mass_kg=`, `GET …/{id}/publications`, `GET /v1/hub/lots/{id}` | `hub_operator`, `hub_approver` |
| Ứng dụng | `POST …/{id}/receipts`, `POST …/{id}/batches`, `POST …/{id}/dispatches`, `POST …/{id}/capacity-snapshots`, `POST /v1/hub/movements/{id}/corrections` | `hub_operator` |

Mọi POST bắt buộc `Idempotency-Key` (R-51) — hai lớp: `shared.idempotency_key` của cụm mkg + `idem_key` trên sổ (kể cả khi bảng idempotency mất khóa, sổ trả lại bản ghi cũ — `TestIdempotentReceiptReplay`). Thao tác đều < 2 s → đồng bộ (R-52 không áp). Phân trang cursor (R-53). Mã lỗi `hub.*` ở `docs/errors.md`.

**Vì sao `hub_operator`/`hub_approver` thay cho `param_setter`/`param_approver`:** hai vai kia là vai phê duyệt tham số của nền tảng (pf/masterdata). Dữ liệu chủ hub thuộc MKG, người duyệt là quản lý vận hành MKG — tách vai để một người có quyền tham số nền tảng không mặc nhiên duyệt được hạ tầng MKG. Quy tắc R-56 giữ nguyên (kiểm ở `app/` và DB).

### 4.5 Sự kiện

| Sự kiện | Hợp đồng van | Khi phát | Payload |
|---|---|---|---|
| `mkg.hub.facility.published v1` | `hub_facility` v1 | Duyệt một phiên bản dữ liệu chủ | `port.HubFacilityPublishedV1` |
| `mkg.hub.capacity.published v1` | `hub_capacity` v1 | Duyệt phiên bản; `POST …/capacity-snapshots` | `port.HubCapacityPublishedV1` |

Không có sự kiện nội bộ nào khác rời module ở G2. Lô, mẻ, xuất **không** phát sự kiện (không được qua van); mkg/costing, mkg/logistics đọc qua `hub/port.Lots` (đồng bộ, cùng tiến trình).

### 4.6 Kiểm thử (`internal/mkg/hub/...`)

| Kiểm thử | Nội dung |
|---|---|
| `domain.TestRatiosFromMassesSumExactlyOne`, `TestCheckRatios`, `TestBuildSourcesRules` | H-1 (ARCH §16) |
| `domain.TestTotalsLossNonNegative`, `TestRiskRankingNotFIFO`, `TestCompensateAndApply`, `TestSnapshotAvailableNeverNegative`, `TestFacilitySpecValidate` | H-2..H-8 (WH FN-12/FN-19) |
| `handler.TestFacilityMasterDataApprovalAndValve` | Phiên bản, R-56 (app + DB), MFA, không xóa cứng, van khi duyệt, gọi lặp quyết định |
| `handler.TestLotRatioInvariant` | H-1 qua API và qua CHECK DB |
| `handler.TestTransformLossProvenanceAndInventory` | H-2, sấy → nén, tồn = Σ lô, dựng lại bảng chiếu (R-22), chuỗi băm |
| `handler.TestDispatchByDegradationRisk` | H-3, H-4 |
| `handler.TestMovementsImmutableCorrectionByCompensation` | RB-1/R-20 (vai ứng dụng + owner: UPDATE/DELETE/TRUNCATE đều hỏng), H-5 |
| `handler.TestTenantIsolation` | RB-2 |
| `handler.TestIdempotentReceiptReplay` | R-51 hai lớp, `hub.id_reused` |
| `handler.TestCapacitySnapshotValveOnlyContractFields` | Van chỉ trường hợp đồng; outbox `event_id` = id bản ghi van; `payload_sha256`; sổ van bất biến |
| `handler.TestValveRuntimeDefaultDeny` | Van từ chối lúc chạy (trường lạ cấp một và lồng; hợp đồng không tồn tại) |

### 4.7 Hub G3 (migration mkg 000009) — khối lượng từ phiếu cân, kiểm kê, `hub.risk.v2`, chuyển kho

| Mục | Hiện thực |
|---|---|
| Tích hợp G3-2 (mkg 000010) | **Ánh xạ lô:** lô gom của fieldops = lệnh gom; phiếu cân mang `lot_ref` của lô cân (lô hub nhận bằng `weigh_ticket_ref` nên id lô hub = `lot_ref` HUB_IN). Liên kết **tường minh** lô cân ↔ lệnh gom ở sổ `mkg_measurement.lot_collection` + bảng chiếu `lot_collection_current` (1 lệnh gom ↔ 1 lô): ghi khi mở phiên cân có `collection_order_ref` hoặc `POST /v1/measurement/lots/{id}/collection` (lô DIRECT); đổi = bản ghi bù có lý do. Không có liên kết = quy ước đồng nhất `lot_ref = order_id`. Lệnh gom nhiều ghe/xe → tách lệnh gom mỗi lần xếp hàng (lô chuyến hàng thật: logistics G4). `weigh_ticket.recorded` thêm `collection_order_ref` (tùy chọn). Adapter `cmd/internal/mkgwire` (api-internal, worker-internal): `measurement/port.RollTotals`/`RollTotalsAsOf`/`CollectionPlots` ← `fieldops/port.RollTotals` + `KgPerRollEstimates` (kg/cuộn học trước thời điểm cân, bỏ chính lô → đối soát tất định); `fieldops/port.WeighedMass` ← `measurement` qua liên kết (đường làm mới tay). Consumer `mkg.fieldops.weigh_ticket` (vai `fieldops_app`: phiếu cân đầu tiên → bảng đệm học kg/cuộn, bỏ phiếu gốc tới sau phiếu bù) và `mkg.measurement.roll_count` (vai `measurement_app`, đọc fieldops bằng pool `fieldops_app`: số cuộn mới → đối soát lại). Van MRV: tuyến DIRECT dùng CUSTOMER_IN × tỷ lệ thửa theo số cuộn của lệnh gom, `mass_basis = CUSTOMER_IN_AGGREGATED`; nguồn gốc lô hub đọc qua `hub/port.LotOrigins` (tenant tường minh, không cần vai hub). Dấu GPS tùy chọn của app hiện trường: sổ `mkg_fieldops.roll_count_location` (không thêm cột vào `roll_count` vì chuỗi băm) |
| Consumer nội bộ (`cmd/worker-internal`, vai `hub_app`, vhost `mkg.internal`) | `mkg.hub.weigh_ticket` ← `mkg.measurement.weigh_ticket.recorded`; `mkg.hub.roll_count` ← `mkg.fieldops.roll_count.recorded`; `mkg.hub.reconciliation` ← `mkg.measurement.reconciliation.completed`. Quorum queue + DLQ, 5 lần giao; idempotent hai lớp: `shared.inbox (event_id, consumer)` + `idem_key` sổ theo id nghiệp vụ (`weigh:<ticket_id>`, `recon:<reconciliation_id>`). Chỉ đọc payload theo struct `measurement/port`, `fieldops/port` — không import nội bộ module khác |
| Q-MKG-04 khối lượng lô | Sổ `weigh_link` (`lot:<lot_ref>`) + bảng chiếu `lot_weigh_current`. HUB_IN về trước lô → `pending`, lô `receipt` nhận bằng `weigh_ticket_ref` (id lô + khối lượng + độ ẩm theo phiếu). Lô nhập tay: giữ số nếu trong dung sai, ngoài dung sai → dịch chuyển `adjustment` (ref `weigh_ticket`), dòng `receive` không đổi; từ đó lô "theo phiếu" và phiếu bù luôn được điều chỉnh đúng chênh. Cần giảm mà hàng đã đi → `variance_unresolved` (chờ kiểm kê). HUB_OUT → gắn phiếu xuất mới nhất có phân bổ từ lô (`dispatch_linked` / `dispatch_variance` / `dispatch_unmatched`). Phiếu gốc tới sau phiếu bù → `stale`. `ticket_required` bật → nhận tay bắt buộc `manual_reason` |
| Q-MKG-09 tham số | `param_version` / `param_decision` (CHECK `actor_ref <> proposer_ref`) / `param_current`. Bộ `risk` = `hub.risk.v2` (hệ số v1 + độ ẩm 18/22 %, nhiệt 40/55 °C, `crit_alert_weight`), bộ `weigh` = `hub.weigh.v1` (dung sai 3 %, sàn 0 kg, `ticket_required` false, 20 kg/cuộn). Chưa duyệt → mặc định **tạm, chờ khách (C-4)**, ghim `…#0`. Phiếu xuất ghim `hub.risk.v2#<n>`. Cảm biến: `port.EnvReadings` (stub `NoEnvReadings`; nguồn thật `ts_env` sau) — mỗi số đo vượt cảnh báo = 1 cảnh báo, vượt nguy hiểm = `crit_alert_weight` |
| Q-MKG-06 kiểm kê | `stocktake` (dòng theo dạng hàng, sổ chụp lúc đếm, phủ mọi dạng hàng có tồn) → `stocktake_decision` (`hub_approver` ≠ người đếm, app + CHECK) → `stocktake_current` (một phiên chờ mỗi cơ sở; đếm lại = `supersedes_id` + lý do). Duyệt khi sổ còn đúng lúc đếm (không thì `hub.stocktake_stale`): thừa → lô `origin = stocktake` nguồn `kind = stocktake` tỷ lệ 1; thiếu → trừ theo `hub.risk.v2`; mọi thay đổi là dịch chuyển `adjustment` ref `stocktake` |
| Q-MKG-10 chuyển kho | `transfer_event` (`dispatched` / `received`) + `transfer_current` (`in_transit` → `received`). Xuất ở A = phiếu `dispatch` (`destination_ref` = B) + dịch chuyển `transfer_out`; nhận ở B: mỗi lô nguồn → lô mới `origin = transfer`, nguồn `kind = lot` tỷ lệ 1, khối lượng nhận chia theo khối lượng xuất (dư lớn nhất); hao hụt đường = xuất − nhận (có dấu). Phiếu HUB_IN của lô nguồn khi đang đi đường → `transfer_arrival`, dùng làm `weigh_ticket_refs` khi nhận. `transfer_out`/`transfer_in` không bù trực tiếp |
| Quy kết hao hụt | `lot_loss` (sổ) + `lot_loss_current` (đối soát mới nhất theo `event_time`); `GET /v1/hub/facilities/{id}/losses` |
| Hàng dự kiến về | `expected_inbound` (bảng chiếu sự kiện đếm cuộn; bản bù thay bản cũ); kg ước tính = cuộn công đoạn muộn nhất × `kg_per_roll_default` (không phải `yield_kg_per_roll`) |
| API | App: `POST/GET /v1/hub/facilities/{id}/stocktakes`, `GET /v1/hub/stocktakes/{id}`, `POST …/{id}/decision` (`hub_approver`), `POST /v1/hub/facilities/{id}/transfers`, `POST /v1/hub/facilities/{id}/transfers/{transfer}/receipt`, `GET /v1/hub/transfers/{id}`, `GET /v1/hub/lots/{id}/measurements`, `GET /v1/hub/facilities/{id}/losses`; nhận lô thêm `weigh_ticket_ref`, `manual_reason`. Admin (MFA): `GET /v1/hub/params/{set}`, `POST …/versions`, `POST …/versions/{n}/decision` |
| Kiểm thử | `handler.TestWeighTicketConsumerIdempotentCompensating`, `TestStocktakeApprovalSoD`, `TestRiskParamsVersionedApproval`, `TestTransferLifecycle`, `TestReconciliationRollCountAndTenantIsolation`; `domain.TestRiskV2*`, `TestHubInDecisionTolerance`, `TestBuildStocktake`, `TestSplitProportionalExact`; `cmd/worker-internal/assembly.TestHubG3ConsumersRegistered`, `TestNoPlatformDependency` (R-03) |

---

## 5. Các module còn lại — thiết kế

Mỗi module: một schema, một vai DB, sổ theo mẫu 8.1, bảng chiếu `*_current`, `port/` là cửa duy nhất cho module mkg khác (R-02). Số migration **đề xuất** — người tích hợp cấp chính thức.

### 5.1 `mkg/fieldops` — lệnh gom, đếm cuộn, nhật ký sản xuất

| Mục | Thiết kế |
|---|---|
| Trách nhiệm | Lệnh gom theo HTX/xã/khung thời gian; phân công tổ gom (máy cuộn, xe); **đếm cuộn tại thửa** (sổ đã có từ G0); nhật ký sản xuất; xác nhận của HTX (`htx_confirmation_ref`, mục 13.3) |
| Aggregate | `CollectionOrder` (lệnh gom), `RollCount` (đếm cuộn — dữ liệu mức tài chính, mục 15), `CrewShift` |
| Sổ | `roll_count` (`lot:<collection_lot>`; đã có — mkg 000001), `collection_order_event` (`order:<id>`), `crew_shift` (`crew:<id>`), `htx_confirmation` (`order:<id>`) |
| Bảng chiếu | `collection_order_current`, `roll_total_current` (theo lệnh gom, theo thửa) |
| Sự kiện | Không sự kiện rời vùng ở G3 ngoài hợp đồng `collection_schedule`, `htx_collection_statement` (§3.2) khi được duyệt |
| Liên kết | → `hub` (lô `receipt` có nguồn `roll_count`/`plot`), → `measurement` (cửa sổ lô để tích phân phiếu cân), → `costing` (quy đổi cuộn → tấn) — qua `port/` |
| Nguồn ngoài | Thửa, HTX của nền tảng: gọi Application API công khai bằng tài khoản dịch vụ tenant MKG (cửa "tiêu thụ không đặc quyền"); lưu `plot_ref`, `htx_ref` UUID thuần (R-06) |
| Đồng bộ ngoại tuyến | App hiện trường MKG (Flutter, `apps/field-app` biến thể MKG) — engine đồng bộ riêng vùng mkg; `id` UUIDv7 + `idem_key` do máy sinh (R-11, mục 15); không ghi đè, xung đột giữ cả hai |
| Vai DB / nghiệp vụ | `fieldops_app` (đã có) / `field_collector`, `field_supervisor` (duyệt lệnh gom ≠ người lập) |
| Migration | mkg 000006 (G3) — g3-conventions §2 |

### 5.2 `mkg/measurement` — ba điểm cân, phiếu cân dẫn xuất, đối soát (mục 13) — **event sourcing mức B: sổ là nguồn**

> **ĐÃ HIỆN THỰC (G3-A2, 30/09/2026)** — migration mkg 000007–000008, schema `mkg_measurement`, vai `measurement_app` / `ingest_mkg`.
> - Sổ: `device_version` + `device_decision` (R-56 app + CHECK DB), `weighing_session` (cửa sổ bộ đếm, bì, ẩm), `weigh_ticket` (`lot:<id>`; `rule_version`, thiết bị/kỳ/khoảng bộ đếm, `batch_ids`, `readings_sha256`, bằng chứng tay; bù bằng `supersedes_id`), `lot_terms` (cách tính giá — tạm tới G4), `tolerance_version` / `loss_norm_version` + `param_decision` (có phiên bản, áp theo thời điểm cân; mặc định tạm cờ 5 % / nghiêm trọng 15 %, định mức sấy theo khoảng ẩm — **chờ khách**), `reconciliation` (`lot:<id>`, lần sau thay lần trước), `dispute_event`, `customer_weight_import` (C-5 tạm), `valve_export`.
> - Bảng chiếu (cùng giao dịch, dựng lại qua `POST /v1/measurement/projections/rebuild`): `lot_mass_current`, `lot_yield_current`, `dispute_current`, `device_current` (kèm sức khỏe từ ingest), `weighing_session_current`, `param_current`, `lot_terms_current`.
> - Hai bảng xuyên tenant KHÔNG RLS, chỉ ghi thêm, chỉ khóa + tenant: `device_directory` (ingest suy ra tenant từ thiết bị, R-16) và `session_directory` (vòng tích phân tìm tenant có phiên đã đóng).
> - Cổng: `TicketWriter`, `LotMass`, `DeviceRegistry` (+ mới `DeviceHealth`, `WeighingSessions`, `RollTotals`) ở `measurement/port`; `measurement.NewIngestPorts` cho `cmd/ingest` (sổ thiết bị, `PendingSessions`, bì theo cửa sổ, sổ phiếu).
> - RB-6: hợp đồng `per_tonne` → kiểm chéo cuộn ↔ khối lượng có cờ; `per_bale` → chỉ tham khảo. Số cuộn qua `RollTotals` (fieldops chưa nối → `pending_rolls`).

| Mục | Thiết kế |
|---|---|
| Trách nhiệm | Nhận số đo cân thô từ agent biên (qua `cmd/ingest`), tích phân thành phiếu cân theo cửa sổ lô (R-70), ba điểm `HUB_IN`, `HUB_OUT`, `CUSTOMER_IN`; tách hao hụt dự kiến/bất thường; đối soát `HUB_OUT → CUSTOMER_IN` theo bảng dung sai có phiên bản (RB-6, C-4); hồ sơ tranh chấp |
| Aggregate | `WeighTicket`, `Reconciliation`, `Dispute`, `ToleranceTable` (có phiên bản, người đặt ≠ người duyệt) |
| Sổ | `ts_weigh.raw_reading` (cụm ts, chuỗi thời gian, không xóa — R-71), `weigh_ticket` (`lot:<id>`, ghi `rule_version` thuật toán tích phân + khoảng `raw_reading` đã dùng), `reconciliation` (`lot:<id>`), `dispute_event` (`dispute:<id>`), `tolerance_version` |
| Bảng chiếu | `lot_mass_current` (HUB_IN/HUB_OUT/CUSTOMER_IN của mỗi lô), `dispute_current` |
| Sự kiện | **Nội bộ** (vhost `mkg.internal`, ADR-0018): `mkg.measurement.weigh_ticket.recorded` v1, `mkg.measurement.reconciliation.completed` v1 (schema `contracts/events-internal/`, struct `measurement/port`); mặc định vẫn là `port/` đồng bộ. Công bố: `mrv_collection_evidence` (§3.2) |
| Bất biến | Phiếu cân chỉ do ingest tạo (R-70); lô `route = DIRECT` không tính hao hụt tách lớp; dung sai theo phiên bản có hiệu lực **lúc sự việc** (mục 16) |
| Vai DB / nghiệp vụ | `measurement_app`, `ingest_mkg` (cụm mkg: đọc sổ thiết bị, ghi phiếu cân), `ingest_ts` + `ts_reader` (cụm ts) — đã khai ở G3-0 / `scale_supervisor`, `device_approver`, `reconciliation_officer`, `dispute_officer`, `tolerance_approver` |
| R-10 | Package `measurement` đã nằm trong `agnofloat` |
| Migration | mkg 000007–000008 + cụm ts 000001 (**đã có ở G3-0**: `ts_weigh.reading_batch`, `ts_weigh.raw_reading`) |
| Hợp đồng đóng băng (G3-0) | `measurement/port`: `Integrator`, `TicketWriter`, `LotMass`, `DeviceRegistry`; `measurement/port/edgewire` + `contracts/edge/batch.v1.json` (ADR-0019) |

### 5.3 `mkg/hub` — xem §4.

### 5.4 `mkg/logistics` — chuyến, chặng đường thủy và đường bộ

| Mục | Thiết kế |
|---|---|
| Trách nhiệm | Chuyến (xe, ghe), chặng, điểm dừng; gắn phiếu xuất hub (`dispatch.destination_ref`) và phiếu cân; telemetry phương tiện (vùng biên → ts) |
| Aggregate | `Trip`, `Leg` |
| Sổ | `trip_event` (`trip:<id>`: planned, departed, arrived, cancelled), `leg_event` |
| Bảng chiếu | `trip_current` |
| Nguồn ngoài | Đồ thị đường thủy / tuyến đường bộ của nền tảng (`pf/waterway`, OSRM) — qua Application API công khai (không import `pf/waterway`, R-03); **kết quả định tuyến đã dùng lưu bền vững** vào chặng (tái hiện chi phí, mục 16) |
| Vai DB / nghiệp vụ | `logistics_app` (mới) / `dispatcher`, `driver` (ghi mốc chuyến trên app) |
| Migration | G4 — cấp từ mkg 000013 |

### 5.5 `mkg/orders` — đơn hàng, giao nhận

| Mục | Thiết kế |
|---|---|
| Trách nhiệm | Đơn bán (khách, dạng hàng, khối lượng, giá — **không bao giờ qua van**), lịch giao, biên bản giao nhận, liên kết `CUSTOMER_IN` của measurement |
| Aggregate | `SalesOrder`, `Delivery` |
| Sổ | `order_event` (`order:<id>`), `delivery_event`; giá `numeric(18,4)` đơn giá, `numeric(18,2)` tiền qua `shared/money` (R-10, `agnofloat` đã áp `orders`) |
| Phê duyệt | Giá/chiết khấu vượt ngưỡng: `sales_officer` lập, `sales_approver` duyệt (R-56) |
| Vai DB | `orders_app` (mới) |
| Migration | G4 — cấp từ mkg 000013 |

### 5.6 `mkg/costing` — giá thành, quyết toán, quy đổi cuộn → tấn — **event sourcing mức B: sổ là nguồn**

| Mục | Thiết kế |
|---|---|
| Trách nhiệm | Quy đổi cuộn → tấn (`yield_kg_per_roll = mass_kg(HUB_IN) / rolls`, mục 13.3), quy đổi ẩm độ, phân bổ chi phí đa chặng (gom, vận chuyển, sấy/nén, lưu kho), quy kết hao hụt về lô nguồn theo **tỷ lệ nguồn của lô** (`hub/port.Lots`), quyết toán HTX theo kỳ |
| Aggregate | `CostEntry`, `Settlement`, `Period` |
| Sổ (nguồn sự thật) | `cost_entry` (`lot:<id>`), `allocation_result` (`period:<id>`), `settlement_event` (`settlement:<id>`), `period_event` (mở/đóng kỳ) |
| Quy tắc | **Mỗi phép tính ghim `rule_version`** vào bản ghi kết quả (mục 16); định mức theo **thời điểm sự việc**; tính lại quá khứ dùng `recorded_at` (R-13) — ghi kết quả mới `supersedes_id`, không sửa |
| Đóng kỳ | Theo mẫu mục 12: `POST …/periods/{id}/close` → 202 + job; worker-internal tính; ảnh chụp đầu vào bất biến; người đóng ≠ người lập bút toán điều chỉnh (R-56) |
| Vai DB / nghiệp vụ | `costing_app` (mới) / `cost_accountant`, `period_closer` |
| R-10 | `agnofloat` đã áp `costing` |
| Migration | G4 — cấp từ mkg 000013 |

### 5.7 `mkg/acctbridge` — bút toán tổng hợp → kế toán pháp định

| Mục | Thiết kế |
|---|---|
| Trách nhiệm | **Chỉ đẩy bút toán tổng hợp theo kỳ, một chiều, tần suất tháng** tới phần mềm kế toán pháp định (mua ngoài). Không đẩy giao dịch chi tiết (mục 16) |
| Sổ | `summary_journal` (`period:<id>`: tài khoản, số tiền tổng, `source_hash` = băm tập kết quả costing đã tổng hợp), `push_attempt` (bảng vận hành) |
| Luồng | Kỳ costing đã đóng → dựng bút toán tổng hợp (job, mẫu mục 12) → `chief_accountant` duyệt (≠ người dựng) → đẩy qua bộ điều hợp; lỗi → thử lại, DLQ, cảnh báo (R-62). Nhà cung cấp trong lãnh thổ (RB-7) |
| Vai DB / nghiệp vụ | `acctbridge_app` (mới) / `accounting_bridge_operator`, `chief_accountant` |
| Migration | G4 — cấp từ mkg 000013 |

### 5.8 Bảng tổng hợp cụm / vai / migration

Số chính thức theo [g3-conventions §2](../dev/g3-conventions.md#2-số-migration) (người tích hợp cấp ở G3-0, 30/09/2026).

| Module | Schema | Vai DB | Migration | Có sẵn? |
|---|---|---|---|---|
| fieldops | `mkg_fieldops` | `fieldops_app` | 000001 (roll_count), **000006** (G3) | vai + 000001 đã có; `fieldops/port` (tên vai, sự kiện nội bộ, van dự thảo) đã có |
| hub | `mkg_hub` | `hub_app` | **000002** (master, lô, mẻ, xuất, dịch chuyển), **000003** (sổ van), **000009** (G3: khối lượng từ phiếu cân, kiểm kê, `hub.risk.v2`) | 000002–000003 **đã làm** |
| measurement | `mkg_measurement` + `ts_weigh` (cụm ts) | `measurement_app`, `ingest_mkg` (mkg); `ingest_ts`, `ts_reader` (ts) | **000007–000008** + ts **000001** | vai + ts 000001 + port đóng băng **đã có (G3-0)** |
| logistics | `mkg_logistics` | `logistics_app` | G4 (từ 000013) | cần thêm vai |
| orders | `mkg_orders` | `orders_app` | G4 (từ 000013) | cần thêm vai |
| costing | `mkg_costing` | `costing_app` | G4 (từ 000013) | cần thêm vai |
| acctbridge | `mkg_acctbridge` | `acctbridge_app` | G4 (từ 000013) | cần thêm vai |
| dự phòng G3 | | | **000010–000012** | xin người tích hợp |
| API nội bộ | `shared.idempotency_key` cụm mkg | `mkg_api` (tương đương `platform_api`) | — (quyền qua `ag_shared_user`, shared 000001) | **đã làm** — `cmd/api-internal/assembly.OpsRole` (Q-MKG-07 đóng) |
| worker-internal | `shared.inbox` cụm mkg | `worker_internal` | — | **khung đã có (G3-0)**: `cmd/worker-internal`, vhost `mkg.internal` (ADR-0018) |

## 6. Tuân thủ bảy ràng buộc cứng (mục 3)

| RB | Cơ chế chung vùng mkg | `mkg/hub` (đã chứng minh bằng kiểm thử) | Module khác (thiết kế) |
|---|---|---|---|
| **RB-1** chỉ ghi thêm | Sổ mẫu 8.1 + `shared.apply_ledger`: REVOKE UPDATE/DELETE/TRUNCATE, trigger `fn_immutable` áp cả owner, chuỗi băm `fn_chain` + neo hằng ngày (R-20); migration không UPDATE/DELETE (R-25, `migratecheck`). Sửa sai = bản ghi bù `supersedes_id` + lý do | Mọi sự việc là sổ (phiên bản, quyết định, lô, mẻ, xuất, dịch chuyển, van); sửa = dòng `correction`; ngừng dùng = phiên bản mới (`TestMovementsImmutableCorrectionByCompensation`, `TestFacilityMasterDataApprovalAndValve`) | measurement, costing: sổ là nguồn (ADR-0003); tính lại = kết quả mới trỏ về kết quả cũ |
| **RB-2** cách ly tenant | `tenant_id` mọi bảng, RLS một policy (R-33), `WithTenantTx` (R-30), `v_missing_rls` rỗng | `TestTenantIsolation` (đọc, ghi, duyệt, xuất van) | như hub; kiểm thử rò rỉ song song qua PgBouncer (R-32) ở G3 |
| **RB-3** dữ liệu cá nhân xóa được | Vùng mkg **không** lưu dữ liệu cá nhân: chỉ `*_ref`; hiển thị tên hộ/HTX qua `vault/identity/client` có mục đích (R-42). Ảnh hiện trường mã hóa phong bì theo hộ (mục 10.2) | Không có trường cá nhân; van không cho qua trường lạ (`TestValveRuntimeDefaultDeny` thử `owner_phone`) | fieldops (ảnh thửa, tên chủ ruộng) dùng vault; tài xế/nhân viên MKG: định danh ở vault tenant MKG |
| **RB-4** giấy phép | Chỉ dependency thuộc `deploy/policies/allowed-licenses.txt` | Không thêm dependency mới | Bộ điều hợp kế toán (acctbridge): giấy phép kiểm lúc chọn nhà cung cấp |
| **RB-5** chuyển giai đoạn không sửa mã | Ranh giới module (`agboundary`: R-02, R-03 cả ở gốc lắp ráp), vai DB theo module, gốc lắp ráp theo vùng; tách `api-internal`/`worker-internal` bằng cấu hình | Module chỉ nối ở `cmd/api-internal/assembly`; `AG_MKG_DB_URL` riêng; phạm vi người dùng chưa có (Q-MKG-03) nhưng không đổi mô hình dữ liệu | như hub |
| **RB-6** đối soát chéo ở biên | Số đo ở biên giữa hai bên có đối soát và bảng dung sai có phiên bản | Hub ghi `mass_kg` của lô/mẻ/xuất; khối lượng **chuẩn** của lô là phiếu cân `HUB_IN`/`HUB_OUT` của measurement (G3) — hub nhận khối lượng từ phiếu cân qua `port/` khi có (Q-MKG-04) | measurement: `HUB_OUT → CUSTOMER_IN` so bảng dung sai `[KÝ]`; fieldops: `htx_confirmation_ref` (đếm cuộn đối soát với HTX) |
| **RB-7** lưu trong lãnh thổ | `residency.CheckEnv` ở mọi lệnh; DB, kho đối tượng, sao lưu, quan sát trong lãnh thổ | `cmd/api-internal` gọi `residency.CheckEnv`; không gọi dịch vụ ngoài | acctbridge: nhà cung cấp kế toán trong lãnh thổ; ingest/edge-agent: đích trong lãnh thổ |

### 6.1 Đối chiếu quy tắc R-xx chính

| Quy tắc | Hiện thực ở vùng mkg |
|---|---|
| R-02 | Chỉ `port/` của module khác; `agboundary` |
| R-03 | `platform` ↔ `mkg` không import nhau (package và gốc lắp ráp); `contractcheck`: sự kiện mkg = hợp đồng van |
| R-05 | Mỗi module một vai chỉ CONNECT `ag_mkg`; truy vấn xuyên schema hỏng lúc chạy |
| R-06 | Tham chiếu xuyên module là UUID thuần (`plot_ref`, `htx_ref`, `destination_ref`, `lot_id` trong nguồn) |
| R-10 | `mass.Kg` numeric(14,3), tỷ lệ numeric(9,6), độ ẩm numeric(5,2), tọa độ `json.Number`/decimal; `agnofloat` áp **toàn bộ** `internal/mkg/hub` (kể cả kiểm thử) |
| R-11, R-13 | UUIDv7; `event_time` (hiện trường) + `recorded_at` (DB đặt) ở mọi sổ |
| R-12 | `domain/` nhận thời gian qua `clock.Clock` |
| R-14 | Không trường cá nhân; không log payload |
| R-15 | `problem.*`, mã `hub.*` |
| R-21, R-22 | Bảng chiếu cùng giao dịch; `RebuildBalances` |
| R-51 | Hai lớp: `shared.idempotency_key` + `idem_key` sổ |
| R-56 | `hub_approver` ≠ người đề xuất — `app/` + CHECK DB |
| R-60, R-61 | Outbox cụm mkg, publisher quét mọi cụm; consumer pf dùng inbox (đợt 2) |
| R-64 | `mkg.hub.facility.published`, `mkg.hub.capacity.published` có schema `contracts/events/`; tương thích ngược do `contractcheck -base` |

---

## 7. Trình tự G2 → G4

| Cổng | Việc |
|---|---|
| **G2 đợt 1 (xong)** | Tài liệu này; `mkg/hub` (000002, 000003); van `hub_facility`/`hub_capacity` v1; `cmd/api-internal` có gốc lắp ráp; `contractcheck` siết (siêu dữ liệu phê duyệt, kiểu lồng nhau, luật vùng mkg); `archlint` R-03 ở gốc lắp ráp + `agnofloat` cho `mkg/hub` |
| G2 đợt 2 | `pf/geo` tiêu thụ van (`HubSink`, inbox, mô hình đọc hub); vai `mkg_api` + `worker_internal` ở `dbadmin`; Compose/Helm cho `api-internal`; client OIDC dịch vụ cho tenant MKG; job chụp sức chứa định kỳ (Q-MKG-02) |
| G3-0 (xong) | Cụm ts + `ts_weigh` (ADR-0017); vhost `mkg.internal`, `outbox.kind`, `//ag:internal-event`, `cmd/worker-internal` (ADR-0018); hợp đồng dây edge ↔ ingest + sổ thiết bị (ADR-0019); port đóng băng `measurement`, `fieldops`, `pf/metrics`; van dự thảo `collection_schedule`, `htx_collection_statement`, `mrv_collection_evidence`; vai DB G3 |
| G3 | `mkg/measurement` + `cmd/ingest` + agent biên theo mẫu cân đã chốt (C-6); `mkg/fieldops` G3 (lệnh gom, app hiện trường MKG, xác nhận HTX); hub nhận khối lượng từ phiếu cân; cảm biến môi trường kho → điểm rủi ro có cảnh báo (`hub.risk.v2`); hợp đồng `collection_schedule`, `htx_collection_statement`, `mrv_collection_evidence` nếu được duyệt; `worker-internal` |
| G4 | `mkg/logistics`, `mkg/orders`, `mkg/costing` (đóng kỳ theo mẫu mục 12), `mkg/acctbridge`; diễn tập dựng lại bảng chiếu trên bản sao dữ liệu thật (R-22); tách đơn vị triển khai bằng cấu hình; diễn tập tải ĐTK 2 |

---

## 8. Câu hỏi mở

| # | Câu hỏi | Đề xuất | Ai | Chặn |
|---|---|---|---|---|
| Q-MKG-01 | Sự kiện **nội bộ** giữa các module mkg (vd phiếu cân → costing) đi đâu, khi exchange RabbitMQ dùng chung với nền tảng? | **Đã chốt (G3-0, ADR-0018):** vhost `mkg.internal` riêng, tài khoản nền tảng không có quyền; `shared.outbox.kind`; `//ag:internal-event` + `contracts/events-internal/`; `contractcheck` bắt payload nội bộ không đi vhost nền tảng; consumer ở `cmd/worker-internal` | Kiến trúc + vận hành | — |
| Q-MKG-02 | Tần suất công bố `hub_capacity`: theo mỗi dịch chuyển, theo lịch, hay theo ngưỡng thay đổi? | G2: job `hub_capacity_daily` của `api-internal` công bố mỗi cơ sở đang dùng **một lần mỗi ngày** (01:15 giờ VN, theo GIS US-WH-01 AC-2 "chu kỳ đồng bộ theo ngày"), cộng khi duyệt và khi chụp tay. Đề xuất bổ sung khi chốt: khi thay đổi > 5 % sức chứa | MKG vận hành | pf/geo độ tươi dữ liệu |
| Q-MKG-03 | Phạm vi người vận hành theo hub (người ở hub A không thao tác hub B)? | Gán `facility_scope` trong claim hoặc bảng phân công ở mkg/hub, kiểm ở `app/` | MKG | ĐTK 2 nhiều hub |
| Q-MKG-04 | Khối lượng lô: nhập tay ở hub hay lấy từ phiếu cân `HUB_IN` (RB-6)? | **Đã làm (G3, §4.7):** consumer `mkg.hub.weigh_ticket`; lô `receipt` nhận bằng `weigh_ticket_ref`; nhập tay giữ số trong dung sai, ngoài dung sai → `adjustment` bù; `ticket_required` → nhập tay cần lý do. Dung sai 3 % **tạm, chờ C-4** | MKG + thương mại (C-4) | — |
| Q-MKG-05 | Sửa sai một mẻ biến đổi (ghi nhầm lô vào/khối lượng ra)? | "Mẻ bù": bản ghi `batch` mới `supersedes_id` + lý do, bù ngược mọi dịch chuyển của mẻ gốc nếu lô ra chưa bị dùng; lô ra đã dùng → chỉ bù bằng điều chỉnh kiểm kê có duyệt | MKG vận hành | — |
| Q-MKG-06 | Kiểm kê (WH FN-20..23) ở hub: điều chỉnh cấp lô có phê duyệt | **Đã làm (G3, mkg 000009, §4.7):** `stocktake` + `stocktake_decision` (người duyệt ≠ người đếm) + dịch chuyển `adjustment` (thừa → lô `kind = stocktake`; thiếu → trừ theo `hub.risk.v2`) | MKG | — |
| Q-MKG-07 | Vai vận hành cho `api-internal` trên `shared.idempotency_key` cụm mkg | **Đã đóng (G2 đợt 2):** vai `mkg_api` (thành viên `ag_shared_user`, chỉ CONNECT `ag_mkg`) trong `dbadmin`, userlist PgBouncer (CI + compose); không cần migration vì quyền trên `shared.idempotency_key` cấp cho `ag_shared_user` | Người tích hợp | Tách quyền khỏi pool `hub_app` |
| Q-MKG-08 | Người đề xuất / phê duyệt thật của hợp đồng van `hub_facility`, `hub_capacity` | Tệp hợp đồng đang ghi vai `mkg-hub-module-owner` / `mkg-data-steward` — MKG điền người thật khi ký | MKG | — |
| Q-MKG-09 | Tham số quy tắc rủi ro (90 ngày, ngưỡng ẩm 18%, trọng số) có phiên bản ở đâu, khi mkg không được dùng `pf/masterdata` (R-03)? | **Đã làm (G3, §4.7):** `param_version`/`param_decision`/`param_current` bộ `risk` (`hub.risk.v2`) và `weigh` (`hub.weigh.v1`); mặc định tạm `#0` chờ khách | MKG | — |
| Q-MKG-10 | Chuyển kho giữa hai cơ sở MKG | **Đã làm (G3, §4.7):** `transfer_event`/`transfer_current`, trạng thái đang đi đường, hao hụt đường. Hủy chuyến (nhận lại ở A) chưa làm | MKG | — |
| Q-MKG-11 | Ngưỡng k của van lọc dữ liệu công (C-3) | — | MKG | tiêu thụ dữ liệu công |
| Q-MKG-12 (RB-2) | Phản hồi của HTX quay về MKG (G3-2): bản ghi van (lịch gom, bảng kê) và sổ phản hồi `pf_cropcycle.collection_response` nằm ở **tenant của phong bì van** (như `pf_geo.hub_view`, `pf_mrv.collection_evidence`). Nếu MKG và chương trình là hai tenant (ADR-0009), quản lý HTX (tenant chương trình) sẽ không thấy bảng kê và `mkg_sync` (tenant MKG) chỉ thấy phản hồi của tenant MKG | **Tạm (G3-2):** luồng kéo `GET /v1/cropcycle/htx-collection/responses` chỉ cho vai `mkg_sync` (client `agrigreen-mkg-sync`), chỉ trả phản hồi có `source_tenant_id` = tenant người gọi, chỉ trường MKG cần (không người phản hồi). Cần chốt: cấp quyền chia sẻ tường minh (tenant cấp → tenant nhận, phạm vi `htx_collection`, mục đích, hạn, nhật ký ở `app/`) để consumer van ghi vào tenant chương trình của HTX và luồng kéo đọc chéo có kiểm; hoặc tài khoản dịch vụ MKG có vai chỉ đọc trong tenant chương trình | Khách + người tích hợp | trước khi bật với dữ liệu thật |

---

## 9. Lệch so với tài liệu kiến trúc và quyết định đã ghi

| Chủ đề | Tài liệu kiến trúc | Hiện thực | Lý do |
|---|---|---|---|
| Sổ xuất của van | "Tệp hợp đồng… CI hỏng build khi có trường ngoài hợp đồng" | Thêm sổ `valve_export` + kiểm lúc chạy + vận chuyển qua `shared.outbox` cụm mkg | Bằng chứng "đã công bố gì" (RB-1) và mặc định từ chối cả lúc chạy; dùng lại `outbox-publisher` đã quét mọi cụm, không thêm tiến trình |
| Sự kiện của vùng mkg | Mục 13.2 nêu `mkg.measurement.weigh_ticket.recorded` qua outbox | G2: mọi sự kiện mkg lên hàng đợi phải là hợp đồng van. G3: sự kiện nội bộ đi vhost riêng `mkg.internal` (`outbox.kind = internal`) | Exchange dùng chung → cần vhost riêng để có "sự kiện nội bộ" thật sự (ADR-0018) |
| Kho chuỗi thời gian | Mục 4: TimescaleDB | PostgreSQL 17 phân vùng tháng thuần ở cụm `ts`, bố cục tương thích Timescale, không TSL | RB-4 (giấy phép TSL), nhà cung cấp trong lãnh thổ (ADR-0017) |
| Vai phê duyệt dữ liệu chủ hub | R-56: `param_setter`/`param_approver` | `hub_operator`/`hub_approver` (R-56 giữ nguyên) | Tách thẩm quyền hạ tầng MKG khỏi thẩm quyền tham số nền tảng (§4.4) |
| Loại cơ sở | "hub: mẻ sấy, mẻ nén, tồn kho" | `kind ∈ {hub, warehouse, yard}`; kho/bãi có thể thuộc hub (`parent_facility_id`) | Prototype có hub/kho/bãi; nền tảng cần cả ba để điều phối |
| Khóa duy nhất sổ | Mẫu 8.1 có `UNIQUE (tenant_id, idem_key)` | `shared.ledger_idem` + `shared.chain_head` | ADR-0012 (bảng phân vùng) |
