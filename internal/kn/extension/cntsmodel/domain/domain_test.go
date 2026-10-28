package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func d(s string) time.Time {
	t, _ := time.Parse(time.DateOnly, s)
	return t
}

func fields(es Errors) map[string]bool {
	m := map[string]bool{}
	for _, e := range es {
		m[e.Field] = true
	}
	return m
}

func TestProjectNormalize(t *testing.T) {
	end := d("2027-01-01")
	b := decimal.RequireFromString("1.5")
	p := Project{Code: " da/01 ", Name: "Dự án", Kind: KindNational, Start: d("2027-03-01"), End: &end, Budget: &b, ManagerRef: uuid.New()}
	f := fields(p.Normalize())
	if p.Code != "DA/01" || !f["end_date"] || !f["budget"] || len(f) != 2 {
		t.Fatalf("dự án: %v %+v", f, p)
	}
}

func TestGrantRoles(t *testing.T) {
	site, hh := uuid.New(), uuid.New()
	cases := []struct {
		g    Grant
		want string
	}{
		{Grant{Role: "admin", Communes: []string{"1"}, ValidFrom: d("2027-01-01")}, "role"},
		{Grant{Role: RoleFollower, Communes: []string{"1"}, ValidFrom: d("2027-01-01")}, "site_refs"},
		{Grant{Role: RoleKSGM, Communes: []string{"1"}, Sites: []uuid.UUID{site}, ValidFrom: d("2027-01-01")}, "site_refs"},
		{Grant{Role: RoleFarmer, Communes: []string{"1"}, ValidFrom: d("2027-01-01")}, "household_refs"},
		{Grant{Role: RoleKSGM, Communes: []string{"1"}, Forms: []string{"BM-20"}, ValidFrom: d("2027-01-01")}, "forms"},
		{Grant{Role: RoleKSGM, ValidFrom: d("2027-01-01")}, "commune_codes"},
	}
	for _, c := range cases {
		if f := fields(c.g.Normalize()); !f[c.want] {
			t.Errorf("%s: muốn lỗi %s, nhận %v", c.g.Role, c.want, f)
		}
	}
	ok := Grant{Role: RoleFarmer, Communes: []string{"1", "1"}, Households: []uuid.UUID{hh, hh}, Forms: []string{"bm-08"}, ValidFrom: d("2027-01-01")}
	if es := ok.Normalize(); len(es) > 0 || len(ok.Communes) != 1 || len(ok.Households) != 1 || ok.Forms[0] != "BM-08" {
		t.Fatalf("nông hộ: %v %+v", es, ok)
	}
}

func TestGrantStatusAndCovers(t *testing.T) {
	to := d("2027-06-30")
	for _, c := range []struct {
		ev, today, want string
	}{{EventGranted, "2026-12-31", StatusPending}, {EventGranted, "2027-03-01", StatusActive}, {EventUpdated, "2027-07-01", StatusExpired},
		{EventRevoked, "2027-03-01", StatusRevoked}} {
		if got := GrantStatus(c.ev, d("2027-01-01"), &to, d(c.today)); got != c.want {
			t.Errorf("%s %s: %s", c.ev, c.today, got)
		}
	}
	site, other := uuid.New(), uuid.New()
	g := Grant{Communes: []string{"A"}, Sites: []uuid.UUID{site}}
	if !g.Covers("A", site, uuid.Nil) || g.Covers("A", other, uuid.Nil) || g.Covers("B", site, uuid.Nil) {
		t.Fatal("Covers theo điểm")
	}
	if !(Grant{Communes: []string{"A"}}).Covers("A", other, uuid.Nil) {
		t.Fatal("quyền theo xã bao mọi điểm của xã")
	}
}

func TestHouseholdKeeper(t *testing.T) {
	h := Household{}
	if f := fields(h.Normalize()); !f["keeper_ref"] {
		t.Fatalf("thiếu cán bộ ghi nhật ký: %v", f)
	}
	v := decimal.RequireFromString("10")
	h = Household{KeeperKind: KeeperSelf, Scale: Scale{Value: &v}}
	if f := fields(h.Normalize()); !f["scale"] {
		t.Fatalf("thiếu đơn vị: %v", f)
	}
}
