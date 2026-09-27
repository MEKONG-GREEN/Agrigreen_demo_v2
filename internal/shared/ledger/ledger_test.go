package ledger

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func validMeta() Meta {
	return Meta{
		ID: ids.New(), TenantID: ids.New(), StreamKey: "lot:1", EventTime: time.Now(),
		ActorRef: ids.New(), IdemKey: "k-12345678", RuleVersion: "v1",
	}
}

func TestMetaValidate(t *testing.T) {
	if err := validMeta().Validate(); err != nil {
		t.Fatal(err)
	}
	m := validMeta()
	m.ID = uuid.New() // v4
	if m.Validate() == nil {
		t.Fatal("id v4 phải bị từ chối")
	}
	m = validMeta()
	reason := "cân lại do sai"
	m.CorrectionReason = &reason
	if m.Validate() == nil {
		t.Fatal("bản ghi bù thiếu supersedes_id phải bị từ chối")
	}
}

func TestClassify(t *testing.T) {
	dup := &pgconn.PgError{Code: "23505", ConstraintName: "ledger_idem_pkey"}
	if !errors.Is(Classify(dup), ErrDuplicate) {
		t.Fatal("23505 trên ledger_idem_pkey phải là ErrDuplicate")
	}
	other := &pgconn.PgError{Code: "23505", ConstraintName: "khac_pkey"}
	if errors.Is(Classify(other), ErrDuplicate) {
		t.Fatal("khóa khác không phải ErrDuplicate")
	}
	if !errors.Is(Classify(&pgconn.PgError{Code: "42501"}), ErrImmutable) {
		t.Fatal("42501 phải là ErrImmutable")
	}
}
