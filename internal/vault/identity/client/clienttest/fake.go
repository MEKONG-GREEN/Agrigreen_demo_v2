// Package clienttest là kho định danh giả trong bộ nhớ cho kiểm thử của module
// KHÁC (R-04: module ngoài chỉ import client và client/clienttest). Không bao
// giờ import từ mã chạy thật.
//
// Hành vi bám kho thật: bắt buộc mục đích, chống trùng SĐT/CCCD theo tenant
// (chuẩn hóa như kho), cặp khóa X25519 thật (Unwrap mở được phong bì do
// internal/shared/envelope tạo), xóa định danh → ErrErased. Tenant lấy từ
// Principal trong ctx (authz), thiếu thì dùng uuid.Nil. KHÔNG kiểm vai — muốn
// thử từ chối thì đặt Fail.
package clienttest

import (
	"context"
	"crypto/rand"
	"slices"
	"sort"
	"sync"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/domain"
)

// Access là một lần truy cập đã ghi — để kiểm module gọi có truyền mục đích đúng.
type Access struct {
	Op      string // create | display | resolve | lookup | unwrap | erase | amend | section_write | section_read
	Purpose client.Purpose
	Ref     uuid.UUID
	Actor   uuid.UUID
}

type subject struct {
	tenant uuid.UUID
	id     client.Identity
	pub    *[32]byte
	priv   *[32]byte
	erased bool
	// sections: ngăn hồ sơ (W11.22) — bản sao, không chia sẻ map với người gọi.
	sections map[string]client.Fields
}

type blindKey struct {
	tenant uuid.UUID
	kind   string
	value  string
}

// Fake là client.Client trong bộ nhớ, an toàn khi dùng song song.
type Fake struct {
	mu       sync.Mutex
	subjects map[uuid.UUID]*subject
	blind    map[blindKey]uuid.UUID
	log      []Access
	// Fail, nếu khác nil, được gọi đầu mỗi thao tác; trả lỗi khác nil → thao tác trả lỗi đó.
	Fail func(op string) error
}

var (
	_ client.Client   = (*Fake)(nil)
	_ client.Profiles = (*Fake)(nil)
)

// New tạo kho giả rỗng.
func New() *Fake {
	return &Fake{subjects: map[uuid.UUID]*subject{}, blind: map[blindKey]uuid.UUID{}}
}

// Accesses trả bản sao nhật ký truy cập.
func (f *Fake) Accesses() []Access {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Access(nil), f.log...)
}

func caller(ctx context.Context) (uuid.UUID, uuid.UUID) {
	if p, ok := authz.FromContext(ctx); ok {
		return p.TenantID, p.ActorRef
	}
	return uuid.Nil, uuid.Nil
}

func (f *Fake) start(ctx context.Context, op string, purpose client.Purpose, needPurpose bool) (uuid.UUID, uuid.UUID, error) {
	if f.Fail != nil {
		if err := f.Fail(op); err != nil {
			return uuid.Nil, uuid.Nil, err
		}
	}
	if needPurpose && purpose == "" {
		return uuid.Nil, uuid.Nil, client.ProblemPurposeRequired
	}
	tid, actor := caller(ctx)
	return tid, actor, nil
}

// get trả chủ thể thuộc tenant; phải giữ khóa.
func (f *Fake) get(tid, ref uuid.UUID) (*subject, error) {
	s, ok := f.subjects[ref]
	if !ok || s.tenant != tid {
		return nil, client.ProblemNotFound
	}
	if s.erased {
		return s, client.ProblemErased
	}
	return s, nil
}

func (f *Fake) record(op string, purpose client.Purpose, ref, actor uuid.UUID) {
	f.log = append(f.log, Access{Op: op, Purpose: purpose, Ref: ref, Actor: actor})
}

// Create thực hiện client.Client.
func (f *Fake) Create(ctx context.Context, purpose client.Purpose, id client.Identity) (client.Created, error) {
	tid, actor, err := f.start(ctx, domain.OpCreate, purpose, true)
	if err != nil {
		return client.Created{}, err
	}
	var keys []blindKey
	if id.Phone != "" {
		p, err := domain.NormalizePhone(id.Phone)
		if err != nil {
			return client.Created{}, problem.Validation.WithField("phone", err.Error())
		}
		id.Phone = p
		keys = append(keys, blindKey{tid, domain.BlindPhone, p})
	}
	if id.NationalID != "" {
		n, err := domain.NormalizeNationalID(id.NationalID)
		if err != nil {
			return client.Created{}, problem.Validation.WithField("national_id", err.Error())
		}
		id.NationalID = n
		keys = append(keys, blindKey{tid, domain.BlindNationalID, n})
	}
	if id.FullName == "" || !domain.ValidArea(id.AreaCode) {
		return client.Created{}, problem.Validation.WithField("full_name", "bắt buộc; area_code bắt buộc")
	}
	id.FullName = domain.NormalizeName(id.FullName)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		if ref, ok := f.blind[k]; ok {
			f.record(domain.OpCreate, purpose, ref, actor)
			return client.Created{Ref: ref, PublicKey: append([]byte(nil), f.subjects[ref].pub[:]...)}, client.ProblemDuplicate
		}
	}
	pub, priv, err := envelope.GenerateKeyPair(rand.Reader)
	if err != nil {
		return client.Created{}, err
	}
	ref := ids.New()
	f.subjects[ref] = &subject{tenant: tid, id: id, pub: pub, priv: priv}
	for _, k := range keys {
		f.blind[k] = ref
	}
	f.record(domain.OpCreate, purpose, ref, actor)
	return client.Created{Ref: ref, PublicKey: append([]byte(nil), pub[:]...)}, nil
}

// Display thực hiện client.Client.
func (f *Fake) Display(ctx context.Context, purpose client.Purpose, ref uuid.UUID) (client.Display, error) {
	tid, actor, err := f.start(ctx, domain.OpDisplay, purpose, true)
	if err != nil {
		return client.Display{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.get(tid, ref)
	f.record(domain.OpDisplay, purpose, ref, actor)
	if s != nil && s.erased {
		return client.Display{Ref: ref, Label: client.ErasedLabel}, err
	}
	if err != nil {
		return client.Display{}, err
	}
	return client.Display{Ref: ref, Label: domain.Label(s.id.FullName), MaskedPhone: domain.MaskPhone(s.id.Phone)}, nil
}

// Resolve thực hiện client.Client.
func (f *Fake) Resolve(ctx context.Context, purpose client.Purpose, ref uuid.UUID) (client.Identity, error) {
	tid, actor, err := f.start(ctx, domain.OpResolve, purpose, true)
	if err != nil {
		return client.Identity{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.get(tid, ref)
	f.record(domain.OpResolve, purpose, ref, actor)
	if err != nil {
		return client.Identity{}, err
	}
	return s.id, nil
}

// Lookup thực hiện client.Client.
func (f *Fake) Lookup(ctx context.Context, purpose client.Purpose, phoneOrNationalID string) (uuid.UUID, error) {
	tid, actor, err := f.start(ctx, domain.OpLookup, purpose, true)
	if err != nil {
		return uuid.Nil, err
	}
	cands := domain.LookupCandidates(phoneOrNationalID)
	if len(cands) == 0 {
		return uuid.Nil, problem.Validation.WithField("value", "phải là SĐT Việt Nam hoặc CCCD/CMND")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range cands {
		if ref, ok := f.blind[blindKey{tid, c.Kind, c.Value}]; ok {
			f.record(domain.OpLookup, purpose, ref, actor)
			return ref, nil
		}
	}
	f.record(domain.OpLookup, purpose, uuid.Nil, actor)
	return uuid.Nil, client.ProblemNotFound
}

// Unwrap thực hiện client.Client.
func (f *Fake) Unwrap(ctx context.Context, purpose client.Purpose, ref uuid.UUID, wrappedDEK []byte) ([]byte, error) {
	tid, actor, err := f.start(ctx, domain.OpUnwrap, purpose, true)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.get(tid, ref)
	f.record(domain.OpUnwrap, purpose, ref, actor)
	if err != nil {
		return nil, err
	}
	dek, err := envelope.UnwrapDEK(wrappedDEK, s.pub, s.priv)
	if err != nil {
		return nil, client.ProblemUnwrapFailed
	}
	return dek, nil
}

// Erase thực hiện client.Client: xóa định danh, hủy khóa bí mật, giữ dấu mộ.
func (f *Fake) Erase(ctx context.Context, ref uuid.UUID, reason string) error {
	tid, actor, err := f.start(ctx, domain.OpErase, client.PurposeErasure, false)
	if err != nil {
		return err
	}
	if !domain.ValidReason(reason) {
		return problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.get(tid, ref)
	f.record(domain.OpErase, client.PurposeErasure, ref, actor)
	if s != nil && s.erased {
		return nil
	}
	if err != nil {
		return err
	}
	for k, r := range f.blind {
		if r == ref {
			delete(f.blind, k)
		}
	}
	clear(s.priv[:])
	s.id, s.priv, s.erased, s.sections = client.Identity{Kind: s.id.Kind, AreaCode: s.id.AreaCode}, nil, true, nil
	return nil
}

// HouseholdPubKeys thực hiện client.Client: nông hộ chưa xóa theo ref, sắp theo Ref.
// Như kho thật: vai farmer hoặc vai đọc khóa (nếu Principal có trong ctx); 1–MaxPubKeyRefs ref.
func (f *Fake) HouseholdPubKeys(ctx context.Context, refs []uuid.UUID) ([]client.PubKey, error) {
	tid, _, err := f.start(ctx, "pubkeys", "", false)
	if err != nil {
		return nil, err
	}
	if p, ok := authz.FromContext(ctx); ok && !slices.ContainsFunc([]string{client.RoleFarmer, client.RoleRegistrar, client.RoleDisplay,
		client.RoleEvidenceViewer, client.RolePubKeySync}, p.Has) {
		return nil, problem.Forbidden
	}
	if len(refs) == 0 || len(refs) > client.MaxPubKeyRefs {
		return nil, problem.Validation.WithField("ref", "cần 1–50 mã chủ thể")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []client.PubKey
	for _, ref := range refs {
		s, ok := f.subjects[ref]
		if ok && s.tenant == tid && !s.erased && s.id.Kind == client.SubjectHousehold &&
			!slices.ContainsFunc(out, func(k client.PubKey) bool { return k.Ref == ref }) {
			out = append(out, client.PubKey{Ref: ref, AreaCode: s.id.AreaCode, PublicKey: append([]byte(nil), s.pub[:]...)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref.String() < out[j].Ref.String() })
	return out, nil
}

// PubKeys thực hiện client.Client, sắp theo Ref.
func (f *Fake) PubKeys(ctx context.Context, areaCodes []string) ([]client.PubKey, error) {
	tid, _, err := f.start(ctx, "pubkeys", "", false)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, a := range areaCodes {
		want[a] = true
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []client.PubKey
	for ref, s := range f.subjects {
		if s.tenant == tid && !s.erased && want[s.id.AreaCode] {
			out = append(out, client.PubKey{Ref: ref, AreaCode: s.id.AreaCode, PublicKey: append([]byte(nil), s.pub[:]...)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref.String() < out[j].Ref.String() })
	return out, nil
}

// Amend thực hiện client.Profiles (W11.22): kiểm mục đích thuộc danh mục của thao tác như kho thật.
func (f *Fake) Amend(ctx context.Context, purpose client.Purpose, ref uuid.UUID, id client.Identity) error {
	tid, actor, err := f.start(ctx, domain.OpAmend, purpose, true)
	if err != nil {
		return err
	}
	if !domain.AllowedPurpose(domain.OpAmend, string(purpose)) {
		return problem.Validation.WithField("purpose", "không thuộc danh mục của thao tác amend")
	}
	if id.FullName == "" {
		return problem.Validation.WithField("full_name", "bắt buộc")
	}
	var phone string
	if id.Phone != "" {
		if phone, err = domain.NormalizePhone(id.Phone); err != nil {
			return problem.Validation.WithField("phone", err.Error())
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.get(tid, ref)
	f.record(domain.OpAmend, purpose, ref, actor)
	if err != nil {
		return err
	}
	k := blindKey{tid, domain.BlindPhone, phone}
	if phone != "" {
		if other, ok := f.blind[k]; ok && other != ref {
			return client.ProblemDuplicate
		}
	}
	if s.id.Phone != "" {
		delete(f.blind, blindKey{tid, domain.BlindPhone, s.id.Phone})
	}
	if phone != "" {
		f.blind[k] = ref
	}
	s.id.FullName, s.id.Phone, s.id.Address = domain.NormalizeName(id.FullName), phone, id.Address
	return nil
}

// sectionCheck: mục đích thuộc danh mục của op VÀ là mục đích của ngăn (như kho thật).
func sectionCheck(op string, purpose client.Purpose, section string) error {
	want, ok := domain.SectionPurpose(section)
	if !domain.AllowedPurpose(op, string(purpose)) || !ok || want != string(purpose) {
		return client.ProblemSectionInvalid
	}
	return nil
}

// PutSection thực hiện client.Profiles.
func (f *Fake) PutSection(ctx context.Context, purpose client.Purpose, ref uuid.UUID, section string, fields client.Fields) error {
	tid, actor, err := f.start(ctx, domain.OpSectionWrite, purpose, true)
	if err != nil {
		return err
	}
	if err := sectionCheck(domain.OpSectionWrite, purpose, section); err != nil {
		return err
	}
	if err := domain.ValidSectionFields(section, fields); err != nil {
		return client.ProblemSectionInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.get(tid, ref)
	f.record(domain.OpSectionWrite, purpose, ref, actor)
	if err != nil {
		return err
	}
	if s.sections == nil {
		s.sections = map[string]client.Fields{}
	}
	cp := client.Fields{}
	for k, v := range fields {
		cp[k] = v
	}
	s.sections[section] = cp
	return nil
}

// GetSection thực hiện client.Profiles.
func (f *Fake) GetSection(ctx context.Context, purpose client.Purpose, ref uuid.UUID, section string) (client.Fields, error) {
	tid, actor, err := f.start(ctx, domain.OpSectionRead, purpose, true)
	if err != nil {
		return nil, err
	}
	if err := sectionCheck(domain.OpSectionRead, purpose, section); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.get(tid, ref)
	f.record(domain.OpSectionRead, purpose, ref, actor)
	if err != nil {
		return nil, err
	}
	out := client.Fields{}
	for k, v := range s.sections[section] {
		out[k] = v
	}
	return out, nil
}
