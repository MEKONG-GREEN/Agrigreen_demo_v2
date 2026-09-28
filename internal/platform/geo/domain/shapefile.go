package domain

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/unicode/norm"
)

// Bộ đọc Shapefile tối thiểu (ESRI Shapefile Technical Description, 07/1998):
// chỉ lớp vùng — Polygon (5), PolygonZ (15), PolygonM (25; bỏ Z/M) — cùng bảng
// thuộc tính dBASE (.dbf) và tệp hệ toạ độ (.prj). Tải lên là gói .zip chứa
// .shp, .dbf, .prj (.shx, .cpg tùy chọn: bản ghi được đọc tuần tự từ .shp).
//
// Hệ toạ độ: chỉ nhận WGS84 kinh/vĩ độ (EPSG:4326). VN-2000 và mọi hệ chiếu
// phẳng (UTM, TM-3°) bị từ chối với geo.crs_unsupported — hệ thống không tự
// chuyển VN-2000 (cần 7 tham số và kinh tuyến trục theo tỉnh; chuyển sai lệch
// hàng chục mét là sai ranh giới thửa).

// Giới hạn gói Shapefile (chống zip bomb, R-52).
const (
	maxShapefileEntry = 32 << 20 // mỗi tệp sau giải nén
	maxZipEntries     = 64
)

// Kiểu hình trong .shp.
const (
	shpNull     = 0
	shpPolygon  = 5
	shpPolygonZ = 15
	shpPolygonM = 25
)

var shpTypeNames = map[int32]string{
	1: "Point", 3: "PolyLine", 8: "MultiPoint", 11: "PointZ", 13: "PolyLineZ", 18: "MultiPointZ",
	21: "PointM", 23: "PolyLineM", 28: "MultiPointM", 31: "MultiPatch",
}

// ErrShapefileNotZip: tải .shp rời — thiếu thuộc tính và hệ toạ độ.
var ErrShapefileNotZip = violation(CodeFormat,
	"Shapefile phải tải lên dạng một gói .zip gồm các tệp .shp, .shx, .dbf và .prj cùng tên")

// parseShapefileZip đọc gói .zip Shapefile thành các đối tượng. Lỗi trả về là lỗi
// cả tệp (thiếu tệp, hệ toạ độ không hỗ trợ, không phải lớp vùng).
func parseShapefileZip(data []byte) ([]Feature, error) {
	if !bytes.HasPrefix(data, []byte("PK")) {
		return nil, ErrShapefileNotZip
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, violation(CodeFormat, "Gói .zip hỏng hoặc không đọc được")
	}
	if len(zr.File) > maxZipEntries {
		return nil, violation(CodeFormat, "Gói .zip có quá nhiều tệp — chỉ chứa một lớp Shapefile")
	}
	parts := map[string]map[string]*zip.File{} // tên gốc (chữ thường) → phần mở rộng → tệp
	for _, f := range zr.File {
		name := f.Name
		if f.FileInfo().IsDir() || strings.HasPrefix(name, "__MACOSX/") || strings.HasPrefix(path.Base(name), "._") {
			continue
		}
		ext := strings.ToLower(path.Ext(name))
		base := strings.ToLower(strings.TrimSuffix(name, path.Ext(name)))
		if parts[base] == nil {
			parts[base] = map[string]*zip.File{}
		}
		parts[base][ext] = f
	}
	var layer map[string]*zip.File
	for _, p := range parts {
		if p[".shp"] != nil {
			if layer != nil {
				return nil, violation(CodeFormat, "Gói .zip chỉ được chứa một lớp Shapefile (một tệp .shp)")
			}
			layer = p
		}
	}
	if layer == nil {
		return nil, violation(CodeFormat, "Gói .zip không có tệp .shp")
	}
	if layer[".dbf"] == nil {
		return nil, violation(CodeFormat, "Thiếu tệp .dbf (bảng thuộc tính) cùng tên với tệp .shp")
	}
	if layer[".prj"] == nil {
		return nil, violation(CodeCRSUnsupported, "Thiếu tệp .prj — không xác định được hệ toạ độ; hãy xuất Shapefile kèm .prj ở WGS84 (EPSG:4326)")
	}
	prj, err := readZipEntry(layer[".prj"])
	if err != nil {
		return nil, err
	}
	if err := CheckPRJ(string(prj)); err != nil {
		return nil, err
	}
	shp, err := readZipEntry(layer[".shp"])
	if err != nil {
		return nil, err
	}
	dbf, err := readZipEntry(layer[".dbf"])
	if err != nil {
		return nil, err
	}
	var cpg string
	if f := layer[".cpg"]; f != nil {
		b, err := readZipEntry(f)
		if err != nil {
			return nil, err
		}
		cpg = string(b)
	}
	shapes, err := readSHP(shp)
	if err != nil {
		return nil, err
	}
	attrs, deleted, err := readDBF(dbf, cpg)
	if err != nil {
		return nil, err
	}
	if len(attrs) != len(shapes) {
		return nil, violation(CodeFormat, "Số bản ghi .dbf ("+strconv.Itoa(len(attrs))+") khác số hình .shp ("+strconv.Itoa(len(shapes))+")")
	}
	out := make([]Feature, 0, len(shapes))
	for i, s := range shapes {
		if deleted[i] {
			continue // bản ghi đã đánh dấu xóa trong .dbf
		}
		f := Feature{Index: i + 1, Props: shapefileProps(attrs[i])}
		f.Name = firstProp(f.Props, "name", "ten", "ten_thua", "ma_thua")
		if f.Name == "" && f.Props["so_thua"] != "" {
			f.Name = "Thửa " + f.Props["so_thua"]
			if f.Props["so_to"] != "" {
				f.Name = "Tờ " + f.Props["so_to"] + " thửa " + f.Props["so_thua"]
			}
		}
		if s.err != nil {
			f.Err = s.err
		} else {
			f.Geom = s.geom
			f.Points, f.Err = f.Geom.Normalize()
		}
		out = append(out, f)
	}
	return out, nil
}

func readZipEntry(f *zip.File) ([]byte, error) {
	if f.UncompressedSize64 > maxShapefileEntry {
		return nil, violation(CodeFormat, "Tệp "+path.Base(f.Name)+" trong gói .zip quá lớn")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, violation(CodeFormat, "Không đọc được tệp "+path.Base(f.Name)+" trong gói .zip")
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, maxShapefileEntry+1))
	if err != nil {
		return nil, violation(CodeFormat, "Không đọc được tệp "+path.Base(f.Name)+" trong gói .zip")
	}
	if len(b) > maxShapefileEntry {
		return nil, violation(CodeFormat, "Tệp "+path.Base(f.Name)+" trong gói .zip quá lớn")
	}
	return b, nil
}

// shapefileProps: tên trường .dbf (tối đa 10 ký tự, thường viết hoa) đưa về chữ
// thường, thêm bí danh dài cho các trường hay gặp.
func shapefileProps(attrs map[string]string) map[string]string {
	p := make(map[string]string, len(attrs)+2)
	for k, v := range attrs {
		p[strings.ToLower(k)] = v
	}
	alias := func(canon string, keys ...string) {
		if p[canon] != "" {
			return
		}
		for _, k := range keys {
			if v := p[k]; v != "" {
				p[canon] = v
				return
			}
		}
	}
	alias("cooperative_code", "ma_htx", "coop_code", "cooperativ")
	alias("cooperative_ref", "coop_ref")
	alias("household_ref", "hh_ref")
	alias("commune_code", "ma_xa", "commune")
	return p
}

// ---------------------------------------------------------------------------
// .prj (WKT ESRI hoặc OGC)
// ---------------------------------------------------------------------------

// CheckPRJ nhận hệ toạ độ địa lý WGS84 (EPSG:4326); từ chối VN-2000 và mọi hệ
// chiếu phẳng với thông báo nêu cách chuyển.
func CheckPRJ(wkt string) error {
	w := strings.TrimSpace(strings.TrimPrefix(wkt, "\xef\xbb\xbf"))
	if w == "" {
		return violation(CodeCRSUnsupported, "Tệp .prj rỗng — không xác định được hệ toạ độ")
	}
	up := strings.ToUpper(w)
	name := wktName(w)
	compact := strings.NewReplacer("_", "", "-", "", " ", "").Replace(up)
	switch {
	case strings.Contains(compact, "VN2000") || strings.Contains(compact, "VIETNAM2000"):
		return violation(CodeCRSUnsupported, "Tệp dùng hệ toạ độ VN-2000 ("+name+"). Hệ thống chỉ nhận WGS84 (EPSG:4326) và không tự chuyển "+
			"VN-2000 — hãy chuyển (Reproject) sang EPSG:4326 bằng QGIS/ArcGIS với tham số chuyển đổi VN-2000 → WGS84 của Bộ TN&MT "+
			"và đúng kinh tuyến trục của tỉnh, rồi tải lên lại")
	case strings.HasPrefix(up, "PROJCS[") || strings.HasPrefix(up, "PROJCRS[") || strings.HasPrefix(up, "PROJECTEDCRS["):
		return violation(CodeCRSUnsupported, "Tệp dùng hệ toạ độ phẳng ("+name+") — chỉ nhận kinh độ/vĩ độ WGS84 (EPSG:4326); hãy chuyển (Reproject) sang EPSG:4326 rồi tải lên lại")
	case strings.HasPrefix(up, "GEOGCS[") || strings.HasPrefix(up, "GEOGCRS[") || strings.HasPrefix(up, "GEODCRS[") || strings.HasPrefix(up, "GEOGRAPHICCRS["):
		if strings.Contains(compact, "WGS1984") || strings.Contains(compact, "WGS84") || strings.Contains(up, "WORLD GEODETIC SYSTEM 1984") ||
			strings.Contains(compact, `AUTHORITY["EPSG","4326"]`) || strings.Contains(compact, `ID["EPSG",4326]`) {
			return nil
		}
	}
	return violation(CodeCRSUnsupported, "Hệ toạ độ "+name+" không được hỗ trợ — chỉ nhận WGS84 (EPSG:4326)")
}

// wktName lấy tên đầu tiên trong WKT: PROJCS["VN_2000_UTM_Zone_48N", ...] → VN_2000_UTM_Zone_48N.
func wktName(w string) string {
	i := strings.IndexByte(w, '"')
	if i < 0 {
		return "không rõ tên"
	}
	j := strings.IndexByte(w[i+1:], '"')
	if j < 0 || j > 120 {
		return "không rõ tên"
	}
	return w[i+1 : i+1+j]
}

// ---------------------------------------------------------------------------
// .shp
// ---------------------------------------------------------------------------

type shpShape struct {
	geom MultiPolygon
	err  error
}

// readSHP đọc tuần tự các bản ghi. Mỗi bản ghi thành một hình hoặc một lỗi riêng.
func readSHP(b []byte) ([]shpShape, error) {
	if len(b) < 100 || binary.BigEndian.Uint32(b[0:4]) != 9994 {
		return nil, violation(CodeFormat, "Tệp .shp không đúng định dạng Shapefile")
	}
	typ := int32(binary.LittleEndian.Uint32(b[32:36]))
	switch typ {
	case shpPolygon, shpPolygonZ, shpPolygonM, shpNull:
	default:
		n := shpTypeNames[typ]
		if n == "" {
			n = "kiểu " + strconv.Itoa(int(typ))
		}
		return nil, violation(CodeInvalidGeometry, "Shapefile phải là lớp vùng (Polygon); tệp là lớp "+n)
	}
	var out []shpShape
	off := 100
	for off+8 <= len(b) {
		clen := int(binary.BigEndian.Uint32(b[off+4:off+8])) * 2
		start, end := off+8, off+8+clen
		if clen < 4 || end > len(b) || end < start {
			return nil, violation(CodeFormat, "Tệp .shp bị cắt cụt hoặc hỏng (bản ghi "+strconv.Itoa(len(out)+1)+")")
		}
		out = append(out, parsePolygonRecord(b[start:end]))
		if len(out) > MaxFeatures {
			return nil, violation(CodeFormat, "Tối đa "+strconv.Itoa(MaxFeatures)+" đối tượng mỗi lần nhập")
		}
		off = end
	}
	return out, nil
}

func parsePolygonRecord(c []byte) shpShape {
	typ := int32(binary.LittleEndian.Uint32(c[0:4]))
	switch typ {
	case shpNull:
		return shpShape{err: violation(CodeInvalidGeometry, "đối tượng không có hình (Null shape)")}
	case shpPolygon, shpPolygonZ, shpPolygonM:
	default:
		return shpShape{err: violation(CodeInvalidGeometry, "chỉ nhận vùng khép kín (Polygon)")}
	}
	bad := shpShape{err: violation(CodeInvalidGeometry, "bản ghi Polygon hỏng")}
	if len(c) < 44 {
		return bad
	}
	numParts := int(int32(binary.LittleEndian.Uint32(c[36:40])))
	numPoints := int(int32(binary.LittleEndian.Uint32(c[40:44])))
	if numParts <= 0 || numPoints <= 0 || numParts > numPoints || numPoints > 4*MaxPoints {
		if numPoints > 4*MaxPoints {
			return shpShape{err: violation(CodeInvalidGeometry, "ranh giới có quá "+strconv.Itoa(MaxPoints)+" đỉnh")}
		}
		return bad
	}
	ptsAt := 44 + 4*numParts
	if len(c) < ptsAt+16*numPoints {
		return bad
	}
	idx := make([]int, numParts+1)
	for i := range numParts {
		idx[i] = int(int32(binary.LittleEndian.Uint32(c[44+4*i:])))
	}
	idx[numParts] = numPoints
	var rings []Ring
	for i := range numParts {
		from, to := idx[i], idx[i+1]
		if from < 0 || to > numPoints || from >= to {
			return bad
		}
		r := make(Ring, 0, to-from)
		for k := from; k < to; k++ {
			at := ptsAt + 16*k
			x := math.Float64frombits(binary.LittleEndian.Uint64(c[at:]))
			y := math.Float64frombits(binary.LittleEndian.Uint64(c[at+8:]))
			r = append(r, Position{x, y})
		}
		rings = append(rings, r)
	}
	return shpShape{geom: assembleRings(rings)}
}

// assembleRings ghép vòng thành MultiPolygon: theo đặc tả, vòng ngoài theo chiều
// kim đồng hồ, lỗ ngược chiều. Mỗi lỗ gắn vào vòng ngoài chứa đỉnh đầu của nó;
// lỗ không nằm trong vòng ngoài nào (tệp ghi sai chiều) được coi là vòng ngoài.
func assembleRings(rings []Ring) MultiPolygon {
	var mp MultiPolygon
	var holes []Ring
	for _, r := range rings {
		if signedArea(r) <= 0 { // chiều kim đồng hồ (y hướng lên)
			mp = append(mp, Polygon{Outer: r})
		} else {
			holes = append(holes, r)
		}
	}
	for _, h := range holes {
		placed := false
		for i := range mp {
			if len(h) > 0 && pointInRing(h[0], mp[i].Outer) {
				mp[i].Holes = append(mp[i].Holes, h)
				placed = true
				break
			}
		}
		if !placed {
			mp = append(mp, Polygon{Outer: reverseRing(h)})
		}
	}
	return mp
}

func signedArea(r Ring) float64 {
	var s float64
	for i := range r {
		j := (i + 1) % len(r)
		s += r[i][0]*r[j][1] - r[j][0]*r[i][1]
	}
	return s / 2
}

func reverseRing(r Ring) Ring {
	out := make(Ring, len(r))
	for i, p := range r {
		out[len(r)-1-i] = p
	}
	return out
}

// pointInRing: thuật toán tia (even-odd).
func pointInRing(p Position, r Ring) bool {
	in := false
	for i, j := 0, len(r)-1; i < len(r); j, i = i, i+1 {
		a, b := r[i], r[j]
		if (a[1] > p[1]) != (b[1] > p[1]) && p[0] < (b[0]-a[0])*(p[1]-a[1])/(b[1]-a[1])+a[0] {
			in = !in
		}
	}
	return in
}

// ---------------------------------------------------------------------------
// .dbf (dBASE III/IV)
// ---------------------------------------------------------------------------

type dbfField struct {
	name   string
	offset int
	length int
}

// readDBF đọc mọi bản ghi thành map tên trường → giá trị (đã cắt khoảng trắng).
// deleted[i] = bản ghi i đã đánh dấu xóa.
func readDBF(b []byte, cpg string) ([]map[string]string, []bool, error) {
	bad := violation(CodeFormat, "Tệp .dbf không đúng định dạng dBASE")
	if len(b) < 33 {
		return nil, nil, bad
	}
	n := int(binary.LittleEndian.Uint32(b[4:8]))
	headerLen := int(binary.LittleEndian.Uint16(b[8:10]))
	recLen := int(binary.LittleEndian.Uint16(b[10:12]))
	if headerLen < 33 || headerLen > len(b) || recLen < 1 || n < 0 || n > MaxFeatures*4 {
		if n > MaxFeatures*4 {
			return nil, nil, violation(CodeFormat, "Tối đa "+strconv.Itoa(MaxFeatures)+" đối tượng mỗi lần nhập")
		}
		return nil, nil, bad
	}
	dec := dbfDecoder(cpg, b[29])
	var fields []dbfField
	off := 1 // byte 0 của bản ghi là cờ xóa
	for p := 32; p+32 <= headerLen && b[p] != 0x0D; p += 32 {
		raw := b[p : p+11]
		if i := bytes.IndexByte(raw, 0); i >= 0 {
			raw = raw[:i]
		}
		l := int(b[p+16])
		if b[p+11] == 'C' {
			l += int(b[p+17]) << 8 // trường chữ dài > 255 (Clipper/FoxPro)
		}
		fields = append(fields, dbfField{name: strings.TrimSpace(string(raw)), offset: off, length: l})
		off += l
	}
	if off > recLen {
		return nil, nil, bad
	}
	if headerLen+n*recLen > len(b) {
		return nil, nil, violation(CodeFormat, "Tệp .dbf bị cắt cụt")
	}
	recs := make([]map[string]string, n)
	deleted := make([]bool, n)
	for i := range n {
		rec := b[headerLen+i*recLen : headerLen+(i+1)*recLen]
		deleted[i] = rec[0] == '*'
		m := make(map[string]string, len(fields))
		for _, f := range fields {
			m[f.name] = decodeDBFText(dec, rec[f.offset:f.offset+f.length])
		}
		recs[i] = m
	}
	return recs, deleted, nil
}

// dbfDecoder chọn bảng mã theo .cpg (ưu tiên) hoặc mã ngôn ngữ (LDID) ở byte 29.
// nil = UTF-8 (hoặc tự đoán: UTF-8 hợp lệ thì dùng, không thì Windows-1258).
func dbfDecoder(cpg string, ldid byte) encoding.Encoding {
	c := strings.ToUpper(strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.TrimSpace(cpg)))
	switch c {
	case "UTF8", "65001":
		return nil
	case "1258", "CP1258", "WINDOWS1258", "ANSI1258":
		return charmap.Windows1258
	case "1252", "CP1252", "WINDOWS1252", "ANSI1252", "88591", "ISO88591", "LATIN1":
		return charmap.Windows1252
	}
	switch ldid {
	case 0x03, 0x57:
		return charmap.Windows1252
	}
	return nil
}

func decodeDBFText(dec encoding.Encoding, raw []byte) string {
	raw = bytes.TrimRight(raw, " \x00")
	raw = bytes.TrimLeft(raw, " ")
	if len(raw) == 0 {
		return ""
	}
	if dec == nil {
		if utf8.Valid(raw) {
			return norm.NFC.String(string(raw))
		}
		dec = charmap.Windows1258
	}
	s, err := dec.NewDecoder().Bytes(raw)
	if err != nil {
		return ""
	}
	return norm.NFC.String(strings.TrimSpace(string(s)))
}
