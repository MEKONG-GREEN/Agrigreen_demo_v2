package extension

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
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
		mdport.CatalogCNTSSpecies: {
			"group":       {Required: true, Normalize: catalogOneOf("gia_suc", "gia_cam", "thu_cung", "yen", "thuy_san", "khac")},
			"unit":        {Required: true, Normalize: catalogNonEmpty(20)},
			"herd_column": {Required: false, Normalize: catalogBoolAttr},
			"order":       order,
			"description": desc,
		},
		mdport.CatalogCNTSBreed: {
			mdport.AttrSpecies: {Required: true, Normalize: catalogStageRefNamed("mã loài")},
			"description":      desc,
		},
		mdport.CatalogCNTSDisease: {
			mdport.AttrSpecies: {Required: false, Normalize: catalogNormalizeCodes},
			"notifiable":       {Required: false, Normalize: catalogBoolAttr},
			"description":      desc,
		},
		mdport.CatalogCNTSVaccine: {
			"diseases":         {Required: false, Normalize: catalogNormalizeCodes},
			mdport.AttrSpecies: {Required: true, Normalize: catalogNonEmptyCodeList},
			"report_group":     {Required: true, Normalize: catalogOneOf("heo", "bo", "cum_gia_cam", "dai", "khac")},
			"order":            order,
			"description":      desc,
		},
		mdport.CatalogCNTSChemical: {
			"unit":        {Required: true, Normalize: catalogNonEmpty(20)},
			"target":      {Required: false, Normalize: catalogOneOf("gia_suc", "gia_cam", "chung")},
			"description": desc,
		},
		mdport.CatalogCNTSSlaughterFacility: {
			mdport.AttrFacilityRef:  {Required: true, Normalize: catalogUuidAttr},
			mdport.AttrCommuneCode:  {Required: true, Normalize: catalogCommuneCode},
			mdport.AttrFacilityKind: {Required: true, Normalize: catalogOneOf(mdport.FacilityConcentrated, mdport.FacilitySmall)},
			mdport.AttrStampsGS:     {Required: false, Normalize: catalogStampList},
			mdport.AttrStampsGC:     {Required: false, Normalize: catalogStampList},
			mdport.AttrSpecies:      {Required: false, Normalize: catalogNormalizeCodes},
			"address":               {Required: false, Normalize: catalogText(300)},
			"description":           desc,
		},
		mdport.CatalogCNTSAquacultureObject: {
			"scale_unit":  {Required: true, Normalize: catalogOneOf("ha", "long", "veo", "m3")},
			"description": desc,
		},
		mdport.CatalogCNTSFishingGroup: {
			"unit":        {Required: false, Normalize: catalogText(20)},
			"order":       order,
			"description": desc,
		},
		mdport.CatalogCNTSIndicator: {
			"unit":        {Required: false, Normalize: catalogText(20)},
			"value_type":  {Required: true, Normalize: catalogOneOf("decimal", "integer", "percent")},
			"description": desc,
		},
		mdport.CatalogCNTSModelType: {
			"sector":           {Required: true, Normalize: catalogOneOf("chan_nuoi", "thuy_san")},
			mdport.AttrSpecies: {Required: false, Normalize: catalogNormalizeCodes},
			"description":      desc,
		},
		mdport.CatalogCNTSDocumentType: {
			"description": desc,
		},
		mdport.CatalogCNTSVaccinationSchedule: {
			"model_type":     {Required: false, Normalize: catalogStageRefNamed("mã loại mô hình")},
			"breed":          {Required: false, Normalize: catalogStageRefNamed("mã giống")},
			mdport.AttrDoses: {Required: true, Normalize: catalogDosesAttr},
			"description":    desc,
		},
	}
	names := map[string]string{
		mdport.CatalogCNTSSpecies:             "Vật nuôi",
		mdport.CatalogCNTSBreed:               "Giống vật nuôi",
		mdport.CatalogCNTSVaccine:             "Vaccine",
		mdport.CatalogCNTSVaccinationSchedule: "Quy trình tiêm",
		mdport.CatalogCNTSDisease:             "Bệnh",
		mdport.CatalogCNTSChemical:            "Hóa chất",
		mdport.CatalogCNTSSlaughterFacility:   "Cơ sở giết mổ",
		mdport.CatalogCNTSAquacultureObject:   "Đối tượng nuôi thủy sản",
		mdport.CatalogCNTSFishingGroup:        "Nhóm khai thác",
		mdport.CatalogCNTSIndicator:           "Chỉ tiêu theo dõi",
		mdport.CatalogCNTSModelType:           "Loại mô hình",
		mdport.CatalogCNTSDocumentType:        "Loại văn bản",
	}

	keys := []string{
		mdport.CatalogCNTSSpecies, mdport.CatalogCNTSBreed, mdport.CatalogCNTSVaccine, mdport.CatalogCNTSVaccinationSchedule,
		mdport.CatalogCNTSDisease, mdport.CatalogCNTSChemical, mdport.CatalogCNTSSlaughterFacility, mdport.CatalogCNTSAquacultureObject,
		mdport.CatalogCNTSFishingGroup, mdport.CatalogCNTSIndicator, mdport.CatalogCNTSModelType, mdport.CatalogCNTSDocumentType,
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

func catalogParseDoses(s string) ([]mdport.Dose, error) {
	var out []mdport.Dose
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
		d := mdport.Dose{Vaccine: strings.TrimSpace(v), Shot: n, AgeDays: a}
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
	slices.SortStableFunc(out, func(a, b mdport.Dose) int {
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

func catalogFormatDoses(ds []mdport.Dose) string {
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
	case mdport.CatalogCNTSVaccinationSchedule:
		if it.Attrs["model_type"] == "" && it.Attrs["breed"] == "" {
			return mdport.FieldError{Field: "attrs.model_type", Err: errors.New("quy trình tiêm gắn loại mô hình hoặc giống")}
		}
	case mdport.CatalogCNTSSlaughterFacility:
		gs, gc := catalogCodes(it.Attrs[mdport.AttrStampsGS]), catalogCodes(it.Attrs[mdport.AttrStampsGC])
		for _, c := range gs {
			if slices.Contains(gc, c) {
				return mdport.FieldError{Field: "attrs." + mdport.AttrStampsGC, Err: fmt.Errorf("mã mộc dấu %s trùng giữa GS và GC", c)}
			}
		}
	}
	return nil
}
