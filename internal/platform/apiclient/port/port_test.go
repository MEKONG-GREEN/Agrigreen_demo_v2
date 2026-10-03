package port_test

import (
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// ADR-0022 mục 4: dạng khóa agk_<prefix 12 base32>_<bí mật 43 base64url>; sai dạng → không có prefix.
func TestParseKey_ADR0022(t *testing.T) {
	secret := strings.Repeat("A", 42) + "_"
	prefix, ok := port.ParseKey("agk_abcdefgh2345_" + secret)
	if !ok || prefix != "abcdefgh2345" {
		t.Fatalf("khóa hợp lệ: %q %v", prefix, ok)
	}
	for _, bad := range []string{"", "agk_abcdefgh2345", "agk_ABCDEFGH2345_" + secret, "agk_abcdefgh2345_" + secret + "x",
		"xyz_abcdefgh2345_" + secret, "agk_abcdefgh1345_" + secret, " agk_abcdefgh2345_" + secret} {
		if _, ok := port.ParseKey(bad); ok {
			t.Errorf("khóa sai dạng được nhận: %q", bad)
		}
	}
}

// G5-0 hợp đồng đóng băng: mọi sản phẩm dữ liệu có hằng tên ở port, loại cấp quyền dp_* hợp lệ ở
// pf/party (khớp CHECK pf 000047) và scope theo ProductScope.
func TestG5FrozenDataProducts(t *testing.T) {
	all, err := dataproduct.All()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{port.ProductCollectionVolume: false, port.ProductHubCapacity: false, port.ProductMRVPackageStatus: false}
	for _, p := range all {
		if _, ok := names[p.Name]; !ok {
			t.Errorf("sản phẩm %s chưa có hằng tên trong apiclient/port", p.Name)
		}
		names[p.Name] = true
		if !partyport.ValidKind(p.GrantKind) || partyport.DataProductKind(p.Name) != p.GrantKind {
			t.Errorf("%s: loại cấp quyền %s chưa có trong party/port.ShareKinds", p.Name, p.GrantKind)
		}
		if !p.HasScope(port.ProductScope(p.Name, port.ActionRead)) {
			t.Errorf("%s: thiếu scope %s", p.Name, port.ProductScope(p.Name, port.ActionRead))
		}
	}
	for n, seen := range names {
		if !seen {
			t.Errorf("hằng %s không có hợp đồng contracts/dataproduct", n)
		}
	}
}
