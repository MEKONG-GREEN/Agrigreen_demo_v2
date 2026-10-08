package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"regexp"
	"slices"
	"sort"
	"time"
	"unicode/utf8"
)

// ReorgDate là ngày sắp xếp đơn vị hành chính 2025 có hiệu lực: từ ngày này chỉ
// còn hai cấp tỉnh → xã, cấp huyện chấm dứt.
var ReorgDate = Day(2025, time.July, 1)

// GazetteerFormat là định dạng tệp danh mục đơn vị hành chính — dùng cho dữ liệu
// nhúng và cho Admin API nhập danh mục.
const GazetteerFormat = "agrigreen.admin_units/v1"

// MaxImportUnits giới hạn số đơn vị trong một lần nhập (thân request ≤ 1 MB).
const MaxImportUnits = 5000

var unitCodeRe = regexp.MustCompile(`^[0-9A-Z][0-9A-Z.-]{0,39}$`)

// Lỗi kiểm dữ liệu đơn vị hành chính.
var (
	ErrUnitCode       = errors.New("mã đơn vị phải là chữ hoa/số, 1–40 ký tự [0-9A-Z.-]")
	ErrUnitName       = errors.New("tên bắt buộc, tối đa 200 ký tự")
	ErrUnitLevel      = errors.New("level phải là province, commune hoặc district")
	ErrUnitParent     = errors.New("tỉnh không có đơn vị cha; xã/huyện bắt buộc có parent_code hợp lệ")
	ErrUnitValidity   = errors.New("valid_from bắt buộc (YYYY-MM-DD); valid_to phải sau valid_from")
	ErrUnitSuccessors = errors.New("successors chỉ khai khi đã có valid_to, mã hợp lệ, không trùng")
	ErrUnitDistrict   = errors.New("cấp huyện chấm dứt từ 01/07/2025: đơn vị district phải có valid_to ≤ 2025-07-01")
	ErrUnitDuplicate  = errors.New("trùng (code, valid_from) trong cùng tệp")
	ErrUnitConflict   = errors.New("phiên bản đã có với nội dung khác — chỉ được đóng hiệu lực phiên bản đang mở")
	ErrGazetteer      = errors.New("tệp phải theo định dạng " + GazetteerFormat + " và có 1–5000 đơn vị")
)

// Unit là một phiên bản hiệu lực của một đơn vị hành chính.
type Unit struct {
	Code       string
	Name       string
	Level      partyport.Level
	ParentCode string
	ValidFrom  time.Time  // ngày (Day)
	ValidTo    *time.Time // ngày, không bao gồm; nil = còn hiệu lực
	Successors []string
}

// ValidAt cho biết đơn vị có hiệu lực trong ngày day.
func (u Unit) ValidAt(day time.Time) bool {
	return !u.ValidFrom.After(day) && (u.ValidTo == nil || u.ValidTo.After(day))
}

// Validate kiểm các quy tắc không cần đọc DB.
func (u Unit) Validate() error {
	switch {
	case !unitCodeRe.MatchString(u.Code):
		return FieldError{"code", ErrUnitCode}
	case u.Name == "" || utf8.RuneCountInString(u.Name) > 200:
		return FieldError{"name", ErrUnitName}
	case u.Level != partyport.LevelProvince && u.Level != partyport.LevelCommune && u.Level != partyport.LevelDistrict:
		return FieldError{"level", ErrUnitLevel}
	case u.Level == partyport.LevelProvince && u.ParentCode != "",
		u.Level != partyport.LevelProvince && !unitCodeRe.MatchString(u.ParentCode),
		u.ParentCode == u.Code:
		return FieldError{"parent_code", ErrUnitParent}
	case u.ValidFrom.IsZero() || (u.ValidTo != nil && !u.ValidTo.After(u.ValidFrom)):
		return FieldError{"valid_to", ErrUnitValidity}
	case u.Level == partyport.LevelDistrict && (u.ValidTo == nil || u.ValidTo.After(ReorgDate)):
		return FieldError{"valid_to", ErrUnitDistrict}
	}
	if len(u.Successors) > 0 && u.ValidTo == nil {
		return FieldError{"successors", ErrUnitSuccessors}
	}
	seen := map[string]bool{}
	for _, s := range u.Successors {
		if !unitCodeRe.MatchString(s) || seen[s] {
			return FieldError{"successors", ErrUnitSuccessors}
		}
		seen[s] = true
	}
	return nil
}

// SameContent so phần bất biến của hai phiên bản (không gồm việc đóng hiệu lực).
func (u Unit) SameContent(o Unit) bool {
	return u.Code == o.Code && u.ValidFrom.Equal(o.ValidFrom) && u.Name == o.Name && u.Level == o.Level && u.ParentCode == o.ParentCode
}

// ImportAction là việc cần làm với một phiên bản khi nhập danh mục.
type ImportAction int

// Các việc.
const (
	ActionUnchanged ImportAction = iota
	ActionCreate
	ActionClose
)

// PlanImport quyết định việc với phiên bản in so với phiên bản đã có cùng
// (code, valid_from). Nhập lặp cùng tệp không đổi gì (idempotent). Chỉ cho phép
// đóng hiệu lực một phiên bản đang mở; mọi khác biệt khác là xung đột — lịch sử
// địa giới không bị sửa, muốn đổi thì đóng và mở phiên bản mới.
func PlanImport(existing *Unit, in Unit) (ImportAction, error) {
	if existing == nil {
		return ActionCreate, nil
	}
	if !existing.SameContent(in) {
		return 0, ErrUnitConflict
	}
	switch {
	case existing.ValidTo == nil && in.ValidTo == nil:
		return ActionUnchanged, nil
	case existing.ValidTo == nil:
		return ActionClose, nil
	case in.ValidTo != nil && existing.ValidTo.Equal(*in.ValidTo) && sameSet(existing.Successors, in.Successors):
		return ActionUnchanged, nil
	}
	return 0, ErrUnitConflict
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	sort.Strings(x)
	sort.Strings(y)
	return slices.Equal(x, y)
}

// FindFunc tìm một phiên bản của mã code: at = phiên bản có hiệu lực trong ngày
// day; latest = phiên bản mới nhất bắt đầu không muộn hơn day. nil nếu không có.
type FindFunc func(code string, day time.Time) (*Unit, error)

// maxSuccessorHops chặn vòng lặp khi dữ liệu kế thừa lỗi.
const maxSuccessorHops = 64

// ResolveCurrent đi theo chuỗi kế thừa từ mã code tới các mã có hiệu lực trong
// ngày day. Mã còn hiệu lực trả chính nó; mã đã sáp nhập trả mã kế thừa; mã bị
// chia tách trả nhiều mã. Mã bị bãi bỏ không có kế thừa trả port.ErrUnitNotFound.
func ResolveCurrent(code string, day time.Time, at, latest FindFunc) ([]string, error) {
	if u, err := at(code, day); err != nil {
		return nil, err
	} else if u != nil {
		return []string{u.Code}, nil
	}
	start, err := latest(code, day)
	if err != nil {
		return nil, err
	}
	if start == nil {
		return nil, partyport.ErrUnitNotFound
	}
	type key struct {
		code string
		from time.Time
	}
	seen := map[key]bool{}
	out := map[string]bool{}
	queue := []Unit{*start}
	for hops := 0; len(queue) > 0; hops++ {
		if hops > maxSuccessorHops {
			return nil, fmt.Errorf("masterdata: chuỗi kế thừa của %s quá dài hoặc có vòng", code)
		}
		u := queue[0]
		queue = queue[1:]
		k := key{u.Code, u.ValidFrom}
		if seen[k] {
			continue
		}
		seen[k] = true
		if u.ValidAt(day) {
			out[u.Code] = true
			continue
		}
		if u.ValidTo == nil || u.ValidTo.After(day) {
			continue // chưa có hiệu lực tại day
		}
		for _, s := range u.Successors {
			next, err := at(s, *u.ValidTo)
			if err != nil {
				return nil, err
			}
			if next != nil {
				queue = append(queue, *next)
			}
		}
	}
	if len(out) == 0 {
		return nil, partyport.ErrUnitNotFound
	}
	codes := make([]string, 0, len(out))
	for c := range out {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return codes, nil
}

// GazetteerFile là tệp danh mục đơn vị hành chính (định dạng GazetteerFormat).
type GazetteerFile struct {
	Format string          `json:"format"`
	Source string          `json:"source"`
	Note   string          `json:"note,omitempty"`
	Units  []GazetteerUnit `json:"units"`
}

// GazetteerUnit là một dòng của tệp danh mục.
type GazetteerUnit struct {
	Code       string   `json:"code"`
	Name       string   `json:"name"`
	Level      string   `json:"level"`
	ParentCode string   `json:"parent_code,omitempty"`
	ValidFrom  string   `json:"valid_from"`
	ValidTo    *string  `json:"valid_to,omitempty"`
	Successors []string `json:"successors,omitempty"`
}

// ParseGazetteer đọc và kiểm tệp danh mục. Trả nguồn dữ liệu và các phiên bản.
func ParseGazetteer(raw []byte) (string, []Unit, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f GazetteerFile
	if err := dec.Decode(&f); err != nil {
		return "", nil, FieldError{"body", ErrGazetteer}
	}
	if f.Format != GazetteerFormat || len(f.Units) == 0 || len(f.Units) > MaxImportUnits {
		return "", nil, FieldError{"format", ErrGazetteer}
	}
	src := f.Source
	if src == "" || utf8.RuneCountInString(src) > 300 {
		return "", nil, FieldError{"source", errors.New("source bắt buộc, tối đa 300 ký tự")}
	}
	units := make([]Unit, 0, len(f.Units))
	seen := map[string]bool{}
	for i, g := range f.Units {
		prefix := fmt.Sprintf("units[%d].", i)
		u := Unit{Code: g.Code, Name: g.Name, Level: partyport.Level(g.Level), ParentCode: g.ParentCode, Successors: g.Successors}
		from, err := ParseDay(g.ValidFrom)
		if err != nil {
			return "", nil, FieldError{prefix + "valid_from", ErrUnitValidity}
		}
		u.ValidFrom = from
		if g.ValidTo != nil {
			to, err := ParseDay(*g.ValidTo)
			if err != nil {
				return "", nil, FieldError{prefix + "valid_to", ErrUnitValidity}
			}
			u.ValidTo = &to
		}
		if err := u.Validate(); err != nil {
			var fe FieldError
			if errors.As(err, &fe) {
				return "", nil, FieldError{prefix + fe.Field, fe.Err}
			}
			return "", nil, err
		}
		k := u.Code + "@" + g.ValidFrom
		if seen[k] {
			return "", nil, FieldError{prefix + "code", ErrUnitDuplicate}
		}
		seen[k] = true
		units = append(units, u)
	}
	return src, units, nil
}
