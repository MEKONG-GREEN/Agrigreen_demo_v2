# ADR-0024: Một điểm kiểm soát xuất dữ liệu (DLP) cho van, Public API, xuất gói MRV, xuất báo cáo

**Trạng thái:** Đề xuất · **Ngày:** 01/10/2026 (G5-0) · **Thành phần:** `pf/metering` (`port.Checkpoint`, sổ `pf_metering.egress_event`), `pf/reports`, `pf/mrv`, consumer van `pf.geo.hub_valve`, `pf.cropcycle.htx_collection_valve`, `pf.mrv.collection_evidence`, `cmd/api-public` · **Liên quan:** mục 18 kiến trúc (Dữ liệu — Phòng chống thất thoát dữ liệu), [ADR-0020](0020-lien-thong-platform-mkg-khac-tenant.md), [ADR-0022](0022-public-api-ben-thu-ba.md), [ADR-0023](0023-so-do-luong-su-dung-metering.md)

## Bối cảnh

Mục 18 của kiến trúc: "Mọi đường xuất (van công bố, Public API, xuất package, xuất báo cáo) đi qua **một** điểm kiểm soát có nhật ký, hạn mức, và cảnh báo khi khối lượng bất thường".

Hiện trạng ở cuối G4:

| Đường xuất | Đang có |
|---|---|
| Xuất báo cáo (`pf/reports`) | `app.egress` (`internal/platform/reports/app/dlp.go`) là điểm kiểm soát **cục bộ**, gọi trong cùng giao dịch: nhật ký `pf_reports.export_audit`, hạn mức người/ngày `reports.daily_export_quota` (50) → 429, ngưỡng bất thường `reports.export_anomaly_threshold` (30) → `pf.reports.export.anomaly` v1 |
| Xuất gói MRV (`pf/mrv`) | Lưu gói đã xuất kèm băm ảnh chụp (mục 16). Không có hạn mức, không có cảnh báo khối lượng |
| Van công bố (MKG → nền tảng) | Hợp đồng mặc định từ chối, kiểm cả lúc build lẫn lúc chạy. MKG có sổ xuất `mkg_<module>.valve_export`. Phía nền tảng có cấp quyền `valve_*` (ADR-0020). Không có hạn mức khối lượng |
| Bề mặt đối tác `/v1/partner/*` (ADR-0020) | Sổ đọc `pf_party.partner_read_log` ở tenant bên cấp |
| Public API | chưa có (ADR-0022) |

Vùng mkg không được import vùng nền tảng (R-03). Vì vậy một thư viện kiểm soát "dùng chung" không thể là module nền tảng mà vùng mkg gọi được.

## Quyết định

### 1. Điểm kiểm soát duy nhất ở `pf/metering`

Cổng `meteringport.Checkpoint`:

```go
Pass(ctx, Egress) (Decision, error)
```

Đây là **điểm kiểm soát duy nhất** của mọi đường xuất phía nền tảng. Mỗi lần gọi, trong một giao dịch của `metering_app`:

1. **Ghi nhật ký:** một dòng sổ `pf_metering.egress_event`, theo mẫu mục 8, chuỗi băm. Tenant của dòng là **chủ dữ liệu rời đi**.
   - Nội dung: kênh, sản phẩm hoặc hợp đồng, phiên bản, bên nhận (`counterparty_kind` + `counterparty_ref`: khách hàng API, tenant bên cấp, `actor_ref` người xuất), số phần tử, số byte, quyết định, lý do.
   - Không có dữ liệu cá nhân, không có giá trị truy vấn.
2. **Kiểm hạn mức:** bộ đếm `egress_counter_current` theo (tenant dữ liệu × kênh × bên nhận × ngày giờ Việt Nam), so với tham số `metering.egress.<kênh>.daily_records_quota`.
3. **Phát hiện bất thường:** khối lượng ngày vượt `metering.egress.<kênh>.anomaly_factor` × trung vị 28 ngày trước (cần ít nhất 7 ngày có dữ liệu). Không đủ mẫu thì so với ngưỡng tuyệt đối `metering.egress.<kênh>.anomaly_records`.
   - Lần **đầu** vượt trong ngày phát `pf.metering.egress.anomaly_detected` v1. Consumer `pf.notify.egress_anomaly` báo quản trị nền tảng của tenant chủ dữ liệu.
   - Đồng thời tăng metric `ag_egress_anomalies_total{channel}` để Alertmanager cảnh báo.
4. **Metric** (nhãn cardinality thấp, R-91): `ag_egress_records_total{channel,decision}`, `ag_egress_bytes_total{channel}`, `ag_egress_denied_total{channel,reason}`, `ag_egress_checkpoint_errors_total{channel}`.

Tham số hạn mức và bất thường ở `pf/masterdata` (setter ≠ approver). Mặc định ghi ở g5-conventions §1.2: tạm, chờ khách.

### 2. Hai chế độ, chuyển bằng cấu hình (R-01)

| `AG_EGRESS_MODE` | Hành vi |
|---|---|
| `observe` (mặc định, ĐTK 1) | Ghi nhật ký, đếm, cảnh báo, **không bao giờ từ chối**. Checkpoint lỗi (DB) → log `event=egress outcome=checkpoint_error` + metric, thao tác **vẫn tiếp tục**. Không đổi hành vi hiện có |
| `enforce` (Phase K) | Vượt hạn mức → từ chối (`Decision.Allowed = false`, `metering.egress_quota_exceeded`). Checkpoint lỗi → thao tác thất bại (503 / giao lại) |

**Public API luôn chạy như `enforce` và đóng khi lỗi**, không phụ thuộc biến này. Ở Public API, sổ sử dụng (ADR-0023) và dòng egress ghi chung một giao dịch.

### 3. Từng đường xuất đi qua checkpoint thế nào (không phá đường đang chạy)

Module nhận `Checkpoint` qua `Deps`/`Use…` ở gốc lắp ráp. **Nil → hành vi cũ**: các kiểm thử hiện có không đổi.

| Kênh (`port.Channel…`) | Ai gọi | Lúc nào | Từ chối khi `enforce` |
|---|---|---|---|
| `public_api` | `api-public` qua `Ledger.Record` | sau khi dựng phản hồi, trước khi gửi byte | 429 / 503, không gửi dữ liệu |
| `mrv_package` | `pf/mrv` (`app`) | khi nhận yêu cầu xuất (phần tử = số bằng chứng ước) và khi cấp URL tải gói (`PackageURL`, byte = cỡ gói) | 429 `metering.egress_quota_exceeded` / 503 |
| `report_export` | `pf/reports` | ngay **sau** `app.egress` cục bộ ở cả ba lối: yêu cầu xuất, lịch, tải. Lần tải bảng kê metering cũng dùng kênh này | như trên |
| `valve` | consumer van của nền tảng (`pf/geo`, `pf/cropcycle`, `pf/mrv`) | sau `ValveTargets`, **mỗi bên cấp một lần**, trước khi ghi vào tenant bên cấp. Tenant dữ liệu = tenant phong bì (MKG), bên nhận = tenant bên cấp | consumer trả lỗi → giao lại → DLQ (R-62) + cảnh báo. Sửa nguyên nhân rồi chuyển lại từ DLQ; xử lý vẫn idempotent |
| `partner_share` | `pf/party` `RecordRead` (bề mặt `/v1/partner/*`) | cùng lúc ghi `partner_read_log` | tùy chọn ở G5 (P2). `partner_read_log` vẫn là sổ của cấp quyền |

Ba lựa chọn đi kèm bảng trên:

- **Báo cáo giữ điểm kiểm soát cục bộ.** Hạn mức theo người, trong cùng giao dịch, và sự kiện `pf.reports.export.anomaly` v1 vẫn là thẩm quyền cho người dùng nội bộ. Checkpoint chung thêm hạn mức theo tenant/kênh và nhật ký thống nhất. Không thay thế, không đổi mã lỗi hiện có (`reports.export_quota_exceeded`).
- **Van kiểm ở phía nhận, không ở MKG.** Phía nhận (nền tảng) là nơi biết dữ liệu đi tới bên cấp nào (`ValveTargets`). Ở đó checkpoint dùng chung được một sổ và một bộ cảnh báo với ba kênh kia, không cần module hay vai mới ở cụm mkg. Phía MKG giữ nguyên lớp mặc định từ chối theo hợp đồng và sổ `valve_export`. Hai sổ đối chiếu được theo `event_id`.
- **Không đi qua checkpoint:** sự kiện nội bộ MKG (`mkg.internal`, ADR-0018) và bút toán kế toán (`mkg/acctbridge`). Đó là luồng trong vùng mkg hoặc ra kế toán pháp định của chính MKG. Mục 18 không liệt kê chúng là đường xuất của nền tảng. Ghi ở g5-conventions để rà lại khi đánh giá cấp độ.

### 4. Đọc nhật ký

- `GET /v1/metering/egress?channel=&from=&to=&cursor=` (Admin API): vai `metering_viewer` hoặc `platform_admin` của **tenant chủ dữ liệu**, chỉ thấy dòng của tenant mình (RLS).
- `GET /v1/metering/egress/anomalies`: danh sách cảnh báo.
- Bên cấp thấy cả bên thứ ba nào đọc gì, bao nhiêu (ADR-0022).

## Hệ quả

- Mục 18 được đáp ứng bằng một cổng, một sổ, một bộ cảnh báo cho bốn đường xuất mà mục 18 liệt kê, cộng bề mặt `/v1/partner/*`.
- Ở ĐTK 1 hành vi không đổi (`observe`). Phase K chuyển `enforce` bằng cấu hình (ADR-0025).
- Thêm một giao dịch `metering_app` cho mỗi lần xuất báo cáo, gói MRV và mỗi phong bì van × bên cấp. Khối lượng nhỏ so với lời gọi Public API.
- Kiểm thử bắt buộc:
  - mỗi kênh ghi đúng một dòng (giao lặp phong bì van → không ghi thêm: idempotent theo (`event_id`, bên cấp));
  - `observe` không từ chối, kể cả khi checkpoint lỗi;
  - `enforce` từ chối đúng mã;
  - lần đầu vượt ngưỡng → đúng một sự kiện bất thường;
  - cách ly tenant.
- Việc mở, chờ khách:
  - hạn mức và ngưỡng theo kênh;
  - người nhận cảnh báo (bộ phận an ninh của MKG hay quản trị của từng tenant);
  - có chặn `valve` khi vượt hạn mức hay chỉ cảnh báo.

## Ghi chú triển khai (03/10/2026 — quyết định A4, A5 của chủ sản phẩm)

- **A5 — xuất nội bộ KN đi qua điểm kiểm soát ở chế độ quan sát, không bao giờ bị chặn.** `meteringport.Egress` có thêm cờ `Internal bool` (bổ sung, tương thích ngược — mặc định `false` giữ hành vi cũ). `Internal = true` → `Egress.EffectiveMode` luôn trả `observe`: `Guard` không bao giờ từ chối (kể cả checkpoint lỗi) và `Checkpoint.Pass` (pf/metering) đánh giá ở `observe` — dòng `egress_event` ghi `mode = observe`, `decision = allowed` (lý do `quota_exceeded` nếu vượt), vẫn được đếm vào bộ đếm ngày và phát hiện bất thường, kể cả khi `AG_EGRESS_MODE=enforce`. Hai đường dùng cờ này, kênh `report_export`, bên nhận = cán bộ xuất (`actor`):
  - "Xuất Excel/PDF" lịch công tác (`pf/extension`, `app/schedule_export.go`, sản phẩm `kn_work_schedule`, phần tử = số dòng) — `extension.Module.UseEgress` nối ở `cmd/api-platform/assembly`;
  - tải tệp thửa KN `GET /v1/kn/plots/export` (`pf/geo`, `app/kn.go` `KNExport`, sản phẩm `kn_plot_file`, phần tử = số thửa).
  Kênh ra ngoài (`Internal = false`: xuất báo cáo, gói MRV, van, Public API, liên kết Chi cục) vẫn bị từ chối ở `enforce`. Kiểm thử: `TestGuardInternalNeverDenied_A5` (port), `TestCheckpointInternalObserveUnderEnforce_A5` (checkpoint thật, enforce + vượt hạn mức), `TestFN_TI_02_07_UnregisteredBulkRemindersExport` và `TestFN_XA_03_05_CreateListGetUpdateCorrectLog` (handler, enforce + checkpoint giả luôn từ chối: vẫn 200, lần xuất mang `Internal`).
- **A4 — liên kết tải báo cáo Chi cục 7 ngày.** Lần mở liên kết công khai `GET /v1/public/report-links/{token}` đi qua `app.egress` cục bộ (export_audit kind `download`) rồi checkpoint dùng chung kênh `report_export` với bên nhận = người nhận Chi cục (mã danh bạ) — đây là đường ra ngoài nên **không** dùng `Internal`; `enforce` vượt hạn mức → 429 và sổ `dispatch_link_access` outcome `egress_denied` (`TestA4_ChiCucDownloadLink7Days`).
