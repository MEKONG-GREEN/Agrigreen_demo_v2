package app

// Ghi liên hệ ngoài tỉnh và thẻ Đơn vị (FN-13 B4: HCTH, Admin thêm, sửa, ngừng hiển thị).

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// areaContact là mã địa bàn của chủ thể liên hệ ngoài tỉnh ở kho — không phân phối khóa xuống thiết bị hiện trường.
const areaContact = "contact"

func (s *Service) manager(ctx context.Context) (caller, error) {
	c, err := s.who(ctx)
	if err != nil {
		return c, err
	}
	if !c.manage {
		return c, port.ErrManageForbidden
	}
	return c, nil
}

func fieldProblem(err error) error {
	var fe domain.FieldError
	if errors.As(err, &fe) {
		return problem.Validation.WithField(fe.Field, fe.Err.Error())
	}
	return err
}

// PersonInput là dữ liệu nhập của liên hệ ngoài tỉnh. Con trỏ nil = giữ nguyên (khi sửa).
type PersonInput struct {
	FullName     *string
	Phone        *string
	BirthYear    *string
	Email        *string
	Level        *string
	Organization *string
	Title        *string
	Hidden       *bool
}

func trimp(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// checkPII kiểm phần PII (họ tên ≤ 200, năm sinh, email). SĐT do kho chuẩn hóa/kiểm (lỗi chỉ mang tên trường).
func checkPII(in PersonInput, create bool) error {
	if in.FullName != nil || create {
		if n := utf8.RuneCountInString(strings.Join(strings.Fields(trimp(in.FullName)), " ")); n < 1 || n > 200 {
			return problem.Validation.WithField(domain.FieldFullName, "bắt buộc, tối đa 200 ký tự")
		}
	}
	if (in.Phone != nil || create) && trimp(in.Phone) == "" {
		return problem.Validation.WithField(domain.FieldPhone, "bắt buộc (FN-13 BR-06)")
	}
	if err := domain.CheckBirthYear(trimp(in.BirthYear)); err != nil {
		return fieldProblem(err)
	}
	return fieldProblem(domain.CheckEmail(domain.FieldEmail, trimp(in.Email)))
}

// vaultErr quy lỗi kho: trùng SĐT → ErrDuplicatePerson; lỗi khác giữ nguyên (problem của kho chỉ mang tên trường).
func vaultErr(err error) error {
	if errors.Is(err, client.ErrDuplicate) {
		return port.ErrDuplicatePerson
	}
	return err
}

// CreatePerson thêm liên hệ ngoài tỉnh: chủ thể mới ở kho (họ tên, SĐT), ngăn contact.card (email, năm sinh), phiên bản
// đầu ở sổ. SĐT đã thuộc một chủ thể (cán bộ thuộc tỉnh hoặc liên hệ khác) → ErrDuplicatePerson (BR-01).
func (s *Service) CreatePerson(ctx context.Context, in PersonInput, idem string) (Detail, error) {
	c, err := s.manager(ctx)
	if err != nil {
		return Detail{}, err
	}
	if err := checkPII(in, true); err != nil {
		return Detail{}, err
	}
	meta := domain.PersonMeta{Level: trimp(in.Level), Organization: domain.Text(trimp(in.Organization)), Title: domain.Text(trimp(in.Title))}
	if err := meta.Check(); err != nil {
		return Detail{}, fieldProblem(err)
	}
	if idem != "" {
		var found bool
		var ref uuid.UUID
		if err := s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
			r, ok, err := store.PersonByIdem(ctx, tx, "create:"+idem)
			found, ref = ok, r.SubjectRef
			return err
		}); err != nil {
			return Detail{}, err
		}
		if found {
			return s.GetPerson(ctx, ref)
		}
	}
	prof, err := s.profiles()
	if err != nil {
		return Detail{}, err
	}
	created, err := s.Vault.Create(ctx, client.PurposeContactDirectory, client.Identity{Kind: client.SubjectPerson,
		FullName: trimp(in.FullName), Phone: trimp(in.Phone), AreaCode: areaContact})
	if err != nil {
		return Detail{}, vaultErr(err)
	}
	if f := cardSection(trimp(in.Email), trimp(in.BirthYear)); len(f) > 0 {
		if err := prof.PutSection(ctx, client.PurposeContactDirectory, created.Ref, client.SectionContactCard, f); err != nil {
			return Detail{}, err
		}
	}
	key := "create:" + idem
	if idem == "" {
		key = "create:" + ids.New().String()
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		_, err := store.InsertPerson(ctx, tx, c.tenant, store.PersonRow{SubjectRef: created.Ref, Meta: meta,
			Version: store.Version{ID: ids.New(), Change: "created", Actor: c.ref}}, s.now(), key)
		return err
	})
	if err != nil {
		return Detail{}, err
	}
	return s.GetPerson(ctx, created.Ref)
}

func cardSection(email, year string) client.Fields {
	f := client.Fields{}
	if email != "" {
		f["email"] = email
	}
	if year != "" {
		f["birth_year"] = year
	}
	return f
}

// RevisePerson sửa / ngừng hiển thị / hiện lại liên hệ ngoài tỉnh. Cán bộ thuộc tỉnh → ErrStaffReadOnly (sửa ở FN-07).
// Sổ ghi TÊN trường đã đổi (cả trường PII ở kho), không giá trị (BR-05).
func (s *Service) RevisePerson(ctx context.Context, id uuid.UUID, in PersonInput, idem string) (Detail, error) {
	c, err := s.manager(ctx)
	if err != nil {
		return Detail{}, err
	}
	if err := checkPII(in, false); err != nil {
		return Detail{}, err
	}
	var cur store.PersonRow
	var found bool
	if err := s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		h, err := store.PersonHistory(ctx, tx, id)
		if len(h) > 0 {
			cur, found = h[len(h)-1], true
		}
		return err
	}); err != nil {
		return Detail{}, err
	}
	if !found {
		if s.isStaff(ctx, id) {
			return Detail{}, port.ErrStaffReadOnly
		}
		return Detail{}, port.ErrNotFound
	}
	next := cur.Meta
	if in.Level != nil {
		next.Level = trimp(in.Level)
	}
	if in.Organization != nil {
		next.Organization = domain.Text(trimp(in.Organization))
	}
	if in.Title != nil {
		next.Title = domain.Text(trimp(in.Title))
	}
	if err := next.Check(); err != nil {
		return Detail{}, fieldProblem(err)
	}
	changed := cur.Meta.Changed(next)
	if in.FullName != nil || in.Phone != nil || in.Email != nil || in.BirthYear != nil {
		pii, err := s.revisePII(ctx, id, in)
		if err != nil {
			return Detail{}, err
		}
		changed = append(changed, pii...)
	}
	hidden := cur.Hidden
	if in.Hidden != nil && *in.Hidden != cur.Hidden {
		hidden = *in.Hidden
		changed = append(changed, "hidden")
	}
	if len(changed) == 0 {
		return s.GetPerson(ctx, id)
	}
	change := "updated"
	if slices.Equal(changed, []string{"hidden"}) {
		change = map[bool]string{true: "hidden", false: "shown"}[hidden]
	}
	key := "revise:" + idem
	if idem == "" {
		key = "revise:" + ids.New().String()
	}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		_, err := store.InsertPerson(ctx, tx, c.tenant, store.PersonRow{SubjectRef: id, Meta: next,
			Version: store.Version{ID: ids.New(), Hidden: hidden, Change: change, ChangedFields: changed, Actor: c.ref}}, s.now(), key)
		return err
	})
	if err != nil && !errors.Is(err, ledger.ErrDuplicate) {
		return Detail{}, err
	}
	return s.GetPerson(ctx, id)
}

// revisePII sửa họ tên/SĐT (kho: Amend) và email/năm sinh (ngăn contact.card) khi khác giá trị hiện có. Trả tên trường đổi.
func (s *Service) revisePII(ctx context.Context, id uuid.UUID, in PersonInput) ([]string, error) {
	prof, err := s.profiles()
	if err != nil {
		return nil, err
	}
	cards, err := s.cards(ctx, []uuid.UUID{id}, domain.SetFull)
	if err != nil {
		return nil, err
	}
	cur, ok := cards[id]
	if !ok || cur.Erased {
		return nil, port.ErrNotFound
	}
	var changed []string
	name, phone := cur.FullName, cur.Phone
	if in.FullName != nil && strings.Join(strings.Fields(trimp(in.FullName)), " ") != cur.FullName {
		name = trimp(in.FullName)
		changed = append(changed, domain.FieldFullName)
	}
	if in.Phone != nil && domain.Digits(trimp(in.Phone)) != domain.Digits(domain.LocalPhone(cur.Phone)) {
		phone = trimp(in.Phone)
		changed = append(changed, domain.FieldPhone)
	}
	if len(changed) > 0 {
		if err := prof.Amend(ctx, client.PurposeContactDirectory, id, client.Identity{FullName: name, Phone: phone}); err != nil {
			return nil, vaultErr(err)
		}
	}
	email, year := cur.Email, cur.BirthYear
	sec := false
	if in.Email != nil && trimp(in.Email) != cur.Email {
		email, sec = trimp(in.Email), true
		changed = append(changed, domain.FieldEmail)
	}
	if in.BirthYear != nil && trimp(in.BirthYear) != cur.BirthYear {
		year, sec = trimp(in.BirthYear), true
		changed = append(changed, domain.FieldBirthYear)
	}
	if sec {
		if err := prof.PutSection(ctx, client.PurposeContactDirectory, id, client.SectionContactCard, cardSection(email, year)); err != nil {
			return nil, err
		}
	}
	return changed, nil
}

func (s *Service) isStaff(ctx context.Context, id uuid.UUID) bool {
	if s.Staff == nil {
		return false
	}
	st, err := s.Staff.Staff(ctx)
	return err == nil && slices.ContainsFunc(st, func(e port.StaffEntry) bool { return e.SubjectRef == id })
}

// --- Thẻ Đơn vị. ---

// Unit là một dòng thẻ Đơn vị. Bộ SetBasic chỉ có tên, cấp, SĐT (FN-13 B3).
type Unit struct {
	ID      uuid.UUID
	Name    string
	Level   string
	Phone   string
	Email   string
	Address string
	OrgRef  *uuid.UUID
	Hidden  bool
}

// UnitList là thẻ Đơn vị.
type UnitList struct {
	Set   domain.FieldSet
	Items []Unit
}

// ListUnits trả thẻ Đơn vị theo vai; q tìm theo tên đơn vị (không dấu) hoặc SĐT.
func (s *Service) ListUnits(ctx context.Context, q, level string, includeHidden bool) (UnitList, error) {
	c, err := s.who(ctx)
	if err != nil {
		return UnitList{}, err
	}
	out := UnitList{Set: c.set}
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		rows, err := store.CurrentUnits(ctx, tx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if (r.Hidden && (!includeHidden || !c.manage)) || (level != "" && r.In.Level != level) {
				continue
			}
			if !domain.Match(q, r.In.Name, deref(r.In.Phone)) {
				continue
			}
			u := Unit{ID: r.UnitID, Name: r.In.Name, Level: r.In.Level, Phone: deref(r.In.Phone)}
			if c.set == domain.SetFull {
				u.Email, u.Address, u.OrgRef, u.Hidden = deref(r.In.Email), deref(r.In.Address), r.OrgRef, r.Hidden
			}
			out.Items = append(out.Items, u)
		}
		return nil
	})
	slices.SortStableFunc(out.Items, func(a, b Unit) int {
		if d := levelOrder[a.Level] - levelOrder[b.Level]; d != 0 {
			return d
		}
		return strings.Compare(domain.Fold(a.Name), domain.Fold(b.Name))
	})
	return out, err
}

// UnitWrite là dữ liệu ghi thẻ Đơn vị. Hidden nil = giữ nguyên.
type UnitWrite struct {
	In     domain.UnitInput
	OrgRef *uuid.UUID
	Hidden *bool
}

// SaveUnit thêm (id = Nil) hoặc sửa thẻ Đơn vị.
func (s *Service) SaveUnit(ctx context.Context, id uuid.UUID, w UnitWrite, idem string) (Unit, error) {
	c, err := s.manager(ctx)
	if err != nil {
		return Unit{}, err
	}
	in, err := w.In.Normalize()
	if err != nil {
		return Unit{}, fieldProblem(err)
	}
	key := "unit:" + idem
	if idem == "" {
		key = "unit:" + ids.New().String()
	}
	var saved store.UnitRow
	err = s.tx(ctx, c.tenant, func(tx pgx.Tx) error {
		if idem != "" {
			if r, ok, err := store.UnitByIdem(ctx, tx, key); err != nil || ok {
				saved = r
				return err
			}
		}
		row := store.UnitRow{UnitID: id, OrgRef: w.OrgRef, In: in, Version: store.Version{ID: ids.New(), Change: "created", Actor: c.ref}}
		if id == uuid.Nil {
			row.UnitID = ids.New()
			if w.Hidden != nil {
				row.Hidden = *w.Hidden
			}
		} else {
			h, err := store.UnitHistory(ctx, tx, id)
			if err != nil {
				return err
			}
			if len(h) == 0 {
				return port.ErrNotFound
			}
			cur := h[len(h)-1]
			row.Hidden = cur.Hidden
			if w.OrgRef == nil {
				row.OrgRef = cur.OrgRef
			}
			row.ChangedFields = cur.In.Changed(in)
			if w.Hidden != nil && *w.Hidden != cur.Hidden {
				row.Hidden = *w.Hidden
				row.ChangedFields = append(row.ChangedFields, "hidden")
			}
			if len(row.ChangedFields) == 0 {
				saved = cur
				return nil
			}
			row.Change = "updated"
			if slices.Equal(row.ChangedFields, []string{"hidden"}) {
				row.Change = map[bool]string{true: "hidden", false: "shown"}[row.Hidden]
			}
		}
		if _, err := store.InsertUnit(ctx, tx, c.tenant, row, s.now(), key); err != nil {
			return err
		}
		saved = row
		return nil
	})
	if err != nil {
		return Unit{}, err
	}
	return Unit{ID: saved.UnitID, Name: saved.In.Name, Level: saved.In.Level, Phone: deref(saved.In.Phone), Email: deref(saved.In.Email),
		Address: deref(saved.In.Address), OrgRef: saved.OrgRef, Hidden: saved.Hidden}, nil
}
