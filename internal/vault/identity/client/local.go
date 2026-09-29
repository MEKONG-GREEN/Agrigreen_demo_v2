package client

import (
	"context"

	"github.com/google/uuid"
)

// NewLocal bọc một hiện thực chạy cùng tiến trình (dịch vụ app của kho định
// danh, dựng bởi internal/vault/identity) thành Client. Bộ chuyển đổi chặn
// sớm lời gọi thiếu mục đích; kiểm vai, tenant và nhật ký do dịch vụ làm theo
// Principal trong ctx — giống hệt đường HTTP.
func NewLocal(service Client) Client { return local{s: service} }

type local struct{ s Client }

var _ Client = local{}

func (l local) Create(ctx context.Context, purpose Purpose, id Identity) (Created, error) {
	if purpose == "" {
		return Created{}, ProblemPurposeRequired
	}
	return l.s.Create(ctx, purpose, id)
}

func (l local) Display(ctx context.Context, purpose Purpose, ref uuid.UUID) (Display, error) {
	if purpose == "" {
		return Display{}, ProblemPurposeRequired
	}
	return l.s.Display(ctx, purpose, ref)
}

func (l local) Resolve(ctx context.Context, purpose Purpose, ref uuid.UUID) (Identity, error) {
	if purpose == "" {
		return Identity{}, ProblemPurposeRequired
	}
	return l.s.Resolve(ctx, purpose, ref)
}

func (l local) Lookup(ctx context.Context, purpose Purpose, phoneOrNationalID string) (uuid.UUID, error) {
	if purpose == "" {
		return uuid.Nil, ProblemPurposeRequired
	}
	return l.s.Lookup(ctx, purpose, phoneOrNationalID)
}

func (l local) Unwrap(ctx context.Context, purpose Purpose, ref uuid.UUID, wrappedDEK []byte) ([]byte, error) {
	if purpose == "" {
		return nil, ProblemPurposeRequired
	}
	return l.s.Unwrap(ctx, purpose, ref, wrappedDEK)
}

func (l local) Erase(ctx context.Context, ref uuid.UUID, reason string) error {
	return l.s.Erase(ctx, ref, reason)
}

func (l local) HouseholdPubKeys(ctx context.Context, refs []uuid.UUID) ([]PubKey, error) {
	return l.s.HouseholdPubKeys(ctx, refs)
}

func (l local) PubKeys(ctx context.Context, areaCodes []string) ([]PubKey, error) {
	return l.s.PubKeys(ctx, areaCodes)
}
