// Package port là giao diện công khai DUY NHẤT của module pf/party mà module
// khác được import (R-02).
package port

import (
	"context"

	"github.com/google/uuid"
)

// ConsentChecker cho module khác kiểm đồng ý theo mục đích trước khi xử lý dữ
// liệu cá nhân (mục 10.5). Tenant lấy từ context.
type ConsentChecker interface {
	HasConsent(ctx context.Context, subjectRef uuid.UUID, purpose string) (bool, error)
}

// Vai nghiệp vụ của module (tên vai trong Keycloak).
const (
	RoleConsentWriter = "consent_writer"
	RoleConsentReader = "consent_reader"
)

// Sự kiện module phát ra (schema trong contracts/events/).
const (
	EventConsentRecorded = "pf.party.consent.recorded"
)

// ConsentRecordedV1 là payload của pf.party.consent.recorded, schema_version 1.
//
//ag:event pf.party.consent.recorded v1
type ConsentRecordedV1 struct {
	ConsentID   uuid.UUID `json:"consent_id"`
	SubjectRef  uuid.UUID `json:"subject_ref"`
	Purpose     string    `json:"purpose"`
	Status      string    `json:"status"`
	TextVersion string    `json:"text_version"`
	EventTime   string    `json:"event_time"`
	Seq         int64     `json:"seq"`
}
