package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// BulkAction là hành động áp cho nhiều tài khoản (KN US-ADM-03).
type BulkAction string

// Hành động loạt.
const (
	BulkLock   BulkAction = "lock"
	BulkUnlock BulkAction = "unlock"
	BulkResend BulkAction = "resend"
)

// MaxBulk: số tài khoản tối đa mỗi lần.
const MaxBulk = 100

// BulkInput là một lần thao tác loạt.
type BulkInput struct {
	Action  BulkAction
	UserIDs []uuid.UUID
	Reason  string // bắt buộc với lock/unlock
	IdemKey string
}

// BulkItem là kết quả cho một tài khoản. Code/Detail không chứa dữ liệu cá nhân.
type BulkItem struct {
	UserID   uuid.UUID
	OK       bool
	Replayed bool
	Code     string
	Detail   string
}

// Bulk áp một hành động cho các tài khoản đã chọn trong một lần xác nhận (KN
// US-ADM-03). Mỗi tài khoản xử lý độc lập, kết quả theo từng tài khoản; khóa
// idem của từng mục là <Idempotency-Key>:<user_id>.
func (s *Service) Bulk(ctx context.Context, in BulkInput) ([]BulkItem, error) {
	a, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	if err := requireKey(in.IdemKey); err != nil {
		return nil, err
	}
	switch in.Action {
	case BulkLock, BulkUnlock, BulkResend:
	default:
		return nil, problem.Validation.WithField("action", "phải là lock, unlock hoặc resend")
	}
	if len(in.UserIDs) == 0 || len(in.UserIDs) > MaxBulk {
		return nil, problem.Validation.WithField("user_ids", "chọn từ 1 đến 100 tài khoản")
	}
	seen := map[uuid.UUID]bool{}
	out := make([]BulkItem, 0, len(in.UserIDs))
	for _, id := range in.UserIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		key := in.IdemKey + ":" + id.String()
		var res Result
		var err error
		switch in.Action {
		case BulkLock:
			res, err = s.lock(ctx, a, LockInput{UserID: id, Reason: in.Reason, IdemKey: key})
		case BulkUnlock:
			res, err = s.unlock(ctx, a, LockInput{UserID: id, Reason: in.Reason, IdemKey: key})
		case BulkResend:
			res, err = s.resend(ctx, a, ResendInput{UserID: id, IdemKey: key})
		}
		it := BulkItem{UserID: id, OK: err == nil, Replayed: res.Replayed}
		if err != nil {
			p := problem.From(err)
			if p.Status >= 500 && p.Code == problem.Internal.Code {
				return nil, err
			}
			it.Code, it.Detail = p.Code, p.Detail
			if it.Detail == "" {
				it.Detail = p.Title
			}
			for _, fe := range p.Errors {
				it.Detail = fe.Reason
			}
		}
		out = append(out, it)
	}
	return out, nil
}
