package contact

import (
	"bytes"
	"os"
	"testing"
)

// Nguồn menu của module là đúng tệp WH-A menus/base.json mà bộ sinh compatibility đọc (KNSPLIT-2-MENU-KN).
func TestMenuSourceIsModuleRegistration(t *testing.T) {
	raw, err := os.ReadFile("menus/base.json")
	if err != nil {
		t.Fatal(err)
	}
	got := MenuSource()
	if !bytes.Equal(got, raw) {
		t.Fatal("MenuSource differs from menus/base.json")
	}
	got[0] = 'x'
	if MenuSource()[0] == 'x' {
		t.Fatal("MenuSource leaked embedded bytes")
	}
}
