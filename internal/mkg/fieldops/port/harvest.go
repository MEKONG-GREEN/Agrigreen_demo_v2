package port

// ---------------------------------------------------------------------------
// Nghiệm thu G3 — proto FM-01/02 dùng cổng HarvestCalendar (ADR-0020, htxfeed.go): ngày gặt
// của thửa theo vụ đọc qua bề mặt partner GET /v1/partner/season-harvest-dates. Thêm vào,
// không đổi phần đã có.
// ---------------------------------------------------------------------------

// JobHarvestDates là tên job làm mới bảng đệm ngày gặt của các lệnh gom đang mở (AG_MKG_JOBS).
const JobHarvestDates = "fieldops_harvest_dates"

// ---------------------------------------------------------------------------
// R-INT-02 (kiến trúc v4.1 §39, §41.4) — bản sao danh mục HTX/thửa trong cụm mkg (mkg 000033). Thêm vào,
// không đổi phần đã có.
// ---------------------------------------------------------------------------

// JobDirectory là tên job làm mới bản sao danh mục HTX/thửa của nền tảng (AG_MKG_JOBS).
const JobDirectory = "fieldops_directory"
