package domain

import (
	"time"

	"github.com/google/uuid"
)

// Trạng thái xung đột.
const (
	ConflictOpen     = "open"
	ConflictResolved = "resolved"
)

// Conflict là một mục trong sổ đăng ký xung đột (C-8): máy chủ giữ cả hai bản
// ghi, đơn vị phụ trách xử lý trước DueAt.
type Conflict struct {
	TenantID         uuid.UUID
	ID               uuid.UUID
	Entity           string
	FirstRecordID    uuid.UUID
	FirstIdemKey     string
	DeviceID         uuid.UUID
	ReportedBy       uuid.UUID
	OwnerOrgRef      *uuid.UUID
	OpenedAt         time.Time
	DueAt            time.Time
	Status           string
	ResolvedAt       *time.Time
	ResolvedBy       *uuid.UUID
	ResolutionNote   *string
	ResolvedRecordID *uuid.UUID
}

// Overdue cho biết xung đột quá SLA tại thời điểm at (đang mở và qua DueAt, hoặc
// đã đóng sau DueAt).
func (c Conflict) Overdue(at time.Time) bool {
	if c.Status == ConflictResolved && c.ResolvedAt != nil {
		return c.ResolvedAt.After(c.DueAt)
	}
	return at.After(c.DueAt)
}
