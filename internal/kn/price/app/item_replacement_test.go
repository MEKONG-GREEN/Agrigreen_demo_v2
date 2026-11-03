package app

import (
	"context"
	"errors"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestCreateItemTxRequiresVerifiedReplacementPermission(t *testing.T) {
	c := caller{ref: ids.New(), tenant: ids.New(), level: "TINH", steward: true}
	other := c
	other.tenant = ids.New()
	actor := c
	actor.ref = ids.New()
	for name, permission := range map[string]replacementPermission{
		"missing": {}, "unverified": {caller: c},
		"other tenant": {caller: other, verified: true}, "other actor": {caller: actor, verified: true},
	} {
		t.Run(name, func(t *testing.T) {
			// A private caller (including proposal approval) cannot reach a TX read,
			// number allocation or either ledger without the external capability.
			var service ItemService
			proposal := ids.New()
			_, err := service.createItemTx(context.Background(), nil, c, ItemInput{ReplacesCode: "MH00001"}, permission, &proposal, "test")
			if !errors.Is(err, errNotSteward) {
				t.Fatalf("unverified replacement reached DB: %v", err)
			}
		})
	}
}
