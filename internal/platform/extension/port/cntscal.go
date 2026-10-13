package port

import "github.com/google/uuid"

// W11.46 (D4 CN6, BRD CNTS v3.7 FN-15, UR-TINH-10): lịch trực KSGM BM-03.

// EventCNTSDutyChanged: lịch trực KSGM của một tháng vừa lưu phiên bản mới và có viên chức bị đổi ca (thêm, bớt, đổi
// ca) — "Đổi ca → thông báo viên chức liên quan" (UR-TINH-10), "Lịch hiện ở Cấp Xã trong 1 phút" (FN-15 AC-01). Consumer
// (pf/notify) gửi thông báo cho officer_refs.
const EventCNTSDutyChanged = "pf.extension.cnts_duty.changed"

// CNTSDutyChangedV1 là payload của pf.extension.cnts_duty.changed. Không có dữ liệu cá nhân ngoài mã tham chiếu cán bộ.
//
//ag:event pf.extension.cnts_duty.changed v1
type CNTSDutyChangedV1 struct {
	Month       string      `json:"month"`
	RosterID    uuid.UUID   `json:"roster_id"`
	Version     int64       `json:"version"`
	OfficerRefs []uuid.UUID `json:"officer_refs"`
	ActorRef    uuid.UUID   `json:"actor_ref"`
	Department  string      `json:"department"`
	EventTime   string      `json:"event_time"`
}
