### pf/extension (`dom/extension` — bộ đọc nguồn module của Kỳ KHTC, W11.64)

| HTTP | code | Khi nào |
|---|---|---|
| 403 | `extension.khtc_refresh_not_owner` | "Cập nhật số liệu" bởi cán bộ KHTC không phụ trách kỳ (⚑3 mặc định: người phụ trách = người mở kỳ). Kỳ đã đóng/hủy trả `extension.khtc_period_terminal` (DB: `khtc_snapshot_terminal`) |
