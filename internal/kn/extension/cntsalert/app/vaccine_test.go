package app

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	caldomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/domain"
)

// Khóa chống trùng mũi tiêm gắn ngày dự kiến; lần sinh lại có hậu tố ":rN" và doseBase trả về khóa gốc (Codex #168).
func TestDoseScopeGenerations(t *testing.T) {
	b := uuid.MustParse("0b6c1c8e-6c84-4c4e-9a43-1d2b3c4d5e6f")
	d := caldomain.Dose{Vaccine: "VX_ND_F", Shot: 1, Expected: time.Date(2027, 10, 20, 0, 0, 0, 0, time.UTC)}
	base := doseScope(b, d, 0)
	if !strings.HasSuffix(base, ":VX_ND_F/1:2027-10-20") {
		t.Fatalf("khóa gốc phải gắn ngày dự kiến: %s", base)
	}
	moved := d
	moved.Expected = moved.Expected.AddDate(0, 0, 1)
	if doseScope(b, moved, 0) == base {
		t.Fatal("dời ngày dự kiến phải đổi khóa")
	}
	r2 := doseScope(b, d, 1)
	if r2 != base+":r2" || doseBase(r2) != base || doseBase(base) != base || doseBase(doseScope(b, d, 12)) != base {
		t.Fatalf("lần sinh lại: %s → %s", r2, doseBase(r2))
	}
	if x := "dose:" + b.String() + ":VX_r/1:2027-10-20"; doseBase(x) != x {
		t.Fatalf("mã vaccine chứa ':r' không bị cắt nhầm: %s", doseBase(x))
	}
}
