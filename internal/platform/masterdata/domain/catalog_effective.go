package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// EffectiveRuleVersion ghim vào mọi dòng sổ catalog_effective.
const EffectiveRuleVersion = "pf.masterdata.catalog_effective/v1"

var ErrValidFrom = errors.New("valid_from theo YYYY-MM-DD, trong khoảng 2000-01-01 … 2100-12-31")

// ValidateValidFrom kiểm ngày hiệu lực (ngày nghiệp vụ).
func ValidateValidFrom(d time.Time) error {
	if d.Before(Day(2000, 1, 1)) || d.After(Day(2100, 12, 31)) {
		return FieldError{Field: "valid_from", Err: ErrValidFrom}
	}
	return nil
}

// Version là một giá trị có hiệu lực từ ngày của mục danh mục (sổ catalog_effective).
type Version struct {
	ID         uuid.UUID
	Catalog    string
	Code       string
	ChangeID   uuid.UUID
	ValidFrom  time.Time // ngày nghiệp vụ
	ValidTo    *time.Time
	After      Snapshot
	EventTime  time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	Seq        int64
	RecordedAt time.Time
}

// Item dựng mục từ giá trị của phiên bản.
func (v Version) Item() Item {
	return Item{Catalog: v.Catalog, Code: v.Code, Name: v.After.Name, Attrs: v.After.Attrs, Active: v.After.Active, ValidFrom: v.ValidFrom}
}
