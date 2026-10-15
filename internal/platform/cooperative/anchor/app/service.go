// Package app: use case của neo HTX (D2 — CEO 13/10/2026, ADR-0052) trong pf/cooperative.
//
//   - Create: KN hoặc MKG lập HTX TẠM — gắn neo cho một Nút HTX (đã có hồ sơ) bằng MST chuẩn hóa hoặc mã tạm. MST đã
//     có neo / đã gắn hồ sơ HTX khác → 409 (không gộp ngầm: bên lập sau dùng neo đã có — problem trả mã neo đó).
//   - AttachMST: gắn MST cho neo mã tạm (sự kiện mst_attached).
//   - Claim: HTX thật (tài khoản đã kích hoạt, MST đã xác minh) nhận quyền — một lần mỗi neo, lũy đẳng, nguyên tử (một
//     giao dịch: sổ + bảng chiếu + bí danh + khóa nhận quyền), có chạy thử (dry_run), vết kiểm toán (details của dòng
//     sổ) và báo cáo xung đột khi Nút đích đã có dữ liệu. Không sửa dòng nào của sổ dữ liệu — chỉ lật trạng thái xác
//     nhận ("Chưa xác nhận" → "Đã xác nhận"), không sao chép dữ liệu.
//   - Backfill: neo cho HTX có trước D2 (chuyển dữ liệu hiện có sang mô hình đồng sở hữu), có chạy thử.
//   - Confirmed / CurrentOrg: trạng thái xác nhận theo Nút (mọi đường đọc cấp báo cáo) và phân giải bí danh (đồng bộ
//     ngoại tuyến sau nhận quyền).
package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/anchor/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/anchor/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Vai (đã khai ở pf/party, pf/accounts — không khai lại ở cooperative/port để không đổi tập quyền của module).
const (
	roleRegistryAdmin   = "registry_admin"
	roleKNNationalAdmin = "kn_national_admin"
	roleKNProvinceAdmin = "kn_province_admin"
)

var (
	readRoles    = []string{roleRegistryAdmin, authz.RolePlatformAdmin, roleKNNationalAdmin, roleKNProvinceAdmin}
	knCreateRole = readRoles
	// MKG vận hành nền tảng: lập HTX tạm "do MKG" cần platform_admin (giả định ADR-0052 §4).
	mkgCreateRole = []string{authz.RolePlatformAdmin}
	adminRoles    = []string{roleRegistryAdmin, authz.RolePlatformAdmin}
)

// Mã lỗi (docs/errors.md — pf/cooperative, neo HTX D2).
var (
	ErrNotFound     = problem.New(http.StatusNotFound, "cooperative.htx_anchor_not_found", "Không có neo HTX này")
	ErrMSTMalformed = problem.New(http.StatusUnprocessableEntity, "cooperative.htx_anchor_mst_malformed",
		"Mã số thuế không đúng dạng: 10 chữ số, hoặc 10 chữ số + mã chi nhánh 3 chữ số (001–999)")
	ErrMSTTaken = problem.New(http.StatusConflict, "cooperative.htx_anchor_mst_taken",
		"Mã số thuế đã có neo HTX (hoặc đã gắn hồ sơ HTX khác) — dùng neo đã có")
	ErrOrgNotCooperative = problem.New(http.StatusUnprocessableEntity, "cooperative.htx_anchor_org_not_cooperative",
		"Nút không phải HTX có hồ sơ")
	ErrOrgAnchored     = problem.New(http.StatusConflict, "cooperative.htx_anchor_org_anchored", "Nút HTX đã thuộc một neo khác")
	ErrProfileMismatch = problem.New(http.StatusUnprocessableEntity, "cooperative.htx_anchor_mst_profile_mismatch",
		"Mã số thuế khác mã số thuế trong hồ sơ HTX của Nút")
	ErrMSTPresent  = problem.New(http.StatusConflict, "cooperative.htx_anchor_mst_present", "Neo đã có mã số thuế")
	ErrMSTRequired = problem.New(http.StatusUnprocessableEntity, "cooperative.htx_anchor_mst_required",
		"Nhận quyền cần mã số thuế — gắn mã số thuế cho neo mã tạm trước")
	ErrMSTMismatch = problem.New(http.StatusUnprocessableEntity, "cooperative.htx_anchor_mst_mismatch",
		"Mã số thuế đã xác minh khác mã số thuế của neo")
	ErrAlreadyClaimed = problem.New(http.StatusConflict, "cooperative.htx_anchor_already_claimed",
		"Neo đã được nhận quyền bởi tài khoản / Nút khác")
	ErrClaimConflict = problem.New(http.StatusConflict, "cooperative.htx_anchor_claim_conflict",
		"Nút HTX đích đã có dữ liệu — xem conflict_report, xử lý trùng lặp trước khi nhận quyền")
	ErrCreatedBy = problem.New(http.StatusUnprocessableEntity, "cooperative.htx_anchor_created_by", "created_by phải là kn hoặc mkg")
)

// Service là use case neo HTX.
type Service struct {
	Creator partyport.CooperativeRegistrar // cổng tạo Nút + hồ sơ, chỉ dùng khi new_cooperative
	DB      tenant.Beginner                // đã bọc vai r_cooperative
	Clock   clock.Clock
	// failAfterWrite (chỉ kiểm thử): lỗi trả sau khi ghi xong mọi dòng của nhận quyền, trước commit.
	failAfterWrite func() error
}

// Anchor là neo kèm bí danh và trạng thái xác nhận.
type Anchor struct {
	store.Anchor
	Aliases      []uuid.UUID
	Confirmation string
}

func view(a store.Anchor, aliases []uuid.UUID) Anchor {
	return Anchor{Anchor: a, Aliases: aliases, Confirmation: domain.Confirmation(a.Status == domain.StatusClaimed)}
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

func normMST(raw string) (string, error) {
	m, err := domain.NormalizeMST(raw)
	if err != nil {
		return "", ErrMSTMalformed.WithField("mst", err.Error())
	}
	return m, nil
}

func taken(a *store.Anchor) error {
	return ErrMSTTaken.With("existing_anchor_id", a.AnchorID.String()).With("existing_created_by", a.CreatedByKind).
		With("existing_status", a.Status)
}

// CreateInput là đầu vào lập HTX tạm.
type CreateInput struct {
	OrgRef         uuid.UUID
	NewCooperative *partyport.CooperativeRegistration
	CreatedBy      string // kn | mkg
	MST            string // rỗng = chưa có MST → mã tạm
	AgreementRef   string // thỏa thuận HTX (căn cứ cấp phép KN) — tùy chọn
	IdemKey        string
}

// Create lập neo HTX tạm. Trả (neo, đã có từ trước theo IdemKey).
func (s *Service) Create(ctx context.Context, in CreateInput) (Anchor, bool, error) {
	roles := knCreateRole
	switch in.CreatedBy {
	case domain.CreatedByKN:
	case domain.CreatedByMKG:
		roles = mkgCreateRole
	default:
		return Anchor{}, false, ErrCreatedBy
	}
	p, err := authz.Require(ctx, roles...)
	if err != nil {
		return Anchor{}, false, err
	}
	var mst *string
	if strings.TrimSpace(in.MST) != "" {
		m, err := normMST(in.MST)
		if err != nil {
			return Anchor{}, false, err
		}
		mst = &m
	}
	var agreement *string
	if a := strings.TrimSpace(in.AgreementRef); a != "" {
		if len([]rune(a)) < 3 || len([]rune(a)) > 200 {
			return Anchor{}, false, problem.Validation.WithField("agreement_ref", "3–200 ký tự")
		}
		agreement = &a
	}
	if in.NewCooperative != nil {
		if in.OrgRef != uuid.Nil {
			return Anchor{}, false, problem.Validation.WithField("new_cooperative", "chỉ chọn org_ref hoặc new_cooperative")
		}
		if _, err := authz.Require(ctx, adminRoles...); err != nil {
			return Anchor{}, false, err
		}
		if in.IdemKey == "" {
			return Anchor{}, false, problem.IdempotencyRequired
		}
		// Đọc kết quả cũ trước khi tạo Nút. MST đã có neo cũng bị chặn trước bước tạo.
		var existing Anchor
		found := false
		err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			prev, err := store.EventByIdem(ctx, tx, "anchor-create:"+in.IdemKey)
			if err != nil {
				return err
			}
			if prev != nil {
				found = true
				return s.load(ctx, tx, prev.AnchorID, &existing)
			}
			if mst != nil {
				prior, err := store.ByKey(ctx, tx, *mst)
				if err != nil {
					return err
				}
				if prior != nil {
					return taken(prior)
				}
			}
			return nil
		})
		if err != nil || found {
			return existing, found, err
		}
		if s.Creator == nil {
			return Anchor{}, false, problem.New(http.StatusServiceUnavailable, "cooperative.registrar_unavailable", "Chưa nối luồng tạo HTX cho Neo")
		}
		registration := *in.NewCooperative
		registration.IdemKey = "anchor-org:" + in.IdemKey
		registration.TaxCode = ""
		if mst != nil {
			registration.TaxCode = *mst
		}
		org, err := s.Creator.RegisterCooperative(ctx, registration)
		if err != nil {
			return Anchor{}, false, err
		}
		in.OrgRef = org
	}
	idem := "anchor-create:" + in.IdemKey
	var out Anchor
	var replayed bool
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.EventByIdem(ctx, tx, idem); err != nil || prev != nil {
			if prev == nil {
				return err
			}
			replayed = true
			return s.load(ctx, tx, prev.AnchorID, &out)
		}
		a, err := s.insertAnchor(ctx, tx, p, in.OrgRef, in.CreatedBy, mst, agreement, "provisional_created", idem)
		if err != nil {
			return err
		}
		out = view(a, []uuid.UUID{a.OrgRef})
		return nil
	})
	return out, replayed, err
}

func (s *Service) load(ctx context.Context, tx pgx.Tx, id uuid.UUID, out *Anchor) error {
	a, err := store.ByID(ctx, tx, id, false)
	if err != nil {
		return err
	}
	if a == nil {
		return ErrNotFound
	}
	al, err := store.Aliases(ctx, tx, id)
	if err != nil {
		return err
	}
	*out = view(*a, al)
	return nil
}

// insertAnchor kiểm Nút + MST rồi ghi sổ, bảng chiếu, bí danh trong giao dịch tx.
func (s *Service) insertAnchor(ctx context.Context, tx pgx.Tx, p authz.Principal, org uuid.UUID, by string, mst, agreement *string,
	action, idem string) (store.Anchor, error) {
	prof, err := store.ProfileByOrg(ctx, tx, org)
	if err != nil {
		return store.Anchor{}, err
	}
	if prof == nil {
		return store.Anchor{}, ErrOrgNotCooperative.WithField("org_ref", "không có hồ sơ HTX")
	}
	if cur, err := store.ByOrg(ctx, tx, org); err != nil || cur != nil {
		if cur != nil {
			return store.Anchor{}, ErrOrgAnchored.With("existing_anchor_id", cur.AnchorID.String())
		}
		return store.Anchor{}, err
	}
	id := ids.New()
	key := domain.TempCode(id)
	if mst != nil {
		key = *mst
		if prof.TaxCode != "" && prof.TaxCode != *mst {
			return store.Anchor{}, ErrProfileMismatch.With("profile_tax_code", prof.TaxCode)
		}
		if cur, err := store.ByKey(ctx, tx, *mst); err != nil || cur != nil {
			if cur != nil {
				return store.Anchor{}, taken(cur)
			}
			return store.Anchor{}, err
		}
		if other, err := store.ProfileByTaxCode(ctx, tx, *mst); err != nil || (other != nil && other.OrgRef != org) {
			if err != nil {
				return store.Anchor{}, err
			}
			return store.Anchor{}, ErrMSTTaken.With("existing_org_ref", other.OrgRef.String())
		}
	}
	now := s.Clock.Now()
	ev := store.Event{ID: ids.New(), TenantID: p.TenantID, AnchorID: id, Action: action, AnchorKey: key, MST: mst, CreatedByKind: by,
		ProvisionalOrgRef: org, OrgRef: org, AgreementRef: agreement, EventTime: now, ActorRef: p.ActorRef, IdemKey: idem}
	seq, err := store.InsertEvent(ctx, tx, ev)
	if err != nil {
		return store.Anchor{}, err
	}
	a := store.Anchor{TenantID: p.TenantID, AnchorID: id, AnchorKey: key, MST: mst, Status: domain.StatusProvisional, CreatedByKind: by,
		CreatedBy: p.ActorRef, ProvisionalOrgRef: org, OrgRef: org, AgreementRef: agreement, LastEventID: ev.ID, LastSeq: seq,
		CreatedAt: now, UpdatedAt: now}
	if err := store.InsertAnchor(ctx, tx, a); err != nil {
		if errors.Is(err, store.ErrUnique) {
			return store.Anchor{}, ErrMSTTaken
		}
		return store.Anchor{}, err
	}
	if err := store.InsertAlias(ctx, tx, p.TenantID, org, id, "provisional", ev.ID); err != nil {
		if errors.Is(err, store.ErrUnique) {
			return store.Anchor{}, ErrOrgAnchored
		}
		return store.Anchor{}, err
	}
	return a, nil
}

// Get đọc một neo kèm sổ (vết kiểm toán).
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Anchor, []store.Event, error) {
	p, err := authz.Require(ctx, readRoles...)
	if err != nil {
		return Anchor{}, nil, err
	}
	var out Anchor
	var evs []store.Event
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := s.load(ctx, tx, id, &out); err != nil {
			return err
		}
		var e error
		evs, e = store.Events(ctx, tx, id)
		return e
	})
	return out, evs, err
}

// List liệt kê neo (status, tiền tố khóa/MST, trang theo anchor_id).
func (s *Service) List(ctx context.Context, f store.Filter) ([]Anchor, *uuid.UUID, error) {
	p, err := authz.Require(ctx, readRoles...)
	if err != nil {
		return nil, nil, err
	}
	if f.Status != "" && f.Status != domain.StatusProvisional && f.Status != domain.StatusClaimed {
		return nil, nil, problem.Validation.WithField("status", "provisional | claimed")
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		if m, err := domain.NormalizeMST(q); err == nil {
			q = m
		}
		f.Q = strings.ToUpper(q)
	}
	lim := f.Limit
	f.Limit++
	var out []Anchor
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		as, err := store.List(ctx, tx, f)
		if err != nil {
			return err
		}
		for _, a := range as {
			al, err := store.Aliases(ctx, tx, a.AnchorID)
			if err != nil {
				return err
			}
			out = append(out, view(a, al))
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	var next *uuid.UUID
	if len(out) > lim {
		out = out[:lim]
		n := out[lim-1].AnchorID
		next = &n
	}
	return out, next, nil
}

// AttachMST gắn MST cho neo mã tạm.
func (s *Service) AttachMST(ctx context.Context, id uuid.UUID, raw, idemKey string) (Anchor, error) {
	p, err := authz.Require(ctx, append(append([]string{}, adminRoles...), roleKNNationalAdmin, roleKNProvinceAdmin)...)
	if err != nil {
		return Anchor{}, err
	}
	mst, err := normMST(raw)
	if err != nil {
		return Anchor{}, err
	}
	idem := "anchor-mst:" + id.String() + ":" + idemKey
	var out Anchor
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.EventByIdem(ctx, tx, idem); err != nil || prev != nil {
			if prev == nil {
				return err
			}
			return s.load(ctx, tx, id, &out)
		}
		a, err := store.ByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if a == nil {
			return ErrNotFound
		}
		if a.MST != nil {
			if *a.MST == mst {
				return s.load(ctx, tx, id, &out)
			}
			return ErrMSTPresent.With("mst", *a.MST)
		}
		if cur, err := store.ByKey(ctx, tx, mst); err != nil || cur != nil {
			if cur != nil {
				return taken(cur)
			}
			return err
		}
		for _, org := range []uuid.UUID{a.OrgRef} {
			prof, err := store.ProfileByOrg(ctx, tx, org)
			if err != nil {
				return err
			}
			if prof != nil && prof.TaxCode != "" && prof.TaxCode != mst {
				return ErrProfileMismatch.With("profile_tax_code", prof.TaxCode)
			}
		}
		if other, err := store.ProfileByTaxCode(ctx, tx, mst); err != nil || (other != nil && other.OrgRef != a.OrgRef) {
			if err != nil {
				return err
			}
			return ErrMSTTaken.With("existing_org_ref", other.OrgRef.String())
		}
		ev := store.Event{ID: ids.New(), TenantID: p.TenantID, AnchorID: id, Action: "mst_attached", AnchorKey: mst, MST: &mst,
			CreatedByKind: a.CreatedByKind, ProvisionalOrgRef: a.ProvisionalOrgRef, OrgRef: a.OrgRef, AgreementRef: a.AgreementRef,
			Details: mustJSON(map[string]any{"previous_key": a.AnchorKey}), EventTime: s.Clock.Now(), ActorRef: p.ActorRef, IdemKey: idem}
		seq, err := store.InsertEvent(ctx, tx, ev)
		if err != nil {
			return err
		}
		if err := store.SetMST(ctx, tx, id, mst, ev.ID, seq); err != nil {
			if errors.Is(err, store.ErrUnique) {
				return ErrMSTTaken
			}
			return err
		}
		return s.load(ctx, tx, id, &out)
	})
	return out, err
}

// ClaimInput là đầu vào nhận quyền.
type ClaimInput struct {
	TargetOrgRef    uuid.UUID // Nil = Nút tạm của neo (trường hợp thường: tài khoản HTX thật kích hoạt trên chính Nút đó)
	AccountRef      uuid.UUID // tài khoản HTX đã kích hoạt (sub)
	VerifiedMST     string
	VerificationRef string // chứng cứ xác minh MST (số văn bản tra cứu, biên bản…)
	DryRun          bool
	IdemKey         string
}

// Party là một phía của chuyển chủ trong vết kiểm toán.
type Party struct {
	Party      string     `json:"party"` // provisional | htx
	CreatedBy  string     `json:"created_by,omitempty"`
	AccountRef *uuid.UUID `json:"account_ref,omitempty"`
	OrgRef     uuid.UUID  `json:"org_ref"`
}

// Moved là số dòng (không sao chép) chuyển chủ từ bên tạm sang HTX, theo Nút.
type Moved struct {
	OrgRef uuid.UUID        `json:"org_ref"`
	Counts store.DataCounts `json:"counts"`
}

// Conflict là một mục của báo cáo xung đột.
type Conflict struct {
	OrgRef uuid.UUID        `json:"org_ref"`
	Reason string           `json:"reason"`
	Counts store.DataCounts `json:"counts"`
}

// ClaimReport là kết quả (và vết kiểm toán) của nhận quyền.
type ClaimReport struct {
	AnchorID     uuid.UUID  `json:"anchor_id"`
	DryRun       bool       `json:"dry_run"`
	WouldSucceed bool       `json:"would_succeed"`
	Replayed     bool       `json:"replayed"`
	From         Party      `json:"from"`
	To           Party      `json:"to"`
	Moved        []Moved    `json:"moved"`
	AliasAdded   *uuid.UUID `json:"alias_added,omitempty"`
	Conflicts    []Conflict `json:"conflicts"`
	Confirmation struct {
		Before string `json:"before"`
		After  string `json:"after"`
	} `json:"confirmation"`
	DataCopied bool       `json:"data_copied"`
	EventID    *uuid.UUID `json:"event_id,omitempty"`
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// Claim nhận quyền HTX thật cho neo. Thành công → báo cáo (đã ghi details vào sổ); dry_run → báo cáo không ghi.
func (s *Service) Claim(ctx context.Context, id uuid.UUID, in ClaimInput) (ClaimReport, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return ClaimReport{}, err
	}
	if in.AccountRef == uuid.Nil {
		return ClaimReport{}, problem.Validation.WithField("account_ref", "bắt buộc — tài khoản HTX đã kích hoạt")
	}
	vref := strings.TrimSpace(in.VerificationRef)
	if n := len([]rune(vref)); n < 3 || n > 200 {
		return ClaimReport{}, problem.Validation.WithField("verification_ref", "3–200 ký tự — chứng cứ xác minh mã số thuế")
	}
	idem := "anchor-claim:" + id.String() + ":" + in.IdemKey
	var rep ClaimReport
	run := func(tx pgx.Tx) error {
		if !in.DryRun {
			if prev, err := store.EventByIdem(ctx, tx, idem); err != nil || prev != nil {
				if prev == nil {
					return err
				}
				if err := json.Unmarshal(prev.Details, &rep); err != nil {
					return err
				}
				rep.Replayed = true
				return nil
			}
		}
		a, err := store.ByID(ctx, tx, id, !in.DryRun)
		if err != nil {
			return err
		}
		if a == nil {
			return ErrNotFound
		}
		target := in.TargetOrgRef
		if target == uuid.Nil {
			target = a.ProvisionalOrgRef
		}
		if a.Status == domain.StatusClaimed {
			// Một lần nhận quyền mỗi neo: gọi lại cùng tài khoản + Nút → trả kết quả đã ghi (lũy đẳng).
			if a.OrgRef == target && a.HTXAccountRef != nil && *a.HTXAccountRef == in.AccountRef {
				evs, err := store.Events(ctx, tx, id)
				if err != nil {
					return err
				}
				for _, e := range evs {
					if e.Action == "claimed" {
						if err := json.Unmarshal(e.Details, &rep); err != nil {
							return err
						}
					}
				}
				rep.Replayed, rep.DryRun = true, in.DryRun
				return nil
			}
			return ErrAlreadyClaimed.With("org_ref", a.OrgRef.String())
		}
		if a.MST == nil {
			return ErrMSTRequired
		}
		vm, err := normMST(in.VerifiedMST)
		if err != nil {
			return err
		}
		if vm != *a.MST {
			return ErrMSTMismatch
		}
		rep = ClaimReport{AnchorID: id, DryRun: in.DryRun, Moved: []Moved{}, Conflicts: []Conflict{},
			From: Party{Party: "provisional", CreatedBy: a.CreatedByKind, OrgRef: a.OrgRef},
			To:   Party{Party: "htx", AccountRef: &in.AccountRef, OrgRef: target}}
		rep.Confirmation.Before, rep.Confirmation.After = domain.ConfirmationUnconfirmed, domain.ConfirmationConfirmed
		aliases, err := store.Aliases(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, org := range aliases {
			c, err := store.CountData(ctx, tx, org)
			if err != nil {
				return err
			}
			rep.Moved = append(rep.Moved, Moved{OrgRef: org, Counts: c})
		}
		if target != a.OrgRef {
			prof, err := store.ProfileByOrg(ctx, tx, target)
			if err != nil {
				return err
			}
			if prof == nil {
				return ErrOrgNotCooperative.WithField("target_org_ref", "không có hồ sơ HTX")
			}
			if prof.TaxCode != "" && prof.TaxCode != *a.MST {
				rep.Conflicts = append(rep.Conflicts, Conflict{OrgRef: target, Reason: "tax_code_differs"})
			}
			if other, err := store.ByOrg(ctx, tx, target); err != nil {
				return err
			} else if other != nil && other.AnchorID != id {
				rep.Conflicts = append(rep.Conflicts, Conflict{OrgRef: target, Reason: "target_has_other_anchor"})
			}
			c, err := store.CountData(ctx, tx, target)
			if err != nil {
				return err
			}
			if c.Total() > 0 {
				rep.Conflicts = append(rep.Conflicts, Conflict{OrgRef: target, Reason: "target_has_data", Counts: c})
			}
			t := target
			rep.AliasAdded = &t
		}
		rep.WouldSucceed = len(rep.Conflicts) == 0
		if in.DryRun {
			return nil
		}
		if !rep.WouldSucceed {
			return ErrClaimConflict.With("conflict_report", rep)
		}
		now := s.Clock.Now()
		evID := ids.New()
		rep.EventID = &evID
		ev := store.Event{ID: evID, TenantID: p.TenantID, AnchorID: id, Action: "claimed", AnchorKey: a.AnchorKey, MST: a.MST,
			CreatedByKind: a.CreatedByKind, ProvisionalOrgRef: a.ProvisionalOrgRef, OrgRef: target, HTXAccountRef: &in.AccountRef,
			VerificationRef: &vref, AgreementRef: a.AgreementRef, Details: mustJSON(rep), EventTime: now, ActorRef: p.ActorRef, IdemKey: idem}
		seq, err := store.InsertEvent(ctx, tx, ev)
		if err != nil {
			return err
		}
		if err := store.InsertClaim(ctx, tx, p.TenantID, id, evID, target, now); err != nil {
			if errors.Is(err, store.ErrUnique) {
				return ErrAlreadyClaimed
			}
			return err
		}
		if target != a.OrgRef {
			if err := store.InsertAlias(ctx, tx, p.TenantID, target, id, "claimed", evID); err != nil {
				if errors.Is(err, store.ErrUnique) {
					return ErrOrgAnchored
				}
				return err
			}
		}
		if err := store.SetClaimed(ctx, tx, id, target, in.AccountRef, p.ActorRef, now, evID, seq); err != nil {
			return err
		}
		if s.failAfterWrite != nil {
			return s.failAfterWrite()
		}
		return nil
	}
	opts := pgx.TxOptions{}
	if in.DryRun {
		opts.AccessMode = pgx.ReadOnly
	}
	err = tenant.WithTenantTxOptions(ctx, s.DB, p.TenantID, opts, run)
	if errors.Is(err, ledger.ErrDuplicate) {
		return ClaimReport{}, ErrAlreadyClaimed
	}
	return rep, err
}

// BackfillResult là kết quả neo dữ liệu có trước D2.
type BackfillResult struct {
	DryRun   bool           `json:"dry_run"`
	Created  int            `json:"created"`
	WithMST  int            `json:"with_mst"`
	TempCode int            `json:"temp_code"`
	Items    []BackfillItem `json:"items"`
	Skipped  []BackfillSkip `json:"skipped"`
}

// BackfillItem là một HTX được (hoặc sẽ được) neo.
type BackfillItem struct {
	OrgRef    uuid.UUID  `json:"org_ref"`
	AnchorKey string     `json:"anchor_key,omitempty"`
	MST       *string    `json:"mst"`
	AnchorID  *uuid.UUID `json:"anchor_id,omitempty"`
}

// BackfillSkip là HTX bị bỏ qua (MST hồ sơ sai dạng → neo mã tạm; không bỏ qua, ghi lý do).
type BackfillSkip struct {
	OrgRef uuid.UUID `json:"org_ref"`
	Reason string    `json:"reason"`
}

// Backfill neo mọi HTX chưa có neo (tối đa limit mỗi lượt): HTX có trước D2 do KN lập → neo provisional, created_by
// kn, khóa = MST hồ sơ (nếu đúng dạng) hoặc mã tạm. Không đổi dòng dữ liệu nào: chủ sở hữu (MKG + HTX) suy từ neo.
func (s *Service) Backfill(ctx context.Context, dryRun bool, limit int) (BackfillResult, error) {
	p, err := authz.Require(ctx, adminRoles...)
	if err != nil {
		return BackfillResult{}, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	res := BackfillResult{DryRun: dryRun, Items: []BackfillItem{}, Skipped: []BackfillSkip{}}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		profs, err := store.UnanchoredProfiles(ctx, tx, limit)
		if err != nil {
			return err
		}
		for _, pr := range profs {
			var mst *string
			if pr.TaxCode != "" {
				if m, err := domain.NormalizeMST(pr.TaxCode); err == nil && m == pr.TaxCode {
					mst = &m
				} else {
					res.Skipped = append(res.Skipped, BackfillSkip{OrgRef: pr.OrgRef, Reason: "tax_code_malformed_temp_code_used"})
				}
			}
			it := BackfillItem{OrgRef: pr.OrgRef, MST: mst}
			if mst != nil {
				res.WithMST++
			} else {
				res.TempCode++
			}
			if !dryRun {
				a, err := s.insertAnchor(ctx, tx, p, pr.OrgRef, domain.CreatedByKN, mst, nil, "legacy_backfilled",
					"anchor-backfill:"+pr.OrgRef.String())
				if err != nil {
					return err
				}
				id := a.AnchorID
				it.AnchorID, it.AnchorKey = &id, a.AnchorKey
				res.Created++
			}
			res.Items = append(res.Items, it)
		}
		return nil
	})
	return res, err
}

// Confirmed trả trạng thái đã xác nhận theo Nút trong tenant của context (không kiểm vai — bên gọi đã kiểm).
func (s *Service) Confirmed(ctx context.Context, orgs []uuid.UUID) (map[uuid.UUID]bool, error) {
	var out map[uuid.UUID]bool
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.Confirmed(ctx, tx, orgs)
		return e
	})
	return out, err
}

// CurrentOrg phân giải Nút qua bí danh của neo (đồng bộ ngoại tuyến: bản ghi tạo dưới Nút tạm trước khi nhận quyền).
func (s *Service) CurrentOrg(ctx context.Context, tid, org uuid.UUID) (uuid.UUID, error) {
	cur := org
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		cur, e = store.CurrentOrg(ctx, tx, org)
		return e
	})
	return cur, err
}

// Now cho handler (thời điểm phản hồi).
func (s *Service) Now() time.Time { return s.Clock.Now() }
