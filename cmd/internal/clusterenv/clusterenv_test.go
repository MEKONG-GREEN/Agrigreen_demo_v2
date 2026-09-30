package clusterenv

import (
	"reflect"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestParseDefaultIsAllClusters(t *testing.T) {
	got, err := Parse("  ")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, migrations.Clusters) {
		t.Fatalf("rỗng phải là mọi cụm, được %v", got)
	}
}

func TestParseKeepsCanonicalOrder(t *testing.T) {
	got, err := Parse("ts, MKG")
	if err != nil {
		t.Fatal(err)
	}
	want := []migrations.Cluster{migrations.MKG, migrations.TS}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("được %v, muốn %v", got, want)
	}
	if !Has(got, migrations.TS) || Has(got, migrations.PF) {
		t.Fatalf("Has sai: %v", got)
	}
}

func TestParseRejectsUnknownAndDuplicate(t *testing.T) {
	for _, v := range []string{"pf,xx", "mkg,mkg", ",", "platform"} {
		if _, err := Parse(v); err == nil {
			t.Errorf("%q phải lỗi", v)
		}
	}
}

func TestBaseURLOverride(t *testing.T) {
	t.Setenv("AG_MKG_DB_URL", "postgres://pg-mkg-rw.agrigreen-data.svc:5432/?sslmode=require")
	if got := BaseURL(migrations.MKG, "postgres://shared:5432/"); got != "postgres://pg-mkg-rw.agrigreen-data.svc:5432/?sslmode=require" {
		t.Fatalf("mkg: %s", got)
	}
	t.Setenv("AG_PF_DB_URL", "")
	if got := BaseURL(migrations.PF, "postgres://shared:5432/"); got != "postgres://shared:5432/" {
		t.Fatalf("pf phải dùng URL chung: %s", got)
	}
	if EnvBaseURL(migrations.Vault) != "AG_VAULT_DB_URL" {
		t.Fatal(EnvBaseURL(migrations.Vault))
	}
}
