package port

import "github.com/google/uuid"

// W11.49 (D4 CN9, BRD CNTS v3.7 FN-11): Báo cáo dịch bệnh BM-10.

// EventOutbreakReported: ổ dịch mới đã gửi Cấp Tỉnh (Cấp Xã lập BM-10 hoặc xác nhận báo nghi dịch). Nguồn của cảnh báo
// Khẩn số 12 (FN-11 BR-01, FN-13 — W11.43) và lớp ổ dịch trên bản đồ (FN-18 — W11.52).
const EventOutbreakReported = "pf.extension.outbreak.reported"

// OutbreakReportedV1 là payload của pf.extension.outbreak.reported. Không chứa dữ liệu cá nhân (không hộ, không tọa
// độ — consumer đọc qua API có phân quyền).
//
//ag:event pf.extension.outbreak.reported v1
type OutbreakReportedV1 struct {
	OutbreakRef  uuid.UUID  `json:"outbreak_ref"`
	VersionID    uuid.UUID  `json:"version_id"`
	CommuneCode  string     `json:"commune_code"`
	DiseaseCode  string     `json:"disease_code"`
	SpeciesCode  string     `json:"species_code"`
	Notifiable   bool       `json:"notifiable"`
	SuspectRef   *uuid.UUID `json:"suspect_ref"`
	ReceivedAt   string     `json:"received_at"`
	ReportedAt   string     `json:"reported_at"`
	ReporterRef  uuid.UUID  `json:"reporter_ref"`
	Department   string     `json:"department"`
	HerdSize     int64      `json:"herd_size"`
	Dead         int64      `json:"dead"`
	AsOf         string     `json:"as_of"`
	RuleVersion  string     `json:"rule_version"`
	ProvinceCode *string    `json:"province_code"`
}
