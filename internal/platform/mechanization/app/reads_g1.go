package app

import (
	"context"

	"github.com/google/uuid"
)

// coopCode trả Mã HTX (pf/party) — lỗi tra cứu → rỗng, không chặn báo cáo (G-10).
func (s *Service) coopCode(ctx context.Context, ref uuid.UUID, cache map[string]string) string {
	key := "code:" + ref.String()
	if c, ok := cache[key]; ok {
		return c
	}
	code := ""
	if o, err := s.Orgs.Get(ctx, ref); err == nil {
		code = o.Code
	}
	cache[key] = code
	return code
}
