// Package domain: quy tắc thuần của pf/metrics — định nghĩa chỉ số có phiên bản,
// ngày nghiệp vụ, băm đầu vào, gập nhật ký lớp GIS thành ảnh chụp.
package domain

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Def là một phiên bản định nghĩa đã nạp: port.Definition + phần khai báo thêm.
type Def struct {
	port.Definition
	Description string
	Grain       port.Grain
	ViewerRoles []string
	File        string
	SHA256      [32]byte // băm nội dung tệp (ghim ở definition_pin)
}

type rawDef struct {
	Key           string   `yaml:"key"`
	Version       int      `yaml:"version"`
	Title         string   `yaml:"title"`
	Description   string   `yaml:"description"`
	Unit          string   `yaml:"unit"`
	Grain         string   `yaml:"grain"`
	Aggregation   string   `yaml:"aggregation"`
	SourceModule  string   `yaml:"source_module"`
	Source        string   `yaml:"source"`
	RuleVersion   string   `yaml:"rule_version"`
	EffectiveFrom string   `yaml:"effective_from"`
	ProposedBy    string   `yaml:"proposed_by"`
	ApprovedBy    string   `yaml:"approved_by"`
	ViewerRoles   []string `yaml:"viewer_roles"`
}

var (
	keyRe    = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+$`)
	moduleRe = regexp.MustCompile(`^pf/[a-z]+$`)
)

// Catalog là mọi phiên bản định nghĩa, theo key rồi theo phiên bản tăng dần.
type Catalog struct {
	byKey map[port.Key][]Def
}

// ParseDate đọc YYYY-MM-DD thành ngày nghiệp vụ.
func ParseDate(s string) (clock.Date, error) {
	t, err := time.Parse(time.DateOnly, strings.TrimSpace(s))
	if err != nil {
		return clock.Date{}, fmt.Errorf("ngày %q phải dạng YYYY-MM-DD", s)
	}
	return clock.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, nil
}

// DateTime trả 00:00 UTC mang ngày d (so sánh, lưu cột date).
func DateTime(d clock.Date) time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

// DateOf chuyển cột date (00:00 UTC) về clock.Date.
func DateOf(t time.Time) clock.Date {
	t = t.UTC()
	return clock.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
}

// Before cho biết a < b.
func Before(a, b clock.Date) bool { return DateTime(a).Before(DateTime(b)) }

// AddDays cộng n ngày.
func AddDays(d clock.Date, n int) clock.Date { return DateOf(DateTime(d).AddDate(0, 0, n)) }

// Closed cho biết ngày d đã kết thúc tại now (now ≥ Cutoff(d)).
func Closed(d clock.Date, now time.Time) bool { return !now.Before(port.Cutoff(d)) }

// Load nạp và kiểm mọi tệp *.yaml trong fsys.
func Load(fsys fs.FS) (*Catalog, error) {
	files, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, err
	}
	c := &Catalog{byKey: map[port.Key][]Def{}}
	var errs []error
	for _, f := range files {
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		d, err := parse(f, b)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		c.byKey[d.Key] = append(c.byKey[d.Key], d)
	}
	for k, list := range c.byKey {
		sort.Slice(list, func(i, j int) bool { return list[i].Version < list[j].Version })
		for i, d := range list {
			if d.Version != i+1 {
				errs = append(errs, fmt.Errorf("%s: phiên bản phải liên tục từ 1 (thiếu v%d)", k, i+1))
				break
			}
			if i > 0 && !Before(list[i-1].EffectiveFrom, d.EffectiveFrom) {
				errs = append(errs, fmt.Errorf("%s v%d: effective_from phải sau phiên bản trước", k, d.Version))
			}
			if i > 0 && (d.Unit != list[0].Unit || d.Grain != list[0].Grain) {
				errs = append(errs, fmt.Errorf("%s v%d: không đổi đơn vị/grain giữa các phiên bản — dùng key mới", k, d.Version))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return c, nil
}

func parse(file string, b []byte) (Def, error) {
	var r rawDef
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil {
		return Def{}, fmt.Errorf("%s: %w", file, err)
	}
	var errs []string
	bad := func(s string) { errs = append(errs, s) }
	if !keyRe.MatchString(r.Key) {
		bad("key phải dạng <nhóm>.<tên> chữ thường")
	}
	if r.Version < 1 {
		bad("version ≥ 1")
	}
	if want := r.Key + ".v" + strconv.Itoa(r.Version) + ".yaml"; path.Base(file) != want {
		bad("tên tệp phải là " + want)
	}
	for f, v := range map[string]string{"title": r.Title, "description": r.Description, "unit": r.Unit, "source": r.Source,
		"rule_version": r.RuleVersion, "proposed_by": r.ProposedBy, "approved_by": r.ApprovedBy} {
		if strings.TrimSpace(v) == "" {
			bad("thiếu " + f)
		}
	}
	g := port.Grain(r.Grain)
	if g != port.GrainDay && g != port.GrainSeason {
		bad("grain phải là day hoặc season")
	}
	a := port.Aggregation(r.Aggregation)
	if !slices.Contains([]port.Aggregation{port.AggSum, port.AggCount, port.AggAvg, port.AggMax, port.AggLast}, a) {
		bad("aggregation không hợp lệ")
	}
	if !moduleRe.MatchString(r.SourceModule) {
		bad("source_module phải dạng pf/<module>")
	}
	eff, err := ParseDate(r.EffectiveFrom)
	if err != nil {
		bad("effective_from: " + err.Error())
	}
	// R-56: người đề xuất ≠ người duyệt.
	if strings.TrimSpace(r.ProposedBy) != "" && strings.EqualFold(strings.TrimSpace(r.ProposedBy), strings.TrimSpace(r.ApprovedBy)) {
		bad("proposed_by phải khác approved_by (R-56)")
	}
	if len(errs) > 0 {
		return Def{}, fmt.Errorf("%s: %s", file, strings.Join(errs, "; "))
	}
	return Def{
		Definition: port.Definition{
			Key: port.Key(r.Key), Version: r.Version, Title: r.Title, Unit: r.Unit, Aggregation: a,
			SourceModule: r.SourceModule, Source: r.Source, RuleVersion: r.RuleVersion, EffectiveFrom: eff,
			ProposedBy: r.ProposedBy, ApprovedBy: r.ApprovedBy,
		},
		Description: strings.TrimSpace(r.Description), Grain: g, ViewerRoles: r.ViewerRoles, File: path.Base(file),
		SHA256: sha256.Sum256(b),
	}, nil
}

// Effective trả phiên bản có hiệu lực vào ngày d (effective_from ≤ d, lớn nhất).
func (c *Catalog) Effective(key port.Key, d clock.Date) (Def, bool) {
	list := c.byKey[key]
	for i := len(list) - 1; i >= 0; i-- {
		if !Before(d, list[i].EffectiveFrom) {
			return list[i], true
		}
	}
	return Def{}, false
}

// Versions trả mọi phiên bản của key.
func (c *Catalog) Versions(key port.Key) []Def { return slices.Clone(c.byKey[key]) }

// All trả mọi phiên bản, theo key rồi phiên bản.
func (c *Catalog) All() []Def {
	keys := make([]string, 0, len(c.byKey))
	for k := range c.byKey {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	var out []Def
	for _, k := range keys {
		out = append(out, c.byKey[port.Key(k)]...)
	}
	return out
}

// Keys trả mọi key theo thứ tự.
func (c *Catalog) Keys() []port.Key {
	var out []port.Key
	seen := map[port.Key]bool{}
	for _, d := range c.All() {
		if !seen[d.Key] {
			seen[d.Key] = true
			out = append(out, d.Key)
		}
	}
	return out
}
