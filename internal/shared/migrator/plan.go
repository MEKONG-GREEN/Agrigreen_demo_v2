// Package migrator là bộ chạy migration v2 (W7.1, DB-12, R-DB-12, ADR-0038): lịch sử migration theo
// module trong chính schema của module (<schema>.schema_migrations), thứ tự topo theo đồ thị depends của
// module.yaml, và một tệp là "chờ áp" khi và chỉ khi nó VẮNG trong sổ — số nhỏ hơn số đã áp không còn bị bỏ
// qua im lặng như golang-migrate (kế hoạch v4.1 §1 mục 6).
//
// Bố cục tệp giữ nguyên migrations/<cụm>/NNNNNN_*.up.sql (đánh số chung mỗi cụm); mỗi tệp được GẮN cho một
// module theo cùng quy tắc với agmodgraph (tools/archlint): tiền tố tên <cụm>_<module>, nếu không thì suy từ
// schema nó đụng (chỉ đụng schema của MỘT module), còn lại là module giả "<cụm>" (tệp toàn cụm — nhiều module
// hoặc không module, vd vai nhóm, view công bố). Thư mục shared/ là module giả "shared" (thư viện
// internal/shared) và luôn chạy trước. Thư mục theo module là bước sau (ADR-0038 §5).
package migrator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// SharedDir là thư mục migration của thư viện internal/shared, áp trước thư mục cụm ở mọi database.
const SharedDir = "shared"

// Module là phần module.yaml bộ chạy cần.
type Module struct {
	Name       string
	Kind       modreg.Kind
	Depends    []string
	OwnsSchema []string
}

// CatalogModules trả mọi module từ danh mục sinh (modreg, W1.4) — nhị phân không mang module.yaml.
func CatalogModules() []Module {
	var out []Module
	for _, e := range modreg.Catalog() {
		out = append(out, Module{Name: e.Name, Kind: e.Kind, Depends: modreg.Depends(e.Name), OwnsSchema: e.OwnsSchema})
	}
	return out
}

// File là một tệp migration đã gắn module.
type File struct {
	Dir     string // "shared" hoặc tên cụm
	Name    string // NNNNNN_ten.up.sql
	Version int64
	// Owner: module chủ (vd "pf/party"), hoặc module giả "shared" / "<cụm>" (Pseudo).
	Owner  string
	Pseudo bool
	// LedgerSchemas: schema ứng viên chứa sổ của Owner theo thứ tự ưu tiên (owns_schema); rỗng → shared.
	LedgerSchemas []string
	Checksum      string // sha256 hex của nội dung (CRLF → LF)
	SQL           string
	touches       []string
	refs          []string
}

// Key là định danh của tệp trong sổ: "<thư mục>/<tên>".
func (f File) Key() string { return f.Dir + "/" + f.Name }

var nameRe = regexp.MustCompile(`^(\d{6})_([a-z0-9_]+)\.up\.sql$`)

// Checksum trả tổng kiểm của nội dung tệp (bỏ khác biệt CRLF/LF giữa máy checkout).
func Checksum(sql string) string {
	sum := sha256.Sum256([]byte(strings.ReplaceAll(sql, "\r\n", "\n")))
	return hex.EncodeToString(sum[:])
}

// LoadDir đọc mọi tệp *.up.sql của một thư mục và gắn module. Thứ tự trả: theo số.
func LoadDir(fsys fs.FS, dir string, mods []Module) ([]File, error) {
	names, err := fs.Glob(fsys, dir+"/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	owner := schemaOwners(mods)
	var out []File
	seen := map[int64]string{}
	for _, p := range names {
		base := path.Base(p)
		m := nameRe.FindStringSubmatch(base)
		if m == nil {
			return nil, fmt.Errorf("migrator: %s: tên tệp phải là NNNNNN_ten.up.sql", p)
		}
		v, _ := strconv.ParseInt(m[1], 10, 64)
		if prev, dup := seen[v]; dup {
			return nil, fmt.Errorf("migrator: %s: trùng số %06d với %s", p, v, prev)
		}
		seen[v] = base
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return nil, err
		}
		f := File{Dir: dir, Name: base, Version: v, SQL: string(data), Checksum: Checksum(string(data))}
		a := Analyze(string(data))
		f.touches, f.refs = a.Touches, a.Refs
		if dir == SharedDir {
			f.Owner, f.Pseudo = SharedDir, true
		} else {
			f.Owner = Attribute(dir, m[2], a.Touches, mods, owner)
			if f.Owner == "" {
				f.Owner, f.Pseudo = dir, true
			}
		}
		if !f.Pseudo {
			for _, mod := range mods {
				if mod.Name == f.Owner {
					f.LedgerSchemas = slices.Clone(mod.OwnsSchema)
				}
			}
		}
		out = append(out, f)
	}
	return out, nil
}

func schemaOwners(mods []Module) map[string]string {
	owner := map[string]string{}
	for _, m := range mods {
		for _, s := range m.OwnsSchema {
			owner[s] = m.Name
		}
	}
	return owner
}

// Attribute gắn tệp (stem = phần tên sau số) cho module — cùng quy tắc agmodgraph (tools/archlint/modgraph.go):
// module dài nhất có <zone>_<đoạn> là tiền tố của stem; nếu không, module DUY NHẤT sở hữu các schema tệp đụng;
// "" nếu không xác định được (tệp toàn cụm).
func Attribute(dir, stem string, touches []string, mods []Module, owner map[string]string) string {
	best := ""
	for _, m := range mods {
		key := strings.Replace(m.Name, "/", "_", 1)
		if (stem == key || strings.HasPrefix(stem, key+"_")) && len(m.Name) > len(best) {
			best = m.Name
		}
	}
	if best != "" {
		return best
	}
	only := ""
	for _, s := range touches {
		o := owner[s]
		if o == "" {
			continue
		}
		if only != "" && only != o {
			return ""
		}
		only = o
	}
	return only
}

const sqlIdent = `"?([a-z_][a-z0-9_]*)"?\s*\.\s*"?[a-z_]`

var (
	reCreateSchema = regexp.MustCompile(`(?i)\bCREATE\s+SCHEMA\s+(?:IF\s+NOT\s+EXISTS\s+)?"?([a-z_][a-z0-9_]*)`)
	// reTouch: câu DDL tạo/sửa/xóa đối tượng trong một schema (nhóm 1 là schema) — giống agmodgraph.
	reTouch = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:UNLOGGED\s+)?(?:TABLE|VIEW|MATERIALIZED\s+VIEW|FUNCTION|PROCEDURE|SEQUENCE|TYPE|DOMAIN|AGGREGATE)\s+(?:IF\s+NOT\s+EXISTS\s+)?` + sqlIdent),
		regexp.MustCompile(`(?i)\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(?:[a-z_][a-z0-9_]*\s+)?ON\s+(?:ONLY\s+)?` + sqlIdent),
		regexp.MustCompile(`(?i)\bCREATE\s+(?:OR\s+REPLACE\s+)?(?:CONSTRAINT\s+)?TRIGGER\s+[a-z_][a-z0-9_]*\s+[^;]*?\bON\s+` + sqlIdent),
		regexp.MustCompile(`(?i)\bCREATE\s+POLICY\s+[a-z_][a-z0-9_]*\s+ON\s+` + sqlIdent),
		regexp.MustCompile(`(?i)\bALTER\s+(?:TABLE|VIEW|MATERIALIZED\s+VIEW|FUNCTION|PROCEDURE|SEQUENCE|TYPE|DOMAIN|INDEX)\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?` + sqlIdent),
		regexp.MustCompile(`(?i)\bDROP\s+(?:TABLE|VIEW|MATERIALIZED\s+VIEW|FUNCTION|PROCEDURE|SEQUENCE|TYPE|DOMAIN|INDEX)\s+(?:CONCURRENTLY\s+)?(?:IF\s+EXISTS\s+)?` + sqlIdent),
		regexp.MustCompile(`(?i)\bDROP\s+(?:TRIGGER|POLICY)\s+(?:IF\s+EXISTS\s+)?[a-z_][a-z0-9_]*\s+ON\s+` + sqlIdent),
		regexp.MustCompile(`(?i)\bCOMMENT\s+ON\s+(?:TABLE|COLUMN|VIEW|MATERIALIZED\s+VIEW|FUNCTION|INDEX|SEQUENCE|TYPE)\s+` + sqlIdent),
		regexp.MustCompile(`(?i)\b(?:ALTER|DROP)\s+SCHEMA\s+(?:IF\s+EXISTS\s+)?"?([a-z_][a-z0-9_]*)`),
	}
	// reSetSchema: ALTER TABLE|SEQUENCE|FUNCTION a.x SET SCHEMA b — chỉ tính đụng schema đích (W3.3).
	reSetSchema    = regexp.MustCompile(`(?i)\bALTER\s+(?:TABLE|SEQUENCE|FUNCTION)\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?"?[a-z_][a-z0-9_]*"?\s*\.\s*"?[a-z_][a-z0-9_]*"?\s*(?:\([^)]*\))?\s+SET\s+SCHEMA\s+"?([a-z_][a-z0-9_]*)"?`)
	reRef          = regexp.MustCompile(`(?i)\b([a-z_][a-z0-9_]*)"?\s*\.\s*"?[a-z_]`)
	reLineComment  = regexp.MustCompile(`--[^\n]*`)
	reBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// Analysis là kết quả phân tích tĩnh một tệp.
type Analysis struct {
	Creates []string // CREATE SCHEMA
	Touches []string // schema có đối tượng bị tạo/sửa/xóa (gồm Creates và đích SET SCHEMA)
	MoveTo  []string // đích ALTER … SET SCHEMA
	Refs    []string // mọi tiền tố "x." (ứng viên schema được tham chiếu — lọc theo schema đã biết khi xếp thứ tự)
}

// Analyze phân tích tĩnh SQL (bỏ chú thích) — cùng biểu thức với agmodgraph.
func Analyze(src string) Analysis {
	sql := reBlockComment.ReplaceAllString(src, " ")
	sql = reLineComment.ReplaceAllString(sql, " ")
	var a Analysis
	for _, m := range reCreateSchema.FindAllStringSubmatch(sql, -1) {
		a.Creates = appendUnique(a.Creates, strings.ToLower(m[1]))
	}
	for _, m := range reRef.FindAllStringSubmatch(sql, -1) {
		a.Refs = appendUnique(a.Refs, strings.ToLower(m[1]))
	}
	sql = reSetSchema.ReplaceAllStringFunc(sql, func(s string) string {
		if m := reSetSchema.FindStringSubmatch(s); m != nil {
			a.MoveTo = appendUnique(a.MoveTo, strings.ToLower(m[1]))
		}
		return " "
	})
	a.Touches = slices.Clone(a.Creates)
	for _, s := range a.MoveTo {
		a.Touches = appendUnique(a.Touches, s)
	}
	for _, re := range reTouch {
		for _, m := range re.FindAllStringSubmatch(sql, -1) {
			a.Touches = appendUnique(a.Touches, strings.ToLower(m[1]))
		}
	}
	for _, s := range a.Touches {
		a.Refs = appendUnique(a.Refs, s)
	}
	return a
}

func appendUnique(xs []string, x string) []string {
	if slices.Contains(xs, x) {
		return xs
	}
	return append(xs, x)
}

// Plan là thứ tự áp đầy đủ cho một database (cụm): shared/ trước, rồi thư mục cụm.
type Plan struct {
	Cluster migrations.Cluster
	Files   []File
}

// BuildPlan dựng kế hoạch cho cụm c từ fsys (thường là migrations.FS).
func BuildPlan(fsys fs.FS, c migrations.Cluster, mods []Module) (*Plan, error) {
	shared, err := LoadDir(fsys, SharedDir, mods)
	if err != nil {
		return nil, err
	}
	own, err := LoadDir(fsys, string(c), mods)
	if err != nil {
		return nil, err
	}
	p := &Plan{Cluster: c}
	p.Files = append(p.Files, Order(shared, mods)...)
	p.Files = append(p.Files, Order(own, mods)...)
	return p, nil
}

// Ranks trả hạng topo của mỗi module: 0 cho module không phụ thuộc, còn lại 1 + hạng lớn nhất của module nó
// phụ thuộc. Chu trình depends (còn ngoại lệ agmodgraph) được gộp thành một thành phần liên thông mạnh cùng hạng.
func Ranks(mods []Module) map[string]int {
	idx := map[string]int{}
	for i, m := range mods {
		idx[m.Name] = i
	}
	// Tarjan SCC.
	n := len(mods)
	index, low, comp := make([]int, n), make([]int, n), make([]int, n)
	for i := range index {
		index[i], comp[i] = -1, -1
	}
	var stack []int
	onStack := make([]bool, n)
	next, ncomp := 0, 0
	var strong func(v int)
	strong = func(v int) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, d := range mods[v].Depends {
			w, ok := idx[d]
			if !ok {
				continue
			}
			if index[w] < 0 {
				strong(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] == index[v] {
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp[w] = ncomp
				if w == v {
					break
				}
			}
			ncomp++
		}
	}
	for v := range mods {
		if index[v] < 0 {
			strong(v)
		}
	}
	// Tarjan đánh số thành phần theo thứ tự topo ngược (phụ thuộc trước) — tính hạng theo thứ tự đó.
	rank := make([]int, ncomp)
	members := make([][]int, ncomp)
	for v := range mods {
		members[comp[v]] = append(members[comp[v]], v)
	}
	for c := 0; c < ncomp; c++ {
		r := 0
		for _, v := range members[c] {
			for _, d := range mods[v].Depends {
				if w, ok := idx[d]; ok && comp[w] != c {
					r = max(r, rank[comp[w]]+1)
				}
			}
		}
		rank[c] = r
	}
	out := map[string]int{}
	for v, m := range mods {
		out[m.Name] = rank[comp[v]]
	}
	return out
}

func kindOrder(k modreg.Kind) int {
	switch k {
	case modreg.KindKernel:
		return 0
	case modreg.KindCapability:
		return 1
	case modreg.KindDomain:
		return 2
	}
	return 3
}

// closure trả tập module mà mỗi module phụ thuộc (bắc cầu).
func closure(mods []Module) map[string]map[string]bool {
	deps := map[string][]string{}
	for _, m := range mods {
		deps[m.Name] = m.Depends
	}
	out := map[string]map[string]bool{}
	for _, m := range mods {
		seen := map[string]bool{}
		st := slices.Clone(m.Depends)
		for len(st) > 0 {
			d := st[len(st)-1]
			st = st[:len(st)-1]
			if seen[d] {
				continue
			}
			seen[d] = true
			st = append(st, deps[d]...)
		}
		out[m.Name] = seen
	}
	return out
}

// Order xếp các tệp CỦA MỘT thư mục theo thứ tự topo module, giữ mọi ràng buộc lịch sử:
//
//  1. cùng module: theo số;
//  2. tệp toàn cụm (module giả): rào chắn — mọi tệp số nhỏ hơn trước nó, mọi tệp số lớn hơn sau nó;
//  3. A phụ thuộc (bắc cầu) B: tệp của B có số nhỏ hơn đứng trước tệp của A;
//  4. tệp g tham chiếu schema s (tiền tố "s." trong SQL) mà tệp f số nhỏ hơn đã đụng: f trước g.
//
// Mọi cạnh đi từ số nhỏ tới số lớn nên đồ thị không chu trình và thứ tự số luôn là một thứ tự hợp lệ (đúng thứ
// tự golang-migrate đã áp). Trong các tệp sẵn sàng, chọn theo (hạng topo module, tầng kind, tên module, số):
// k/* trước cap/* trước dom/* khi lịch sử cho phép.
func Order(files []File, mods []Module) []File {
	n := len(files)
	if n == 0 {
		return nil
	}
	rank := Ranks(mods)
	kind := map[string]modreg.Kind{}
	for _, m := range mods {
		kind[m.Name] = m.Kind
	}
	dep := closure(mods)
	known := map[string]bool{}
	for _, m := range mods {
		for _, s := range m.OwnsSchema {
			known[s] = true
		}
	}
	for _, f := range files {
		for _, s := range f.touches {
			known[s] = true
		}
	}
	indeg := make([]int, n)
	succ := make([][]int, n)
	for i := range files {
		for j := range files {
			if files[i].Version >= files[j].Version {
				continue
			}
			f, g := files[i], files[j]
			edge := f.Owner == g.Owner || f.Pseudo || g.Pseudo || dep[g.Owner][f.Owner]
			if !edge {
				for _, s := range g.refs {
					if known[s] && slices.Contains(f.touches, s) {
						edge = true
						break
					}
				}
			}
			if edge {
				succ[i] = append(succ[i], j)
				indeg[j]++
			}
		}
	}
	less := func(a, b File) bool {
		ka := []int{rank[a.Owner], kindOrder(kind[a.Owner])}
		kb := []int{rank[b.Owner], kindOrder(kind[b.Owner])}
		if a.Pseudo {
			ka = []int{-1, -1}
		}
		if b.Pseudo {
			kb = []int{-1, -1}
		}
		if c := slices.Compare(ka, kb); c != 0 {
			return c < 0
		}
		if a.Owner != b.Owner {
			return a.Owner < b.Owner
		}
		return a.Version < b.Version
	}
	done := make([]bool, n)
	out := make([]File, 0, n)
	for len(out) < n {
		pick := -1
		for i := range files {
			if !done[i] && indeg[i] == 0 && (pick < 0 || less(files[i], files[pick])) {
				pick = i
			}
		}
		done[pick] = true
		out = append(out, files[pick])
		for _, j := range succ[pick] {
			indeg[j]--
		}
	}
	return out
}
