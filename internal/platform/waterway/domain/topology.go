package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
)

// SegmentInput là một đoạn ĐÃ XÁC NHẬN đưa vào bộ dựng (bản hiện hành tại as_of).
type SegmentInput struct {
	Ref          uuid.UUID
	RevisionID   uuid.UUID
	Revision     int
	RowHash      string // hex row_hash của bản sửa — vào băm tập đoạn
	Name         string
	Class        string
	DataSource   string
	LengthM      decimal.Decimal
	Points       []Point
	WidthM       *decimal.Decimal
	DepthM       *decimal.Decimal
	DeclaredTons *decimal.Decimal
}

// StructureInput là một công trình đang dùng gắn với đoạn.
type StructureInput struct {
	Ref              uuid.UUID
	RevisionID       uuid.UUID
	RowHash          string
	Kind             string
	Name             string
	SegmentRef       uuid.UUID
	At               Point
	ClearanceHeightM *decimal.Decimal
	ClearanceWidthM  *decimal.Decimal
	DepthM           *decimal.Decimal
}

// Vertex là một đỉnh của đồ thị (id bigint cho pgRouting).
type Vertex struct {
	ID     int64
	At     Point
	Degree int
}

// Edge là một cạnh: phần của một đoạn giữa hai đỉnh liên tiếp.
type Edge struct {
	ID, Source, Target int64
	Points             []Point
	SegmentRef         uuid.UUID
	SegmentRevisionID  uuid.UUID
	DataSource         string
	Constraints        Constraints
	Capacity           Capacity
}

// Loại phát hiện khi dựng.
const (
	FindingCrossing    = "crossing"     // hai tuyến cắt nhau không có đỉnh chung — đã chèn đỉnh
	FindingTJoin       = "t_join"       // đầu tuyến nằm trên thân tuyến kia — đã chèn đỉnh nối
	FindingGapJoined   = "gap_joined"   // khe hở ≤ ngưỡng nối tự động — đã nối
	FindingGapReported = "gap_reported" // khe hở > ngưỡng nối tự động, ≤ ngưỡng báo cáo — chỉ báo cáo
	FindingDangling    = "dangling"     // đỉnh chỉ nối một cạnh (đầu tuyến treo)
	FindingIsolated    = "isolated"     // tuyến không nối với tuyến nào khác
)

// Finding là một phát hiện khi dựng.
type Finding struct {
	ID          int64
	Kind        string
	SegmentRefs []uuid.UUID
	At          Point
	GapM        *decimal.Decimal
	AutoFixed   bool
	Message     string
}

// SegmentSummary là một đoạn đã vào phiên bản kèm tải trọng bất lợi nhất.
type SegmentSummary struct {
	Input     SegmentInput
	Capacity  Capacity
	EdgeCount int
}

// Graph là kết quả dựng — hàm thuần của (tập đoạn, tập công trình, tham số).
type Graph struct {
	Vertices         []Vertex
	Edges            []Edge
	Findings         []Finding
	Segments         []SegmentSummary
	Components       int
	StructureCount   int
	SegmentSetSHA256 string
	GraphSHA256      string
}

// SegmentSetHash băm tập đoạn + công trình + tham số dựng: cùng đầu vào → cùng
// băm, bất kể thứ tự.
func SegmentSetHash(segs []SegmentInput, structs []StructureInput, p BuildParams) (string, error) {
	type ref struct {
		Ref        string `json:"ref"`
		RevisionID string `json:"revision_id"`
		RowHash    string `json:"row_hash"`
	}
	var ss, st []ref
	for _, s := range segs {
		ss = append(ss, ref{s.Ref.String(), s.RevisionID.String(), s.RowHash})
	}
	for _, s := range structs {
		st = append(st, ref{s.Ref.String(), s.RevisionID.String(), s.RowHash})
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].Ref < ss[j].Ref })
	sort.Slice(st, func(i, j int) bool { return st[i].Ref < st[j].Ref })
	vs := append([]VesselClass(nil), p.Vessels...)
	SortVessels(vs)
	p.Vessels = vs
	b, err := json.Marshal(struct {
		RuleVersion string      `json:"rule_version"`
		Params      BuildParams `json:"params"`
		Segments    []ref       `json:"segments"`
		Structures  []ref       `json:"structures"`
	}{RuleVersion, p, ss, st})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ---------------------------------------------------------------------------
// Bộ dựng

type insertion struct {
	piece int
	t     float64
	x, y  float64
	id    int // chỉ số trong danh sách chèn của đoạn, để tra khóa điểm sau khi trộn
}

type builder struct {
	p        BuildParams
	snap     float64
	joinMax  float64
	report   float64
	onSeg    float64
	proj     projection
	segs     []SegmentInput
	xy       [][][2]float64 // tọa độ phẳng của đỉnh gốc từng đoạn
	ins      [][]insertion
	forced   [][2]pointRef // cặp điểm (đoạn, chèn/đỉnh) buộc hợp nhất
	findings []Finding

	// sau khi trộn
	keys    [][]int // khóa điểm toàn cục của từng đoạn theo thứ tự
	px, py  []float64
	ll      []Point
	seg     []int
	node    []bool
	parent  []int
	insKey  [][]int // khóa của từng điểm chèn
	origKey [][]int // khóa của từng đỉnh gốc
}

type pointRef struct {
	seg    int
	vertex int // ≥ 0: đỉnh gốc; -1: dùng insert
	insert int
}

func (b *builder) find(k int) int {
	for b.parent[k] != k {
		b.parent[k] = b.parent[b.parent[k]]
		k = b.parent[k]
	}
	return k
}

func (b *builder) union(a, c int) {
	ra, rc := b.find(a), b.find(c)
	if ra == rc {
		return
	}
	// Gốc là khóa nhỏ hơn → đại diện tất định.
	if rc < ra {
		ra, rc = rc, ra
	}
	b.parent[rc] = ra
}

func dec2(v float64) *decimal.Decimal {
	d := decimal.NewFromFloat(v).Round(2)
	return &d
}

func (b *builder) segName(i int) string { return b.segs[i].Name }

// Build dựng đồ thị tất định từ tập đoạn đã xác nhận (quy tắc prototype
// gis/waterwayNetwork.ts): chèn đỉnh tại giao cắt và nối chữ T, gộp điểm trong
// ngưỡng snap, nối khe hở ≤ ngưỡng nối tự động, báo cáo khe hở tới ngưỡng báo
// cáo; tách đoạn thành cạnh tại đỉnh; suy tải trọng từng cạnh theo quy tắc
// phương tiện với công trình vượt sông.
func Build(segs []SegmentInput, structs []StructureInput, p BuildParams) (Graph, error) {
	if err := p.Validate(); err != nil {
		return Graph{}, err
	}
	setHash, err := SegmentSetHash(segs, structs, p)
	if err != nil {
		return Graph{}, err
	}
	b := &builder{p: p}
	b.snap = p.Tolerances.SnapM.InexactFloat64()
	b.joinMax = p.Tolerances.AutoJoinMaxM.InexactFloat64()
	b.report = p.Tolerances.ReportGapMaxM.InexactFloat64()
	b.onSeg = p.Tolerances.OnSegmentM.InexactFloat64()

	// Thứ tự tất định: theo segment_ref; bỏ đỉnh trùng liên tiếp.
	b.segs = append([]SegmentInput(nil), segs...)
	sort.Slice(b.segs, func(i, j int) bool { return bytes.Compare(b.segs[i].Ref[:], b.segs[j].Ref[:]) < 0 })
	var sumLat, sumLng float64
	n := 0
	for i := range b.segs {
		var pts []Point
		for _, pt := range b.segs[i].Points {
			if len(pts) == 0 || pts[len(pts)-1] != pt {
				pts = append(pts, pt)
			}
		}
		b.segs[i].Points = pts
		for _, pt := range pts {
			sumLat += pt.Lat
			sumLng += pt.Lng
			n++
		}
	}
	g := Graph{SegmentSetSHA256: setHash}
	if n == 0 {
		g.GraphSHA256 = graphHash(g)
		return g, nil
	}
	b.proj = newProjection(Point{Lat: sumLat / float64(n), Lng: sumLng / float64(n)})
	b.xy = make([][][2]float64, len(b.segs))
	b.ins = make([][]insertion, len(b.segs))
	for i, s := range b.segs {
		for _, pt := range s.Points {
			x, y := b.proj.xy(pt)
			b.xy[i] = append(b.xy[i], [2]float64{x, y})
		}
	}

	b.nodeCrossings()
	b.nodeTJoins()
	b.merge()
	b.snapPoints()
	b.joinGaps()
	return b.emit(g, structs)
}

func bbox(pts [][2]float64, pad float64) [4]float64 {
	r := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range pts {
		r[0], r[1] = math.Min(r[0], p[0]), math.Min(r[1], p[1])
		r[2], r[3] = math.Max(r[2], p[0]), math.Max(r[3], p[1])
	}
	return [4]float64{r[0] - pad, r[1] - pad, r[2] + pad, r[3] + pad}
}

func overlaps(a, c [4]float64) bool {
	return a[0] <= c[2] && c[0] <= a[2] && a[1] <= c[3] && c[1] <= a[3]
}

// nearAny: điểm (x, y) cách một đỉnh gốc hoặc điểm đã chèn của đoạn i ≤ tol.
func (b *builder) nearAny(i int, x, y, tol float64) bool {
	for _, p := range b.xy[i] {
		if math.Hypot(p[0]-x, p[1]-y) <= tol {
			return true
		}
	}
	for _, in := range b.ins[i] {
		if math.Hypot(in.x-x, in.y-y) <= tol {
			return true
		}
	}
	return false
}

func (b *builder) nearEndpoint(i int, x, y, tol float64) bool {
	pts := b.xy[i]
	return math.Hypot(pts[0][0]-x, pts[0][1]-y) <= tol || math.Hypot(pts[len(pts)-1][0]-x, pts[len(pts)-1][1]-y) <= tol
}

func (b *builder) addInsert(i, piece int, t, x, y float64) int {
	id := len(b.ins[i])
	b.ins[i] = append(b.ins[i], insertion{piece: piece, t: t, x: x, y: y, id: id})
	return id
}

func (b *builder) addFinding(kind string, refs []uuid.UUID, x, y float64, gap *decimal.Decimal, fixed bool, msg string) {
	sorted := append([]uuid.UUID(nil), refs...)
	sort.Slice(sorted, func(i, j int) bool { return bytes.Compare(sorted[i][:], sorted[j][:]) < 0 })
	b.findings = append(b.findings, Finding{Kind: kind, SegmentRefs: sorted, At: b.proj.inv(x, y), GapM: gap, AutoFixed: fixed, Message: msg})
}

// nodeCrossings: hai tuyến cắt nhau giữa đoạn mà chưa có đỉnh chung → chèn giao điểm vào cả hai.
func (b *builder) nodeCrossings() {
	boxes := make([][4]float64, len(b.segs))
	for i := range b.segs {
		boxes[i] = bbox(b.xy[i], b.snap)
	}
	for i := 0; i < len(b.segs); i++ {
		for j := i + 1; j < len(b.segs); j++ {
			if !overlaps(boxes[i], boxes[j]) {
				continue
			}
			pa, pb := b.xy[i], b.xy[j]
			for m := 1; m < len(pa); m++ {
				for k := 1; k < len(pb); k++ {
					t, u, ok := intersect(pa[m-1][0], pa[m-1][1], pa[m][0], pa[m][1], pb[k-1][0], pb[k-1][1], pb[k][0], pb[k][1])
					if !ok {
						continue
					}
					x := pa[m-1][0] + t*(pa[m][0]-pa[m-1][0])
					y := pa[m-1][1] + t*(pa[m][1]-pa[m-1][1])
					inI := !b.nearAny(i, x, y, b.snap)
					inJ := !b.nearAny(j, x, y, b.snap)
					if !inI && !inJ {
						continue // đã có đỉnh chung (trong ngưỡng snap)
					}
					if inI {
						b.addInsert(i, m-1, t, x, y)
					}
					if inJ {
						b.addInsert(j, k-1, u, x, y)
					}
					refs := []uuid.UUID{b.segs[i].Ref, b.segs[j].Ref}
					if b.nearEndpoint(i, x, y, b.snap) || b.nearEndpoint(j, x, y, b.snap) {
						b.addFinding(FindingTJoin, refs, x, y, dec2(0), true,
							fmt.Sprintf("Đầu tuyến %s/%s chạm thân tuyến kia nhưng thiếu đỉnh nối — đã chèn đỉnh.", b.segName(i), b.segName(j)))
					} else {
						b.addFinding(FindingCrossing, refs, x, y, nil, true,
							fmt.Sprintf("%s và %s cắt nhau nhưng không có đỉnh chung — đã chèn đỉnh tại giao điểm.", b.segName(i), b.segName(j)))
					}
				}
			}
		}
	}
}

// nodeTJoins: đầu tuyến cách thân tuyến khác ≤ ngưỡng nối chữ T (chưa chạm) → chèn
// điểm chiếu vào tuyến kia và buộc hợp nhất với đầu tuyến.
func (b *builder) nodeTJoins() {
	boxes := make([][4]float64, len(b.segs))
	for i := range b.segs {
		boxes[i] = bbox(b.xy[i], b.onSeg)
	}
	for j := range b.segs {
		pts := b.xy[j]
		for _, vi := range []int{0, len(pts) - 1} {
			ex, ey := pts[vi][0], pts[vi][1]
			for i := range b.segs {
				if i == j || ex < boxes[i][0] || ex > boxes[i][2] || ey < boxes[i][1] || ey > boxes[i][3] {
					continue
				}
				if b.nearAny(i, ex, ey, b.snap) {
					continue // gộp bằng snap
				}
				best, bestPiece, bestT := math.Inf(1), -1, 0.0
				var cx, cy float64
				for m := 1; m < len(b.xy[i]); m++ {
					a, c := b.xy[i][m-1], b.xy[i][m]
					d, t, x, y := projectOnPiece(ex, ey, a[0], a[1], c[0], c[1])
					if d < best {
						best, bestPiece, bestT, cx, cy = d, m-1, t, x, y
					}
				}
				if bestPiece < 0 || best > b.onSeg {
					continue
				}
				id := b.addInsert(i, bestPiece, bestT, cx, cy)
				b.forced = append(b.forced, [2]pointRef{{seg: j, vertex: vi}, {seg: i, vertex: -1, insert: id}})
				b.addFinding(FindingTJoin, []uuid.UUID{b.segs[i].Ref, b.segs[j].Ref}, cx, cy, dec2(best), true,
					fmt.Sprintf("Đầu tuyến %s nằm sát thân tuyến %s (cách %.1f m) nhưng thiếu đỉnh nối — đã chèn đỉnh.", b.segName(j), b.segName(i), best))
			}
		}
	}
}

// merge trộn điểm chèn vào đỉnh gốc theo thứ tự trên tuyến và cấp khóa toàn cục.
func (b *builder) merge() {
	b.keys = make([][]int, len(b.segs))
	b.insKey = make([][]int, len(b.segs))
	b.origKey = make([][]int, len(b.segs))
	add := func(i int, x, y float64, ll Point) int {
		k := len(b.px)
		b.px = append(b.px, x)
		b.py = append(b.py, y)
		b.ll = append(b.ll, ll)
		b.seg = append(b.seg, i)
		b.node = append(b.node, false)
		b.parent = append(b.parent, k)
		b.keys[i] = append(b.keys[i], k)
		return k
	}
	for i := range b.segs {
		ins := append([]insertion(nil), b.ins[i]...)
		sort.SliceStable(ins, func(a, c int) bool {
			if ins[a].piece != ins[c].piece {
				return ins[a].piece < ins[c].piece
			}
			return ins[a].t < ins[c].t
		})
		b.insKey[i] = make([]int, len(b.ins[i]))
		pts := b.xy[i]
		next := 0
		for v := range pts {
			b.origKey[i] = append(b.origKey[i], add(i, pts[v][0], pts[v][1], b.segs[i].Points[v]))
			// Điểm chèn trên mảnh v → v+1.
			for next < len(ins) && ins[next].piece == v {
				in := ins[next]
				next++
				last := b.keys[i][len(b.keys[i])-1]
				if math.Hypot(b.px[last]-in.x, b.py[last]-in.y) < 1e-6 {
					b.node[last] = true
					b.insKey[i][in.id] = last
					continue
				}
				if v+1 < len(pts) && math.Hypot(pts[v+1][0]-in.x, pts[v+1][1]-in.y) < 1e-6 {
					b.insKey[i][in.id] = -(v + 2) // trỏ tới đỉnh gốc v+1, gán sau
					continue
				}
				kk := add(i, in.x, in.y, b.proj.inv(in.x, in.y))
				b.node[kk] = true
				b.insKey[i][in.id] = kk
			}
		}
		// Giải các điểm chèn trùng đỉnh gốc phía sau.
		for id, kk := range b.insKey[i] {
			if kk < 0 {
				v := -kk - 2
				key := b.vertexKey(i, v)
				b.node[key] = true
				b.insKey[i][id] = key
			}
		}
		// Đầu và cuối tuyến luôn là đỉnh.
		b.node[b.keys[i][0]] = true
		b.node[b.keys[i][len(b.keys[i])-1]] = true
	}
}

// vertexKey trả khóa toàn cục của đỉnh gốc v của đoạn i.
func (b *builder) vertexKey(i, v int) int { return b.origKey[i][v] }

func (b *builder) refKey(r pointRef) int {
	if r.vertex >= 0 {
		return b.vertexKey(r.seg, r.vertex)
	}
	return b.insKey[r.seg][r.insert]
}

type cell struct{ x, y int64 }

// snapPoints gộp điểm của các tuyến KHÁC NHAU cách nhau ≤ ngưỡng snap thành một đỉnh.
func (b *builder) snapPoints() {
	grid := map[cell][]int{}
	c := func(k int) cell {
		return cell{int64(math.Floor(b.px[k] / b.snap)), int64(math.Floor(b.py[k] / b.snap))}
	}
	for k := range b.px {
		grid[c(k)] = append(grid[c(k)], k)
	}
	for k := range b.px {
		ck := c(k)
		for dx := int64(-1); dx <= 1; dx++ {
			for dy := int64(-1); dy <= 1; dy++ {
				for _, q := range grid[cell{ck.x + dx, ck.y + dy}] {
					if q <= k || b.seg[q] == b.seg[k] {
						continue
					}
					if math.Hypot(b.px[q]-b.px[k], b.py[q]-b.py[k]) <= b.snap {
						b.node[k], b.node[q] = true, true
						b.union(k, q)
					}
				}
			}
		}
	}
	for _, f := range b.forced {
		a, c := b.refKey(f[0]), b.refKey(f[1])
		b.node[a], b.node[c] = true, true
		b.union(a, c)
	}
}

// joinGaps: hai tuyến chưa có đỉnh chung, khe hở giữa hai đầu tuyến trong
// (snap, báo cáo] → báo cáo; ≤ ngưỡng nối tự động → nối (mỗi đầu tuyến nối tối đa một lần).
func (b *builder) joinGaps() {
	roots := make([]map[int]bool, len(b.segs))
	for i := range b.segs {
		roots[i] = map[int]bool{}
		for _, k := range b.keys[i] {
			if b.node[k] {
				roots[i][b.find(k)] = true
			}
		}
	}
	shares := func(i, j int) bool {
		for r := range roots[i] {
			if roots[j][r] {
				return true
			}
		}
		return false
	}
	type cand struct {
		i, j   int
		ki, kj int
		d      float64
	}
	var cands []cand
	ends := func(i int) []int { return []int{b.keys[i][0], b.keys[i][len(b.keys[i])-1]} }
	boxes := make([][4]float64, len(b.segs))
	for i := range b.segs {
		boxes[i] = bbox(b.xy[i], b.report)
	}
	for i := 0; i < len(b.segs); i++ {
		for j := i + 1; j < len(b.segs); j++ {
			if !overlaps(boxes[i], boxes[j]) || shares(i, j) {
				continue
			}
			best := cand{d: math.Inf(1)}
			for _, ki := range ends(i) {
				for _, kj := range ends(j) {
					d := math.Hypot(b.px[ki]-b.px[kj], b.py[ki]-b.py[kj])
					if d > b.snap && d < best.d {
						best = cand{i: i, j: j, ki: ki, kj: kj, d: d}
					}
				}
			}
			if best.d <= b.report {
				cands = append(cands, best)
			}
		}
	}
	sort.SliceStable(cands, func(a, c int) bool {
		if cands[a].d != cands[c].d {
			return cands[a].d < cands[c].d
		}
		if cands[a].i != cands[c].i {
			return cands[a].i < cands[c].i
		}
		return cands[a].j < cands[c].j
	})
	used := map[int]bool{}
	for _, c := range cands {
		mx, my := (b.px[c.ki]+b.px[c.kj])/2, (b.py[c.ki]+b.py[c.kj])/2
		refs := []uuid.UUID{b.segs[c.i].Ref, b.segs[c.j].Ref}
		gap := dec2(c.d)
		switch {
		case c.d <= b.joinMax && !used[c.ki] && !used[c.kj]:
			used[c.ki], used[c.kj] = true, true
			b.union(c.ki, c.kj)
			b.addFinding(FindingGapJoined, refs, mx, my, gap, true,
				fmt.Sprintf("Khe hở %.0f m giữa đầu tuyến %s và %s — lỗi số hóa, đã nối tự động.", c.d, b.segName(c.i), b.segName(c.j)))
		case c.d <= b.joinMax:
			b.addFinding(FindingGapReported, refs, mx, my, gap, false,
				fmt.Sprintf("Khe hở %.0f m giữa đầu tuyến %s và %s — đầu tuyến đã được nối với tuyến khác, không nối thêm.", c.d, b.segName(c.i), b.segName(c.j)))
		default:
			b.addFinding(FindingGapReported, refs, mx, my, gap, false,
				fmt.Sprintf("Khe hở %.0f m giữa đầu tuyến %s và %s vượt ngưỡng nối tự động %s m — rất có thể là hai con kênh khác nhau, không nối.",
					c.d, b.segName(c.i), b.segName(c.j), b.p.Tolerances.AutoJoinMaxM.String()))
		}
	}
}

func roundPt(p Point) Point { return Point{Lng: Round7(p.Lng), Lat: Round7(p.Lat)} }

// emit cấp id đỉnh/cạnh, suy tải trọng, tính phát hiện đầu treo/cô lập và băm.
func (b *builder) emit(g Graph, structs []StructureInput) (Graph, error) {
	vid := map[int]int64{} // gốc → id đỉnh
	for k := range b.px {
		if !b.node[k] {
			continue
		}
		r := b.find(k)
		if _, ok := vid[r]; ok {
			continue
		}
		vid[r] = int64(len(g.Vertices) + 1)
		g.Vertices = append(g.Vertices, Vertex{ID: vid[r], At: roundPt(b.ll[r])})
	}
	segEdges := make([][]int, len(b.segs))
	for i, s := range b.segs {
		keys := b.keys[i]
		start := keys[0]
		pts := []Point{roundPt(b.ll[b.find(start)])}
		for _, k := range keys[1:] {
			if !b.node[k] {
				pt := roundPt(b.ll[k])
				if pt != pts[len(pts)-1] {
					pts = append(pts, pt)
				}
				continue
			}
			end := roundPt(b.ll[b.find(k)])
			if end != pts[len(pts)-1] {
				pts = append(pts, end)
			}
			src, dst := vid[b.find(start)], vid[b.find(k)]
			if len(pts) >= 2 && (src != dst || len(pts) >= 3) {
				e := Edge{ID: int64(len(g.Edges) + 1), Source: src, Target: dst, Points: pts,
					SegmentRef: s.Ref, SegmentRevisionID: s.RevisionID, DataSource: s.DataSource}
				segEdges[i] = append(segEdges[i], len(g.Edges))
				g.Edges = append(g.Edges, e)
			}
			start = k
			pts = []Point{end}
		}
	}

	// Công trình: ràng buộc cạnh gần nhất của đoạn mang nó.
	segIdx := map[uuid.UUID]int{}
	for i, s := range b.segs {
		segIdx[s.Ref] = i
	}
	sorted := append([]StructureInput(nil), structs...)
	sort.Slice(sorted, func(i, j int) bool { return bytes.Compare(sorted[i].Ref[:], sorted[j].Ref[:]) < 0 })
	edgeStructs := map[int][]StructureInput{}
	for _, st := range sorted {
		i, ok := segIdx[st.SegmentRef]
		if !ok || len(segEdges[i]) == 0 {
			continue
		}
		g.StructureCount++
		sx, sy := b.proj.xy(st.At)
		best, bestE := math.Inf(1), -1
		for _, ei := range segEdges[i] {
			pts := g.Edges[ei].Points
			for m := 1; m < len(pts); m++ {
				ax, ay := b.proj.xy(pts[m-1])
				cx, cy := b.proj.xy(pts[m])
				if d, _, _, _ := projectOnPiece(sx, sy, ax, ay, cx, cy); d < best {
					best, bestE = d, ei
				}
			}
		}
		edgeStructs[bestE] = append(edgeStructs[bestE], st)
	}
	for ei := range g.Edges {
		s := b.segs[segIdx[g.Edges[ei].SegmentRef]]
		c := edgeConstraints(s, edgeStructs[ei])
		g.Edges[ei].Constraints = c
		g.Edges[ei].Capacity = Derive(c, s.DeclaredTons, b.p.Vessels, b.p.Safety)
	}
	for i, s := range b.segs {
		sum := SegmentSummary{Input: s, EdgeCount: len(segEdges[i])}
		if len(segEdges[i]) == 0 {
			sum.Capacity = Derive(edgeConstraints(s, nil), s.DeclaredTons, b.p.Vessels, b.p.Safety)
		} else {
			sum.Capacity = g.Edges[segEdges[i][0]].Capacity
			for _, ei := range segEdges[i][1:] {
				sum.Capacity = Worse(sum.Capacity, g.Edges[ei].Capacity)
			}
		}
		g.Segments = append(g.Segments, sum)
	}

	// Bậc đỉnh, cụm liên thông, đầu treo, tuyến cô lập.
	deg := make([]int, len(g.Vertices)+1)
	parent := make([]int64, len(g.Vertices)+1)
	for i := range parent {
		parent[i] = int64(i)
	}
	find := func(x int64) int64 {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for _, e := range g.Edges {
		deg[e.Source]++
		deg[e.Target]++
		ra, rb := find(e.Source), find(e.Target)
		if ra != rb {
			if rb < ra {
				ra, rb = rb, ra
			}
			parent[rb] = ra
		}
	}
	compSegs := map[int64]map[uuid.UUID]bool{}
	for _, e := range g.Edges {
		r := find(e.Source)
		if compSegs[r] == nil {
			compSegs[r] = map[uuid.UUID]bool{}
		}
		compSegs[r][e.SegmentRef] = true
	}
	g.Components = len(compSegs)
	vertexSeg := map[int64]uuid.UUID{}
	for _, e := range g.Edges {
		if _, ok := vertexSeg[e.Source]; !ok {
			vertexSeg[e.Source] = e.SegmentRef
		}
		if _, ok := vertexSeg[e.Target]; !ok {
			vertexSeg[e.Target] = e.SegmentRef
		}
	}
	for i := range g.Vertices {
		v := &g.Vertices[i]
		v.Degree = deg[v.ID]
		if v.Degree == 1 {
			x, y := b.proj.xy(v.At)
			b.addFinding(FindingDangling, []uuid.UUID{vertexSeg[v.ID]}, x, y, nil, false,
				fmt.Sprintf("Đỉnh %d chỉ nối một cạnh (đầu tuyến treo) — kiểm tra tuyến có thực sự kết thúc tại đây.", v.ID))
		}
	}
	if len(b.segs) > 1 {
		for _, e := range g.Edges {
			r := find(e.Source)
			if len(compSegs[r]) != 1 || compSegs[r][uuid.Nil] {
				continue
			}
			x, y := b.proj.xy(e.Points[0])
			b.addFinding(FindingIsolated, []uuid.UUID{e.SegmentRef}, x, y, nil, false,
				fmt.Sprintf("Tuyến %s không nối với tuyến nào khác — hub/nhà máy ở cụm khác sẽ không tìm được đường thủy qua tuyến này.",
					b.segs[segIdx[e.SegmentRef]].Name))
			compSegs[r][uuid.Nil] = true // chỉ báo một lần mỗi cụm
		}
	}
	for i := range b.findings {
		b.findings[i].ID = int64(i + 1)
		b.findings[i].At = roundPt(b.findings[i].At)
	}
	g.Findings = b.findings
	g.GraphSHA256 = graphHash(g)
	return g, nil
}

// edgeConstraints: rộng hẹp nhất, sâu cạn nhất, tĩnh không thấp nhất trên cạnh.
// Công trình không lấp chỗ thiếu số liệu của đoạn (không đoán); cầu/đường dây
// điện chưa đo tĩnh không và cống/âu chưa đo bề rộng làm cạnh CHƯA KẾT LUẬN được.
func edgeConstraints(s SegmentInput, structs []StructureInput) Constraints {
	c := Constraints{WidthM: s.WidthM, DepthM: s.DepthM, WidthFrom: "segment", DepthFrom: "segment"}
	widthUnknown, clearUnknown := false, false
	for _, st := range structs {
		ref := st.Ref.String()
		switch st.Kind {
		case port.StructureBridge, port.StructurePowerLine:
			c.ClearanceRequired = true
			if st.ClearanceHeightM == nil {
				clearUnknown = true
			} else if c.ClearanceM == nil || st.ClearanceHeightM.LessThan(*c.ClearanceM) {
				h := *st.ClearanceHeightM
				c.ClearanceM, c.ClearanceFrom = &h, ref
			}
		case port.StructureCulvert, port.StructureLock:
			if st.ClearanceWidthM == nil {
				widthUnknown = true
			}
			if st.ClearanceHeightM != nil {
				c.ClearanceRequired = true
				if c.ClearanceM == nil || st.ClearanceHeightM.LessThan(*c.ClearanceM) {
					h := *st.ClearanceHeightM
					c.ClearanceM, c.ClearanceFrom = &h, ref
				}
			}
		}
		if st.ClearanceWidthM != nil && c.WidthM != nil && st.ClearanceWidthM.LessThan(*c.WidthM) {
			w := *st.ClearanceWidthM
			c.WidthM, c.WidthFrom = &w, ref
		}
		if st.DepthM != nil && c.DepthM != nil && st.DepthM.LessThan(*c.DepthM) {
			d := *st.DepthM
			c.DepthM, c.DepthFrom = &d, ref
		}
	}
	if widthUnknown {
		c.WidthM, c.WidthFrom = nil, ""
	}
	if clearUnknown {
		c.ClearanceM, c.ClearanceFrom = nil, ""
	}
	return c
}

func fmtF(v float64) string { return strconv.FormatFloat(v, 'f', 7, 64) }

func decStr(d *decimal.Decimal) string {
	if d == nil {
		return "-"
	}
	return d.StringFixed(2)
}

// graphHash băm nội dung đồ thị theo dạng chuẩn (đỉnh, cạnh, tải trọng, phát hiện).
func graphHash(g Graph) string {
	var sb strings.Builder
	sb.WriteString(RuleVersion + "\n")
	for _, v := range g.Vertices {
		fmt.Fprintf(&sb, "v|%d|%s|%s|%d\n", v.ID, fmtF(v.At.Lng), fmtF(v.At.Lat), v.Degree)
	}
	for _, e := range g.Edges {
		fmt.Fprintf(&sb, "e|%d|%d|%d|%s|%s|%s|%s|%t|%s|%s|", e.ID, e.Source, e.Target, e.SegmentRef, e.SegmentRevisionID,
			decStr(e.Capacity.MaxLoadTons), decStr(e.Capacity.Derived), e.Capacity.Certain, e.Capacity.Limiting, e.Capacity.StructureRef)
		for _, p := range e.Points {
			sb.WriteString(fmtF(p.Lng) + " " + fmtF(p.Lat) + ",")
		}
		sb.WriteString("\n")
	}
	for _, f := range g.Findings {
		fmt.Fprintf(&sb, "f|%d|%s|%s|%s|%s|%t|", f.ID, f.Kind, fmtF(f.At.Lng), fmtF(f.At.Lat), decStr(f.GapM), f.AutoFixed)
		for _, r := range f.SegmentRefs {
			sb.WriteString(r.String() + ",")
		}
		sb.WriteString("\n")
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}
