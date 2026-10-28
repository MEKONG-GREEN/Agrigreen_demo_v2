// Package app là phần cài đặt của pf/export (xem tài liệu gói gốc internal/platform/export).
package app

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: giao dịch của module chạy SET LOCAL ROLE r_export (ADR-0028).
var DBModule = db.Module("pf/export")

// Service là năng lực xuất văn bản.
type Service struct {
	DB        tenant.Beginner
	Clock     clock.Clock
	Access    port.Access
	templates map[string][]doctpl.Template // khóa → phiên bản tăng dần
}

var _ port.Exporter = (*Service)(nil)

// NewService kiểm và nạp mẫu đăng ký. Mẫu sai, trùng khóa+phiên bản → lỗi khởi động.
func NewService(b tenant.Beginner, clk clock.Clock, acc port.Access, regs []port.TemplateRegistration) (*Service, error) {
	s := &Service{DB: b, Clock: clk, Access: acc, templates: map[string][]doctpl.Template{}}
	for _, reg := range regs {
		for _, t := range reg.Templates {
			if t.Owner == "" {
				t.Owner = reg.Owner
			}
			if err := doctpl.Validate(t); err != nil {
				return nil, err
			}
			for _, old := range s.templates[t.Key] {
				if old.Version == t.Version {
					return nil, fmt.Errorf("%w: %s v%d đăng ký hai lần", doctpl.ErrTemplate, t.Key, t.Version)
				}
			}
			s.templates[t.Key] = append(s.templates[t.Key], t)
		}
	}
	for k := range s.templates {
		slices.SortFunc(s.templates[k], func(a, b doctpl.Template) int { return cmp.Compare(a.Version, b.Version) })
	}
	return s, nil
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return db.WithModuleTenantTx(ctx, s.DB, DBModule, tid, fn)
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func principal(ctx context.Context) (authz.Principal, error) {
	p, ok := authz.FromContext(ctx)
	if !ok || p.TenantID == uuid.Nil {
		return authz.Principal{}, problem.Unauthenticated
	}
	return p, nil
}

// Template trả mẫu đã đăng ký (version 0 = lớn nhất).
func (s *Service) Template(key string, version int) (doctpl.Template, bool) {
	vs := s.templates[key]
	if len(vs) == 0 {
		return doctpl.Template{}, false
	}
	if version == 0 {
		return vs[len(vs)-1], true
	}
	for _, t := range vs {
		if t.Version == version {
			return t, true
		}
	}
	return doctpl.Template{}, false
}

func uniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// record ghi sổ phiên bản mẫu lần đầu dùng trong tenant; đã có mà khác băm → ErrTemplateDrift (bất biến).
func (s *Service) record(ctx context.Context, p authz.Principal, t doctpl.Template) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	hash := t.Hash()
	for attempt := 0; attempt < 2; attempt++ {
		err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			old, e := store.GetTemplate(ctx, tx, t.Key, t.Version)
			if e != nil || old != nil {
				if e == nil && old.Hash != hash {
					return port.ErrTemplateDrift.WithDetail(fmt.Sprintf("%s v%d", t.Key, t.Version))
				}
				return e
			}
			return store.InsertTemplate(ctx, tx, p.TenantID, store.TemplateVersion{ID: ids.New(), Key: t.Key, Version: t.Version,
				Title: t.Title, Owner: t.Owner, Raw: raw, Hash: hash, ActorRef: p.ActorRef, At: s.now()})
		})
		if !uniqueViolation(err) {
			return err
		}
	}
	return err
}

// Export dựng và ghi sổ một lần xuất (xem port.Exporter).
func (s *Service) Export(ctx context.Context, req port.Request) (port.Run, error) {
	p, err := principal(ctx)
	if err != nil {
		return port.Run{}, err
	}
	if s.DB == nil {
		return port.Run{}, port.ErrUnavailable
	}
	if !port.ValidSubject(req.Subject) || !port.ValidFileName(req.FileName) || req.IdemKey == "" || len(req.IdemKey) > 120 {
		return port.Run{}, port.ErrInvalid.WithDetail("subject, tên tệp .docx hoặc khóa idempotency không hợp lệ")
	}
	if req.Number != nil && (!port.ValidSeries(req.Number.Series) || req.Number.Var == "") {
		return port.Run{}, port.ErrInvalid.WithDetail("ký hiệu số văn bản không hợp lệ")
	}
	if req.Date != nil && req.Date.Var == "" {
		return port.Run{}, port.ErrInvalid.WithDetail("thiếu biến ngày")
	}
	t, ok := s.Template(req.Template, req.Version)
	if !ok {
		return port.Run{}, port.ErrTemplateNotFound
	}
	if err := s.record(ctx, p, t); err != nil {
		return port.Run{}, err
	}
	idem := "export:" + req.Subject + ":" + req.IdemKey
	var out *store.Run
	for attempt := 0; attempt < 3; attempt++ {
		err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
			var e error
			if out, e = store.RunByIdem(ctx, tx, idem); e != nil || out != nil {
				return e
			}
			out, e = s.render(ctx, tx, p, t, req)
			if e != nil {
				return e
			}
			return store.InsertRun(ctx, tx, p.TenantID, out, idem)
		})
		// Hai lần xuất đồng thời cùng subject tranh cấp số hoặc cùng khóa idempotency: lần thua đọc lại kết quả của lần thắng.
		if !uniqueViolation(err) {
			break
		}
	}
	if err != nil {
		return port.Run{}, err
	}
	return toPort(*out), nil
}

func (s *Service) render(ctx context.Context, tx pgx.Tx, p authz.Principal, t doctpl.Template, req port.Request) (*store.Run, error) {
	at := s.now()
	data := doctpl.Data{Vars: map[string]string{}, Lists: req.Data.Lists}
	for k, v := range req.Data.Vars {
		data.Vars[k] = v
	}
	run := &store.Run{ID: ids.New(), Subject: req.Subject, Template: t.Key, TemplateVersion: t.Version, TemplateHash: t.Hash(),
		FileName: req.FileName, ContentType: docx.ContentType, ActorRef: p.ActorRef, At: at}
	if n := req.Number; n != nil {
		if v := strings.TrimSpace(data.Vars[n.Var]); v != "" {
			run.DocNumber = v
		} else {
			num, err := store.NumberFor(ctx, tx, n.Series, req.Subject)
			if err != nil {
				return nil, err
			}
			if num == nil {
				year := at.In(clock.Zone()).Year()
				allocated, err := store.AllocateNumber(ctx, tx, p.TenantID, p.ActorRef, n.Series, year, req.Subject, at, ids.New())
				if err != nil {
					return nil, err
				}
				num = &allocated
			}
			run.DocNumber, run.DocNumberAuto = num.Text(), true
			data.Vars[n.Var] = num.Text()
		}
	}
	if d := req.Date; d != nil && strings.TrimSpace(data.Vars[d.Var]) == "" {
		data.Vars[d.Var] = doctpl.PlaceDateLine(d.Place, at)
	}
	body, err := doctpl.RenderDOCX(t, data)
	if err != nil {
		if errors.Is(err, doctpl.ErrData) || errors.Is(err, doctpl.ErrTemplate) || errors.Is(err, docx.ErrInvalid) {
			return nil, port.ErrInvalid.WithDetail(err.Error())
		}
		return nil, err
	}
	sum := sha256.Sum256(body)
	run.Body, run.BodyHash, run.Size, run.InputHash = body, hex.EncodeToString(sum[:]), int64(len(body)), data.Hash()
	return run, nil
}

func toPort(r store.Run) port.Run {
	return port.Run{ID: r.ID, Seq: r.Seq, Subject: r.Subject, Template: r.Template, TemplateVersion: r.TemplateVersion,
		TemplateHash: r.TemplateHash, InputHash: r.InputHash, DocNumber: r.DocNumber, DocNumberAuto: r.DocNumberAuto,
		FileName: r.FileName, ContentType: r.ContentType, Body: r.Body, BodyHash: r.BodyHash, Size: r.Size, ActorRef: r.ActorRef, At: r.At}
}

func verified(r *store.Run) (port.Run, error) {
	if r == nil {
		return port.Run{}, port.ErrRunNotFound
	}
	sum := sha256.Sum256(r.Body)
	if hex.EncodeToString(sum[:]) != r.BodyHash {
		return port.Run{}, port.ErrCorrupt
	}
	r.Size = int64(len(r.Body))
	return toPort(*r), nil
}

// Run trả lần xuất của subject kèm byte (đã kiểm băm).
func (s *Service) Run(ctx context.Context, subject string, id uuid.UUID) (port.Run, error) {
	p, err := principal(ctx)
	if err != nil {
		return port.Run{}, err
	}
	if !port.ValidSubject(subject) {
		return port.Run{}, port.ErrRunNotFound
	}
	var r *store.Run
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error { var e error; r, e = store.RunByID(ctx, tx, subject, id); return e })
	if err != nil {
		return port.Run{}, err
	}
	return verified(r)
}

// Runs trả lịch sử lần xuất của subject.
func (s *Service) Runs(ctx context.Context, subject string) ([]port.Run, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if !port.ValidSubject(subject) {
		return []port.Run{}, nil
	}
	return s.runs(ctx, p, store.RunFilter{Subject: subject, Limit: 100})
}

func (s *Service) runs(ctx context.Context, p authz.Principal, f store.RunFilter) ([]port.Run, error) {
	var rows []store.Run
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error { var e error; rows, e = store.Runs(ctx, tx, f); return e })
	if err != nil {
		return nil, err
	}
	out := make([]port.Run, 0, len(rows))
	for _, r := range rows {
		out = append(out, toPort(r))
	}
	return out, nil
}
