# Hợp đồng cột view công bố `_pub`

View công bố `pf_<module>_pub.v<N>_<tên>` (R-DB-10, W6.3 — [modules.md](../../docs/dev/modules.md#view-công-bố-_pub-w63)) là hợp đồng có phiên bản như port. Thư mục này ghi **cột** của các view có bên đọc ngoài `pf/metrics` — trước hết là **nguồn module của KHTC** (W11.10; BRD KN 10/2026, đối chiếu Phụ lục D X6, BG10 phần dữ liệu): KHTC chỉ đọc số liệu **đã chốt** của các phòng qua view `_pub`, cùng tenant, không qua cấp phép chia sẻ, không cần `r_agg` ([đối chiếu §2.2](../../docs/kn/doi-chieu-brd-kn-2026-10.md)).

## Hợp đồng hiện có

| View | Chủ | Trạng thái | Hạt | Ghi chú |
|---|---|---|---|---|
| [`pf_extension_pub.v1_consolidation_closed`](pf_extension_pub.consolidation_closed.v1.json) | `pf/extension` | **published** (pf 000144) | kỳ × biểu | Số liệu đã chốt (lần tổng hợp các xã lúc chốt) theo phòng × tỉnh × khóa kỳ + cờ chốt biểu/kỳ/khóa kỳ. Trồng trọt bây giờ; CNTS vào cùng view (`department_code = chan_nuoi_thuy_san`) |
| [`pf_price_pub.v1_board_daily`](pf_price_pub.board_daily.v1.json) | `pf/price` | **published** (pf 000152, W11.76) | ngày × mặt hàng | Bảng giá đã duyệt theo ngày, min/max toàn tỉnh (Q-084) |
| [`pf_price_pub.v1_ticket_week`](pf_price_pub.ticket_week.v1.json) | `pf/price` | **published** (pf 000152, W11.76) | tuần báo cáo × xã báo giá | Số phiếu giá đã thu thập theo tuần báo cáo + cờ tuần đã chốt |
| [`pf_extension_pub.v1_closed_figures`](pf_extension_pub.closed_figures.v1.json) | `pf/extension` | **published** (pf 20261013150110) | kỳ × biểu | Họ `closed_figures/v1` ([ADR-0053](../../docs/adr/0053-hop-dong-so-lieu-da-chot-closed-figures.md)) — nguồn vòng đời báo cáo cũ (Trồng trọt), chiếu lại `v1_consolidation_closed` về cột chung |
| [`pf_forms_pub.v1_closed_figures`](pf_forms_pub.closed_figures.v1.json) | `pf/forms` | **published** (pf 20261013150108) | kỳ native (một biểu) | Họ `closed_figures/v1` — kỳ native `cap/forms` (CNTS `kn_cnts`), ảnh chụp đóng băng lúc chốt, chỉ trường số |

## Họ hợp đồng `closed_figures/v1` (ADR-0053)

- Hai view, hai module chủ, **cột và thứ tự giống hệt** (`family` trong tệp JSON; `pubview.FamilyProblems` kiểm). KHTC đọc qua MỘT port `extension/port.KHTCSources` (vai `r_extension`, thành viên `r_extension_pub`, `r_forms_pub`, `r_price_pub` — `dbadmin.PubReaders`), không rẽ nhánh theo cột `source`.
- `rows`: `[{key, unit, unit_kind, parent, region, indicator, nil_report, cells, confirmation}]`, chỉ ô số; `totals`: dòng tổng của nguồn hoặc `[]` (kỳ native — bên đọc cộng `rows`).
- Danh tính kỳ nguồn (D8 — mỗi phòng một lịch tuần): `(calendar_id, calendar_version, period_key)` — khóa GỐC, chưa quy đổi. Quy đổi sang tuần KHTC: bảng chuyển đổi có phiên bản sau port (`ClosedFigures.Converted`, `kn.crosswalk/v1;attribution=source_end` — ADR-0053 §6).
- `key_closed` của từng view chỉ tính trong nguồn đó; port tính `KeyClosedAll` trên cả họ.
- Trồng trọt giữ vòng đời báo cáo lâu dài (W11.04 hủy): hai nguồn chính thức. Phòng mới dùng kỳ native: thêm profile vào `pf_forms_pub.v1_closed_figures` bằng `CREATE OR REPLACE VIEW`. Đổi cột = `closed_figures/v2` ở mọi nguồn cùng lúc.
- Nguồn gốc cho ảnh chụp KHTC (W11.64): `source_module`, `closed_at`, `figures_hash` (sha256 số đã chốt, không gồm trạng thái xác nhận); D2: `confirmation_status`, `rows[].confirmation`, `status_hash` riêng. Port KHTC duy nhất `extension/port.KHTCSources` đọc thêm `pf_price_pub` (Báo giá) — ADR-0053 §7, §8.

## "Nguồn đã chốt" (QĐ-6, Q-048 — đề xuất của dev, chờ PO)

- Số liệu bên đọc nhận là **ảnh chụp đã chốt**: với vòng đời báo cáo KN là lần tổng hợp các xã (`consolidation_run` + `consolidation_snapshot`, bất biến) mới nhất của kỳ × biểu ghi **không muộn hơn** lần chốt gần nhất chạm biểu (`closed`, `auto_closed`, `reclosed`). Lần "Cập nhật" tay sau khi mở lại là số tạm, không lọt vào view cho tới khi chốt lại.
- Nguồn sửa được tới khi chốt; mở lại một xã × biểu làm cờ chốt về `false` nhưng số đã chốt trước đó vẫn trả (KHTC hiện "Nguồn đã thay đổi" — W11.64).
- Cờ "nguồn đã chốt cho tuần X" = `key_closed`: **mọi** biểu của **mọi** kỳ cùng (phòng, tỉnh, loại kỳ, khóa kỳ) đã chốt và không xã × biểu nào đang mở lại.
- Khóa tra cứu là `period_key` **đã lưu** lúc phát hành (W11.05b, sổ `report_period_key`; tuần Thứ Tư – Thứ Ba theo W11.03). Kỳ không có khóa (vụ, đột xuất, phát hành trước pf 000143) không có dòng.
- KHTC đọc lũy kế của nguồn (Q-047 đề xuất): cột lũy kế nằm trong `rows[].cells`/`totals[].cells` theo mẫu biểu.

## Dạng tệp `<schema>_pub.<tên>.v<N>.json`

```json
{
  "view": "<schema>_pub.v<N>_<tên>",
  "directive": "<schema>_pub.<tên>/v<N>",
  "owner": "pf/<module chủ>",
  "status": "published | planned",
  "package": "<gói>", "migration": "<pf 000xxx>",
  "consumers": ["..."], "purpose": "...", "grain": "...", "key": ["<cột>"], "closed_rule": "...",
  "personal_data": false,
  "family": "<tên>/v<N> — tùy chọn, ADR-0053",
  "columns": [{ "name": "<cột>", "type": "<format_type của PostgreSQL>", "nullable": false, "description": "..." }]
}
```

- `columns` theo **thứ tự** cột của view; `type` đúng chuỗi `format_type()` (vd `timestamp with time zone`, `text[]`, `numeric`). Không cột số thực (R-CODE-01), không dữ liệu cá nhân (R-14: không `actor_ref`, tên người).
- `published`: view đã dựng, khai `//ag:pubview <schema>_pub.<tên> v<N>` trong `port/` của module chủ. `planned`: cột đã chốt, view chưa dựng — **chưa** được khai.

## Cưỡng chế

- `go test ./contracts/pubview`: siêu dữ liệu hợp lệ; `published` có trong `provides.publications` của đúng module chủ; `planned` chưa khai.
- `TestPubViewsDeclaredAndPrivileges_W63` (`internal/platform/metrics/handler`, tích hợp): cột view thật (tên, kiểu, thứ tự) trùng hợp đồng `published`; đếm số view `_pub` đã khai.
- `TestMetricsReadsOnlyDeclaredPubViews_W63`: view có hợp đồng `published` ở đây không bắt buộc `pf/metrics` đọc (bên đọc là KHTC); `pf/metrics` vẫn chỉ đọc view đã khai.
- Đổi cột/ý nghĩa = `v<N+1>` mới (tệp mới), view cũ giữ tới khi không còn bên đọc. Đổi bảng nội bộ (vd thêm nguồn CNTS bằng `UNION ALL`) được `CREATE OR REPLACE VIEW` nếu giữ nguyên cột và ý nghĩa.

## Việc của W11.76 để dựng hai view giá

Đã làm ở W11.76: bảng nguồn pf 000151 (`pf_price.board`, `board_line`, `board_ticket`), view pf 000152 (chỉ `pf_price_pub`). Chính sách RLS của bảng nguồn là `p_tenant` chuẩn (`shared.apply_ledger`) — không gọi hàm mới, không cần cấp thêm EXECUTE cho `r_agg`.

`pf_price_pub` và vai `r_price_pub` (thành viên `r_metrics`, `r_agg`) đã có từ pf 000138. W11.76 cần:

1. Bảng nguồn trong `pf_price` (sổ `shared.apply_ledger`, FORCE RLS): phiên bản bảng giá ngày đã duyệt (ngày, số phiên bản, lúc duyệt, **khóa tuần `week_key`/`week_year` lưu lúc duyệt** theo lịch kỳ hệ thống W11.03, trạng thái mở duyệt), dòng bảng giá (mặt hàng, min/max/bình quân `numeric`, số xã có giá) và phiếu báo giá W11.75 có liên kết tới phiên bản bảng giá (phiếu xóa mềm/thay thế không đếm).
2. Một migration **chỉ đụng `pf_price_pub`** (`<số>_pf_price_pub_*` hoặc `<số>_pf_pub_*`): `CREATE VIEW pf_price_pub.v1_board_daily`, `pf_price_pub.v1_ticket_week` đúng cột và thứ tự của hai tệp hợp đồng; `GRANT SELECT … TO r_price_pub`. Nếu chính sách RLS của bảng nguồn gọi hàm ngoài `shared.current_tenant()`, cấp EXECUTE cho `r_agg` (modules.md mục `r_agg`).
3. `//ag:pubview pf_price_pub.board_daily v1` và `//ag:pubview pf_price_pub.ticket_week v1` trong `internal/platform/price/port/port.go`, chạy `go run ./tools/modgen`.
4. Đổi `status` hai tệp thành `published`, sửa `migration`; số view trong `TestPubViewsDeclaredAndPrivileges_W63` +2 và số module công bố 4 → 5; `TestContracts_W1110` không đổi số hợp đồng.
5. `days_scheduled` tính theo lịch báo giá có phiên bản (pf 000139: ngày trong tuần + ngoại lệ) tại ngày đó; `week_closed` = `days_approved = days_scheduled` và không ngày nào đang mở duyệt.
