package edgewire

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Bộ vector dùng chung với mọi hiện thực khác của hợp đồng (vd bộ điều hợp mẫu
// cân viết ngôn ngữ khác). Sinh lại: go test ./internal/mkg/measurement/port/edgewire -run TestVectors -update
var update = flag.Bool("update", false, "ghi lại contracts/edge/batch.v1.vectors.json")

const contractsDir = "../../../../../contracts/edge"

type validVector struct {
	Name          string          `json:"name"`
	JSON          json.RawMessage `json:"json"`
	Canonical     string          `json:"canonical"`
	PayloadSHA256 string          `json:"payload_sha256_hex"`
	Signature     string          `json:"signature"`
}

type invalidVector struct {
	Name string          `json:"name"`
	JSON json.RawMessage `json:"json"`
	Code string          `json:"code"`
}

type vectorFile struct {
	Format         string          `json:"format"`
	Note           string          `json:"note"`
	PrivateSeedHex string          `json:"private_seed_hex"`
	PublicKeyHex   string          `json:"public_key_hex"`
	KeyID          string          `json:"key_id"`
	Valid          []validVector   `json:"valid"`
	Invalid        []invalidVector `json:"invalid"`
}

func testKey() ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	return ed25519.NewKeyFromSeed(seed)
}

func intp(v int) *int { return &v }

func sampleBatch() Batch {
	return Batch{
		Format: Format, DeviceID: "scale-hub01-in", Epoch: 1, CounterFrom: 41, CounterTo: 43, NTPOffsetMs: intp(-12),
		Readings: []Reading{
			{Counter: 41, DeviceTime: "2026-12-03T00:14:22.100Z", ValueKg: "0.000", Stable: true, Unit: UnitKg},
			{Counter: 42, DeviceTime: "2026-12-03T00:14:22.600Z", ValueKg: "1250.500", Stable: false, Unit: UnitKg, Raw: "U1QsR1MsKzAwMTI1MC41a2cNCg=="},
			{Counter: 43, DeviceTime: "2026-12-03T00:14:23.100Z", ValueKg: "1250.520", Stable: true, Unit: UnitKg},
		},
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// build dựng bộ vector tất định (Ed25519 tất định theo khóa + thông điệp).
func build(t *testing.T) vectorFile {
	t.Helper()
	priv := testKey()
	pub := priv.Public().(ed25519.PublicKey)
	vf := vectorFile{
		Format: Format,
		Note: "Vector kiểm thử hợp đồng lô số đo edge-agent → ingest (ADR-0019). KHÓA CHỈ DÙNG CHO KIỂM THỬ — " +
			"không bao giờ đăng ký vào sổ thiết bị. canonical là dạng chuẩn đã ký (LF cuối mỗi dòng).",
		PrivateSeedHex: hex.EncodeToString(priv.Seed()),
		PublicKeyHex:   hex.EncodeToString(pub),
		KeyID:          KeyID(pub),
	}
	addValid := func(name string, b Batch) Batch {
		if err := Sign(priv, &b); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		c, _ := b.Canonical()
		sum := sha256.Sum256(c)
		vf.Valid = append(vf.Valid, validVector{Name: name, JSON: mustJSON(t, b), Canonical: string(c),
			PayloadSHA256: hex.EncodeToString(sum[:]), Signature: b.Signature})
		return b
	}
	base := addValid("three_readings_with_raw", sampleBatch())
	addValid("single_reading_ntp_unsynced_tonnes_display", Batch{
		Format: Format, DeviceID: "scale-hub01-out", Epoch: 2, CounterFrom: 1, CounterTo: 1, NTPOffsetMs: nil,
		Readings: []Reading{{Counter: 1, DeviceTime: "2026-12-31T23:59:59.999Z", ValueKg: "-0.500", Stable: false, Unit: UnitT}},
	})

	// Ca hỏng: sửa lô đã ký, hoặc JSON sai hợp đồng.
	mutate := func(name, code string, f func(m map[string]any)) {
		var m map[string]any
		if err := json.Unmarshal(mustJSON(t, base), &m); err != nil {
			t.Fatal(err)
		}
		f(m)
		vf.Invalid = append(vf.Invalid, invalidVector{Name: name, JSON: mustJSON(t, m), Code: code})
	}
	reading := func(m map[string]any, i int) map[string]any { return m["readings"].([]any)[i].(map[string]any) }
	mutate("tampered_value_after_signing", "edge.signature", func(m map[string]any) { reading(m, 2)["value_kg"] = "1350.520" })
	mutate("tampered_stable_after_signing", "edge.signature", func(m map[string]any) { reading(m, 0)["stable"] = false })
	mutate("tampered_ntp_offset_after_signing", "edge.signature", func(m map[string]any) { m["ntp_offset_ms"] = 0 })
	mutate("counter_gap", "edge.counter_range", func(m map[string]any) { reading(m, 1)["counter"] = 44 })
	mutate("count_mismatch", "edge.counter_range", func(m map[string]any) { m["counter_to"] = 44 })
	mutate("value_two_decimals", "edge.reading", func(m map[string]any) { reading(m, 1)["value_kg"] = "1250.50" })
	mutate("value_negative_zero", "edge.reading", func(m map[string]any) { reading(m, 0)["value_kg"] = "-0.000" })
	mutate("device_time_offset_not_utc", "edge.reading", func(m map[string]any) { reading(m, 0)["device_time"] = "2026-12-03T07:14:22.100+07:00" })
	mutate("raw_base64_unpadded", "edge.reading", func(m map[string]any) { reading(m, 1)["raw"] = "U1QsR1MsKzAwMTI1MC41a2cNCg" })
	mutate("unit_unknown", "edge.reading", func(m map[string]any) { reading(m, 0)["unit"] = "lb" })
	mutate("key_id_other_key", "edge.key_id", func(m map[string]any) { m["key_id"] = "0000000000000000" })
	mutate("unknown_field", "edge.decode", func(m map[string]any) { m["battery_pct"] = 80 })
	mutate("missing_ntp_offset", "edge.decode", func(m map[string]any) { delete(m, "ntp_offset_ms") })
	mutate("counter_float", "edge.decode", func(m map[string]any) { m["epoch"] = json.Number("1.5") })
	mutate("wrong_format", "edge.format", func(m map[string]any) { m["format"] = "agrigreen.edge.batch.v2" })
	mutate("device_id_uppercase", "edge.device_id", func(m map[string]any) { m["device_id"] = "Scale-01" })
	mutate("signature_short", "edge.signature", func(m map[string]any) { m["signature"] = "AAAA" })
	return vf
}

func TestVectors(t *testing.T) {
	path := filepath.Join(contractsDir, "batch.v1.vectors.json")
	got := build(t)
	if *update {
		b, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("thiếu tệp vector (chạy với -update): %v", err)
	}
	var vf vectorFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatal(err)
	}
	seed, _ := hex.DecodeString(vf.PrivateSeedHex)
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if hex.EncodeToString(pub) != vf.PublicKeyHex || KeyID(pub) != vf.KeyID {
		t.Fatal("khóa trong tệp vector không nhất quán")
	}
	if len(vf.Valid) == 0 || len(vf.Invalid) == 0 {
		t.Fatal("tệp vector rỗng")
	}
	for _, v := range vf.Valid {
		b, err := Decode(v.JSON)
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		if err := Verify(pub, b); err != nil {
			t.Fatalf("%s: chữ ký phải hợp lệ: %v", v.Name, err)
		}
		c, _ := b.Canonical()
		if string(c) != v.Canonical {
			t.Fatalf("%s: dạng chuẩn lệch:\n%q\n%q", v.Name, c, v.Canonical)
		}
		sum, _ := b.PayloadSHA256()
		if hex.EncodeToString(sum[:]) != v.PayloadSHA256 {
			t.Fatalf("%s: payload_sha256 lệch", v.Name)
		}
		// Ed25519 tất định: ký lại cho đúng chữ ký của vector.
		b2 := b
		if err := Sign(ed25519.NewKeyFromSeed(seed), &b2); err != nil || b2.Signature != v.Signature {
			t.Fatalf("%s: ký lại phải ra đúng chữ ký vector: %v", v.Name, err)
		}
	}
	for _, v := range vf.Invalid {
		b, err := Decode(v.JSON)
		if err == nil {
			err = Verify(pub, b)
		}
		if CodeOf(err) != v.Code {
			t.Errorf("%s: muốn mã %s, nhận %v", v.Name, v.Code, err)
		}
	}
	if !*update && !bytes.Equal(normalize(t, raw), normalize(t, mustJSON(t, got))) {
		t.Fatal("tệp vector khác bộ sinh — chạy lại với -update và xem lại diff")
	}
}

func normalize(t *testing.T, b []byte) []byte {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return mustJSON(t, v)
}

// Dạng chuẩn tách được duy nhất: đổi một trường bất kỳ làm đổi dạng chuẩn.
func TestCanonicalCoversEveryField(t *testing.T) {
	base := sampleBatch()
	base.KeyID = KeyID(testKey().Public().(ed25519.PublicKey))
	c0, err := base.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(c0), Format+"\n") || !strings.HasSuffix(string(c0), "\n") || strings.Contains(string(c0), "\r") {
		t.Fatalf("dạng chuẩn sai khung: %q", c0)
	}
	muts := map[string]func(b *Batch){
		"device_id": func(b *Batch) { b.DeviceID = "scale-hub01-inx" },
		"epoch":     func(b *Batch) { b.Epoch = 2 },
		"counters": func(b *Batch) {
			b.CounterFrom, b.CounterTo = 42, 44
			b.Readings[0].Counter, b.Readings[1].Counter, b.Readings[2].Counter = 42, 43, 44
		},
		"key_id":       func(b *Batch) { b.KeyID = "ffffffffffffffff" },
		"ntp_null":     func(b *Batch) { b.NTPOffsetMs = nil },
		"ntp_value":    func(b *Batch) { b.NTPOffsetMs = intp(12) },
		"device_time":  func(b *Batch) { b.Readings[0].DeviceTime = "2026-12-03T00:14:22.101Z" },
		"value_kg":     func(b *Batch) { b.Readings[0].ValueKg = "0.001" },
		"stable":       func(b *Batch) { b.Readings[0].Stable = false },
		"unit":         func(b *Batch) { b.Readings[0].Unit = UnitT },
		"raw_added":    func(b *Batch) { b.Readings[0].Raw = "AA==" },
		"raw_removed":  func(b *Batch) { b.Readings[1].Raw = "" },
		"signature_ok": func(b *Batch) { b.Signature = "x" }, // chữ ký KHÔNG thuộc dạng chuẩn
	}
	for name, f := range muts {
		b := base
		b.Readings = append([]Reading(nil), base.Readings...)
		f(&b)
		c, err := b.Canonical()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if same := bytes.Equal(c, c0); same != (name == "signature_ok") {
			t.Errorf("%s: dạng chuẩn giống nhau = %v", name, same)
		}
	}
}

// Verify chỉ tin khóa của sổ thiết bị: khóa khác (dù lô tự khai key_id của nó) bị từ chối.
func TestVerifyRejectsOtherKey(t *testing.T) {
	b := sampleBatch()
	if err := Sign(testKey(), &b); err != nil {
		t.Fatal(err)
	}
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, 32))
	if err := Verify(other.Public().(ed25519.PublicKey), b); CodeOf(err) != "edge.key_id" {
		t.Fatalf("khóa khác phải bị từ chối: %v", err)
	}
	forged := b
	if err := Sign(other, &forged); err != nil {
		t.Fatal(err)
	}
	if err := Verify(testKey().Public().(ed25519.PublicKey), forged); err == nil {
		t.Fatal("lô ký bằng khóa lạ phải bị từ chối")
	}
}

func TestMaxReadings(t *testing.T) {
	b := Batch{Format: Format, DeviceID: "scale-big", Epoch: 1, CounterFrom: 1, CounterTo: MaxReadings + 1, KeyID: "0123456789abcdef"}
	for i := int64(1); i <= MaxReadings+1; i++ {
		b.Readings = append(b.Readings, Reading{Counter: i, DeviceTime: "2026-12-03T00:00:00.000Z", ValueKg: "1.000", Unit: UnitKg})
	}
	if err := b.Validate(); CodeOf(err) != "edge.counter_range" {
		t.Fatalf("lô quá %d số đo phải bị từ chối: %v", MaxReadings, err)
	}
}

// JSON Schema contracts/edge/batch.v1.json khớp struct Go (trường và required).
func TestSchemaMatchesStructs(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(contractsDir, "batch.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	var readings struct {
		Items struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"items"`
	}
	if err := json.Unmarshal(s.Properties["readings"], &readings); err != nil {
		t.Fatal(err)
	}
	check := func(what string, typ reflect.Type, props map[string]json.RawMessage, required []string, optional ...string) {
		var tags []string
		for i := 0; i < typ.NumField(); i++ {
			tags = append(tags, strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
		}
		var keys []string
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(tags)
		sort.Strings(keys)
		if !reflect.DeepEqual(tags, keys) {
			t.Errorf("%s: trường schema %v khác struct %v", what, keys, tags)
		}
		want := map[string]bool{}
		for _, k := range tags {
			want[k] = true
		}
		for _, o := range optional {
			delete(want, o)
		}
		got := map[string]bool{}
		for _, r := range required {
			got[r] = true
		}
		if !reflect.DeepEqual(want, got) {
			t.Errorf("%s: required schema %v, cần %v", what, required, want)
		}
	}
	check("batch", reflect.TypeFor[Batch](), s.Properties, s.Required)
	check("reading", reflect.TypeFor[Reading](), readings.Items.Properties, readings.Items.Required, "raw")
}
