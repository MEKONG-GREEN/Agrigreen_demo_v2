package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

var (
	ErrSupportUnavailable = problem.New(http.StatusServiceUnavailable, "contact.support_unavailable", "Chưa cấu hình tiếp nhận hỗ trợ tài khoản")
	ErrSupportNotFound    = problem.New(http.StatusNotFound, "contact.support_not_found", "Không tìm thấy yêu cầu hỗ trợ")
	ErrSupportStale       = problem.New(http.StatusConflict, "contact.support_stale", "Yêu cầu đã thay đổi. Hãy tải lại trước khi xử lý")
)

func supportKey(key string) ([]byte, error) {
	k, err := uuid.Parse(key)
	if len(key) != 36 || err != nil || k == uuid.Nil {
		return nil, problem.Validation.WithField("Idempotency-Key", "bắt buộc là UUID của thao tác")
	}
	hash := sha256.Sum256([]byte(k.String()))
	return hash[:], nil
}

func supportAAD(tid, id uuid.UUID) []byte {
	return []byte("kn.account_support/v1\n" + tid.String() + "\n" + id.String())
}

// SubmitSupport never resolves an account or trusts client-supplied tenant/actor/role. A public request is anonymous.
func (s *Service) SubmitSupport(ctx context.Context, in domain.SupportInput, key string) error {
	hash, err := supportKey(key)
	if err != nil {
		return err
	}
	in, err = domain.NormalizeSupport(in)
	if err != nil {
		return err
	}
	cfg := s.Support
	if cfg.TenantID == uuid.Nil || cfg.SubjectRef == uuid.Nil || len(cfg.PublicKey) != envelope.KeySize {
		return ErrSupportUnavailable
	}
	pub, err := envelope.Key32(cfg.PublicKey)
	if err != nil {
		return ErrSupportUnavailable
	}
	plain, err := json.Marshal(in)
	if err != nil {
		return err
	}
	defer clear(plain)
	if len(plain)+envelope.TagSize > 12<<10 {
		return problem.Validation.WithField("message", "Tổng nội dung quá dài. Vui lòng rút gọn rồi gửi lại.")
	}
	// The high-entropy operation UUID is not stored: a database reader cannot dictionary-test contact/message hashes.
	fingerprint := hmac.New(sha256.New, []byte(strings.ToLower(key)))
	_, _ = fingerprint.Write(plain)
	digest := fingerprint.Sum(nil)
	id := ids.New()
	sealed, err := envelope.SealWithAAD(plain, supportAAD(cfg.TenantID, id), pub)
	if err != nil {
		return err
	}
	return s.tx(ctx, cfg.TenantID, func(tx pgx.Tx) error {
		if err := store.LockSupport(ctx, tx, cfg.TenantID.String()+":"+hex.EncodeToString(hash)); err != nil {
			return err
		}
		old, found, err := store.SupportByKey(ctx, tx, hash)
		if err != nil {
			return err
		}
		if found {
			if !hmac.Equal(old.PayloadHash, digest) {
				return problem.IdempotencyConflict
			}
			return nil
		}
		if err := store.InsertSupport(ctx, tx, cfg.TenantID, store.SupportRow{ID: id, KeyHash: hash, PayloadHash: digest, SubjectRef: cfg.SubjectRef, Nonce: sealed.Nonce, WrappedDEK: sealed.WrappedDEK, Ciphertext: sealed.Ciphertext}); err != nil {
			return err
		}
		return store.AppendSupportEvent(ctx, tx, cfg.TenantID, id, uuid.Nil, "submitted", "support:submit:"+id.String(), nil, s.now())
	})
}

func (s *Service) supportAdmin(ctx context.Context) (authz.Principal, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return p, err
	}
	if !p.MFA {
		return p, problem.MFARequired
	}
	if p.TenantID == uuid.Nil || p.ActorRef == uuid.Nil || s.Access.Session == nil {
		return p, problem.Forbidden
	}
	k, err := s.Access.Session(ctx)
	if err != nil {
		return p, err
	}
	if k.Role != authz.KNAdmin || k.ReadOnly {
		return p, problem.Forbidden
	}
	return p, nil
}

// ListSupport has no PII and still commits a queue-access audit before returning.
func (s *Service) ListSupport(ctx context.Context, state string, offset int) ([]store.SupportSummary, error) {
	p, err := s.supportAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if state != "" && state != "new" && state != "resolved" {
		return nil, problem.Validation.WithField("state", "new hoặc resolved")
	}
	if offset < 0 || offset > 10000 {
		return nil, problem.Validation.WithField("offset", "từ 0 đến 10000")
	}
	var rows []store.SupportSummary
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var err error
		rows, err = store.ListSupport(ctx, tx, state, offset)
		if err != nil {
			return err
		}
		return store.AppendSupportEvent(ctx, tx, p.TenantID, uuid.Nil, p.ActorRef, "listed", "support:list:"+ids.New().String(), nil, s.now())
	})
	return rows, err
}

// SupportDetail is transient decrypted content, never an idempotency-cache response.
type SupportDetail struct {
	store.SupportSummary
	domain.SupportInput
}

func (s *Service) GetSupport(ctx context.Context, id uuid.UUID) (SupportDetail, error) {
	p, err := s.supportAdmin(ctx)
	if err != nil {
		return SupportDetail{}, err
	}
	if s.Vault == nil {
		return SupportDetail{}, ErrSupportUnavailable
	}
	var row store.SupportRow
	var summary store.SupportSummary
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var found bool
		var err error
		row, found, err = store.SupportByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if !found {
			return ErrSupportNotFound
		}
		summary, found, err = store.SupportCurrent(ctx, tx, id)
		if err == nil && !found {
			return ErrSupportNotFound
		}
		return err
	})
	if err != nil {
		return SupportDetail{}, err
	}
	// Both vault I/O and crypto occur outside the module transaction. The original token is forwarded unchanged.
	dek, err := s.Vault.Unwrap(ctx, client.PurposeAccountSupport, row.SubjectRef, row.WrappedDEK)
	if err != nil {
		return SupportDetail{}, err
	}
	defer clear(dek)
	plain, err := envelope.DecryptWithDEKAndAAD(dek, row.Nonce, row.Ciphertext, supportAAD(p.TenantID, id))
	if err != nil {
		return SupportDetail{}, err
	}
	defer clear(plain)
	var content domain.SupportInput
	if err = json.Unmarshal(plain, &content); err != nil {
		return SupportDetail{}, err
	}
	if _, err = s.supportAdmin(ctx); err != nil {
		return SupportDetail{}, err
	}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		return store.AppendSupportEvent(ctx, tx, p.TenantID, id, p.ActorRef, "viewed", "support:view:"+ids.New().String(), nil, s.now())
	})
	if err != nil {
		return SupportDetail{}, err
	}
	return SupportDetail{SupportSummary: summary, SupportInput: content}, nil
}

// ResolveSupport checks current authority before every retry lookup. The endpoint uses PUT, so the global POST cache
// cannot replay a success before this authorization or store decrypted content. Permanent ledger replay is actor-bound.
func (s *Service) ResolveSupport(ctx context.Context, id uuid.UUID, expected int64, key string) (store.SupportSummary, error) {
	p, err := s.supportAdmin(ctx)
	if err != nil {
		return store.SupportSummary{}, err
	}
	hash, err := supportKey(key)
	if err != nil {
		return store.SupportSummary{}, err
	}
	if expected < 1 {
		return store.SupportSummary{}, problem.Validation.WithField("expected_version", "bắt buộc, lớn hơn 0")
	}
	idem := "support:resolve:" + id.String() + ":" + p.ActorRef.String() + ":" + hex.EncodeToString(hash)
	var current store.SupportSummary
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := store.LockSupport(ctx, tx, p.TenantID.String()+":"+id.String()); err != nil {
			return err
		}
		var found bool
		var err error
		current, found, err = store.SupportCurrent(ctx, tx, id)
		if err != nil {
			return err
		}
		if !found {
			return ErrSupportNotFound
		}
		version, replay, err := store.SupportResolutionReplay(ctx, tx, idem)
		if err != nil {
			return err
		}
		if replay {
			if version != expected {
				return problem.IdempotencyConflict
			}
			return nil
		}
		if current.Version != expected || current.State != "new" {
			return ErrSupportStale
		}
		if err := store.AppendSupportEvent(ctx, tx, p.TenantID, id, p.ActorRef, "resolved", idem, &expected, s.now()); err != nil {
			return err
		}
		current, _, err = store.SupportCurrent(ctx, tx, id)
		return err
	})
	return current, err
}
