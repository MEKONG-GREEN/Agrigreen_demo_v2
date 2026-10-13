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

// CNTSCatalogs là các danh mục CNTS (thứ tự tab màn "Danh mục CNTS").
var CNTSCatalogs = []string{
	port.CatalogCNTSSpecies, port.CatalogCNTSBreed, port.CatalogCNTSVaccine, port.CatalogCNTSVaccinationSchedule,
	port.CatalogCNTSDisease, port.CatalogCNTSChemical, port.CatalogCNTSSlaughterFacility, port.CatalogCNTSAquacultureObject,
	port.CatalogCNTSFishingGroup, port.CatalogCNTSIndicator, port.CatalogCNTSModelType, port.CatalogCNTSDocumentType,
}

// SyncCatalogs là các danh mục đồng bộ xuống thiết bị (entity catalog_item). W11.42: thêm 12 danh mục CNTS (field-app
// Cấp Xã nhập biểu kỳ CNTS ngoại tuyến cần loài, vaccine, cơ sở giết mổ…) — cùng hợp đồng catalog_item (giá trị hiện
// hành, không theo hiệu lực ngày).
var SyncCatalogs = Catalogs

// EffectiveDated cho biết danh mục lưu giá trị theo hiệu lực từ ngày (mọi danh mục CNTS).
func EffectiveDated(catalog string) bool { return slices.Contains(CNTSCatalogs, catalog) }

// Lỗi kiểm dữ liệu CNTS.
var (
	ErrDoses     = errors.New(`danh sách mũi dạng "<mã vaccine>/<lần>@<ngày tuổi>" ngăn cách dấu phẩy, 1–40 mũi, lần 1–20, ngày tuổi 0–3650`)
	ErrStampCode = errors.New("mã mộc dấu dạng số và dấu chấm (vd 91.589.05.1), tối đa 40 ký tự")
	ErrValidFrom = errors.New("valid_from theo YYYY-MM-DD, trong khoảng 2000-01-01 … 2100-12-31")
)

// stampList: danh sách mã mộc dấu (chữ số, chữ cái, dấu chấm, gạch), chuẩn hóa bỏ trùng, giữ thứ tự nhập; tối đa 20.
func stampList(s string) (string, error) {
	var out []string
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !itemCodeRe.MatchString(c) {
			return "", ErrStampCode
		}
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	if len(out) > 20 {
		return "", errors.New("tối đa 20 mã mộc dấu mỗi loại")
	}
	return strings.Join(out, ","), nil
}

func uuidAttr(s string) (string, error) {
	u, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil || u == uuid.Nil {
		return "", errors.New("phải là UUID")
	}
	return u.String(), nil
}

func boolAttr(s string) (string, error) { return oneOf("true", "false")(strings.TrimSpace(s)) }

func communeCode(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !itemCodeRe.MatchString(s) {
		return "", errors.New("phải là mã xã/phường/đặc khu")
	}
	return s, nil
}

// dosesAttr kiểm và chuẩn hóa danh sách mũi (sắp theo ngày tuổi, rồi mã vaccine, rồi lần).
func dosesAttr(s string) (string, error) {
	ds, err := ParseDoses(s)
	if err != nil {
		return "", err
	}
	return FormatDoses(ds), nil
}

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

func init() {
	desc := attrSpec{false, text(500)}
	order := attrSpec{false, intRange(0, 999)}
	cnts := map[string]map[string]attrSpec{
		port.CatalogCNTSSpecies: {
			"group":       {true, oneOf("gia_suc", "gia_cam", "thu_cung", "yen", "thuy_san", "khac")},
			"unit":        {true, nonEmpty(20)},
			"herd_column": {false, boolAttr},
			"order":       order,
			"description": desc,
		},
		port.CatalogCNTSBreed: {
			port.AttrSpecies: {true, stageRefNamed("mã loài")},
			"description":    desc,
		},
		port.CatalogCNTSDisease: {
			port.AttrSpecies: {false, codeList},
			"notifiable":     {false, boolAttr},
			"description":    desc,
		},
		port.CatalogCNTSVaccine: {
			"diseases":       {false, codeList},
			port.AttrSpecies: {true, nonEmptyCodeList},
			"report_group":   {true, oneOf("heo", "bo", "cum_gia_cam", "dai", "khac")},
			"order":          order,
			"description":    desc,
		},
		port.CatalogCNTSChemical: {
			"unit":        {true, nonEmpty(20)},
			"target":      {false, oneOf("gia_suc", "gia_cam", "chung")},
			"description": desc,
		},
		port.CatalogCNTSSlaughterFacility: {
			port.AttrFacilityRef:  {true, uuidAttr},
			port.AttrCommuneCode:  {true, communeCode},
			port.AttrFacilityKind: {true, oneOf(port.FacilityConcentrated, port.FacilitySmall)},
			port.AttrStampsGS:     {false, stampList},
			port.AttrStampsGC:     {false, stampList},
			port.AttrSpecies:      {false, codeList},
			"address":             {false, text(300)},
			"description":         desc,
		},
		port.CatalogCNTSAquacultureObject: {
			"scale_unit":  {true, oneOf("ha", "long", "veo", "m3")},
			"description": desc,
		},
		port.CatalogCNTSFishingGroup: {
			"unit":        {false, text(20)},
			"order":       order,
			"description": desc,
		},
		port.CatalogCNTSIndicator: {
			"unit":        {false, text(20)},
			"value_type":  {true, oneOf("decimal", "integer", "percent")},
			"description": desc,
		},
		port.CatalogCNTSModelType: {
			"sector":         {true, oneOf("chan_nuoi", "thuy_san")},
			port.AttrSpecies: {false, codeList},
			"description":    desc,
		},
		port.CatalogCNTSDocumentType: {
			"description": desc,
		},
		port.CatalogCNTSVaccinationSchedule: {
			"model_type":   {false, stageRefNamed("mã loại mô hình")},
			"breed":        {false, stageRefNamed("mã giống")},
			port.AttrDoses: {true, dosesAttr},
			"description":  desc,
		},
	}
	for c, spec := range cnts {
		attrSpecs[c] = spec
	}
	names := map[string]string{
		port.CatalogCNTSSpecies:             "Vật nuôi",
		port.CatalogCNTSBreed:               "Giống vật nuôi",
		port.CatalogCNTSVaccine:             "Vaccine",
		port.CatalogCNTSVaccinationSchedule: "Quy trình tiêm",
		port.CatalogCNTSDisease:             "Bệnh",
		port.CatalogCNTSChemical:            "Hóa chất",
		port.CatalogCNTSSlaughterFacility:   "Cơ sở giết mổ",
		port.CatalogCNTSAquacultureObject:   "Đối tượng nuôi thủy sản",
		port.CatalogCNTSFishingGroup:        "Nhóm khai thác",
		port.CatalogCNTSIndicator:           "Chỉ tiêu theo dõi",
		port.CatalogCNTSModelType:           "Loại mô hình",
		port.CatalogCNTSDocumentType:        "Loại văn bản",
	}
	for c, n := range names {
		catalogNames[c] = n
	}
}

// nonEmptyCodeList: danh sách mã bắt buộc có ít nhất một mã.
func nonEmptyCodeList(s string) (string, error) {
	v, err := codeList(s)
	if err == nil && v == "" {
		return "", errors.New("bắt buộc ít nhất một mã")
	}
	return v, err
}

// normalizeCNTS kiểm ràng buộc liên thuộc tính của danh mục CNTS (sau khi đã kiểm từng thuộc tính).
func normalizeCNTS(it Item) error {
	switch it.Catalog {
	case port.CatalogCNTSVaccinationSchedule:
		if it.Attrs["model_type"] == "" && it.Attrs["breed"] == "" {
			return FieldError{"attrs.model_type", errors.New("quy trình tiêm gắn loại mô hình hoặc giống")}
		}
	case port.CatalogCNTSSlaughterFacility:
		gs, gc := CodeList(it.Attrs[port.AttrStampsGS]), CodeList(it.Attrs[port.AttrStampsGC])
		for _, c := range gs {
			if slices.Contains(gc, c) {
				return FieldError{"attrs." + port.AttrStampsGC, fmt.Errorf("mã mộc dấu %s trùng giữa GS và GC", c)}
			}
		}
	}
	return nil
}

// Stamps trả mọi mã mộc dấu (GS rồi GC) của cơ sở giết mổ.
func (it Item) Stamps() []string {
	return append(CodeList(it.Attrs[port.AttrStampsGS]), CodeList(it.Attrs[port.AttrStampsGC])...)
}

// ValidateValidFrom kiểm ngày hiệu lực (ngày nghiệp vụ).
func ValidateValidFrom(d time.Time) error {
	if d.Before(Day(2000, 1, 1)) || d.After(Day(2100, 12, 31)) {
		return FieldError{"valid_from", ErrValidFrom}
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
