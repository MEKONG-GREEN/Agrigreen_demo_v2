// Package app: use case của kho định danh. Giao dịch mở ở đây; vai, mục đích
// và tenant lấy từ Principal của token người dùng cuối (R-16, R-42, R-54).
//
// Service hiện thực client.Client — cmd/identity-vault phục vụ nó qua HTTP,
// client.NewLocal bọc nó cho lời gọi cùng tiến trình.
package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/kms"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/store"
)

// Chuỗi của sổ truy cập cho lần tra/đọc không thấy chủ thể.
const missStream = "miss"

// Mục dùng trong AAD.
const (
	useX25519   = "x25519"
	useDataKey  = "data-key"
	useName     = "identity/full_name"
	usePhone    = "identity/phone"
	useNational = "identity/national_id"
	useAddress  = "identity/address"
)

// Service là use case của kho định danh.
type Service struct {
	DB       tenant.Beginner // pool vai vault_app
	Clock    clock.Clock
	KMS      kms.KMS
	BlindKey []byte    // khóa HMAC của chỉ mục mù (AG_VAULT_BLIND_KEY), tách khỏi KEK
	Rand     io.Reader // nil = crypto/rand
}

var _ client.Client = (*Service)(nil)

// Validate kiểm cấu hình khi dựng.
func (s *Service) Validate() error {
	switch {
	case s.DB == nil || s.Clock == nil || s.KMS == nil:
		return errors.New("vault: thiếu DB, Clock hoặc KMS")
	case len(s.BlindKey) < 32:
		return errors.New("vault: khóa chỉ mục mù phải từ 32 byte")
	}
	return nil
}

func (s *Service) rand() io.Reader {
	if s.Rand != nil {
		return s.Rand
	}
	return rand.Reader
}

// begin kiểm người gọi, mục đích (R-42) rồi vai.
func (s *Service) begin(ctx context.Context, purpose client.Purpose, roles ...string) (authz.Principal, error) {
	if _, ok := authz.FromContext(ctx); !ok {
		return authz.Principal{}, problem.Unauthenticated
	}
	if purpose == "" {
		return authz.Principal{}, client.ProblemPurposeRequired
	}
	if !domain.ValidPurpose(string(purpose)) {
		return authz.Principal{}, problem.Validation.WithField("purpose", "phải khớp ^[a-z][a-z0-9_.]{2,63}$")
	}
	return authz.Require(ctx, roles...)
}

// logAccess ghi một dòng sổ truy cập. ref nil → chuỗi "miss".
func (s *Service) logAccess(ctx context.Context, tx pgx.Tx, p authz.Principal, ref *uuid.UUID, found bool, op string, purpose client.Purpose, outcome string) error {
	stream := missStream
	if found && ref != nil {
		stream = "subject:" + ref.String()
	}
	id := ids.New()
	a := store.Access{
		ID: id, TenantID: p.TenantID, StreamKey: stream, SubjectRef: ref, Op: op, Purpose: string(purpose), Outcome: outcome,
		EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: "access:" + id.String(), RuleVersion: domain.RuleVersion,
	}
	if tid := telemetry.TraceID(ctx); tid != "" {
		a.TraceID = &tid
	}
	return ledger.Classify(store.AppendAccess(ctx, tx, &a))
}

// errBlindRace: request song song vừa ghi cùng chỉ mục mù — chạy lại để trả ErrDuplicate.
var errBlindRace = errors.New("vault: tranh chấp chỉ mục mù")

// Create tạo chủ thể, sinh cặp khóa X25519. Trùng SĐT/CCCD trong tenant →
// trả Created của chủ thể cũ kèm client.ProblemDuplicate.
func (s *Service) Create(ctx context.Context, purpose client.Purpose, in client.Identity) (client.Created, error) {
	p, err := s.begin(ctx, purpose, client.RoleRegistrar)
	if err != nil {
		return client.Created{}, err
	}
	name := domain.NormalizeName(in.FullName)
	v, bad := problem.Validation, false
	if in.Kind != client.SubjectHousehold && in.Kind != client.SubjectPerson {
		v, bad = v.WithField("kind", "phải là household hoặc person"), true
	}
	if n := utf8.RuneCountInString(name); n < 1 || n > 200 {
		v, bad = v.WithField("full_name", "bắt buộc, tối đa 200 ký tự"), true
	}
	if utf8.RuneCountInString(in.Address) > 500 {
		v, bad = v.WithField("address", "tối đa 500 ký tự"), true
	}
	if !domain.ValidArea(in.AreaCode) {
		v, bad = v.WithField("area_code", "bắt buộc, 1–32 ký tự [0-9A-Za-z._-]"), true
	}
	var phone, nid string
	var blinds []store.Blind
	if in.Phone != "" {
		if phone, err = domain.NormalizePhone(in.Phone); err != nil {
			v, bad = v.WithField("phone", err.Error()), true
		} else {
			blinds = append(blinds, store.Blind{Kind: domain.BlindPhone, Value: domain.BlindIndex(s.BlindKey, p.TenantID, domain.BlindPhone, phone)})
		}
	}
	if in.NationalID != "" {
		if nid, err = domain.NormalizeNationalID(in.NationalID); err != nil {
			v, bad = v.WithField("national_id", err.Error()), true
		} else {
			blinds = append(blinds, store.Blind{Kind: domain.BlindNationalID, Value: domain.BlindIndex(s.BlindKey, p.TenantID, domain.BlindNationalID, nid)})
		}
	}
	if bad {
		return client.Created{}, v
	}

	// R-41: mã tham chiếu ngẫu nhiên, không liên quan tới định danh.
	ref := ids.New()
	tid := p.TenantID
	pub, priv, err := envelope.GenerateKeyPair(s.rand())
	if err != nil {
		return client.Created{}, err
	}
	defer clear(priv[:])
	dataKey := make([]byte, 32)
	if _, err := io.ReadFull(s.rand(), dataKey); err != nil {
		return client.Created{}, err
	}
	defer clear(dataKey)
	wrappedPriv, err := s.KMS.Wrap(ctx, priv[:], domain.AAD(useX25519, tid, ref))
	if err != nil {
		return client.Created{}, err
	}
	wrappedDK, err := s.KMS.Wrap(ctx, dataKey, domain.AAD(useDataKey, tid, ref))
	if err != nil {
		return client.Created{}, err
	}
	var row store.Identity
	for _, f := range []struct {
		dst   *[]byte
		use   string
		value string
	}{
		{&row.FullNameCT, useName, name}, {&row.PhoneCT, usePhone, phone},
		{&row.NationalIDCT, useNational, nid}, {&row.AddressCT, useAddress, in.Address},
	} {
		if *f.dst, err = sealField(s.rand(), dataKey, domain.AAD(f.use, tid, ref), f.value); err != nil {
			return client.Created{}, err
		}
	}

	var out client.Created
	var dup bool
	attempt := func() error {
		return tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
			old, found, err := store.FindBlind(ctx, tx, blinds)
			if err != nil {
				return err
			}
			if found {
				k, err := store.GetKey(ctx, tx, old)
				if err != nil {
					return err
				}
				out, dup = client.Created{Ref: old}, true
				if k != nil {
					out.PublicKey = k.PublicKey
				}
				return s.logAccess(ctx, tx, p, &old, true, domain.OpCreate, purpose, domain.OutcomeDuplicate)
			}
			if err := store.InsertSubject(ctx, tx, store.Subject{
				TenantID: tid, Ref: ref, Kind: string(in.Kind), AreaCode: in.AreaCode, CreatedAt: s.Clock.Now(), CreatedBy: p.ActorRef,
			}); err != nil {
				return err
			}
			if err := store.InsertIdentity(ctx, tx, tid, ref, row); err != nil {
				return err
			}
			if err := store.InsertKey(ctx, tx, tid, ref, store.Key{
				PublicKey: pub[:], WrappedPrivateKey: wrappedPriv, WrappedDataKey: wrappedDK, KEKID: s.KMS.KeyID(),
			}); err != nil {
				return err
			}
			for _, b := range blinds {
				ok, err := store.InsertBlind(ctx, tx, tid, ref, b)
				if err != nil {
					return err
				}
				if !ok {
					return errBlindRace
				}
			}
			out = client.Created{Ref: ref, PublicKey: append([]byte(nil), pub[:]...)}
			return s.logAccess(ctx, tx, p, &ref, true, domain.OpCreate, purpose, domain.OutcomeOK)
		})
	}
	err = attempt()
	if errors.Is(err, errBlindRace) {
		err = attempt() // lần hai thấy chỉ mục của bên thắng → trả trùng
	}
	if err != nil {
		return client.Created{}, err
	}
	if dup {
		return out, client.ProblemDuplicate.With("ref", out.Ref.String()).With("public_key", out.PublicKey)
	}
	return out, nil
}

// state là kết quả đọc chủ thể: fail khác nil là lỗi nghiệp vụ (không có, đã
// xóa) trả cho người gọi SAU khi giao dịch ghi sổ đã commit.
type state struct {
	subj *store.Subject
	fail error
}

// subjectState đọc chủ thể và ghi sổ cho trường hợp không có hoặc đã xóa.
func (s *Service) subjectState(ctx context.Context, tx pgx.Tx, p authz.Principal, ref uuid.UUID, op string, purpose client.Purpose, forUpdate bool) (state, error) {
	subj, err := store.GetSubject(ctx, tx, ref, forUpdate)
	if err != nil {
		return state{}, err
	}
	if subj == nil {
		return state{fail: client.ProblemNotFound}, s.logAccess(ctx, tx, p, &ref, false, op, purpose, domain.OutcomeNotFound)
	}
	if subj.ErasedAt != nil {
		return state{subj: subj, fail: client.ProblemErased}, s.logAccess(ctx, tx, p, &ref, true, op, purpose, domain.OutcomeErased)
	}
	return state{subj: subj}, nil
}

// plain là định danh đã giải mã — chỉ trong bộ nhớ.
type plain struct {
	FullName   string `pii:"true"`
	Phone      string `pii:"true"`
	NationalID string `pii:"true"`
	Address    string `pii:"true"`
}

func (s *Service) decrypt(ctx context.Context, tx pgx.Tx, tid, ref uuid.UUID) (plain, error) {
	var out plain
	k, err := store.GetKey(ctx, tx, ref)
	if err != nil {
		return out, err
	}
	row, err := store.GetIdentity(ctx, tx, ref)
	if err != nil {
		return out, err
	}
	if k == nil || row == nil {
		return out, errors.New("vault: chủ thể chưa xóa nhưng thiếu khóa hoặc định danh")
	}
	dk, err := s.KMS.Unwrap(ctx, k.KEKID, k.WrappedDataKey, domain.AAD(useDataKey, tid, ref))
	if err != nil {
		return out, err
	}
	defer clear(dk)
	for _, f := range []struct {
		dst *string
		use string
		ct  []byte
	}{
		{&out.FullName, useName, row.FullNameCT}, {&out.Phone, usePhone, row.PhoneCT},
		{&out.NationalID, useNational, row.NationalIDCT}, {&out.Address, useAddress, row.AddressCT},
	} {
		if *f.dst, err = openField(dk, domain.AAD(f.use, tid, ref), f.ct); err != nil {
			return plain{}, err
		}
	}
	return out, nil
}

// Display trả nhãn hiển thị tối thiểu (mục 10.4). Đã xóa → nhãn "(đã xóa định danh)" kèm client.ProblemErased.
func (s *Service) Display(ctx context.Context, purpose client.Purpose, ref uuid.UUID) (client.Display, error) {
	p, err := s.begin(ctx, purpose, client.RoleRegistrar, client.RoleDisplay, client.RoleEvidenceViewer)
	if err != nil {
		return client.Display{}, err
	}
	var out client.Display
	var result error
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		st, err := s.subjectState(ctx, tx, p, ref, domain.OpDisplay, purpose, false)
		if err != nil || st.fail != nil {
			result = st.fail
			return err
		}
		pl, err := s.decrypt(ctx, tx, p.TenantID, ref)
		if err != nil {
			return err
		}
		out = client.Display{Ref: ref, Label: domain.Label(pl.FullName), MaskedPhone: domain.MaskPhone(pl.Phone)}
		return s.logAccess(ctx, tx, p, &ref, true, domain.OpDisplay, purpose, domain.OutcomeOK)
	})
	if err != nil {
		return client.Display{}, err
	}
	if errors.Is(result, client.ErrErased) {
		return client.Display{Ref: ref, Label: client.ErasedLabel}, result
	}
	return out, result
}

// Resolve trả định danh đầy đủ; mọi lần gọi đều ghi sổ có mục đích và người yêu cầu (R-42).
func (s *Service) Resolve(ctx context.Context, purpose client.Purpose, ref uuid.UUID) (client.Identity, error) {
	p, err := s.begin(ctx, purpose, client.RoleRegistrar)
	if err != nil {
		return client.Identity{}, err
	}
	var out client.Identity
	var result error
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		st, err := s.subjectState(ctx, tx, p, ref, domain.OpResolve, purpose, false)
		if err != nil || st.fail != nil {
			result = st.fail
			return err
		}
		subj := st.subj
		pl, err := s.decrypt(ctx, tx, p.TenantID, ref)
		if err != nil {
			return err
		}
		out = client.Identity{
			Kind: client.SubjectKind(subj.Kind), FullName: pl.FullName, Phone: pl.Phone, NationalID: pl.NationalID,
			Address: pl.Address, AreaCode: subj.AreaCode,
		}
		return s.logAccess(ctx, tx, p, &ref, true, domain.OpResolve, purpose, domain.OutcomeOK)
	})
	if err != nil {
		return client.Identity{}, err
	}
	return out, result
}

// Lookup tìm chủ thể theo SĐT hoặc CCCD qua chỉ mục mù. Không thấy → ProblemNotFound (vẫn ghi sổ).
func (s *Service) Lookup(ctx context.Context, purpose client.Purpose, phoneOrNationalID string) (uuid.UUID, error) {
	p, err := s.begin(ctx, purpose, client.RoleRegistrar)
	if err != nil {
		return uuid.Nil, err
	}
	cands := domain.LookupCandidates(phoneOrNationalID)
	if len(cands) == 0 {
		return uuid.Nil, problem.Validation.WithField("value", "phải là SĐT Việt Nam hoặc CCCD/CMND")
	}
	blinds := make([]store.Blind, 0, len(cands))
	for _, c := range cands {
		blinds = append(blinds, store.Blind{Kind: c.Kind, Value: domain.BlindIndex(s.BlindKey, p.TenantID, c.Kind, c.Value)})
	}
	var ref uuid.UUID
	var found bool
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var err error
		if ref, found, err = store.FindBlind(ctx, tx, blinds); err != nil {
			return err
		}
		if !found {
			return s.logAccess(ctx, tx, p, nil, false, domain.OpLookup, purpose, domain.OutcomeNotFound)
		}
		return s.logAccess(ctx, tx, p, &ref, true, domain.OpLookup, purpose, domain.OutcomeOK)
	})
	if err != nil {
		return uuid.Nil, err
	}
	if !found {
		return uuid.Nil, client.ProblemNotFound
	}
	return ref, nil
}

// Unwrap mở DEK đã seal bằng khóa công khai của chủ thể (xem ảnh minh chứng).
func (s *Service) Unwrap(ctx context.Context, purpose client.Purpose, ref uuid.UUID, wrappedDEK []byte) ([]byte, error) {
	p, err := s.begin(ctx, purpose, client.RoleEvidenceViewer)
	if err != nil {
		return nil, err
	}
	if len(wrappedDEK) != envelope.WrappedDEKLen {
		return nil, problem.Validation.WithField("wrapped_dek", "phải đúng 80 byte (crypto_box_seal của DEK 32 byte)")
	}
	var dek []byte
	var result error
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		st, err := s.subjectState(ctx, tx, p, ref, domain.OpUnwrap, purpose, false)
		if err != nil || st.fail != nil {
			result = st.fail
			return err
		}
		k, err := store.GetKey(ctx, tx, ref)
		if err != nil {
			return err
		}
		if k == nil {
			return errors.New("vault: chủ thể chưa xóa nhưng thiếu khóa")
		}
		privRaw, err := s.KMS.Unwrap(ctx, k.KEKID, k.WrappedPrivateKey, domain.AAD(useX25519, p.TenantID, ref))
		if err != nil {
			return err
		}
		defer clear(privRaw)
		pub, err := envelope.Key32(k.PublicKey)
		if err != nil {
			return err
		}
		priv, err := envelope.Key32(privRaw)
		if err != nil {
			return err
		}
		defer clear(priv[:])
		dek, err = envelope.UnwrapDEK(wrappedDEK, pub, priv)
		if err != nil {
			result = client.ProblemUnwrapFailed
			return s.logAccess(ctx, tx, p, &ref, true, domain.OpUnwrap, purpose, domain.OutcomeRejected)
		}
		return s.logAccess(ctx, tx, p, &ref, true, domain.OpUnwrap, purpose, domain.OutcomeOK)
	})
	if err != nil {
		return nil, err
	}
	if result != nil {
		return nil, result
	}
	return dek, nil
}

// Erase xóa định danh theo yêu cầu (RB-3): xóa định danh, khóa bí mật, chỉ mục
// mù; giữ dấu mộ. Bắt buộc vai RoleEraser và MFA. Gọi lại khi đã xóa → thành công.
func (s *Service) Erase(ctx context.Context, ref uuid.UUID, reason string) error {
	p, err := authz.Require(ctx, client.RoleEraser)
	if err != nil {
		return err
	}
	if !p.MFA {
		return problem.MFARequired
	}
	if !domain.ValidReason(reason) {
		return problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	var result error
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		st, err := s.subjectState(ctx, tx, p, ref, domain.OpErase, client.PurposeErasure, true)
		if err != nil {
			return err
		}
		if errors.Is(st.fail, client.ErrErased) {
			return nil // đã xóa: gọi lặp thành công, sổ đã ghi outcome = erased
		}
		if st.fail != nil {
			result = st.fail
			return nil
		}
		if err := store.Erase(ctx, tx, ref, s.Clock.Now(), p.ActorRef, reason); err != nil {
			return err
		}
		return s.logAccess(ctx, tx, p, &ref, true, domain.OpErase, client.PurposeErasure, domain.OutcomeOK)
	})
	if err != nil {
		return err
	}
	return result
}

var pubKeyRoles = []string{client.RoleRegistrar, client.RoleDisplay, client.RoleEvidenceViewer, client.RolePubKeySync}

// PubKeysPage là một trang khóa công khai theo địa bàn (cursor theo ref, R-53).
func (s *Service) PubKeysPage(ctx context.Context, areas []string, after uuid.UUID, limit int) ([]client.PubKey, *uuid.UUID, error) {
	p, err := authz.Require(ctx, pubKeyRoles...)
	if err != nil {
		return nil, nil, err
	}
	if len(areas) == 0 || len(areas) > 200 {
		return nil, nil, problem.Validation.WithField("area", "cần 1–200 mã địa bàn")
	}
	for _, a := range areas {
		if !domain.ValidArea(a) {
			return nil, nil, problem.Validation.WithField("area", "mã địa bàn sai dạng")
		}
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	var rows []store.PubKey
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ListPubKeys(ctx, tx, areas, after, limit+1)
		return e
	})
	if err != nil {
		return nil, nil, err
	}
	var next *uuid.UUID
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1].Ref
		next = &last
	}
	out := make([]client.PubKey, 0, len(rows))
	for _, r := range rows {
		out = append(out, client.PubKey{Ref: r.Ref, AreaCode: r.AreaCode, PublicKey: r.PublicKey})
	}
	return out, next, nil
}

// HouseholdPubKeys trả khóa công khai của các nông hộ chưa xóa theo ref (client.Client).
// Vai farmer chỉ đọc được theo ref, không liệt kê theo địa bàn; ref lạ/đã xóa/người → bỏ qua.
func (s *Service) HouseholdPubKeys(ctx context.Context, refs []uuid.UUID) ([]client.PubKey, error) {
	p, err := authz.Require(ctx, append(slices.Clone(pubKeyRoles), client.RoleFarmer)...)
	if err != nil {
		return nil, err
	}
	refs = slices.Compact(slices.SortedFunc(slices.Values(refs), func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) }))
	if len(refs) == 0 || len(refs) > client.MaxPubKeyRefs {
		return nil, problem.Validation.WithField("ref", fmt.Sprintf("cần 1–%d mã chủ thể", client.MaxPubKeyRefs))
	}
	var rows []store.PubKey
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		rows, e = store.HouseholdPubKeys(ctx, tx, refs)
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make([]client.PubKey, 0, len(rows))
	for _, r := range rows {
		out = append(out, client.PubKey{Ref: r.Ref, AreaCode: r.AreaCode, PublicKey: r.PublicKey})
	}
	return out, nil
}

// PubKeys trả mọi khóa công khai của chủ thể chưa xóa thuộc các địa bàn.
func (s *Service) PubKeys(ctx context.Context, areaCodes []string) ([]client.PubKey, error) {
	var all []client.PubKey
	var after uuid.UUID
	for {
		page, next, err := s.PubKeysPage(ctx, areaCodes, after, 1000)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if next == nil {
			return all, nil
		}
		after = *next
	}
}
