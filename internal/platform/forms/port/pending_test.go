package port

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestPending293CanonicalOwnedSelectors(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	in := PendingQuery{Profile: "owner", UnitKind: UnitCommune, PeriodIDs: []uuid.UUID{a, b}, Units: []string{"U2", "U1"}, Limit: 1}
	normalized, err := NormalizePendingQuery(in)
	if err != nil {
		t.Fatal(err)
	}
	in.Units[0] = "outside"
	in.PeriodIDs[0] = uuid.Nil
	if normalized.Units[0] != "U1" || normalized.PeriodIDs[0] == uuid.Nil {
		t.Fatal("normalizer aliases caller")
	}
	empty := PendingQuery{Profile: "owner", UnitKind: UnitCommune, Limit: 1}
	nilSlices, e := NormalizePendingQuery(empty)
	if e != nil {
		t.Fatal(e)
	}
	empty.PeriodIDs = []uuid.UUID{}
	empty.Units = []string{}
	empty.TemplateCodes = []string{}
	emptySlices, e := NormalizePendingQuery(empty)
	if e != nil || !reflect.DeepEqual(nilSlices, emptySlices) {
		t.Fatal("nil/empty query not canonical")
	}
}

func TestPending293IncompleteAndAmbiguousGrants(t *testing.T) {
	complete := PendingGrant{Allowed: true, Complete: true, Binding: "current", Scope: PendingScope{}}
	if _, e := NormalizePendingGrant(complete); e != nil {
		t.Fatal("explicit complete empty scope must be usable", e)
	}
	invalid := []PendingGrant{}
	v := complete
	v.Complete = false
	invalid = append(invalid, v)
	v = complete
	v.Binding = ""
	invalid = append(invalid, v)
	v = complete
	v.Scope = PendingScope{AllUnits: true, Units: []string{"U1"}}
	invalid = append(invalid, v)
	v = complete
	v.Scope = PendingScope{PeriodIDs: []uuid.UUID{uuid.Nil}}
	invalid = append(invalid, v)
	v = complete
	v.Scope = PendingScope{Units: []string{"U1", "U1"}}
	invalid = append(invalid, v)
	for _, grant := range invalid {
		if _, e := NormalizePendingGrant(grant); e == nil {
			t.Fatalf("invalid grant accepted: %+v", grant)
		}
	}
}
