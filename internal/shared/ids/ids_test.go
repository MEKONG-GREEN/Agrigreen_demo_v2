package ids

import "testing"

func TestNewIsV7AndOrdered(t *testing.T) {
	a, b := New(), New()
	if a.Version() != 7 || b.Version() != 7 {
		t.Fatal("phải là UUIDv7")
	}
	if a.String() >= b.String() {
		t.Fatalf("UUIDv7 phải tăng dần: %s >= %s", a, b)
	}
}

func TestParseRequiresV7(t *testing.T) {
	if _, err := Parse("6fa459ea-ee8a-4ca4-894e-db77e160355e"); err == nil { // v4
		t.Fatal("phải từ chối UUIDv4")
	}
	if _, err := Parse(New().String()); err != nil {
		t.Fatal(err)
	}
}

func TestParseRefRejectsNil(t *testing.T) {
	if _, err := ParseRef("00000000-0000-0000-0000-000000000000"); err == nil {
		t.Fatal("phải từ chối Nil")
	}
}
