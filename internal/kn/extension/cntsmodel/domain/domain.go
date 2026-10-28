// Package domain: quy tắc thuần của chương trình/dự án, điểm trình diễn, hộ mô hình và cấp quyền cán bộ xã/nông hộ
// của phòng Chăn nuôi – Thủy sản — W11.47, D4 CN7, BRD CNTS v3.7 FN-10 (phần hộ), FN-14, FN-19 (phần CT/DA), UR-XA-04,
// UR-TINH-09, UR-TINH-19. Không phụ thuộc hạ tầng, không time.Now() (R-12).
//
// ADR-0055, Q-KNS-04: mọi bản ghi ở đây là của KN; HTX chỉ được tham chiếu bằng mã (cooperative_ref).
package domain

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// RuleVersion là phiên bản quy tắc ghi vào sổ.
const RuleVersion = "cnts-model/1"

// Loại CT/DA (UR-TINH-19): khuyến nông tỉnh, khuyến nông quốc gia, hợp tác.
const (
	KindProvince = "tinh"
	KindNational = "knqg"
	KindCoop     = "hop_tac"
)

// Sự việc của sổ.
const (
	EventCreated    = "created"
	EventUpdated    = "updated"
	EventClosed     = "closed"
	EventReopened   = "reopened"
	EventWithdrawn  = "withdrawn"
	EventReinstated = "reinstated"
	EventGranted    = "granted"
	EventRevoked    = "revoked"
)

// Trạng thái hiển thị.
const (
	StatusActive    = "active"
	StatusClosed    = "closed"
	StatusWithdrawn = "withdrawn"
	StatusPending   = "pending" // quyền chưa tới ngày hiệu lực
	StatusExpired   = "expired"
	StatusRevoked   = "revoked"
)

// Người ghi nhật ký của hộ mô hình.
const (
	KeeperOfficer = "officer"
	KeeperSelf    = "self"
)

// Vai trong module CNTS cấp ở FN-14 (UR-TINH-09): cán bộ tăng cường, viên chức trực KSGM, cán bộ theo dõi mô hình,
// nông hộ. BR-01: chỉ vai Xã, Nông hộ.
const (
	RoleTangCuong = "tang_cuong"
	RoleKSGM      = "ksgm"
	RoleFollower  = "theo_doi_mo_hinh"
	RoleFarmer    = "nong_ho"
)

// Roles là các vai được cấp.
var Roles = []string{RoleTangCuong, RoleKSGM, RoleFollower, RoleFarmer}

// ScaleUnits là đơn vị quy mô hợp lệ.
var ScaleUnits = []string{"con", "m2", "m3", "ha", "long", "be"}

// DiaryForms là mẫu nhật ký của điểm (BM-08 gà, BM-09 vịt xiêm, BM-19 thủy sản).
var DiaryForms = []string{"BM-08", "BM-09", "BM-19"}

// Giới hạn.
const (
	MaxName        = 300
	MaxText        = 1000
	MaxLongText    = 2000
	MaxAttachments = 20
	MaxTargets     = 200
	MaxForms       = 30
	MinReason      = 5
)

var (
	codeRe    = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,40}$`)
	unitRe    = regexp.MustCompile(`^[A-Za-z0-9._-]{1,20}$`)
	formRe    = regexp.MustCompile(`^BM-(0[1-9]|1[0-9])$`)
	maxBudget = decimal.New(1, 15) // 10^15 đồng
)

// FieldErr là lỗi của một trường.
type FieldErr struct {
	Field  string
	Reason string
}

// Errors là danh sách lỗi trường (422).
type Errors []FieldErr

func (e Errors) Error() string {
	parts := make([]string, 0, len(e))
	for _, x := range e {
		parts = append(parts, x.Field+": "+x.Reason)
	}
	return strings.Join(parts, "; ")
}

func (e *Errors) add(field, reason string) { *e = append(*e, FieldErr{Field: field, Reason: reason}) }

func textOK(s string, max int) bool { return utf8.RuneCountInString(s) <= max }

func (e *Errors) optText(field string, s *string, max int) {
	*s = strings.TrimSpace(*s)
	if !textOK(*s, max) {
		e.add(field, "tối đa "+strconv.Itoa(max)+" ký tự")
	}
}

// ValidUnitCode kiểm dạng mã đơn vị hành chính.
func ValidUnitCode(s string) bool { return unitRe.MatchString(s) }

// ---------------------------------------------------------------------------
// Chương trình, dự án (UR-TINH-19)
// ---------------------------------------------------------------------------

// Project là các trường nghiệp vụ của một phiên bản CT/DA.
type Project struct {
	Code         string
	Name         string
	Kind         string
	Basis        string
	ScaleNote    string
	Start        time.Time
	End          *time.Time
	Budget       *decimal.Decimal
	ManagerRef   uuid.UUID
	ProgramRef   *uuid.UUID
	TrainingNote string
	SuppliesNote string
	Attachments  []uuid.UUID
}

// Normalize chuẩn hóa và kiểm CT/DA.
func (p *Project) Normalize() Errors {
	var e Errors
	p.Code = strings.ToUpper(strings.TrimSpace(p.Code))
	if !codeRe.MatchString(p.Code) {
		e.add("code", "1–40 ký tự chữ, số, . _ / -")
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || !textOK(p.Name, MaxName) {
		e.add("name", "bắt buộc, tối đa 300 ký tự")
	}
	if !slices.Contains([]string{KindProvince, KindNational, KindCoop}, p.Kind) {
		e.add("kind", "tinh, knqg hoặc hop_tac")
	}
	e.optText("basis", &p.Basis, MaxText)
	e.optText("scale_note", &p.ScaleNote, MaxText)
	e.optText("training_note", &p.TrainingNote, MaxLongText)
	e.optText("supplies_note", &p.SuppliesNote, MaxLongText)
	if p.Start.IsZero() {
		e.add("start_date", "bắt buộc")
	}
	if p.End != nil && !p.Start.IsZero() && p.End.Before(p.Start) {
		e.add("end_date", "không trước ngày bắt đầu")
	}
	if p.Budget != nil && (p.Budget.IsNegative() || !p.Budget.Equal(p.Budget.Truncate(0)) || p.Budget.GreaterThanOrEqual(maxBudget)) {
		e.add("budget", "số đồng nguyên, không âm")
	}
	if p.ManagerRef == uuid.Nil {
		e.add("manager_ref", "bắt buộc (chủ nhiệm)")
	}
	if len(p.Attachments) > MaxAttachments {
		e.add("attachment_refs", "tối đa 20 tệp")
	}
	p.Attachments = uniqRefs(p.Attachments)
	return e
}

// ---------------------------------------------------------------------------
// Điểm trình diễn
// ---------------------------------------------------------------------------

// Point là tọa độ WGS84.
type Point struct {
	Lat, Lng float64
	Source   string // gps | map
}

// Validate kiểm tọa độ (trong lãnh thổ Việt Nam mở rộng).
func (p Point) Validate(field string) Errors {
	var e Errors
	if p.Lat < 5 || p.Lat > 25 || p.Lng < 100 || p.Lng > 118 {
		e.add(field, "tọa độ ngoài phạm vi Việt Nam")
	}
	if p.Source != "gps" && p.Source != "map" {
		e.add(field+".source", "gps hoặc map")
	}
	return e
}

// Scale là quy mô có đơn vị.
type Scale struct {
	Value *decimal.Decimal
	Unit  string
}

func (e *Errors) scale(s *Scale) {
	s.Unit = strings.TrimSpace(s.Unit)
	switch {
	case s.Value == nil && s.Unit == "":
	case s.Value == nil || s.Unit == "":
		e.add("scale", "nhập cả quy mô và đơn vị")
	case !s.Value.IsPositive() || s.Value.GreaterThanOrEqual(decimal.New(1, 12)):
		e.add("scale", "phải dương")
	case !slices.Contains(ScaleUnits, s.Unit):
		e.add("scale_unit", "con, m2, m3, ha, long hoặc be")
	default:
		v := s.Value.Round(2)
		s.Value = &v
	}
}

// Site là các trường nghiệp vụ của điểm trình diễn.
type Site struct {
	CommuneCode    string
	Name           string
	ModelType      string
	Scale          Scale
	OfficerRef     *uuid.UUID
	DiaryForm      string
	CooperativeRef *uuid.UUID
	Note           string
}

// Normalize chuẩn hóa và kiểm điểm.
func (s *Site) Normalize() Errors {
	var e Errors
	s.CommuneCode = strings.TrimSpace(s.CommuneCode)
	if !unitRe.MatchString(s.CommuneCode) {
		e.add("commune_code", "bắt buộc")
	}
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" || !textOK(s.Name, MaxName) {
		e.add("name", "bắt buộc, tối đa 300 ký tự")
	}
	s.ModelType = strings.TrimSpace(s.ModelType)
	if s.ModelType == "" || len(s.ModelType) > 64 {
		e.add("model_type_code", "bắt buộc (danh mục loại mô hình CNTS)")
	}
	e.scale(&s.Scale)
	s.DiaryForm = strings.ToUpper(strings.TrimSpace(s.DiaryForm))
	if s.DiaryForm != "" && !slices.Contains(DiaryForms, s.DiaryForm) {
		e.add("diary_form", "BM-08, BM-09 hoặc BM-19")
	}
	if s.OfficerRef != nil && *s.OfficerRef == uuid.Nil {
		s.OfficerRef = nil
	}
	if s.CooperativeRef != nil && *s.CooperativeRef == uuid.Nil {
		s.CooperativeRef = nil
	}
	e.optText("note", &s.Note, MaxText)
	return e
}

// ---------------------------------------------------------------------------
// Hộ mô hình (UR-XA-04)
// ---------------------------------------------------------------------------

// Household là các trường nghiệp vụ của hộ mô hình (không có dữ liệu cá nhân — ở kho định danh).
type Household struct {
	Scale      Scale
	KeeperKind string
	KeeperRef  *uuid.UUID
	SelfEntry  bool
	JoinedOn   *time.Time
	Note       string
}

// Normalize chuẩn hóa và kiểm hộ.
func (h *Household) Normalize() Errors {
	var e Errors
	e.scale(&h.Scale)
	if h.KeeperKind == "" {
		h.KeeperKind = KeeperOfficer
	}
	switch h.KeeperKind {
	case KeeperSelf:
		h.KeeperRef = nil
	case KeeperOfficer:
		if h.KeeperRef == nil || *h.KeeperRef == uuid.Nil {
			e.add("keeper_ref", "chọn cán bộ ghi nhật ký (hoặc chọn hộ tự ghi)")
		}
	default:
		e.add("keeper_kind", "officer hoặc self")
	}
	e.optText("note", &h.Note, MaxText)
	return e
}

// Owner là thông tin chủ hộ gửi vào kho định danh (chỉ tồn tại trong bộ nhớ).
type Owner struct {
	FullName string `pii:"true"`
	Phone    string `pii:"true"`
}

// Validate kiểm thông tin chủ hộ (định dạng SĐT do kho kiểm).
func (o *Owner) Validate() Errors {
	var e Errors
	o.FullName, o.Phone = strings.TrimSpace(o.FullName), strings.TrimSpace(o.Phone)
	if o.FullName == "" || !textOK(o.FullName, 200) {
		e.add("owner_name", "bắt buộc, tối đa 200 ký tự")
	}
	if !textOK(o.Phone, 20) {
		e.add("owner_phone", "tối đa 20 ký tự")
	}
	return e
}

// ---------------------------------------------------------------------------
// Cấp quyền (FN-14)
// ---------------------------------------------------------------------------

// Grant là phạm vi của một lần cấp quyền.
type Grant struct {
	Role       string
	Communes   []string
	Facilities []uuid.UUID
	Sites      []uuid.UUID
	Households []uuid.UUID
	Forms      []string
	ValidFrom  time.Time
	ValidTo    *time.Time
}

// Normalize chuẩn hóa và kiểm phạm vi theo vai.
func (g *Grant) Normalize() Errors {
	var e Errors
	if !slices.Contains(Roles, g.Role) {
		e.add("role", "tang_cuong, ksgm, theo_doi_mo_hinh hoặc nong_ho (BR-01: chỉ vai Xã, Nông hộ trong module CNTS)")
	}
	g.Communes = uniqStrings(g.Communes)
	for _, c := range g.Communes {
		if !unitRe.MatchString(c) {
			e.add("commune_codes", "mã xã không hợp lệ: "+c)
			break
		}
	}
	if len(g.Communes) == 0 {
		e.add("commune_codes", "chọn ít nhất một xã")
	}
	g.Facilities, g.Sites, g.Households = uniqRefs(g.Facilities), uniqRefs(g.Sites), uniqRefs(g.Households)
	if len(g.Communes) > MaxTargets || len(g.Facilities) > MaxTargets || len(g.Sites) > MaxTargets || len(g.Households) > MaxTargets {
		e.add("commune_codes", "tối đa 200 mục mỗi loại địa bàn")
	}
	switch g.Role {
	case RoleKSGM, RoleTangCuong:
		if len(g.Sites) > 0 || len(g.Households) > 0 {
			e.add("site_refs", "vai này giao theo xã/cơ sở giết mổ, không theo điểm/hộ")
		}
	case RoleFollower:
		if len(g.Facilities) > 0 {
			e.add("facility_refs", "cán bộ theo dõi mô hình không giao cơ sở giết mổ")
		}
		if len(g.Sites) == 0 && len(g.Households) == 0 {
			e.add("site_refs", "chọn điểm trình diễn hoặc hộ mô hình được theo dõi")
		}
	case RoleFarmer:
		if len(g.Facilities) > 0 || len(g.Sites) > 0 {
			e.add("facility_refs", "nông hộ chỉ được giao hộ của mình")
		}
		if len(g.Households) != 1 {
			e.add("household_refs", "nông hộ được giao đúng một hộ mô hình")
		}
	}
	g.Forms = uniqStrings(g.Forms)
	for i, f := range g.Forms {
		f = strings.ToUpper(f)
		g.Forms[i] = f
		if !formRe.MatchString(f) {
			e.add("forms", "mã biểu BM-01…BM-19: "+f)
			break
		}
	}
	if len(g.Forms) > MaxForms {
		e.add("forms", "tối đa 30 biểu")
	}
	slices.Sort(g.Forms)
	if g.ValidFrom.IsZero() {
		e.add("valid_from", "bắt buộc")
	}
	if g.ValidTo != nil && g.ValidTo.Before(g.ValidFrom) {
		e.add("valid_to", "không trước ngày hiệu lực")
	}
	return e
}

// GrantStatus là trạng thái của quyền tại ngày today.
func GrantStatus(event string, from time.Time, to *time.Time, today time.Time) string {
	switch {
	case event == EventRevoked:
		return StatusRevoked
	case today.Before(from):
		return StatusPending
	case to != nil && today.After(*to):
		return StatusExpired
	}
	return StatusActive
}

// Covers cho biết quyền (đang hiệu lực) bao điểm site / hộ household. Quyền chỉ theo xã (không có điểm/hộ) bao mọi
// điểm/hộ của các xã được giao.
func (g Grant) Covers(commune string, site uuid.UUID, household uuid.UUID) bool {
	if !slices.Contains(g.Communes, commune) {
		return false
	}
	if len(g.Sites) == 0 && len(g.Households) == 0 {
		return true
	}
	return (site != uuid.Nil && slices.Contains(g.Sites, site)) || (household != uuid.Nil && slices.Contains(g.Households, household))
}

// ValidReason kiểm lý do đóng/rút/thu hồi.
func ValidReason(s string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(s))
	return n >= MinReason && n <= MaxText
}

func uniqStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}

func uniqRefs(in []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(in))
	for _, r := range in {
		if r != uuid.Nil && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return out
}
