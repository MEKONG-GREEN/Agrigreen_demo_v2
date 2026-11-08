package port

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
)

// ReviewOperationRequest is the complete original intent. Tenant, actor and
// current authority come exclusively from the authenticated server context.
type ReviewOperationRequest struct {
	OperationID     uuid.UUID `json:"operation_id"`
	Profile         string    `json:"profile"`
	PeriodID        uuid.UUID `json:"period_id"`
	Unit            string    `json:"unit"`
	SubmissionID    uuid.UUID `json:"submission_id"`
	ExpectedVersion int64     `json:"expected_version"`
	Decision        string    `json:"decision"`
	Note            string    `json:"note"`
}

const (
	ReviewOperationApplied  = "applied"
	ReviewOperationRejected = "rejected"
	ReviewOperationUnknown  = "unknown"
)

var ErrReviewOperationUnavailable = problem.New(http.StatusServiceUnavailable, "forms.review_receipt_unavailable", "Chưa nối policy receipt rà soát")

// NormalizeReviewOperation is shared by execute/read/digest. No numeric string
// parser or client-authority input is introduced by this capability.
func NormalizeReviewOperation(in ReviewOperationRequest) (ReviewOperationRequest, error) {
	bad := func(field string) (ReviewOperationRequest, error) {
		return ReviewOperationRequest{}, problem.Validation.WithField(field, "giá trị không hợp lệ")
	}
	if in.OperationID == uuid.Nil || in.OperationID.Version() != 7 {
		return bad("operation_id")
	}
	if in.PeriodID == uuid.Nil || in.SubmissionID == uuid.Nil || in.ExpectedVersion < 1 {
		return bad("expected")
	}
	if !utf8.ValidString(in.Profile) || len(in.Profile) == 0 || len(in.Profile) > 64 || strings.TrimSpace(in.Profile) != in.Profile || strings.ContainsRune(in.Profile, 0) {
		return bad("profile")
	}
	if !utf8.ValidString(in.Unit) || len(in.Unit) > 200 || strings.ContainsRune(in.Unit, 0) {
		return bad("unit")
	}
	in.Unit = strings.TrimSpace(in.Unit)
	if in.Unit == "" {
		return bad("unit")
	}
	if !utf8.ValidString(in.Note) || len(in.Note) > 4000 || strings.ContainsRune(in.Note, 0) {
		return bad("note")
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > 1000 {
		return bad("note")
	}
	in.Decision = strings.TrimSpace(in.Decision)
	if in.Decision != "accepted" && in.Decision != "returned" {
		return bad("decision")
	}
	if in.Decision == "returned" && in.Note == "" {
		return bad("note")
	}
	return in, nil
}

// ReviewOperationDigest is a comparison value, never authority or proof.
func ReviewOperationDigest(in ReviewOperationRequest) (string, error) {
	req, err := NormalizeReviewOperation(in)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("forms.review_operation/v1\n"), raw...))
	return hex.EncodeToString(sum[:]), nil
}

// ReviewOperationResult is read from the exact inserted native review, inside
// the native transaction. Seq is that row's ledger revision, never head+1.
type ReviewOperationResult struct {
	ReviewID          uuid.UUID  `json:"review_id"`
	ReviewSeq         int64      `json:"review_seq"`
	Reviewer          uuid.UUID  `json:"reviewer"`
	ReviewedAt        time.Time  `json:"reviewed_at"`
	ReviewRecordedAt  time.Time  `json:"review_recorded_at"`
	WorkflowRequestID *uuid.UUID `json:"workflow_request_id"`
}

// ReviewOperationReceipt is immutable evidence only for its own operation.
// Unknown (including missing history) proves neither rollback nor permission
// to replace the operation ID. Notes and form payload are not duplicated here.
type ReviewOperationReceipt struct {
	OperationID     uuid.UUID             `json:"operation_id"`
	TenantID        uuid.UUID             `json:"tenant_id"`
	Profile         string                `json:"profile"`
	PeriodID        uuid.UUID             `json:"period_id"`
	Unit            string                `json:"unit"`
	SubmissionID    uuid.UUID             `json:"submission_id"`
	ExpectedVersion int64                 `json:"expected_version"`
	Decision        string                `json:"decision"`
	RequestDigest   string                `json:"request_digest"`
	ActorRef        uuid.UUID             `json:"actor_ref"`
	AuthorityHash   string                `json:"authority_hash"`
	Outcome         string                `json:"outcome"`
	RejectionCode   string                `json:"rejection_code"`
	Result          ReviewOperationResult `json:"result"`
	RecordedAt      time.Time             `json:"recorded_at"`
}
