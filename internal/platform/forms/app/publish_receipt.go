package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"slices"
	"strings"
	"time"
)

func sameDate(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.UTC().Format(time.DateOnly) == b.UTC().Format(time.DateOnly)
}
func sameReceiptSpec(a, b domain.Spec) bool {
	return a.UnitKind == b.UnitKind && slices.Equal(a.UnitParents, b.UnitParents) && a.MaxSubmissions == b.MaxSubmissions && a.ReviewFlow == b.ReviewFlow && a.PeriodKind == b.PeriodKind && a.PeriodKey == b.PeriodKey && sameDate(a.From, b.From) && sameDate(a.To, b.To) && a.PeriodRule == b.PeriodRule
}
func batchReceipts(ctx context.Context, tx pgx.Tx, a actor, in port.BatchInput, specs []domain.Spec, key string) ([]port.PublishedPeriod, bool, error) {
	out := []port.PublishedPeriod{}
	seen := map[string]bool{}
	found := 0
	existingCount, err := store.PublishReceiptCount(ctx, tx, key)
	if err != nil {
		return nil, false, err
	}
	if existingCount != 0 && existingCount != len(in.Periods) {
		return nil, false, problem.Conflict.WithDetail("Khóa lô đã dùng cho tập kỳ khác")
	}
	for i, period := range in.Periods {
		part := period.Key
		if part == "" {
			part = period.From.Format(time.DateOnly)
		}
		if seen[part] {
			return nil, false, problem.Validation.WithDetail("Khóa kỳ trùng trong cùng lô")
		}
		seen[part] = true
		previous, err := store.PeriodByIdem(ctx, tx, idemOr(key, "period:"+part))
		if err != nil {
			return nil, false, err
		}
		if previous == nil {
			continue
		}
		found++
		existingSpec, err := store.SpecOf(ctx, tx, previous.ID)
		if err != nil {
			return nil, false, err
		}
		sameOwner := a.node == uuid.Nil && previous.OwnerRef == nil || previous.OwnerRef != nil && *previous.OwnerRef == a.node
		if !a.allowsTemplate(previous.TemplateCode) || previous.Profile != in.Profile || previous.TemplateID != in.TemplateID || previous.ActorRef != a.ref || previous.Label != strings.TrimSpace(period.Label) || !previous.DueAt.Equal(period.DueAt) || !slices.Equal(previous.Units, in.Units) || !sameOwner || !sameReceiptSpec(existingSpec, specs[i]) {
			return nil, false, problem.Conflict.WithDetail("Khóa phát hành đã dùng cho nội dung hoặc người gọi khác")
		}
		out = append(out, port.PublishedPeriod{ID: previous.ID, Key: period.Key, Label: previous.Label, DueAt: previous.DueAt})
	}
	if existingCount > 0 && found != existingCount || found > 0 && found != len(in.Periods) {
		return nil, false, problem.Conflict.WithDetail("Lô phát hành chưa có biên nhận đầy đủ")
	}
	return out, found == len(in.Periods), nil
}
