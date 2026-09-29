package domain

import (
	"math/rand"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
)

func d(s string) *decimal.Decimal { v := decimal.RequireFromString(s); return &v }

func seg(name string, w, dep string, pts ...Point) SegmentInput {
	s := SegmentInput{Ref: uuid.NewSHA1(uuid.NameSpaceURL, []byte(name)), Name: name, Class: port.ClassMainCanal, DataSource: SourceSurvey, Points: pts}
	s.RevisionID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(name))
	s.RowHash = "ab"
	if w != "" {
		s.WidthM = d(w)
	}
	if dep != "" {
		s.DepthM = d(dep)
	}
	return s
}

func count(g Graph, kind string) int {
	n := 0
	for _, f := range g.Findings {
		if f.Kind == kind {
			n++
		}
	}
	return n
}

// GIS US-WATER-01 / BR-37: tải trọng SUY RA từ rộng/sâu/tĩnh không, lớp lớn nhất còn qua.
func TestDeriveCapacityFromDimensions(t *testing.T) {
	vs, m := DefaultVessels(), DefaultSafety()
	big := Derive(Constraints{WidthM: d("200"), DepthM: d("8")}, nil, vs, m)
	if !big.Certain || big.MaxLoadTons.String() != "2000" || big.Limiting != LimitNone || big.VesselCode != "sa_lan_2000t" {
		t.Fatalf("sông lớn: %+v", big)
	}
	// Một cây cầu tĩnh không 5 m hạ cả cạnh xuống mức ghe.
	bridge := Derive(Constraints{WidthM: d("40"), DepthM: d("6"), ClearanceM: d("5"), ClearanceRequired: true, ClearanceFrom: "cau-1"}, nil, vs, m)
	if bridge.MaxLoadTons.String() != "100" || bridge.Limiting != DimClearance || bridge.StructureRef != "cau-1" {
		t.Fatalf("cầu thấp: %+v", bridge)
	}
	// Thiếu độ sâu: KHÔNG coi là đi được, không đoán.
	unk := Derive(Constraints{WidthM: d("100")}, nil, vs, m)
	if unk.Certain || unk.MaxLoadTons != nil || unk.Limiting != "unknown:depth" {
		t.Fatalf("thiếu độ sâu: %+v", unk)
	}
	// Thiếu số liệu nhưng có tải trọng khai báo → dùng khai báo, vẫn ghi là chưa chắc chắn.
	decl := Derive(Constraints{WidthM: d("100")}, d("300"), vs, m)
	if decl.Certain || decl.MaxLoadTons.String() != "300" || decl.Limiting != LimitDeclared {
		t.Fatalf("khai báo: %+v", decl)
	}
	// Khai báo nhỏ hơn suy ra → lấy khai báo.
	capped := Derive(Constraints{WidthM: d("200"), DepthM: d("8")}, d("500"), vs, m)
	if capped.MaxLoadTons.String() != "500" || capped.Derived.String() != "2000" || capped.Limiting != LimitDeclared {
		t.Fatalf("giới hạn khai báo: %+v", capped)
	}
	// Không lớp nào qua → 0 tấn, chắc chắn, nêu thông số chặn.
	tiny := Derive(Constraints{WidthM: d("4"), DepthM: d("0.8")}, nil, vs, m)
	if !tiny.Certain || !tiny.MaxLoadTons.IsZero() || tiny.Limiting != DimWidth {
		t.Fatalf("rạch nhỏ: %+v", tiny)
	}
	// Cầu có nhưng chưa đo tĩnh không → chưa kết luận.
	noH := Derive(Constraints{WidthM: d("60"), DepthM: d("5"), ClearanceRequired: true}, nil, vs, m)
	if noH.Certain || noH.MaxLoadTons != nil || noH.Limiting != "unknown:clearance" {
		t.Fatalf("cầu chưa đo: %+v", noH)
	}
}

// GIS BR-37: biên an toàn — kênh vừa khít thân tàu không đi được.
func TestSafetyMargins(t *testing.T) {
	m := DefaultSafety()
	barge := DefaultVessels()[1]
	if CanPass(barge, Constraints{WidthM: &barge.BeamM, DepthM: d("5"), ClearanceM: d("20")}, m).Passes {
		t.Fatal("rộng đúng bằng thân tàu phải không qua")
	}
	w := barge.BeamM.Mul(m.WidthFactor)
	if !CanPass(barge, Constraints{WidthM: &w, DepthM: d("5"), ClearanceM: d("20")}, m).Passes {
		t.Fatal("rộng = thân × hệ số phải qua")
	}
	if CanPass(barge, Constraints{WidthM: d("30"), DepthM: &barge.DraftM}, m).Passes {
		t.Fatal("sâu đúng bằng mớn nước phải không qua")
	}
	dep := barge.DraftM.Add(m.UnderKeelM)
	if !CanPass(barge, Constraints{WidthM: d("30"), DepthM: &dep}, m).Passes {
		t.Fatal("sâu = mớn + nước dưới đáy phải qua")
	}
}

// GIS US-WATER-02/03 AC-2: thông báo kiểm dữ liệu đúng nguyên văn.
func TestValidateSegmentMessages(t *testing.T) {
	s := Segment{Name: " ", Class: "", Status: StatusDraft, DataSource: SourceDigitised, Points: []Point{{105, 10}, {105.1, 10}}}
	errs := ValidateSegment(s)
	if len(errs) != 2 || errs[0].Reason != MsgNameClassRequired {
		t.Fatalf("thiếu tên/phân loại: %+v", errs)
	}
	s.Name, s.Class, s.WidthM = "Kênh A", port.ClassMainCanal, d("0")
	errs = ValidateSegment(s)
	if len(errs) != 1 || errs[0].Field != "width_m" || errs[0].Reason != MsgPositiveNumbers {
		t.Fatalf("rộng ≤ 0: %+v", errs)
	}
	s.WidthM, s.Points = nil, []Point{{105, 10}}
	if errs := ValidateSegment(s); len(errs) != 1 || errs[0].Field != "geometry" {
		t.Fatalf("một đỉnh: %+v", errs)
	}
}

// GIS US-WATER-01: hai tuyến cắt nhau không có đỉnh chung → chèn đỉnh, liên thông.
func TestBuildCrossingIsNoded(t *testing.T) {
	a := seg("cheo-a", "80", "5", Point{106.00, 11.00}, Point{106.10, 11.10})
	b := seg("cheo-b", "80", "5", Point{106.00, 11.10}, Point{106.10, 11.00})
	g, err := Build([]SegmentInput{a, b}, nil, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	if count(g, FindingCrossing) != 1 || !g.Findings[0].AutoFixed {
		t.Fatalf("phải phát hiện giao cắt: %+v", g.Findings)
	}
	if len(g.Vertices) != 5 || len(g.Edges) != 4 || g.Components != 1 {
		t.Fatalf("mong 5 đỉnh, 4 cạnh, 1 cụm: %d %d %d", len(g.Vertices), len(g.Edges), g.Components)
	}
	var center Vertex
	for _, v := range g.Vertices {
		if v.Degree == 4 {
			center = v
		}
	}
	if center.ID == 0 || center.At.Lng < 106.049 || center.At.Lng > 106.051 {
		t.Fatalf("giao điểm phải là đỉnh bậc 4 ở giữa: %+v", g.Vertices)
	}
}

// GIS US-WATER-01: đầu tuyến nằm sát thân tuyến kia (≤ 20 m) → nối chữ T.
func TestBuildTJoin(t *testing.T) {
	main := seg("chinh", "60", "4", Point{105.00, 10.00}, Point{105.02, 10.00})
	// Nhánh dừng cách thân kênh chính ~11 m về phía bắc, giữa đoạn.
	branch := seg("nhanh", "20", "2", Point{105.01, 10.0001}, Point{105.01, 10.01})
	g, err := Build([]SegmentInput{main, branch}, nil, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	if count(g, FindingTJoin) != 1 || g.Components != 1 || len(g.Edges) != 3 {
		t.Fatalf("nối chữ T: findings=%+v edges=%d comps=%d", g.Findings, len(g.Edges), g.Components)
	}
}

// GIS US-WATER-01: điểm trong ngưỡng snap 25 m gộp thành một đỉnh.
func TestBuildSnap(t *testing.T) {
	a := seg("a", "60", "4", Point{105.00, 10.00}, Point{105.01, 10.00})
	// Đầu tuyến b cách đầu cuối của a ~11 m.
	b := seg("b", "60", "4", Point{105.0101, 10.00}, Point{105.02, 10.00})
	g, err := Build([]SegmentInput{a, b}, nil, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Vertices) != 3 || g.Components != 1 || count(g, FindingGapJoined)+count(g, FindingGapReported) != 0 {
		t.Fatalf("snap: %d đỉnh, %d cụm, %+v", len(g.Vertices), g.Components, g.Findings)
	}
}

// GIS US-WATER-01: khe hở nhỏ (≤ 150 m) nối tự động; khe 150 m..1 km chỉ báo cáo; > 1 km không báo.
func TestBuildGaps(t *testing.T) {
	a := seg("khe-nho-a", "60", "4", Point{106.00, 12.00}, Point{106.02, 12.02})
	b := seg("khe-nho-b", "60", "4", Point{106.0209, 12.0209}, Point{106.04, 12.04}) // ~140 m
	c := seg("khe-lon-c", "60", "4", Point{106.20, 12.20}, Point{106.22, 12.22})
	dd := seg("khe-lon-d", "60", "4", Point{106.2230, 12.2230}, Point{106.25, 12.25}) // ~470 m
	e := seg("xa-e", "60", "4", Point{106.50, 12.50}, Point{106.52, 12.52})
	f := seg("xa-f", "60", "4", Point{106.54, 12.54}, Point{106.56, 12.56}) // ~3 km
	g, err := Build([]SegmentInput{a, b, c, dd, e, f}, nil, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	if count(g, FindingGapJoined) != 1 || count(g, FindingGapReported) != 1 {
		t.Fatalf("khe hở: %+v", g.Findings)
	}
	for _, fd := range g.Findings {
		if fd.Kind == FindingGapReported && (fd.AutoFixed || fd.GapM.LessThan(decimal.NewFromInt(400))) {
			t.Fatalf("khe lớn không được nối: %+v", fd)
		}
	}
	// a+b nối thành một cụm; c, d, e, f rời → 5 cụm.
	if g.Components != 5 {
		t.Fatalf("mong 5 cụm, có %d", g.Components)
	}
	if count(g, FindingIsolated) != 4 {
		t.Fatalf("mong 4 tuyến cô lập: %+v", g.Findings)
	}
}

// Dựng tất định: đổi thứ tự đầu vào không đổi đồ thị (id, băm).
func TestBuildDeterministic(t *testing.T) {
	segs := []SegmentInput{
		seg("cheo-a", "80", "5", Point{106.00, 11.00}, Point{106.10, 11.10}),
		seg("cheo-b", "80", "5", Point{106.00, 11.10}, Point{106.10, 11.00}),
		seg("chinh", "60", "4", Point{106.10, 11.10}, Point{106.20, 11.10}),
		seg("nhanh", "20", "2", Point{106.15, 11.1001}, Point{106.15, 11.2}),
	}
	st := []StructureInput{{Ref: uuid.New(), RevisionID: uuid.New(), Kind: port.StructureBridge, SegmentRef: segs[2].Ref,
		At: Point{106.12, 11.10}, ClearanceHeightM: d("4")}}
	g1, err := Build(segs, st, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewSource(7))
	for range 5 {
		shuffled := append([]SegmentInput(nil), segs...)
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		g2, err := Build(shuffled, st, DefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		if g1.GraphSHA256 != g2.GraphSHA256 || g1.SegmentSetSHA256 != g2.SegmentSetSHA256 {
			t.Fatal("dựng phải tất định")
		}
	}
	// Đổi một ngưỡng → băm tập đoạn đổi.
	p := DefaultParams()
	p.Tolerances.SnapM = decimal.NewFromInt(10)
	g3, _ := Build(segs, st, p)
	if g3.SegmentSetSHA256 == g1.SegmentSetSHA256 {
		t.Fatal("tham số dựng phải vào băm")
	}
	// Cầu tĩnh không 4 m ràng buộc đúng MỘT cạnh của đoạn mang nó (ghe cần 3.5 + 0.5 = 4.0 m → còn ghe).
	bridged, others := 0, 0
	for _, e := range g1.Edges {
		if e.SegmentRef != segs[2].Ref {
			continue
		}
		if e.Capacity.Limiting == DimClearance {
			bridged++
			if e.Capacity.MaxLoadTons.String() != "100" || e.Capacity.StructureRef != st[0].Ref.String() {
				t.Fatalf("cạnh có cầu: %+v", e.Capacity)
			}
		} else {
			others++
		}
	}
	if bridged != 1 || others < 1 {
		t.Fatalf("cầu phải ràng buộc đúng một cạnh: %d/%d", bridged, others)
	}
}

func TestSubstring(t *testing.T) {
	pts := []Point{{0, 0}, {1, 0}, {2, 0}}
	s := Substring(pts, 0.25, 0.75)
	if len(s) != 3 || s[0] != (Point{0.5, 0}) || s[2] != (Point{1.5, 0}) {
		t.Fatalf("%v", s)
	}
	r := Substring(pts, 0.75, 0.25)
	if r[0] != (Point{1.5, 0}) || r[len(r)-1] != (Point{0.5, 0}) {
		t.Fatalf("%v", r)
	}
}
