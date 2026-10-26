package contextdata_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func ref(n int) uuid.UUID { return uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%012d", n)) }

const goldenName = "db-context-native.json"

var settingNames = []string{db.VarNodeID, db.VarDept, db.VarXaIDs, db.VarPurpose, db.VarActorRef, db.VarOnBehalfOfRef}

type contextCase struct {
	name      string
	principal *authz.Principal
	purpose   string
}

func (c contextCase) context() context.Context {
	ctx := context.Background()
	if c.principal != nil {
		ctx = authz.WithPrincipal(ctx, *c.principal)
	}
	return authz.WithPurpose(ctx, c.purpose)
}

type contextResult struct {
	Name         string            `json:"name"`
	Settings     map[string]string `json:"settings"`
	RequiresNode bool              `json:"requires_node"`
}

// The matrix is captured before changing the native projection. The fixture is
// an external expectation for later owner adapters, rather than a second copy
// of the policy which would silently change along with the implementation.
func nativeCases() []contextCase {
	out := []contextCase{{name: "job"}, {name: "job-purpose", purpose: "  kn.report  "}}
	realms := []struct {
		name  string
		roles []string
	}{
		{"none", nil}, {"kn-user", []string{"kn_user"}},
		{"coop", []string{"coop_manager"}}, {"officer", []string{"kn_officer"}},
		{"viewer", []string{"plot_viewer", "farmer"}},
		{"admin", []string{"platform_admin"}}, {"province", []string{"kn_province_admin"}},
		{"national", []string{"kn_national_admin"}},
		{"coop-admin", []string{"coop_manager", "platform_admin"}},
		{"officer-province", []string{"kn_officer", "kn_province_admin"}},
		{"officer-national", []string{"kn_officer", "kn_national_admin"}},
		{"coop-officer", []string{"coop_manager", "kn_officer"}},
		{"coop-province", []string{"coop_manager", "kn_province_admin"}},
		{"all", []string{"coop_manager", "kn_officer", "platform_admin", "kn_province_admin", "kn_national_admin"}},
	}
	for _, realm := range realms {
		for _, tokenNode := range []bool{false, true} {
			for _, role := range []string{"", authz.KNXa, authz.KNTinh, authz.KNTW, authz.KNAdmin} {
				for _, selectedNode := range []bool{false, true} {
					if role == "" && selectedNode {
						continue
					}
					p := authz.Principal{ActorRef: ref(1), TenantID: ref(2), Roles: slices.Clone(realm.roles)}
					if tokenNode {
						p.OrgRef = ref(3)
					}
					if role != "" {
						p.KN = &authz.KNContext{ID: ref(4), AssignmentID: ref(5), Role: role, Department: "trong_trot", UnitRef: ref(6)}
						if selectedNode {
							p.KN.OrgRef = ref(7)
						}
					}
					name := fmt.Sprintf("realm/%s/token-node/%t/session/%s/selected-node/%t", realm.name, tokenNode, role, selectedNode)
					out = append(out, contextCase{name: name, principal: &p})
				}
			}
		}
	}
	full := func() *authz.Principal {
		return &authz.Principal{ActorRef: ref(1), TenantID: ref(2), OrgRef: ref(3), Roles: []string{"kn_user"},
			KN: &authz.KNContext{ID: ref(4), AssignmentID: ref(5), Role: authz.KNXa, Department: "chan_nuoi", OrgRef: ref(7), UnitRef: ref(6), Communes: []string{"89011", "89012"}}}
	}
	for _, variant := range []string{"nil-communes", "empty-communes", "escaped-communes", "duplicate-communes", "blank-department", "support", "support-without-account", "zero-actor", "zero-token-tenant", "purpose", "empty-purpose", "view-only", "admin-ignores-token-node", "empty-realm-roles"} {
		p := full()
		purpose := ""
		switch variant {
		case "nil-communes":
			p.KN.Communes = nil
		case "empty-communes":
			p.KN.Communes = []string{}
		case "escaped-communes":
			p.KN.Communes = []string{"a,b", "", `x"y`, `z\w`, "x}y", "NULL", "đơn vị"}
		case "duplicate-communes":
			p.KN.Communes = []string{"89011", "89011", "92001"}
		case "blank-department":
			p.KN.Department = ""
		case "support":
			p.KN.Support = &authz.KNSupport{SessionID: ref(8), OnBehalfOf: ref(9), Reason: "synthetic support"}
		case "support-without-account":
			p.KN.Support = &authz.KNSupport{SessionID: ref(8)}
		case "zero-actor":
			p.ActorRef = uuid.Nil
		case "zero-token-tenant":
			p.TenantID = uuid.Nil
		case "purpose":
			purpose = "  kn.report  "
		case "empty-purpose":
			purpose = "  "
		case "view-only":
			p.KN.ViewOnly = true
		case "admin-ignores-token-node":
			p.KN.Role = authz.KNAdmin
			p.KN.OrgRef = uuid.Nil
		case "empty-realm-roles":
			p.Roles = []string{}
		}
		out = append(out, contextCase{name: "values/" + variant, principal: p, purpose: purpose})
	}
	return out
}

func snapshot() []contextResult {
	cases := nativeCases()
	out := make([]contextResult, 0, len(cases))
	for _, c := range cases {
		r := contextResult{Name: c.name, Settings: db.ContextVarsFrom(c.context()).Settings()}
		if c.principal != nil {
			r.RequiresNode = db.RequiresNode(*c.principal)
		}
		out = append(out, r)
	}
	return out
}

func golden(t *testing.T) []contextResult {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "golden", goldenName))
	if err != nil {
		t.Fatal(err)
	}
	var out []contextResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLegacyContextProjectionContract(t *testing.T) {
	raw, err := json.MarshalIndent(snapshot(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	want, err := os.ReadFile(filepath.Join("..", "golden", goldenName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatal("native context projection differs from frozen contract")
	}
	t.Logf("%d frozen native projection vectors", len(snapshot()))
}
