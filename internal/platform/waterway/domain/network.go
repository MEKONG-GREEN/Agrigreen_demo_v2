package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Trạng thái job dựng.
const (
	JobQueued    = "queued"
	JobCompleted = "completed"
	JobFailed    = "failed"
)

// BuildJob là một yêu cầu dựng mạng (mẫu bất đồng bộ mục 12).
type BuildJob struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Status           string
	Params           BuildParams
	RequestedBy      uuid.UUID
	RequestedAt      time.Time
	IdemKey          string
	NetworkVersionID *uuid.UUID
	VersionNo        *int
	Reused           *bool
	ErrorCode        *string
	CompletedAt      *time.Time
}

// Trạng thái phiên bản mạng.
const (
	VersionBuilt      = "built"
	VersionPublished  = "published"
	VersionSuperseded = "superseded"
)

// NetworkVersion là một phiên bản mạng đã dựng.
type NetworkVersion struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	VersionNo        int
	Status           string
	SegmentSetSHA256 string
	GraphSHA256      string
	Params           BuildParams
	RuleVersion      string
	AsOf             time.Time
	SegmentCount     int
	StructureCount   int
	VertexCount      int
	EdgeCount        int
	ComponentCount   int
	FindingCount     int
	BuildJobID       uuid.UUID
	BuiltBy          uuid.UUID
	BuiltAt          time.Time
	PublishedBy      *uuid.UUID
	PublishedAt      *time.Time
	EffectiveFrom    *time.Time
	SupersededAt     *time.Time
	SupersededBy     *uuid.UUID
}

// Routable cho biết phiên bản dùng được cho định tuyến ghim số (đã từng phát hành).
func (v NetworkVersion) Routable() bool {
	return v.Status == VersionPublished || v.Status == VersionSuperseded
}

// VersionSegment là một đoạn trong phiên bản (lớp bản đồ GIS US-WATER-01).
type VersionSegment struct {
	SegmentRef   uuid.UUID
	RevisionID   uuid.UUID
	Revision     int
	Name         string
	Class        string
	DataSource   string
	LengthM      decimal.Decimal
	WidthM       *decimal.Decimal
	DepthM       *decimal.Decimal
	DeclaredTons *decimal.Decimal
	MaxLoadTons  *decimal.Decimal
	Certain      bool
	Limiting     string
	StructureRef *uuid.UUID
	EdgeCount    int
	Points       []Point
}

// Passable: đoạn cho phương tiện tải trọng minLoad đi qua (chưa kết luận → không).
// minLoad = 0 → mọi đoạn.
func (s VersionSegment) Passable(minLoad decimal.Decimal) bool {
	if !minLoad.IsPositive() {
		return true
	}
	return s.MaxLoadTons != nil && !s.MaxLoadTons.LessThan(minLoad)
}
