# pf/metrics — chỉ số có lịch sử, nhật ký lớp GIS, lưu trữ

Mã nguồn: `internal/platform/metrics`. Migration `pf/000036` (định nghĩa, `daily_value`) và `pf/000037` (`layer_event`, `layer_snapshot`). Vai DB `metrics_app`.

## 1. Định nghĩa chỉ số

- Mỗi tệp `internal/platform/metrics/defs/<key>.v<n>.yaml` là **một** phiên bản. Nội dung gồm: `key`, `version`, `title`, `description`, `unit`, `grain` (`day` hoặc `season`), `aggregation`, `source_module`, `source`, `rule_version`, `effective_from`, `proposed_by`, `approved_by` (phải khác người đề xuất, R-56), `viewer_roles`.
- Đổi công thức thì **thêm tệp phiên bản mới**, với `effective_from` sau phiên bản trước. Không sửa tệp cũ. Đơn vị và grain không đổi giữa các phiên bản; muốn đổi thì dùng key mới.
- Lần đầu một tenant dùng một phiên bản, băm tệp được ghim vào `pf_metrics.definition_pin`. Nếu sau đó tệp bị sửa, job báo `metrics.definition_edited` và không ghi.
- Kiểm thử `TestRepoDefinitionsLoad` nạp và kiểm mọi tệp. Tệp hỏng thì `metrics.New` panic khi khởi động.

Chỉ số hiện có:

| Key | Grain | Nguồn (sổ) | Phạm vi ghi sổ |
|---|---|---|---|
| `kn.sown_area_ha`, `kn.cooperatives_reported`, `kn.open_tasks`, `kn.directive_ack_rate_pct` | ngày | `pf_extension.field_report`, `task_event`, `directive*` | toàn tenant + tỉnh |
| `cgh.coop_sufficiency_pct`, `cgh.coops_shortage` | vụ | `pf_mechanization.balance_run/result` (lần cân đối đã lưu) | toàn tenant + tỉnh, theo vụ |
| `cc.season_area_ha`, `cc.yield_t_per_ha` | vụ | `pf_cropcycle.season_event`, `harvest_declaration` | toàn tenant + xã, theo vụ (năm D và 2 năm trước) |
| `geo.plots_digitised` | ngày | `pf_geo.plot_event`, `plot_boundary` | toàn tenant + xã |

## 2. Giá trị theo ngày

- Giá trị của ngày D được tính chỉ từ bản ghi có `recorded_at < Cutoff(D)`, tức trước 00:00 giờ VN ngày D+1. Bản ghi bù ghi nhận sau mốc này không làm đổi giá trị của D.
- Mỗi module chủ dữ liệu đọc bằng vai DB của chính nó, trong giao dịch chỉ đọc (R-05). Khi có bản sao đọc thì nối pool của bản sao vào nguồn; hiện gốc lắp ráp dùng pool chính.
- Job `metrics_snapshot` chạy lúc 00:45 giờ VN, trên leader, sau job neo 00:30. Job ghi hôm qua cho mọi tenant: tenant có chuỗi đã neo của ngày đó, không có thì lấy tenant có sự kiện outbox.
  - Cùng băm đầu vào thì không ghi dòng mới.
  - Khác băm thì ghi dòng mới, `supersedes_id` trỏ dòng cũ, kèm `correction_reason` dạng "tính lại: …".
- Log: `event=job.metrics_snapshot`, `inserted`, `superseded`, `unchanged`, `no_data`, `snapshots`.

### Backfill / tính lại

```
worker -metrics-backfill=2026-09-01:2026-09-29 [-metrics-tenant=<uuid>] [-metrics-keys=kn.open_tasks,cgh.coops_shortage]
```

- Lệnh chạy lần lượt từng ngày đã kết thúc, ghi sổ chỉ số, rồi chụp các lớp chưa có ảnh chụp.
- Dùng khi:
  - thêm chỉ số mới, cần lấp lịch sử;
  - thêm phiên bản định nghĩa có hiệu lực lùi;
  - sửa lỗi công thức ở nguồn.
- Lệnh chạy lại được an toàn: dòng không đổi được giữ nguyên.
- Cần `AG_DB_DIRECT_URL` (hoặc `AG_DB_URL`) và mật khẩu vai của mọi module pf, như với worker.

## 3. Nhật ký lớp GIS, ảnh chụp, phát lại (GIS FN-18..20)

Consumer `pf.metrics.layer_log` (vai `metrics_app`) nhận các sự kiện dưới đây và ghi `layer_event`. Mỗi dòng có trước/sau, `source_event_id`, `source_type`. Giao lặp không ghi thêm.

| Lớp | Sự kiện nguồn |
|---|---|
| `plot` | `pf.geo.plot.created`, `pf.geo.plot_boundary.recorded` (chỉ bản thành hiện hành), `pf.geo.plot.retired` (→ `retire`, phát lại ẩn thửa), `pf.geo.plot.restored` (→ `restore`) |
| `coop_boundary` | `pf.geo.cooperative_boundary.recorded` |
| `facility` | `pf.geo.facility.recorded` |
| `season_stage` | `pf.cropcycle.season.opened`, `.completed`, `.corrected` (hủy → `retire`) |
| `cgh_balance` | `pf.mechanization.balance.computed` (một đối tượng mỗi vụ) |
| `waterway` | `pf.waterway.network.published` |

Nguồn chưa có:

- **Thời tiết** (FN-19 "giữ cảnh báo lớn nhất ngày"): chưa có module thời tiết.
- **Hub MKG:** van `hub_facility` và `hub_capacity` hiện chỉ cho pf/geo.

Hình học **không** chép vào nhật ký. `after.geometry_ref` trỏ dòng sổ hình học bất biến của pf/geo (`plot_boundary`, `cooperative_boundary`, `facility_event`). `GET /v1/gis/replay?geometry=true` tra hình học as-of qua `geo.LayerGeometry()`.

- **Ảnh chụp (`layer_snapshot`):** job 00:45 chụp mỗi lớp của hôm qua. Ảnh chụp dựng từ nhật ký: ảnh chụp trước cộng các dòng sau watermark và trước cutoff. Chỉ ghi thêm, một ảnh chụp cho mỗi (lớp, ngày).
- **Phát lại D:** lấy ảnh chụp mới nhất ≤ D, rồi áp các dòng có `(recorded_at, id)` lớn hơn watermark và nhỏ hơn `Cutoff(D)`. Kết quả phải bằng dựng thẳng từ nhật ký (kiểm thử `TestGISLayerLogSnapshotReplay`). Để đối chiếu kiểm toán, `Service.FullReplay` dựng thẳng từ nhật ký.
- **Rủi ro còn lại:** `recorded_at` là giờ bắt đầu giao dịch ghi. Một giao dịch consumer mở trước 00:00 mà commit sau lúc chụp (00:45) và có `recorded_at` nhỏ hơn watermark sẽ không có trong ảnh chụp. Nó cũng không được áp khi phát lại. Consumer chỉ chạy giao dịch ngắn nên thực tế không xảy ra. Nếu nghi ngờ, so `ReplayLayer` với `FullReplay`.

## 4. Lưu trữ (GIS US-RETAIN-01, FN-21) — CHUYỂN TẦNG, KHÔNG XÓA (RB-1)

Số ngày lưu **chờ khách**. Prototype dùng các mốc sau; đây chỉ là tham khảo, **tạm, chờ khách**:

| Dữ liệu | Prototype |
|---|---|
| Nhật ký | 730 ngày |
| Ảnh chụp | 365 ngày |
| Nén | sau 90 ngày |

Kế hoạch:

1. **Tầng nóng (PostgreSQL, phân vùng tháng theo `recorded_at`)**
   - `layer_event` và `daily_value` là bảng sổ phân vùng tháng (`shared.apply_ledger`). `migrate maintain` tạo trước phân vùng, xem [partitions.md](partitions.md).
   - `layer_snapshot` không phân vùng: mỗi (lớp, ngày) một dòng, `features` jsonb nén TOAST.
   - Truy vấn API chỉ chạm tầng nóng.
2. **Tầng ấm, khi có số ngày của khách**
   - Tách (`DETACH`) phân vùng tháng cũ của `layer_event` và `daily_value` thành bảng lưu trữ chỉ đọc trong cùng cụm.
   - Giữ chuỗi băm. Neo 00:30 đã ký `chain_head` hằng ngày, nên `verify-chains` vẫn đối chiếu được.
   - Phát lại ngày cũ dùng ảnh chụp gần nhất trong tầng nóng. Chỉ cần tầng ấm khi phát lại trước ảnh chụp cũ nhất còn giữ.
3. **Tầng lạnh**
   - Xuất phân vùng đã tách ra tệp nén.
   - Tệp kèm manifest ký số (cùng khóa và định dạng với manifest neo) và băm từng tệp.
   - Lưu vào kho đối tượng trong lãnh thổ (RB-7), rồi mới `DROP` phân vùng đã tách. Bước `DROP` cần quyết định vận hành có ghi biên bản.
   - Dữ liệu vẫn tồn tại ở tầng lạnh và khôi phục được bằng cách nạp lại phân vùng. Vì vậy đây không phải xóa nghiệp vụ.
4. Ảnh chụp cũ hơn mốc lưu có thể **thưa dần** (giữ ảnh chụp cuối tháng) theo cùng quy trình xuất: trước, xuất ra tầng lạnh; sau đó mới gỡ khỏi tầng nóng. Nhật ký là nguồn sự thật, nên ảnh chụp luôn dựng lại được.

Chưa làm ở G3-A5 (chờ số của khách):

- job chuyển tầng;
- công cụ xuất/nạp lại phân vùng.

Không có câu lệnh DELETE nào trên các bảng này: trigger `shared.fn_immutable` chặn cả owner.
