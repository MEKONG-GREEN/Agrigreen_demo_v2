### pf/hrm (`dom/hrm` — kế hoạch nhân lực năm, W11.27)

Kỳ kế hoạch là kỳ native cap/forms (profile `hrm_plan`): lỗi của vòng đời kỳ/bài nộp giữ mã `forms.*` (`forms.period_closed`,
`forms.submission_accepted`, `forms.submission_conflict`, `forms.values_invalid`, `forms.already_reviewed`,
`forms.submission_not_found`) và `self_approval_forbidden` (rà soát bốn mắt).

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `hrm.plan_not_found` | Năm chưa mở kỳ kế hoạch, hoặc lãnh đạo đơn vị hỏi kỳ không có đơn vị mình |
| 409 | `hrm.plan_exists` | Mở kỳ cho năm đã có kỳ (một kỳ mỗi năm) |
| 404 | `hrm.plan_unit` | Đơn vị không thuộc kỳ, hoặc lãnh đạo đơn vị hỏi đơn vị khác |
| 409 | `hrm.plan_closed` | Lưu nháp sau khi kế hoạch đã chốt; chốt lại kỳ đã chốt (BR-02: chỉ điều chỉnh có lý do) |
| 409 | `hrm.plan_not_closed` | Ghi điều chỉnh khi kế hoạch chưa chốt (trước chốt: đơn vị gửi lại) |
| 503 | `hrm.plan_disabled` | Gốc lắp ráp chưa nối cap/forms cho pf/hrm (`UsePlans`) — lỗi cấu hình |
| 404 | `hrm.plan_no_previous` | "Sao chép từ năm trước": năm trước chưa mở kỳ hoặc đơn vị không gửi |
| 409 | `hrm.plan_all_submitted` | "Nhắc phòng chưa lập" khi mọi đơn vị của kỳ đã gửi kế hoạch (NOTIFY-HCTH-KN #50) |
