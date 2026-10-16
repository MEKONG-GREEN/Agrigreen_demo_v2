# Ghi chú tải: vòng đời báo cáo KN v7.7 (phát hành, tổng hợp, dashboard)

Phạm vi: gói P4 (`internal/kn/extension/reportcycle`). Mục tiêu BRD: dashboard Tỉnh/TW < 5 giây với 500 người dùng
đồng thời (FN-TW-01.01 AC); phát hành 5 biểu × 102 xã × 17 kỳ = 8.670 công việc (FN-TI-02.02, Hình 7).

## Thiết kế để đạt mục tiêu

| Đường | Cách làm | Chỉ mục / bảng |
|---|---|---|
| Phát hành | Một giao dịch: 1 sự kiện sổ đợt + N kỳ + công việc chèn bằng `INSERT … SELECT unnest(...)` theo khối 2.000 dòng (không ghi sổ băm từng công việc — sự kiện `issued` của đợt là nguồn); một sự kiện outbox `pf.extension.report_batch.issued` (thông báo một lần mỗi cán bộ) | `report_task_commune`, `report_task_period`, `report_task_batch` |
| "Cập nhật" | Đọc bản "Đã gửi" mới nhất của mọi công việc đợt × biểu một lần (`SubmissionsByIDs`), tính lại bằng `FormEngine.Evaluate/Totals` trong bộ nhớ, so khác, ghi ảnh chụp + ô thay đổi (unnest) + **chỉ số dựng sẵn** | `consolidation_snapshot`, `consolidation_cell_change_run`, `consolidation_metric` |
| Dashboard | KHÔNG đọc bài nộp: đọc con trỏ `consolidation_latest` của kỳ + `consolidation_metric` của các lần chạy (đã gom theo tỉnh/khu vực/xã) + một truy vấn trạng thái nộp của kỳ — 6–8 truy vấn theo chỉ mục, không phụ thuộc số cột của mẫu | `consolidation_latest` (PK kỳ × biểu), `consolidation_metric` (PK lần chạy × chỉ số × phạm vi × đơn vị), `consolidation_metric_lookup` |
| Tự chốt / tự nhắc | Job 5 phút, chỉ mục một phần theo giờ chốt; tenant tìm bằng `ag_ops_jobs` (2 cột) | `report_period_autoclose`, `report_task_reopened` |

## Kiểm thử ghi số đo

`go test -count=1 -run TestFN_TW_01_01_LoadNote -v ./tests/kn77/` (bỏ qua với `-short`): phát hành 4 biểu × 102 xã × 17 tuần
= 6.936 công việc (bộ seed chỉ có 4 mẫu tuần không có khối kỳ — PL 01/01B/07 chỉ 4–7 khe kỳ, chờ khách Q-01), 102 xã gửi T04
(226 cột), "Cập nhật", rồi 20 yêu cầu dashboard đồng thời × 3. Ngưỡng kiểm thử: phát hành < 20 s, dashboard p95 < 5 s.

Số đo trên máy dev dùng chung (Windows, PostgreSQL 17 cục bộ, nhiều agent chạy song song) ngày 02/10/2026: phát hành 6.936 công việc **0,64 s**; "Cập nhật" 102 xã × T04 (226 cột) **0,35 s**; dashboard 20 đồng thời × 3 — p50 **19 ms**, p95 **1,27 s** (các yêu cầu đầu chờ pool kết nối), max 1,32 s.
Kiểm 500 người dùng thật làm ở staging bằng `tools/loadgen` ([load-drill.md](load-drill.md)) — việc của P8 (UAT N-05).

## Nếu chậm

- Dashboard: kiểm `EXPLAIN` truy vấn `consolidation_metric` theo `run_id = ANY(...)`; số lần chạy mỗi kỳ ≤ số biểu.
- Phát hành > 2 s ở môi trường thật: chuyển sang 202 + job (R-52) — tách bước `InsertTasks` ra consumer theo `report_batch.issued`.
- Tổng hợp T04 (54 cây × 4 cột × 102 xã): thời gian chủ yếu ở `Evaluate`; có thể song song hóa theo biểu.
