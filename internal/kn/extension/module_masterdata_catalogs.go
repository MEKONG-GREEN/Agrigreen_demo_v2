package extension

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/google/uuid"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

//go:embed masterdata_catalogs.json
var masterdataCatalogSeeds []byte

// MasterdataCatalogs owns schema, validation, dates and initial data for twelve catalogs.
func MasterdataCatalogs() mdport.CatalogRegistration {

	desc := mdport.AttributeSpec{Required: false, Normalize: catalogText(500)}
	order := mdport.AttributeSpec{Required: false, Normalize: catalogIntRange(0, 999)}
	cnts := map[string]map[string]mdport.AttributeSpec{
		knport.CatalogCNTSSpecies: {
			"group":       {Required: true, Normalize: catalogOneOf("gia_suc", "gia_cam", "thu_cung", "yen", "thuy_san", "khac")},
			"unit":        {Required: true, Normalize: catalogNonEmpty(20)},
			"herd_column": {Required: false, Normalize: catalogBoolAttr},
			"order":       order,
			"description": desc,
		},
		knport.CatalogCNTSBreed: {
			knport.AttrSpecies: {Required: true, Normalize: catalogStageRefNamed("mã loài")},
			"description":      desc,
		},
		knport.CatalogCNTSDisease: {
			knport.AttrSpecies: {Required: false, Normalize: catalogNormalizeCodes},
			"notifiable":       {Required: false, Normalize: catalogBoolAttr},
			"description":      desc,
		},
		knport.CatalogCNTSVaccine: {
			"diseases":         {Required: false, Normalize: catalogNormalizeCodes},
			knport.AttrSpecies: {Required: true, Normalize: catalogNonEmptyCodeList},
			"report_group":     {Required: true, Normalize: catalogOneOf("heo", "bo", "cum_gia_cam", "dai", "khac")},
			"order":            order,
			"description":      desc,
		},
		knport.CatalogCNTSChemical: {
			"unit":        {Required: true, Normalize: catalogNonEmpty(20)},
			"target":      {Required: false, Normalize: catalogOneOf("gia_suc", "gia_cam", "chung")},
			"description": desc,
		},
		knport.CatalogCNTSSlaughterFacility: {
			knport.AttrFacilityRef:  {Required: true, Normalize: catalogUuidAttr},
			knport.AttrCommuneCode:  {Required: true, Normalize: catalogCommuneCode},
			knport.AttrFacilityKind: {Required: true, Normalize: catalogOneOf(knport.FacilityConcentrated, knport.FacilitySmall)},
			knport.AttrStampsGS:     {Required: false, Normalize: catalogStampList},
			knport.AttrStampsGC:     {Required: false, Normalize: catalogStampList},
			knport.AttrSpecies:      {Required: false, Normalize: catalogNormalizeCodes},
			"address":               {Required: false, Normalize: catalogText(300)},
			"description":           desc,
		},
		knport.CatalogCNTSAquacultureObject: {
			"scale_unit":  {Required: true, Normalize: catalogOneOf("ha", "long", "veo", "m3")},
			"description": desc,
		},
		knport.CatalogCNTSFishingGroup: {
			"unit":        {Required: false, Normalize: catalogText(20)},
			"order":       order,
			"description": desc,
		},
		knport.CatalogCNTSIndicator: {
			"unit":        {Required: false, Normalize: catalogText(20)},
			"value_type":  {Required: true, Normalize: catalogOneOf("decimal", "integer", "percent")},
			"description": desc,
		},
		knport.CatalogCNTSModelType: {
			"sector":           {Required: true, Normalize: catalogOneOf("chan_nuoi", "thuy_san")},
			knport.AttrSpecies: {Required: false, Normalize: catalogNormalizeCodes},
			"description":      desc,
		},
		knport.CatalogCNTSDocumentType: {
			"description": desc,
		},
		knport.CatalogCNTSVaccinationSchedule: {
			"model_type":     {Required: false, Normalize: catalogStageRefNamed("mã loại mô hình")},
			"breed":          {Required: false, Normalize: catalogStageRefNamed("mã giống")},
			knport.AttrDoses: {Required: true, Normalize: catalogDosesAttr},
			"description":    desc,
		},
	}
	names := map[string]string{
		knport.CatalogCNTSSpecies:             "Vật nuôi",
		knport.CatalogCNTSBreed:               "Giống vật nuôi",
		knport.CatalogCNTSVaccine:             "Vaccine",
		knport.CatalogCNTSVaccinationSchedule: "Quy trình tiêm",
		knport.CatalogCNTSDisease:             "Bệnh",
		knport.CatalogCNTSChemical:            "Hóa chất",
		knport.CatalogCNTSSlaughterFacility:   "Cơ sở giết mổ",
		knport.CatalogCNTSAquacultureObject:   "Đối tượng nuôi thủy sản",
		knport.CatalogCNTSFishingGroup:        "Nhóm khai thác",
		knport.CatalogCNTSIndicator:           "Chỉ tiêu theo dõi",
		knport.CatalogCNTSModelType:           "Loại mô hình",
		knport.CatalogCNTSDocumentType:        "Loại văn bản",
	}

	keys := []string{
		knport.CatalogCNTSSpecies, knport.CatalogCNTSBreed, knport.CatalogCNTSVaccine, knport.CatalogCNTSVaccinationSchedule,
		knport.CatalogCNTSDisease, knport.CatalogCNTSChemical, knport.CatalogCNTSSlaughterFacility, knport.CatalogCNTSAquacultureObject,
		knport.CatalogCNTSFishingGroup, knport.CatalogCNTSIndicator, knport.CatalogCNTSModelType, knport.CatalogCNTSDocumentType,
	}
	var seeds []mdport.CatalogSeed
	if err := json.Unmarshal(masterdataCatalogSeeds, &seeds); err != nil {
		panic(err)
	}
	reg := mdport.CatalogRegistration{Seeds: seeds, InvalidEffectiveDateDetail: "valid_from: YYYY-MM-DD, chỉ danh mục Chăn nuôi – Thủy sản"}
	for _, key := range keys {
		c := key
		reg.Definitions = append(reg.Definitions, mdport.CatalogDefinition{
			Catalog: c, Name: names[c], Attributes: cnts[c], EffectiveDated: true, Sync: true, SeedValidFrom: "2000-01-01",
			Constraints: catalogTransactionConstraints(c),
			ValidateAttrs: func(attrs map[string]string) error {
				return catalogNormalizeCNTS(mdport.CatalogItem{Catalog: c, Attrs: attrs})
			},
		})
	}
	return reg
}

var catalogItemCodeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,39}$`)
var (
	errCatalogDoses     = errors.New(`danh sách mũi dạng "<mã vaccine>/<lần>@<ngày tuổi>" ngăn cách dấu phẩy, 1–40 mũi, lần 1–20, ngày tuổi 0–3650`)
	errCatalogStampCode = errors.New("mã mộc dấu dạng số và dấu chấm (vd 91.589.05.1), tối đa 40 ký tự")
)

func catalogIntRange(lo, hi int) func(string) (string, error) {
	return func(s string) (string, error) {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil || n < lo || n > hi {
			return "", fmt.Errorf("phải là số nguyên %d–%d", lo, hi)
		}
		return strconv.Itoa(n), nil
	}
}

func catalogOneOf(vals ...string) func(string) (string, error) {
	return func(s string) (string, error) {
		if !slices.Contains(vals, s) {
			return "", fmt.Errorf("phải là một trong %s", strings.Join(vals, ", "))
		}
		return s, nil
	}
}

func catalogText(max int) func(string) (string, error) {
	return func(s string) (string, error) {
		if utf8.RuneCountInString(s) > max {
			return "", fmt.Errorf("tối đa %d ký tự", max)
		}
		return s, nil
	}
}

func catalogNormalizeCodes(s string) (string, error) {
	var out []string
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !catalogItemCodeRe.MatchString(c) {
			return "", fmt.Errorf("mã %q không hợp lệ", c)
		}
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	if len(out) > 200 {
		return "", errors.New("tối đa 200 mã")
	}
	sort.Strings(out)
	return strings.Join(out, ","), nil
}

func catalogNonEmpty(max int) func(string) (string, error) {
	return func(s string) (string, error) {
		s = strings.TrimSpace(s)
		if s == "" || utf8.RuneCountInString(s) > max {
			return "", fmt.Errorf("bắt buộc, tối đa %d ký tự", max)
		}
		return s, nil
	}
}

func catalogStageRefNamed(what string) func(string) (string, error) {
	return func(s string) (string, error) {
		if !catalogItemCodeRe.MatchString(s) {
			return "", errors.New("phải là " + what)
		}
		return s, nil
	}
}

func catalogCodes(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func catalogStampList(s string) (string, error) {
	var out []string
	for _, c := range strings.Split(s, ",") {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !catalogItemCodeRe.MatchString(c) {
			return "", errCatalogStampCode
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

func catalogUuidAttr(s string) (string, error) {
	u, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil || u == uuid.Nil {
		return "", errors.New("phải là UUID")
	}
	return u.String(), nil
}

func catalogBoolAttr(s string) (string, error) {
	return catalogOneOf("true", "false")(strings.TrimSpace(s))
}

func catalogCommuneCode(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !catalogItemCodeRe.MatchString(s) {
		return "", errors.New("phải là mã xã/phường/đặc khu")
	}
	return s, nil
}

func catalogDosesAttr(s string) (string, error) {
	ds, err := catalogParseDoses(s)
	if err != nil {
		return "", err
	}
	return catalogFormatDoses(ds), nil
}

func catalogParseDoses(s string) ([]knport.Dose, error) {
	var out []knport.Dose
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		vs, age, ok := strings.Cut(part, "@")
		if !ok {
			return nil, errCatalogDoses
		}
		v, shot, ok := strings.Cut(strings.TrimSpace(vs), "/")
		if !ok || !catalogItemCodeRe.MatchString(strings.TrimSpace(v)) {
			return nil, errCatalogDoses
		}
		n, err1 := strconv.Atoi(strings.TrimSpace(shot))
		a, err2 := strconv.Atoi(strings.TrimSpace(age))
		if err1 != nil || err2 != nil || n < 1 || n > 20 || a < 0 || a > 3650 {
			return nil, errCatalogDoses
		}
		d := knport.Dose{Vaccine: strings.TrimSpace(v), Shot: n, AgeDays: a}
		for _, o := range out {
			if o.Vaccine == d.Vaccine && o.Shot == d.Shot {
				return nil, fmt.Errorf("mũi %s lần %d bị lặp", d.Vaccine, d.Shot)
			}
		}
		out = append(out, d)
	}
	if len(out) == 0 || len(out) > 40 {
		return nil, errCatalogDoses
	}
	slices.SortStableFunc(out, func(a, b knport.Dose) int {
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

func catalogFormatDoses(ds []knport.Dose) string {
	parts := make([]string, len(ds))
	for i, d := range ds {
		parts[i] = d.Vaccine + "/" + strconv.Itoa(d.Shot) + "@" + strconv.Itoa(d.AgeDays)
	}
	return strings.Join(parts, ",")
}

func catalogNonEmptyCodeList(s string) (string, error) {
	v, err := catalogNormalizeCodes(s)
	if err == nil && v == "" {
		return "", errors.New("bắt buộc ít nhất một mã")
	}
	return v, err
}

func catalogNormalizeCNTS(it mdport.CatalogItem) error {
	switch it.Catalog {
	case knport.CatalogCNTSVaccinationSchedule:
		if it.Attrs["model_type"] == "" && it.Attrs["breed"] == "" {
			return mdport.FieldError{Field: "attrs.model_type", Err: errors.New("quy trình tiêm gắn loại mô hình hoặc giống")}
		}
	case knport.CatalogCNTSSlaughterFacility:
		gs, gc := catalogCodes(it.Attrs[knport.AttrStampsGS]), catalogCodes(it.Attrs[knport.AttrStampsGC])
		for _, c := range gs {
			if slices.Contains(gc, c) {
				return mdport.FieldError{Field: "attrs." + knport.AttrStampsGC, Err: fmt.Errorf("mã mộc dấu %s trùng giữa GS và GC", c)}
			}
		}
	}
	return nil
}
