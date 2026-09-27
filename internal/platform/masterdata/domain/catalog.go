package domain

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

// CatalogRuleVersion ghim vào mọi dòng sổ catalog_change.
const CatalogRuleVersion = "pf.masterdata.catalog/v1"

// CatalogFormat là định dạng tệp danh mục nhúng.
const CatalogFormat = "agrigreen.catalogs/v1"

// Catalogs là các danh mục module quản lý.
var Catalogs = []string{port.CatalogRiceVariety, port.CatalogFarmStage, port.CatalogMachineType, port.CatalogSeason, port.CatalogPest}

// Hành động ghi vào sổ lịch sử danh mục.
const (
	ActionCreated     = "created"
	ActionUpdated     = "updated"
	ActionDeactivated = "deactivated"
	ActionReactivated = "reactivated"
)

// MinReasonLen là độ dài tối thiểu của lý do ngừng dùng (giống correction_reason).
const MinReasonLen = 10

var itemCodeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$`)

// Lỗi kiểm dữ liệu danh mục.
var (
	ErrCatalogUnknown = errors.New("danh mục không tồn tại; dùng rice_variety, farm_stage, machine_type, season hoặc pest")
	ErrItemCode       = errors.New("mã mục 1–40 ký tự [A-Za-z0-9_.-]")
	ErrItemName       = errors.New("tên bắt buộc, tối đa 200 ký tự")
	ErrReason         = fmt.Errorf("lý do bắt buộc, %d–500 ký tự", MinReasonLen)
)

// KnownCatalog cho biết c là danh mục module quản lý.
func KnownCatalog(c string) bool { return slices.Contains(Catalogs, c) }

// Item là một mục danh mục (bảng hiện trạng).
type Item struct {
	Catalog            string
	Code               string
	Name               string
	Attrs              map[string]string
	Active             bool
	DeactivationReason string
	Revision           int
	UpdatedBy          uuid.UUID
	UpdatedAt          time.Time
}

// Snapshot là trạng thái của mục ghi vào sổ lịch sử (trước → sau).
type Snapshot struct {
	Name               string            `json:"name"`
	Attrs              map[string]string `json:"attrs"`
	Active             bool              `json:"active"`
	DeactivationReason *string           `json:"deactivation_reason,omitempty"`
}

// Snapshot chụp trạng thái hiện tại.
func (it Item) Snapshot() Snapshot {
	s := Snapshot{Name: it.Name, Attrs: it.Attrs, Active: it.Active}
	if it.DeactivationReason != "" {
		r := it.DeactivationReason
		s.DeactivationReason = &r
	}
	return s
}

// Change là một dòng sổ lịch sử danh mục.
type Change struct {
	ID         uuid.UUID
	Catalog    string
	Code       string
	Action     string
	Before     *Snapshot
	After      Snapshot
	Reason     string
	EventTime  time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	Seq        int64
	RecordedAt time.Time
}

// CatalogStreamKey: mỗi mục danh mục một chuỗi băm.
func CatalogStreamKey(catalog, code string) string { return "catalog:" + catalog + ":" + code }

// ValidateReason kiểm lý do ngừng dùng.
func ValidateReason(r string) error {
	n := utf8.RuneCountInString(strings.TrimSpace(r))
	if n < MinReasonLen || n > 500 {
		return FieldError{"reason", ErrReason}
	}
	return nil
}

type attrSpec struct {
	required bool
	check    func(string) (string, error) // trả giá trị đã chuẩn hóa
}

func intRange(lo, hi int) func(string) (string, error) {
	return func(s string) (string, error) {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < lo || n > hi {
			return "", fmt.Errorf("phải là số nguyên %d–%d", lo, hi)
		}
		return strconv.Itoa(n), nil
	}
}

func oneOf(vals ...string) func(string) (string, error) {
	return func(s string) (string, error) {
		if !slices.Contains(vals, s) {
			return "", fmt.Errorf("phải là một trong %s", strings.Join(vals, ", "))
		}
		return s, nil
	}
}

func text(max int) func(string) (string, error) {
	return func(s string) (string, error) {
		if utf8.RuneCountInString(s) > max {
			return "", fmt.Errorf("tối đa %d ký tự", max)
		}
		return s, nil
	}
}

// yield: năng suất kỳ vọng (tấn/ha) là decimal > 0, ≤ 30, tối đa 4 chữ số thập phân.
func yield(s string) (string, error) {
	d, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil || !d.IsPositive() || d.GreaterThan(decimal.NewFromInt(30)) || d.Exponent() < -4 {
		return "", errors.New("phải là số thập phân > 0 và ≤ 30 (tấn/ha), tối đa 4 chữ số lẻ")
	}
	return d.String(), nil
}

func stageRef(s string) (string, error) {
	if !itemCodeRe.MatchString(s) {
		return "", errors.New("phải là mã khâu sản xuất")
	}
	return s, nil
}

var attrSpecs = map[string]map[string]attrSpec{
	port.CatalogRiceVariety: {
		"growth_days":             {true, intRange(1, 365)},
		"expected_yield_t_per_ha": {true, yield},
		"description":             {false, text(500)},
	},
	port.CatalogFarmStage: {
		"pilot":       {true, oneOf("true", "false")}, // cờ thí điểm CGH (BR-06)
		"description": {false, text(500)},
	},
	port.CatalogMachineType: {
		"farm_stage":  {true, stageRef}, // khâu tự gán theo chủng loại (CGH BR-02)
		"description": {false, text(500)},
	},
	port.CatalogSeason: {
		"start_month": {false, intRange(1, 12)},
		"end_month":   {false, intRange(1, 12)},
		"description": {false, text(500)},
	},
	port.CatalogPest: {
		"kind":        {true, oneOf("sau_hai", "benh_hai", "dong_vat_hai", "co_dai")},
		"description": {false, text(500)},
	},
}

// Normalize kiểm và chuẩn hóa mục trước khi ghi: mã, tên, thuộc tính theo từng
// danh mục (thiếu thuộc tính bắt buộc hoặc có thuộc tính lạ đều bị từ chối).
func (it Item) Normalize() (Item, error) {
	if !KnownCatalog(it.Catalog) {
		return it, FieldError{"catalog", ErrCatalogUnknown}
	}
	if !itemCodeRe.MatchString(it.Code) {
		return it, FieldError{"code", ErrItemCode}
	}
	it.Name = strings.TrimSpace(it.Name)
	if it.Name == "" || utf8.RuneCountInString(it.Name) > 200 {
		return it, FieldError{"name", ErrItemName}
	}
	spec := attrSpecs[it.Catalog]
	out := make(map[string]string, len(it.Attrs))
	keys := make([]string, 0, len(it.Attrs))
	for k := range it.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s, ok := spec[k]
		if !ok {
			return it, FieldError{"attrs." + k, errors.New("thuộc tính không có trong danh mục " + it.Catalog)}
		}
		v, err := s.check(it.Attrs[k])
		if err != nil {
			return it, FieldError{"attrs." + k, err}
		}
		out[k] = v
	}
	for k, s := range spec {
		if _, ok := out[k]; s.required && !ok {
			return it, FieldError{"attrs." + k, errors.New("bắt buộc")}
		}
	}
	if it.Catalog == port.CatalogSeason {
		_, a := out["start_month"]
		_, b := out["end_month"]
		if a != b {
			return it, FieldError{"attrs.end_month", errors.New("start_month và end_month đi cùng nhau")}
		}
	}
	it.Attrs = out
	return it, nil
}

// SameContent cho biết hai mục có cùng tên và thuộc tính.
func (it Item) SameContent(o Item) bool {
	if it.Name != o.Name || len(it.Attrs) != len(o.Attrs) {
		return false
	}
	for k, v := range it.Attrs {
		if o.Attrs[k] != v {
			return false
		}
	}
	return true
}
