// Package dataproduct nạp và cưỡng chế hợp đồng SẢN PHẨM DỮ LIỆU của Public API (ADR-0022 mục 9;
// docs/dev/g5-conventions.md §6). Mặc định là TỪ CHỐI: chỉ trường có trong hợp đồng được trả cho
// bên thứ ba; hợp đồng thiếu người phê duyệt, tự phê duyệt, khai có dữ liệu cá nhân, hoặc
// chưa "approved" ở production thì coi như không có.
//
// Dạng tệp <tên>.json (README.md cùng thư mục) dùng lại tập con JSON Schema của van công bố
// (contracts/publication.Node): mô tả MỘT phần tử của danh sách mà route trả về. Hai lớp cưỡng chế:
//   - lúc build: tools/contractcheck — siêu dữ liệu, struct //ag:dataproduct <tên> chỉ có trường
//     của hợp đồng và đúng kiểu, mỗi sản phẩm có tag cùng tên trong contracts/openapi/public.yaml;
//   - lúc chạy: handler của pf/apiclient gọi Get(tên) rồi Product.Validate(json) từng phần tử
//     trước khi trả — khóa lạ, sai kiểu, thiếu trường bắt buộc → không trả gì.
//
// ĐÓNG BĂNG ở G5-0: Get, All, Product.{Validate, Servable, Publication}, các hằng Status*.
package dataproduct

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
)

//go:embed *.json
var files embed.FS

// Trạng thái hợp đồng sản phẩm.
const (
	// StatusDraft: dự thảo — chỉ phục vụ NGOÀI production (dev, staging, kiểm thử). Ba sản phẩm
	// đầu tiên ở G5-0 đều là dự thảo, "chờ khách chốt bên thứ ba đầu tiên".
	StatusDraft = "draft"
	// StatusApproved: chủ dữ liệu và đơn vị vận hành đã phê duyệt — phục vụ ở mọi môi trường.
	StatusApproved = "approved"
	// StatusRetired: ngừng — không phục vụ ở đâu cả (giữ tệp để tra lịch sử phiên bản).
	StatusRetired = "retired"
)

// Aggregation mô tả ngưỡng gom nhóm k của sản phẩm tổng hợp (C-3, Q-MKG-11).
type Aggregation struct {
	// KParam là khóa tham số pf/masterdata giữ k (vd "publicdata.k_min"); có phiên bản, setter ≠ approver.
	KParam string `json:"k_param"`
	// KDefault là k tạm khi tham số chưa đặt ("tạm, chờ khách" — C-3). ≥ 2.
	KDefault int `json:"k_default"`
	// Unit là đơn vị đếm k: số nông hộ (household) hay số HTX (cooperative) phân biệt đóng góp vào ô.
	Unit string `json:"unit"`
	// Suppression: "omit" = ô dưới k bị bỏ khỏi kết quả; số ô bị bỏ báo ở meta.suppressed_cells.
	Suppression string `json:"suppression"`
}

// Export mô tả xuất bất đồng bộ (R-52) của sản phẩm.
type Export struct {
	Enabled bool     `json:"enabled"`
	Formats []string `json:"formats"`
	// Note: điều kiện để bật (vd "chờ C-1 + DPA").
	Note string `json:"note,omitempty"`
}

// Product là một hợp đồng sản phẩm dữ liệu.
type Product struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	// Owner là module nền tảng phục vụ sản phẩm, vd "pf/apiclient".
	Owner string `json:"owner"`
	// Sources là module chủ dữ liệu được đọc qua port/ (pool vai của chính module đó, R-05).
	Sources []string `json:"sources"`
	// DataOwner mô tả tenant chủ dữ liệu (bên cấp cấp quyền GrantKind).
	DataOwner string `json:"data_owner"`
	// GrantKind là loại cấp quyền chia sẻ ADR-0020 bên cấp phải duyệt cho tenant bên thứ ba: "dp_<name>".
	GrantKind string `json:"grant_kind"`
	// Scopes: "dp:<name>:read", "dp:<name>:export".
	Scopes []string `json:"scopes"`
	// Route là route chính, vd "GET /v1/data/collection-volumes".
	Route string `json:"route"`
	// Audience: loại bên thứ ba dự kiến (plant, buyer, verifier, …) — chỉ mô tả.
	Audience []string `json:"audience,omitempty"`
	// Status: draft | approved | retired.
	Status string `json:"status"`
	// CustomerStatus: ghi chú việc chờ khách (vd "chờ khách chốt bên thứ ba đầu tiên").
	CustomerStatus string `json:"customer_status,omitempty"`
	ProposedBy     string `json:"proposed_by"`
	ApprovedBy     string `json:"approved_by"`
	ApprovedAt     string `json:"approved_at"`
	Purpose        string `json:"purpose"`
	// PersonalData PHẢI là false (RB-3: bên thứ ba không bao giờ thấy dữ liệu cá nhân).
	PersonalData *bool        `json:"personal_data"`
	Grain        string       `json:"grain,omitempty"`
	Aggregation  *Aggregation `json:"aggregation,omitempty"`
	Export       *Export      `json:"export,omitempty"`
	Description  string       `json:"description,omitempty"`
	// Node: lược đồ MỘT phần tử (tập con JSON Schema của van công bố).
	publication.Node
}

var (
	nameRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{2,47}$`)
	routeRe = regexp.MustCompile(`^(GET|POST) /v[0-9]+/[a-z0-9/_{}-]+$`)
	statusS = map[string]bool{StatusDraft: true, StatusApproved: true, StatusRetired: true}
	unitS   = map[string]bool{"household": true, "cooperative": true}
)

// Parse đọc một hợp đồng. Lỗi chỉ khi JSON hỏng; vấn đề nội dung xem MetaProblems.
func Parse(raw []byte) (*Product, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	var p Product
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("dataproduct: JSON không hợp lệ: %w", err)
	}
	return &p, nil
}

// Publication trả hợp đồng dạng van công bố cùng lược đồ — dùng lại kiểm siêu dữ liệu, kiểm lược đồ
// và Validate của contracts/publication (một bộ quy tắc mặc định từ chối cho mọi cửa xuất).
func (p *Product) Publication() *publication.Contract {
	return &publication.Contract{Name: p.Name, Version: p.Version, Owner: p.Owner, ProposedBy: p.ProposedBy,
		ApprovedBy: p.ApprovedBy, ApprovedAt: p.ApprovedAt, Purpose: p.Purpose, Node: p.Node}
}

// MetaProblems kiểm siêu dữ liệu bắt buộc và tính nhất quán của hợp đồng sản phẩm.
func (p *Product) MetaProblems() []string {
	var out []string
	if !nameRe.MatchString(p.Name) {
		out = append(out, "name phải khớp ^[a-z][a-z0-9_]{2,47}$")
	}
	for _, mp := range p.Publication().MetaProblems() {
		if strings.HasPrefix(mp, "name phải khớp") {
			continue
		}
		out = append(out, mp)
	}
	if !strings.HasPrefix(p.Owner, "pf/") {
		out = append(out, "owner phải là module nền tảng pf/<module> (Public API chỉ ở vùng nền tảng, R-03)")
	}
	if len(p.Sources) == 0 {
		out = append(out, "thiếu sources (module chủ dữ liệu)")
	}
	for _, s := range p.Sources {
		if !strings.HasPrefix(s, "pf/") {
			out = append(out, "sources chỉ được là module pf/ (vùng mkg tới nền tảng chỉ qua van, R-03): "+s)
		}
	}
	if strings.TrimSpace(p.DataOwner) == "" {
		out = append(out, "thiếu data_owner")
	}
	if p.GrantKind != "dp_"+p.Name {
		out = append(out, fmt.Sprintf("grant_kind phải là %q (loại cấp quyền ADR-0020 của sản phẩm)", "dp_"+p.Name))
	}
	if len(p.Scopes) == 0 {
		out = append(out, "thiếu scopes")
	}
	hasRead := false
	for _, s := range p.Scopes {
		switch s {
		case "dp:" + p.Name + ":read":
			hasRead = true
		case "dp:" + p.Name + ":export":
		default:
			out = append(out, fmt.Sprintf("scope %q không hợp lệ: chỉ dp:%s:read hoặc dp:%s:export", s, p.Name, p.Name))
		}
	}
	if len(p.Scopes) > 0 && !hasRead {
		out = append(out, "scopes phải có dp:"+p.Name+":read")
	}
	if !routeRe.MatchString(p.Route) {
		out = append(out, "route phải có dạng \"GET /v1/...\"")
	}
	if !statusS[p.Status] {
		out = append(out, "status phải là draft | approved | retired")
	}
	if strings.TrimSpace(p.Purpose) == "" {
		out = append(out, "thiếu purpose")
	}
	if p.PersonalData == nil || *p.PersonalData {
		out = append(out, "personal_data phải khai và bằng false (RB-3: bên thứ ba không bao giờ thấy dữ liệu cá nhân)")
	}
	if a := p.Aggregation; a != nil {
		if strings.TrimSpace(a.KParam) == "" {
			out = append(out, "aggregation.k_param bắt buộc (C-3)")
		}
		if a.KDefault < 2 {
			out = append(out, "aggregation.k_default phải ≥ 2")
		}
		if !unitS[a.Unit] {
			out = append(out, "aggregation.unit phải là household | cooperative")
		}
		if a.Suppression != "omit" {
			out = append(out, "aggregation.suppression chỉ hỗ trợ omit")
		}
	}
	if e := p.Export; e != nil && e.Enabled {
		if len(e.Formats) == 0 {
			out = append(out, "export.formats bắt buộc khi export.enabled")
		}
		hasExport := false
		for _, s := range p.Scopes {
			hasExport = hasExport || s == "dp:"+p.Name+":export"
		}
		if !hasExport {
			out = append(out, "export.enabled cần scope dp:"+p.Name+":export")
		}
	}
	return out
}

// Servable cho biết sản phẩm được phục vụ ở môi trường này: approved ở mọi nơi; draft chỉ ngoài
// production (residency.Production); retired không bao giờ.
func (p *Product) Servable(production bool) bool {
	switch p.Status {
	case StatusApproved:
		return true
	case StatusDraft:
		return !production
	}
	return false
}

// HasScope cho biết s là scope của sản phẩm.
func (p *Product) HasScope(s string) bool {
	for _, x := range p.Scopes {
		if x == s {
			return true
		}
	}
	return false
}

// ContractHeader là giá trị header AG-Data-Contract (ADR-0022 mục 8): "<tên>.v<N>".
func (p *Product) ContractHeader() string { return fmt.Sprintf("%s.v%d", p.Name, p.Version) }

// Validate kiểm MỘT phần tử JSON sắp trả cho bên thứ ba theo hợp đồng (mặc định từ chối).
func (p *Product) Validate(item []byte) error { return p.Publication().Validate(item) }

var (
	loadOnce sync.Once
	loaded   map[string]*Product
	loadErr  error
)

func load() {
	loaded = map[string]*Product{}
	entries, err := files.ReadDir(".")
	if err != nil {
		loadErr = err
		return
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := files.ReadFile(e.Name())
		if err != nil {
			loadErr = err
			return
		}
		p, err := Parse(raw)
		if err != nil {
			loadErr = fmt.Errorf("%s: %w", e.Name(), err)
			return
		}
		if len(p.MetaProblems()) == 0 && p.Name+".json" == e.Name() {
			loaded[p.Name] = p
		}
	}
}

// Get trả hợp đồng nhúng theo tên. Hợp đồng có vấn đề siêu dữ liệu coi như không có (mặc định từ
// chối). Gọi Servable trước khi phục vụ.
func Get(name string) (*Product, error) {
	loadOnce.Do(load)
	if loadErr != nil {
		return nil, loadErr
	}
	p, ok := loaded[name]
	if !ok {
		return nil, fmt.Errorf("%w: không có hợp đồng sản phẩm dữ liệu %q hợp lệ", publication.ErrDenied, name)
	}
	return p, nil
}

// All trả mọi hợp đồng hợp lệ, sắp theo tên.
func All() ([]*Product, error) {
	loadOnce.Do(load)
	if loadErr != nil {
		return nil, loadErr
	}
	out := make([]*Product, 0, len(loaded))
	for _, p := range loaded {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
