package domain

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

// Danh mục Chăn nuôi – Thủy sản (W11.41, BRD CNTS v3.7 FN-06; pf 000146). Cùng cơ chế mục + thuộc tính của danh
// mục Trồng trọt; thêm sổ phụ hiệu lực theo ngày (catalog_effective, FN-06 BR-03).

// EffectiveRuleVersion ghim vào mọi dòng sổ catalog_effective.
const EffectiveRuleVersion = "pf.masterdata.catalog_effective/v1"

// Lỗi kiểm dữ liệu CNTS.
var (
	ErrDoses     = errors.New(`danh sách mũi dạng "<mã vaccine>/<lần>@<ngày tuổi>" ngăn cách dấu phẩy, 1–40 mũi, lần 1–20, ngày tuổi 0–3650`)
	ErrValidFrom = errors.New("valid_from theo YYYY-MM-DD, trong khoảng 2000-01-01 … 2100-12-31")
)

// ParseDoses đọc danh sách mũi "<vaccine>/<lần>@<ngày tuổi>,…" (vd "NEWCASTLE_F/1@3,GUMBORO/1@7").
func ParseDoses(s string) ([]port.Dose, error) {
	var out []port.Dose
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		vs, age, ok := strings.Cut(part, "@")
		if !ok {
			return nil, ErrDoses
		}
		v, shot, ok := strings.Cut(strings.TrimSpace(vs), "/")
		if !ok || !itemCodeRe.MatchString(strings.TrimSpace(v)) {
			return nil, ErrDoses
		}
		n, err1 := strconv.Atoi(strings.TrimSpace(shot))
		a, err2 := strconv.Atoi(strings.TrimSpace(age))
		if err1 != nil || err2 != nil || n < 1 || n > 20 || a < 0 || a > 3650 {
			return nil, ErrDoses
		}
		d := port.Dose{Vaccine: strings.TrimSpace(v), Shot: n, AgeDays: a}
		for _, o := range out {
			if o.Vaccine == d.Vaccine && o.Shot == d.Shot {
				return nil, fmt.Errorf("mũi %s lần %d bị lặp", d.Vaccine, d.Shot)
			}
		}
		out = append(out, d)
	}
	if len(out) == 0 || len(out) > 40 {
		return nil, ErrDoses
	}
	slices.SortStableFunc(out, func(a, b port.Dose) int {
		if a.AgeDays != b.AgeDays {
			return a.AgeDays - b.AgeDays
		}
		if c := strings.Compare(a.Vaccine, b.Vaccine); c != 0 {
			return c
		}
		return a.Shot - b.Shot
	})
	return out, nil
}

// FormatDoses ghi danh sách mũi về dạng chuẩn.
func FormatDoses(ds []port.Dose) string {
	parts := make([]string, len(ds))
	for i, d := range ds {
		parts[i] = d.Vaccine + "/" + strconv.Itoa(d.Shot) + "@" + strconv.Itoa(d.AgeDays)
	}
	return strings.Join(parts, ",")
}

// Stamps trả mọi mã mộc dấu (GS rồi GC) của cơ sở giết mổ.
func (it Item) Stamps() []string {
	return append(CodeList(it.Attrs[port.AttrStampsGS]), CodeList(it.Attrs[port.AttrStampsGC])...)
}

// ValidateValidFrom kiểm ngày hiệu lực (ngày nghiệp vụ).
func ValidateValidFrom(d time.Time) error {
	if d.Before(Day(2000, 1, 1)) || d.After(Day(2100, 12, 31)) {
		return FieldError{Field: "valid_from", Err: ErrValidFrom}
	}
	return nil
}

// Version là một giá trị có hiệu lực từ ngày của mục danh mục CNTS (sổ catalog_effective).
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

// ToFacility chuyển mục cơ sở giết mổ sang DTO công khai.
func ToFacility(it Item) port.SlaughterFacility {
	ref, _ := uuid.Parse(it.Attrs[port.AttrFacilityRef])
	return port.SlaughterFacility{
		Ref: ref, Code: it.Code, Name: it.Name, CommuneCode: it.Attrs[port.AttrCommuneCode], Kind: it.Attrs[port.AttrFacilityKind],
		StampsGS: CodeList(it.Attrs[port.AttrStampsGS]), StampsGC: CodeList(it.Attrs[port.AttrStampsGC]),
		Species: CodeList(it.Attrs[port.AttrSpecies]), Address: it.Attrs["address"], Active: it.Active, ValidFrom: it.ValidFrom,
	}
}
