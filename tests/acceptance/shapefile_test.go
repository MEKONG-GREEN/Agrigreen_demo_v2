package acceptance

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// Hệ toạ độ mẫu (.prj dạng ESRI WKT) cho ca nạp Shapefile.
const (
	prjWGS84       = `GEOGCS["GCS_WGS_1984",DATUM["D_WGS_1984",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]]`
	prjVN2000UTM48 = `PROJCS["VN_2000_UTM_Zone_48N",GEOGCS["GCS_VN_2000",DATUM["D_Vietnam_2000",SPHEROID["WGS_1984",6378137.0,298.257223563]],PRIMEM["Greenwich",0.0],UNIT["Degree",0.0174532925199433]],PROJECTION["Transverse_Mercator"],PARAMETER["False_Easting",500000.0],PARAMETER["False_Northing",0.0],PARAMETER["Central_Meridian",105.0],PARAMETER["Scale_Factor",0.9996],PARAMETER["Latitude_Of_Origin",0.0],UNIT["Meter",1.0]]`
)

// shpFeature là một thửa: vòng ngoài [kinh độ, vĩ độ] và thuộc tính .dbf.
type shpFeature struct {
	Ring  [][2]float64
	Attrs map[string]string
}

// shapefileZip dựng gói .zip Shapefile Polygon tối thiểu (.shp, .shx, .dbf, .cpg, .prj)
// theo ESRI Shapefile Technical Description — bản rút gọn chỉ cho kiểm thử chấp nhận
// (tests/ không được import internal/platform/geo/shptest — R-02).
func shapefileZip(t *testing.T, fields []string, feats []shpFeature, prj string) []byte {
	t.Helper()
	be := func(b *bytes.Buffer, v int32) { _ = binary.Write(b, binary.BigEndian, v) }
	le := func(b *bytes.Buffer, v any) { _ = binary.Write(b, binary.LittleEndian, v) }
	header := func(b *bytes.Buffer, words int32, box [4]float64) {
		be(b, 9994)
		for range 5 {
			be(b, 0)
		}
		be(b, words)
		le(b, int32(1000))
		le(b, int32(5))
		for _, v := range box {
			le(b, v)
		}
		for range 4 {
			le(b, float64(0))
		}
	}
	box := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	var body, index bytes.Buffer
	off := int32(50)
	for i, f := range feats {
		ring := append([][2]float64(nil), f.Ring...)
		if ring[0] != ring[len(ring)-1] {
			ring = append(ring, ring[0])
		}
		var s float64 // vòng ngoài theo chiều kim đồng hồ (diện tích có dấu âm)
		for k := 0; k+1 < len(ring); k++ {
			s += ring[k][0]*ring[k+1][1] - ring[k+1][0]*ring[k][1]
		}
		if s > 0 {
			for a, b := 0, len(ring)-1; a < b; a, b = a+1, b-1 {
				ring[a], ring[b] = ring[b], ring[a]
			}
		}
		fb := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		for _, p := range ring {
			fb = [4]float64{math.Min(fb[0], p[0]), math.Min(fb[1], p[1]), math.Max(fb[2], p[0]), math.Max(fb[3], p[1])}
		}
		box = [4]float64{math.Min(box[0], fb[0]), math.Min(box[1], fb[1]), math.Max(box[2], fb[2]), math.Max(box[3], fb[3])}
		var c bytes.Buffer
		le(&c, int32(5))
		for _, v := range fb {
			le(&c, v)
		}
		le(&c, int32(1))
		le(&c, int32(len(ring)))
		le(&c, int32(0))
		for _, p := range ring {
			le(&c, p[0])
			le(&c, p[1])
		}
		be(&body, int32(i+1))
		be(&body, int32(c.Len()/2))
		body.Write(c.Bytes())
		be(&index, off)
		be(&index, int32(c.Len()/2))
		off += 4 + int32(c.Len()/2)
	}
	var shp, shx bytes.Buffer
	header(&shp, int32(50+body.Len()/2), box)
	shp.Write(body.Bytes())
	header(&shx, int32(50+index.Len()/2), box)
	shx.Write(index.Bytes())

	// .dbf dBASE III, mọi trường kiểu C.
	widths := make([]int, len(fields))
	recLen := 1
	for i, f := range fields {
		widths[i] = 1
		for _, ft := range feats {
			widths[i] = max(widths[i], len(ft.Attrs[f]))
		}
		recLen += widths[i]
	}
	var dbf bytes.Buffer
	dbf.Write([]byte{0x03, 126, 9, 28})
	le(&dbf, uint32(len(feats)))
	le(&dbf, uint16(32+32*len(fields)+1))
	le(&dbf, uint16(recLen))
	dbf.Write(make([]byte, 20))
	for i, f := range fields {
		name := make([]byte, 11)
		copy(name, f)
		dbf.Write(name)
		dbf.WriteByte('C')
		dbf.Write(make([]byte, 4))
		dbf.WriteByte(byte(widths[i]))
		dbf.Write(make([]byte, 15))
	}
	dbf.WriteByte(0x0D)
	for _, ft := range feats {
		dbf.WriteByte(' ')
		for j, f := range fields {
			v := []byte(ft.Attrs[f])
			dbf.Write(v)
			dbf.Write(bytes.Repeat([]byte{' '}, widths[j]-len(v)))
		}
	}
	dbf.WriteByte(0x1A)

	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for name, b := range map[string][]byte{".shp": shp.Bytes(), ".shx": shx.Bytes(), ".dbf": dbf.Bytes(), ".cpg": []byte("UTF-8"), ".prj": []byte(prj)} {
		if name == ".prj" && prj == "" {
			continue
		}
		w, err := zw.Create("thua" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
