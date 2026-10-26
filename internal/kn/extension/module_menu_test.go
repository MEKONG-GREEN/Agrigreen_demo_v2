package extension

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

func memberSources(t *testing.T) [][]byte {
	t.Helper()
	var out [][]byte
	for _, path := range []string{"../hrm/menus/base.json", "../price/menus/base.json", "../contact/menus/base.json"} {
		raw, err := os.ReadFile(path) //nolint:gosec // tệp nguồn cố định của repo
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, raw)
	}
	return out
}

// Gốc lắp ráp phải chuyển đúng nguồn của các module thành viên KN; thiếu/thừa/trùng là lỗi khởi động.
func TestMenuCatalogRequiresExactMemberSources(t *testing.T) {
	members := memberSources(t)
	catalog, err := MenuCatalog(members...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Registry(); err != nil {
		t.Fatal(err)
	}
	for name, bad := range map[string][][]byte{
		"missing contact": members[:2],
		"duplicate hrm":   {members[0], members[0], members[1], members[2]},
		"own source":      append([][]byte{[]byte(`{"id":"extension-menus","order":0}`)}, members...),
		"none":            nil,
	} {
		if _, err := MenuCatalog(bad...); err == nil || (name != "own source" && !errors.Is(err, ErrMenuMembers)) {
			t.Errorf("%s: %v", name, err)
		}
	}
	reordered, err := MenuCatalog(members[2], members[0], members[1])
	if err != nil || !reflect.DeepEqual(reordered.Routes(), catalog.Routes()) {
		t.Fatalf("order-independent catalog: %v", err)
	}
}
