package port

import "github.com/google/uuid"

// Yêu cầu hỗ trợ của nông dân App HTX (HTX US-SUPPORT-01) — PA1-HTX-SUPPORT-GPSLOG (#313): chuyển từ pf/extension về
// module chủ HTX (bảng pf_coop.support_request, pf 20261116020730). Log GPS qua consumer GpsLogConsumer như vụ/nhật ký.

// EntitySupportRequest là entity đồng bộ HAI chiều của nông dân (docs/dev/sync-contract.md): chiều lên = yêu cầu gửi
// ngoại tuyến (op create), chiều xuống = yêu cầu của hộ mình. Tên giữ nguyên để App HTX không đổi.
const EntitySupportRequest = "support_request"

// EventSupportRequestSubmitted: nông dân gửi yêu cầu hỗ trợ (thay pf.extension.support_request.submitted).
const EventSupportRequestSubmitted = "pf.cooperative.support_request.submitted"

// SupportRequestSubmittedV1 là payload của pf.cooperative.support_request.submitted. Không có mô tả, ảnh hay toạ độ
// (đọc qua API trong phạm vi) — không PII.
//
//ag:event pf.cooperative.support_request.submitted v1
type SupportRequestSubmittedV1 struct {
	SupportRequestID uuid.UUID `json:"support_request_id"`
	HouseholdRef     uuid.UUID `json:"household_ref"`
	CooperativeRef   uuid.UUID `json:"cooperative_ref"`
	CommuneCode      string    `json:"commune_code"`
	ProvinceCode     string    `json:"province_code"`
	Category         string    `json:"category"`
	Urgency          string    `json:"urgency"`
	PhotoCount       int       `json:"photo_count"`
	GPSMissing       bool      `json:"gps_missing"`
	EventTime        string    `json:"event_time"`
	ReceivedAt       string    `json:"received_at"`
}
