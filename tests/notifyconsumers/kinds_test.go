package notifyconsumers_test

import (
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	"os"
	"testing"
)

func TestOwnedKindsMatchLegacyContractAndAreFresh(t *testing.T) {
	var old struct{ Kinds []string }
	raw, err := os.ReadFile("../golden/kn-notify-kinds.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if len(old.Kinds) != 43 {
		t.Fatal("missing legacy kinds")
	}
	allowed := map[string]bool{}
	for _, kind := range old.Kinds {
		allowed[kind] = true
	}
	extensionKinds, priceKinds := extension.NotificationKinds(), price.NotificationKinds()
	if len(extensionKinds.Kinds) != 16 || len(priceKinds.Kinds) != 9 {
		t.Fatal("source kind counts")
	}
	seen := map[string]bool{}
	for _, kind := range append(extensionKinds.Kinds, priceKinds.Kinds...) {
		if !allowed[kind] || seen[kind] {
			t.Fatalf("unowned or duplicate kind %s", kind)
		}
		seen[kind] = true
	}
	extensionKinds.Kinds[0] = "corrupt"
	priceKinds.Kinds[0] = "corrupt"
	if extension.NotificationKinds().Kinds[0] == "corrupt" || price.NotificationKinds().Kinds[0] == "corrupt" {
		t.Fatal("source declarations share mutable slices")
	}
}
