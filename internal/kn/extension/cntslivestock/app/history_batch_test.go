package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

type historyNamesBatch276 struct {
	client.Client
	names  map[uuid.UUID]client.RevisionNames
	calls  [][]uuid.UUID
	failAt int
}

func (s *historyNamesBatch276) ResolveRevisions(_ context.Context, purpose client.Purpose, refs []uuid.UUID) ([]client.RevisionNames, error) {
	s.calls = append(s.calls, append([]uuid.UUID(nil), refs...))
	if purpose != client.PurposeFieldSupport || len(refs) > client.MaxRevisionRefs {
		return nil, errors.New("request outside revision resolver contract")
	}
	if len(s.calls) == s.failAt {
		return nil, errors.New("required audit failed")
	}
	out := make([]client.RevisionNames, 0, len(refs))
	for _, ref := range refs {
		if n, ok := s.names[ref]; ok {
			out = append(out, n)
		}
	}
	return out, nil
}

func historyNamesFixture276() ([]store.Household, map[uuid.UUID]client.RevisionNames) {
	subject := uuid.Must(uuid.NewV7())
	versions := make([]store.Household, client.MaxRevisionRefs+1)
	names := make(map[uuid.UUID]client.RevisionNames, len(versions))
	for i := range versions {
		ref := uuid.Must(uuid.NewV7())
		versions[i] = store.Household{PersonRef: subject, PIIRevisionRef: &ref, ChangedFields: []string{domain.FieldOwnerName}}
		names[ref] = client.RevisionNames{Ref: ref, SubjectRef: subject, PrevName: fmt.Sprintf("Tên cũ %d", i), NextName: fmt.Sprintf("Tên mới %d", i)}
	}
	return versions, names
}

func TestHistoryNamesBatch276AllKnownVersions(t *testing.T) {
	versions, names := historyNamesFixture276()
	vault := &historyNamesBatch276{names: names}
	service := Service{Vault: vault}
	got := service.revisionNames(context.Background(), versions)
	if len(got) != len(names) {
		t.Fatalf("valid history lost names: got %d revisions, want all %d", len(got), len(names))
	}
	for ref, want := range names {
		if got[ref] != want {
			t.Fatalf("wrong immutable revision names for %s", ref)
		}
	}
	if len(vault.calls) < 2 {
		t.Fatal("history exceeding one bounded vault request was not batched")
	}
}

func TestHistoryNamesBatch276AuditFailureHidesAllNames(t *testing.T) {
	versions, names := historyNamesFixture276()
	vault := &historyNamesBatch276{names: names, failAt: 2}
	service := Service{Vault: vault}
	got := service.revisionNames(context.Background(), versions)
	if len(vault.calls) != 2 {
		t.Fatalf("required later audit failure not reached: calls=%d", len(vault.calls))
	}
	if len(got) != 0 {
		t.Fatalf("failed history audit exposed %d plaintext revisions", len(got))
	}
}
