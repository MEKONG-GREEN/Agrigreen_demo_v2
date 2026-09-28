// Package shptest dựng gói .zip Shapefile (Polygon) trong bộ nhớ cho kiểm thử
// đường nhập của pf/geo — theo ESRI Shapefile Technical Description (07/1998)
// và định dạng dBASE III. Không dùng trong mã chạy thật.
package shptest

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"math"
)

// Hệ toạ độ mẫu (.prj dạng ESRI WKT).
const (
	WGS84 = `GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]]`
	// VN2000UTM48 là VN-2000 múi UTM 48N (tham số elipsoid WGS84; phép chuyển datum không ghi trong .prj).
	VN2000UTM48 = `PROJCS["VN_2000_UTM_Zone_48N",GEOGCS["GCS_VN_2000",DATUM["D_Vietnam_2000",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",105.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`
	// UTM48N là WGS84 múi UTM 48N (hệ chiếu phẳng).
	UTM48N = `PROJCS["WGS_1984_UTM_Zone_48N",GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",105.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`
)

// Point là [x, y] = [kinh độ, vĩ độ].
type Point [2]float64

// Polygon là vòng ngoài và các lỗ (chiều vòng do Zip tự đặt theo đặc tả).
type Polygon struct {
	Outer []Point
	Holes [][]Point
}

// Feature là một bản ghi: các mảnh vùng (nil = Null shape) và thuộc tính .dbf.
type Feature struct {
	Parts []Polygon
	Attrs map[string]string
}

// Options điều chỉnh gói.
type Options struct {
	Base    string // tên tệp gốc, mặc định "thua"
	NoDBF   bool
	NoCPG   bool
	Deleted []int // chỉ số bản ghi đánh dấu xóa trong .dbf
}

// Zip dựng gói .zip gồm .shp, .shx, .dbf (trường kiểu C), .cpg (UTF-8) và
// .prj (bỏ qua nếu prj rỗng).
func Zip(fields []string, feats []Feature, prj string, opt Options) []byte {
	if opt.Base == "" {
		opt.Base = "thua"
	}
	shp, shx := shapes(feats)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	put := func(name string, b []byte) {
		w, err := zw.Create(opt.Base + name)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write(b); err != nil {
			panic(err)
		}
	}
	put(".shp", shp)
	put(".shx", shx)
	if !opt.NoDBF {
		put(".dbf", dbf(fields, feats, opt.Deleted))
	}
	if !opt.NoCPG {
		put(".cpg", []byte("UTF-8"))
	}
	if prj != "" {
		put(".prj", []byte(prj))
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func area(r []Point) float64 {
	var s float64
	for i := range r {
		j := (i + 1) % len(r)
		s += r[i][0]*r[j][1] - r[j][0]*r[i][1]
	}
	return s / 2
}

// orient khép vòng và đặt chiều: cw = theo chiều kim đồng hồ (vòng ngoài).
func orient(r []Point, cw bool) []Point {
	out := append([]Point(nil), r...)
	if len(out) > 0 && out[0] != out[len(out)-1] {
		out = append(out, out[0])
	}
	if (area(out) < 0) != cw {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out
}

type le struct{ b *bytes.Buffer }

func (w le) i32(v int32)   { _ = binary.Write(w.b, binary.LittleEndian, v) }
func (w le) f64(v float64) { _ = binary.Write(w.b, binary.LittleEndian, v) }
func be32(b *bytes.Buffer, v int32) {
	_ = binary.Write(b, binary.BigEndian, v)
}

func header(b *bytes.Buffer, fileWords int32, box [4]float64) {
	be32(b, 9994)
	for range 5 {
		be32(b, 0)
	}
	be32(b, fileWords)
	w := le{b}
	w.i32(1000)
	w.i32(5)
	for _, v := range box {
		w.f64(v)
	}
	for range 4 {
		w.f64(0)
	}
}

func shapes(feats []Feature) ([]byte, []byte) {
	var recs [][]byte
	box := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, f := range feats {
		var c bytes.Buffer
		w := le{&c}
		if f.Parts == nil {
			w.i32(0)
			recs = append(recs, c.Bytes())
			continue
		}
		var rings [][]Point
		for _, p := range f.Parts {
			rings = append(rings, orient(p.Outer, true))
			for _, h := range p.Holes {
				rings = append(rings, orient(h, false))
			}
		}
		fb := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		n := 0
		for _, r := range rings {
			for _, p := range r {
				fb[0], fb[1] = math.Min(fb[0], p[0]), math.Min(fb[1], p[1])
				fb[2], fb[3] = math.Max(fb[2], p[0]), math.Max(fb[3], p[1])
				n++
			}
		}
		box[0], box[1] = math.Min(box[0], fb[0]), math.Min(box[1], fb[1])
		box[2], box[3] = math.Max(box[2], fb[2]), math.Max(box[3], fb[3])
		w.i32(5)
		for _, v := range fb {
			w.f64(v)
		}
		w.i32(int32(len(rings)))
		w.i32(int32(n))
		at := 0
		for _, r := range rings {
			w.i32(int32(at))
			at += len(r)
		}
		for _, r := range rings {
			for _, p := range r {
				w.f64(p[0])
				w.f64(p[1])
			}
		}
		recs = append(recs, c.Bytes())
	}
	if math.IsInf(box[0], 0) {
		box = [4]float64{}
	}
	var body bytes.Buffer
	var index bytes.Buffer
	off := 50 // sau đầu tệp 100 byte = 50 từ 16 bit
	for i, c := range recs {
		be32(&body, int32(i+1))
		be32(&body, int32(len(c)/2))
		body.Write(c)
		be32(&index, int32(off))
		be32(&index, int32(len(c)/2))
		off += 4 + len(c)/2
	}
	var shp, shx bytes.Buffer
	header(&shp, int32(50+body.Len()/2), box)
	shp.Write(body.Bytes())
	header(&shx, int32(50+index.Len()/2), box)
	shx.Write(index.Bytes())
	return shp.Bytes(), shx.Bytes()
}

func dbf(fields []string, feats []Feature, deleted []int) []byte {
	widths := make([]int, len(fields))
	for i, f := range fields {
		widths[i] = 1
		for _, ft := range feats {
			widths[i] = max(widths[i], len(ft.Attrs[f]))
		}
	}
	recLen := 1
	for _, w := range widths {
		recLen += w
	}
	headerLen := 32 + 32*len(fields) + 1
	var b bytes.Buffer
	b.Write([]byte{0x03, 126, 9, 28}) // dBASE III, ngày 2026-09-28
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(feats)))
	_ = binary.Write(&b, binary.LittleEndian, uint16(headerLen))
	_ = binary.Write(&b, binary.LittleEndian, uint16(recLen))
	b.Write(make([]byte, 20))
	for i, f := range fields {
		name := make([]byte, 11)
		copy(name, f)
		b.Write(name)
		b.WriteByte('C')
		b.Write(make([]byte, 4))
		b.WriteByte(byte(widths[i]))
		b.WriteByte(0)
		b.Write(make([]byte, 14))
	}
	b.WriteByte(0x0D)
	del := map[int]bool{}
	for _, d := range deleted {
		del[d] = true
	}
	for i, ft := range feats {
		if del[i] {
			b.WriteByte('*')
		} else {
			b.WriteByte(' ')
		}
		for j, f := range fields {
			v := []byte(ft.Attrs[f])
			b.Write(v)
			b.Write(bytes.Repeat([]byte{' '}, widths[j]-len(v)))
		}
	}
	b.WriteByte(0x1A)
	return b.Bytes()
}
