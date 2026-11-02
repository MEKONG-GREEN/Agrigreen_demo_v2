package app

import (
	"bytes"
	"io"
	"testing"
)

func TestArchive199BudgetCannotBeBypassedByCopy(t *testing.T) {
	b := &archiveBudgetBuffer{limit: 10}
	if n, err := io.Copy(b, bytes.NewBufferString("12345678901")); err == nil || n != 0 || len(b.Bytes()) != 0 {
		t.Fatal("copy bypassed hard cap", n, err)
	}
	if n, err := b.Write([]byte("1234567890")); err != nil || n != 10 {
		t.Fatal(n, err)
	}
	if n, err := b.Write([]byte("x")); err == nil || n != 0 || len(b.Bytes()) != 10 {
		t.Fatal("overflow modified buffer", n, err)
	}
}
