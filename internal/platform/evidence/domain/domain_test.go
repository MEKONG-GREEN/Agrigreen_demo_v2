package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestParseFileMeta(t *testing.T) {
	now := time.Now().UTC()
	pub, _, _ := envelope.GenerateKeyPair(nil)
	s, err := envelope.Seal([]byte("ảnh"), pub)
	if err != nil {
		t.Fatal(err)
	}
	m := envelope.NewMetadata(ids.New().String(), s, "image/jpeg", now.Format(time.RFC3339))
	m.GPS = &envelope.GPS{Lat: 10.03, Lon: 105.78, AccuracyM: 6.5}
	f, errs := ParseFileMeta(m, int64(len(s.Ciphertext)), now)
	if len(errs) > 0 || f.GPSLat == nil || *f.GPSLat != 10.03 {
		t.Fatalf("metadata hợp lệ: %v", errs)
	}
	bad := m
	bad.WrappedDEK = bad.WrappedDEK[:79]
	if _, errs := ParseFileMeta(bad, int64(len(s.Ciphertext)), now); len(errs) != 1 || errs[0].Field != "meta.wrapped_dek" {
		t.Fatalf("wrapped_dek 79 byte: %v", errs)
	}
	bad = m
	bad.V, bad.SubjectRef = 2, ""
	if _, errs := ParseFileMeta(bad, port.MaxFileBytes+1, now); len(errs) != 3 {
		t.Fatalf("v, subject_ref, kích thước: %v", errs)
	}
}

func TestValidateSubject(t *testing.T) {
	if errs := ValidateSubject(port.Subject{Module: "pf/cooperative", Type: "diary_entry", ID: ids.New()}); len(errs) != 0 {
		t.Fatalf("bộ ba hợp lệ: %v", errs)
	}
	for _, s := range []port.Subject{
		{Module: "cooperative", Type: "plot", ID: ids.New()},
		{Module: "dom/cooperative", Type: "plot", ID: ids.New()},
		{Module: "pf/cooperative", Type: "Plot", ID: ids.New()},
		{Module: "pf/cooperative", Type: "plot", ID: uuid.Nil},
	} {
		if errs := ValidateSubject(s); len(errs) != 1 {
			t.Errorf("%+v: %v", s, errs)
		}
	}
}

func TestVisible(t *testing.T) {
	commune := authz.Scope{Units: []string{"30001"}}
	cases := []struct {
		name string
		sc   authz.Scope
		r    port.Resolved
		want bool
	}{
		{"cùng xã", commune, port.Resolved{CommuneCode: "30001"}, true},
		{"xã khác", commune, port.Resolved{CommuneCode: "30002"}, false},
		{"qua cấp phép chia sẻ", commune, port.Resolved{CommuneCode: "30002", ViaShareGrant: true}, true},
		{"toàn quốc", authz.Scope{All: true}, port.Resolved{CommuneCode: "30002"}, true},
		{"chủ thể không có mã đơn vị", commune, port.Resolved{}, false},
		{"phạm vi rỗng", authz.Scope{}, port.Resolved{CommuneCode: "30001"}, false},
	}
	for _, c := range cases {
		if got := Visible(c.sc, c.r); got != c.want {
			t.Errorf("%s: %v, muốn %v", c.name, got, c.want)
		}
	}
}
