package app

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
)

func (s *Service) selectedCNTSTemplate(ctx context.Context, in CNTSSeriesInput) (formsport.TemplateRef, error) {
	if in.TemplateID == uuid.Nil || in.TemplateVersion < 1 {
		return formsport.TemplateRef{}, problem.Validation.WithField("template_id", "ID và phiên bản phải được chọn cùng nhau")
	}
	cat, err := s.designCatalog()
	if err != nil {
		return formsport.TemplateRef{}, err
	}
	v, err := cat.TemplateVersion(ctx, domain.ProfileCNTS, in.Code, in.TemplateVersion)
	if err != nil {
		return formsport.TemplateRef{}, err
	}
	if v.ID != in.TemplateID || v.Code != in.Code || v.Profile != domain.ProfileCNTS || v.Version != in.TemplateVersion {
		return formsport.TemplateRef{}, formsport.ErrTemplateNotFound
	}
	return formsport.TemplateRef{ID: v.ID, Code: v.Code, Version: v.Version, Title: v.Title, Fields: v.Fields}, nil
}

func (s *Service) currentCNTSWrite(k port.SessionContext) error {
	if k.ReadOnly() {
		return ErrReadOnly
	}
	if !k.ExpiresAt.IsZero() && !k.ExpiresAt.After(s.now()) {
		return problem.Forbidden.WithDetail("Ngữ cảnh phiên đã hết hạn")
	}
	if k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(k.Support.Reason)) < 10 {
		return ErrSupportReason
	}
	return nil
}

// AuthorizeCNTSPublish is also called before HTTP response replay. The selected
// account session is resolved by accounts before the module middleware executes.
func (s *Service) AuthorizeCNTSPublish(ctx context.Context) error {
	_, k, err := port.RequireSession(ctx, port.SessionRoleProvince)
	if err != nil {
		return err
	}
	if err := s.currentCNTSWrite(k); err != nil {
		return err
	}
	if !designWriter(k, domain.CNTSBM17) {
		return errDesignWriter
	}
	return nil
}

func (s *Service) AuthorizeCNTSDesignWrite(ctx context.Context, code string) error {
	k, err := designReader(ctx)
	if err != nil {
		return err
	}
	if err := s.currentCNTSWrite(k); err != nil {
		return err
	}
	if !knownDesign(code) {
		return formsport.ErrTemplateNotFound
	}
	if domain.CNTSLocked(code) {
		return errDesignLocked
	}
	if !designWriter(k, code) {
		return errDesignWriter
	}
	return nil
}
