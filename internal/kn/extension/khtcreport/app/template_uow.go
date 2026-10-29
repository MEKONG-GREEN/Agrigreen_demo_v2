package app

import (
	"context"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	templatestore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// checkNewFixedSelection rechecks a first draft's pre-read selection in its
// existing pf_extension write transaction. It shares the publication lock and
// uses that same connection; no port call, capture or nested transaction occurs.
// Existing persisted revisions must not call this function to rebind history.
func checkNewFixedSelection(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, pinned *port.KHTCTemplateSnapshot, content domain.Draft) error {
	if !templatedomain.Fixed(content.Data.Template) {
		return nil
	}
	if pinned == nil {
		return ErrInvalid.WithDetail("Bản soạn mới chưa ghim mẫu")
	}
	if _, err := templatedomain.PinnedDefinition(*pinned, content.Data.Template, content.Data.Period.From); err != nil {
		return ErrInvalid.WithCause(err)
	}
	if err := templatestore.Lock(ctx, tx, tenantID, pinned.Key); err != nil {
		return err
	}
	first, err := templatestore.FirstEffective(ctx, tx, pinned.Key)
	if err != nil {
		return err
	}
	legacy := first == nil || content.Data.Period.From.Before(*first)
	if legacy {
		if pinned.Mode != "legacy" {
			return ErrVersion.WithDetail("Mẫu áp dụng đã thay đổi, tải lại trước khi lưu")
		}
		return nil
	}
	if pinned.Mode != "published" {
		return ErrVersion.WithDetail("Mẫu vừa được phát hành, tải lại trước khi lưu")
	}
	publication, err := templatestore.Applicable(ctx, tx, pinned.Key, content.Data.Period.From)
	if err != nil {
		return err
	}
	if publication == nil {
		return ErrInvalid.WithDetail("Thiếu mẫu đã phát hành cho kỳ này")
	}
	if publication.VersionID != pinned.ID || publication.Version != pinned.Version || publication.Hash != pinned.Hash || !publication.AppliesFrom.Equal(pinned.ApplicableFrom) {
		return ErrVersion.WithDetail("Mẫu áp dụng đã thay đổi, tải lại trước khi lưu")
	}
	version, err := templatestore.ByID(ctx, tx, pinned.ID)
	if err != nil {
		return err
	}
	if version == nil || version.Key != pinned.Key || version.Version != pinned.Version || version.Hash != pinned.Hash || version.Rule != pinned.Rule {
		return ErrInvalid.WithDetail("Tham chiếu mẫu đã ghim không khớp")
	}
	_, actual, err := version.Definition.BytesHash()
	if err != nil {
		return ErrInvalid.WithCause(err)
	}
	if actual != pinned.Hash {
		return ErrInvalid.WithDetail("Băm nguồn mẫu không khớp")
	}
	return nil
}
