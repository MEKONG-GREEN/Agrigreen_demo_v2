package domain

// Job tổng hợp vượt đơn vị (W6.4, ADR-0031 §4, R-DB-11): định nghĩa có phiên bản trong mã (đăng ký lúc biên dịch),
// đọc CHỈ view công bố _pub, áp ngưỡng k trên nhóm SAU khi gộp mọi tenant; quy tắc thuần, không I/O. Danh mục ở DB
// (pf_metrics.agg_job, pf 000129) giữ băm định nghĩa đã duyệt bốn mắt; bộ chạy từ chối khi băm/phiên bản lệch.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// AggRuleVersion là phiên bản quy tắc ghi vào sổ danh mục/nhật ký chạy (rule_version).
const AggRuleVersion = "agg-2026.10"

// KFloorDefault là ngưỡng k tạm (ADR-0031 §7, K-8, C-4: publicdata.k_min = 10) khi tham số chưa đặt.
const KFloorDefault = 10

// AggJob là một phiên bản định nghĩa job tổng hợp.
//
// Query chạy dưới vai tổng hợp trong lát của MỘT tenant (app.tenant_id đặt bởi bộ chạy), chỉ đọc các view trong
// Views; trả len(Dims) cột text (chiều nhóm) rồi len(Measures) cột text số thập phân (cộng dồn qua tenant).
type AggJob struct {
	ID       string   // '<miền>.<tên>', vd cc.harvest_by_commune
	Version  int      // tăng khi đổi bất kỳ phần nào của định nghĩa
	Views    []string // view _pub đọc: pf_<module>_pub.v<N>_<tên>
	Query    string
	Dims     []string
	Measures []string
	Purpose  string // mục đích (10–500 ký tự)
	KMin     int    // k của job — không nhỏ hơn ngưỡng đã ký (k sàn)
}

// Lỗi quy tắc.
var (
	ErrAggJobID      = errors.New("job_id phải dạng <miền>.<tên> (chữ thường, số, _)")
	ErrAggVersion    = errors.New("version phải từ 1")
	ErrAggViews      = errors.New("views: 1–20 view _pub dạng pf_<module>_pub.v<N>_<tên>, không trùng")
	ErrAggQuery      = errors.New("query: bắt buộc, ≤ 8000 ký tự, chỉ đọc view đã khai")
	ErrAggColumns    = errors.New("dims/measures: ít nhất một chiều và một số đo, tên chữ thường")
	ErrAggPurpose    = errors.New("purpose: 10–500 ký tự")
	ErrKBelowFloor   = errors.New("k nhỏ hơn ngưỡng k sàn (publicdata.k_min)")
	errAggTenantRefs = errors.New("query không được tự lọc/đổi tenant (app.tenant_id, set_config)")
)

var (
	aggJobIDRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}\.[a-z][a-z0-9_]{0,59}$`)
	aggViewRe   = regexp.MustCompile(`^pf_[a-z0-9_]+_pub\.v[0-9]+_[a-z0-9_]+$`)
	aggColRe    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	aggSchemaRe = regexp.MustCompile(`(?i)\b([a-z_][a-z0-9_]*)\s*\.\s*[a-z_][a-z0-9_]*`)
)

// ValidateAggJob kiểm định nghĩa và k của job so với k sàn.
func ValidateAggJob(j AggJob, kFloor int) error {
	if !aggJobIDRe.MatchString(j.ID) {
		return ErrAggJobID
	}
	if j.Version < 1 || j.Version > 100000 {
		return ErrAggVersion
	}
	if len(j.Views) == 0 || len(j.Views) > 20 {
		return ErrAggViews
	}
	seen := map[string]bool{}
	for _, v := range j.Views {
		if !aggViewRe.MatchString(v) || seen[v] {
			return ErrAggViews
		}
		seen[v] = true
	}
	q := strings.TrimSpace(j.Query)
	if q == "" || len([]rune(q)) > 8000 {
		return ErrAggQuery
	}
	if err := queryReadsOnly(q, seen); err != nil {
		return err
	}
	if len(j.Dims) == 0 || len(j.Measures) == 0 {
		return ErrAggColumns
	}
	for _, c := range slices.Concat(j.Dims, j.Measures) {
		if !aggColRe.MatchString(c) {
			return ErrAggColumns
		}
	}
	if n := len([]rune(strings.TrimSpace(j.Purpose))); n < 10 || n > 500 {
		return ErrAggPurpose
	}
	return CheckK(j.KMin, kFloor)
}

// queryReadsOnly: mọi tham chiếu <schema>.<đối tượng> trong câu SQL phải là view đã khai (không bảng nội bộ, không
// k_ref, không hàm của schema khác); câu không được tự đặt tenant. Quyền DB của vai tổng hợp là lớp chặn thật —
// đây là kiểm sớm lúc đề xuất để người duyệt thấy đúng tập view.
func queryReadsOnly(q string, views map[string]bool) error {
	low := strings.ToLower(q)
	if strings.Contains(low, "app.tenant_id") || strings.Contains(low, "set_config") {
		return errAggTenantRefs
	}
	for _, m := range aggSchemaRe.FindAllStringSubmatchIndex(low, -1) {
		ref := strings.Join(strings.Fields(low[m[0]:m[1]]), "")
		schema := low[m[2]:m[3]]
		if !strings.HasSuffix(schema, "_pub") {
			// bí danh bảng (vd l.mass_kg) — chỉ chấp nhận khi "schema" không phải tên schema hệ thống/module
			if strings.HasPrefix(schema, "pf_") || strings.HasPrefix(schema, "mkg_") || schema == "k_ref" ||
				schema == "shared" || schema == "public" || strings.HasPrefix(schema, "pg_") || schema == "information_schema" {
				return ErrAggQuery
			}
			continue
		}
		if !views[ref] {
			return ErrAggQuery
		}
	}
	return nil
}

// CheckK: k phải ≥ 2 và ≥ k sàn (ADR-0031 §4, §7).
func CheckK(k, kFloor int) error {
	if kFloor < 2 {
		kFloor = 2
	}
	if k < kFloor {
		return fmt.Errorf("%w: k = %d < %d", ErrKBelowFloor, k, kFloor)
	}
	return nil
}

// KUsed là k áp cho một lần chạy: lớn nhất của k job và k sàn hiện hành.
func KUsed(j AggJob, kFloor int) int { return max(j.KMin, kFloor, 2) }

// Hash là SHA-256 (hex) dạng chuẩn của định nghĩa: mọi trường, view đã sắp, khoảng trắng của câu SQL gom lại.
func (j AggJob) Hash() string {
	views := slices.Clone(j.Views)
	sort.Strings(views)
	parts := []string{
		"id=" + j.ID, "version=" + strconv.Itoa(j.Version), "views=" + strings.Join(views, ","),
		"query=" + strings.Join(strings.Fields(j.Query), " "), "dims=" + strings.Join(j.Dims, ","),
		"measures=" + strings.Join(j.Measures, ","), "purpose=" + strings.TrimSpace(j.Purpose), "k_min=" + strconv.Itoa(j.KMin),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

// SortedViews trả danh sách view đã sắp (dạng ghi vào danh mục).
func (j AggJob) SortedViews() []string {
	v := slices.Clone(j.Views)
	sort.Strings(v)
	return v
}

// AggGroup là một nhóm đầu ra: giá trị chiều, tổng số đo qua mọi tenant, số tenant góp.
type AggGroup struct {
	Dims         []string
	Measures     []decimal.Decimal
	Contributors int
}

// AggAccumulator gộp dòng của từng tenant thành nhóm toàn nền tảng.
type AggAccumulator struct {
	measures int
	groups   map[string]*AggGroup
	seen     map[string]map[string]bool // khóa nhóm → tenant đã góp
	Rows     int64
}

// NewAggAccumulator tạo bộ gộp cho job j.
func NewAggAccumulator(j AggJob) *AggAccumulator {
	return &AggAccumulator{measures: len(j.Measures), groups: map[string]*AggGroup{}, seen: map[string]map[string]bool{}}
}

// Add cộng một dòng (dims, measures dạng chuỗi thập phân) của tenant.
func (a *AggAccumulator) Add(tenant string, dims []string, measures []string) error {
	if len(measures) != a.measures {
		return fmt.Errorf("agg: dòng có %d số đo, muốn %d", len(measures), a.measures)
	}
	key := strings.Join(dims, "\x1f")
	g := a.groups[key]
	if g == nil {
		g = &AggGroup{Dims: slices.Clone(dims), Measures: make([]decimal.Decimal, a.measures)}
		a.groups[key] = g
		a.seen[key] = map[string]bool{}
	}
	for i, s := range measures {
		d, err := decimal.NewFromString(s)
		if err != nil {
			return fmt.Errorf("agg: số đo %q: %w", s, err)
		}
		g.Measures[i] = g.Measures[i].Add(d)
	}
	if !a.seen[key][tenant] {
		a.seen[key][tenant] = true
		g.Contributors++
	}
	a.Rows++
	return nil
}

// Suppress trả các nhóm có ít nhất k tenant góp (sắp theo chiều) và số nhóm bị ẩn. Đầu ra chỉ có nhóm, không có
// tổng: không cần ẩn ô bù (ADR-0031 §4) — job công bố cả tổng phải tự thêm bước đó.
func (a *AggAccumulator) Suppress(k int) ([]AggGroup, int) {
	var out []AggGroup
	hidden := 0
	for _, g := range a.groups {
		if g.Contributors < k {
			hidden++
			continue
		}
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return slices.Compare(out[i].Dims, out[j].Dims) < 0 })
	return out, hidden
}
