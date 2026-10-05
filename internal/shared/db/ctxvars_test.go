package db

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// W2.4: ánh xạ Principal/KNContext/mục đích → biến app.*; thiếu giá trị → rỗng.
func TestContextVarsFrom_W24(t *testing.T) {
	actor, org, node, unit, helped := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	all := func(m map[string]string) string {
		var b strings.Builder
		for _, k := range []string{VarNodeID, VarDept, VarXaIDs, VarPurpose, VarActorRef, VarOnBehalfOfRef} {
			b.WriteString(k + "=" + m[k] + ";")
		}
		return b.String()
	}
	cases := []struct {
		name string
		ctx  context.Context
		want map[string]string
	}{
		{"không Principal (consumer/job)", context.Background(), map[string]string{}},
		{"chỉ mục đích", authz.WithPurpose(context.Background(), " khuyen-nong "), map[string]string{VarPurpose: "khuyen-nong"}},
		{"token không KN: Nút = org_ref",
			authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: actor, OrgRef: org}),
			map[string]string{VarNodeID: org.String(), VarActorRef: actor.String()}},
		{"KN Cấp Xã: Nút, phòng ban, xã của ngữ cảnh (không lùi về org_ref)",
			authz.WithPurpose(authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: actor, OrgRef: org,
				KN: &authz.KNContext{Role: authz.KNXa, Department: "TT", OrgRef: node, UnitRef: unit, Communes: []string{"30280", "30283"}}}), "khuyen-nong"),
			map[string]string{VarNodeID: node.String(), VarDept: "TT", VarXaIDs: `{"30280","30283"}`, VarPurpose: "khuyen-nong", VarActorRef: actor.String()}},
		{"KN Admin không Nút → node rỗng",
			authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: actor, OrgRef: org, KN: &authz.KNContext{Role: authz.KNAdmin}}),
			map[string]string{VarActorRef: actor.String()}},
		{"chế độ hỗ trợ: on_behalf_of_ref",
			authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: actor,
				KN: &authz.KNContext{Role: authz.KNXa, OrgRef: node, Support: &authz.KNSupport{SessionID: uuid.New(), OnBehalfOf: helped, Reason: "hỗ trợ"}}}),
			map[string]string{VarNodeID: node.String(), VarActorRef: actor.String(), VarOnBehalfOfRef: helped.String()}},
		{"hỗ trợ không gắn tài khoản → on_behalf_of_ref rỗng",
			authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: actor,
				KN: &authz.KNContext{Role: authz.KNXa, Support: &authz.KNSupport{SessionID: uuid.New()}}}),
			map[string]string{VarActorRef: actor.String()}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, want := all(ContextVarsFrom(c.ctx).Settings()), all(c.want); got != want {
				t.Errorf("\n nhận %s\n muốn %s", got, want)
			}
		})
	}
}

func TestTextArrayLiteral(t *testing.T) {
	for in, want := range map[string]string{
		"":        "",
		"a":       `{"a"}`,
		"a|b":     `{"a","b"}`,
		`x"y|z\w`: `{"x\"y","z\\w"}`,
		"a,b|":    `{"a,b",""}`,
	} {
		var xs []string
		if in != "" {
			xs = strings.Split(in, "|")
		}
		if got := textArray(xs); got != want {
			t.Errorf("textArray(%q) = %s, muốn %s", xs, got, want)
		}
	}
}

// Câu lệnh đặt biến: một lượt khứ hồi, chỉ set_config(…, true) (LOCAL), đủ biến theo đúng thứ tự tham số.
func TestSetConfigSQLIsLocalAndComplete(t *testing.T) {
	if strings.Contains(setConfigSQL, "false") || strings.Count(setConfigSQL, ", true)") != 7 {
		t.Fatalf("mọi set_config phải là LOCAL: %s", setConfigSQL)
	}
	if n := len(ContextVars{}.args()); n != 6 {
		t.Fatalf("args %d, muốn 6", n)
	}
	for i, k := range []string{"role", VarNodeID, VarDept, VarXaIDs, VarPurpose, VarActorRef, VarOnBehalfOfRef} {
		frag := "set_config('" + k + "', $" + string(rune('1'+i)) + ", true)"
		if !strings.Contains(setConfigSQL, frag) {
			t.Errorf("thiếu %s", frag)
		}
	}
}
