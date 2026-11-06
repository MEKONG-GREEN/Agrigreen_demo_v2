package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PNQuery215 struct {
	SnapshotID     uuid.UUID
	Program, Level string
}

type PNSource215 struct {
	ID         uuid.UUID `json:"id"`
	Version    int64     `json:"version"`
	CapturedAt time.Time `json:"captured_at"`
}

type PNProgress215 struct {
	Department string                    `json:"department"`
	Period     store.PNPeriod215         `json:"period"`
	Status     string                    `json:"status"`
	Snapshot   *PNSource215              `json:"snapshot"`
	Programs   []domain.PNProgram215     `json:"programs"`
	Rows       []domain.PNProgressRow215 `json:"rows"`
}

type PNPeriods215 struct {
	Department string              `json:"department"`
	Items      []store.PNPeriod215 `json:"items"`
	HasMore    bool                `json:"has_more"`
	NextAfter  *uuid.UUID          `json:"next_after"`
}

func (s *Service) pnReader215(ctx context.Context, department string) (caller, error) {
	p, k, err := port.RequireSession(ctx)
	if err != nil {
		return caller{}, err
	}
	if !slices.Contains([]string{"trong_trot", "chan_nuoi_thuy_san", "thong_tin_truyen_thong", "hanh_chinh_tong_hop"}, department) || k.Role != port.SessionRoleProvince && k.Role != port.SessionRoleAdmin {
		return caller{}, problem.Forbidden
	}
	if k.Role != port.SessionRoleAdmin && k.Department != department {
		return caller{}, problem.Forbidden
	}
	if s.DB == nil {
		return caller{}, ErrDependency
	}
	return caller{principal: p, kn: k}, nil
}

func (s *Service) PNProgressPeriods215(ctx context.Context, department string, year int, after uuid.UUID) (PNPeriods215, error) {
	out := PNPeriods215{Department: department, Items: []store.PNPeriod215{}}
	c, err := s.pnReader215(ctx, department)
	if err != nil {
		return out, err
	}
	if year < 2000 || year > 2100 {
		return out, ErrInvalid
	}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		var e error
		out.Items, e = store.PNPeriods215(ctx, tx, department, year, after)
		return e
	})
	if err != nil {
		return PNPeriods215{}, err
	}
	if out.Items == nil {
		out.Items = []store.PNPeriod215{}
	}
	if len(out.Items) > 50 {
		out.HasMore = true
		out.Items = out.Items[:50]
		id := out.Items[49].ID
		out.NextAfter = &id
	}
	return out, nil
}

func (s *Service) PNProgramProgress215(ctx context.Context, department string, id uuid.UUID, q PNQuery215) (PNProgress215, error) {
	out := PNProgress215{Department: department, Status: "not_prepared", Rows: []domain.PNProgressRow215{}, Programs: []domain.PNProgram215{}}
	c, err := s.pnReader215(ctx, department)
	if err != nil {
		return PNProgress215{}, err
	}
	filter := domain.Filter{Program: q.Program, Level: q.Level}
	if filter.Validate() != nil {
		return PNProgress215{}, ErrInvalid
	}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		p, term, e := loadPeriod(ctx, tx, id)
		if e != nil {
			return e
		}
		if p.Type != "program" || term == "cancelled" {
			return ErrNotFound
		}
		if !slices.Contains(p.Departments, department) {
			return problem.Forbidden
		}
		out.Period = store.PNPeriod215{ID: p.ID, Name: p.Name, Year: p.Year, From: p.From, To: p.To, Cutoff: p.Cutoff}
		snapshot, e := store.PNSnapshot215(ctx, tx, id, q.SnapshotID)
		if e != nil {
			return e
		}
		if snapshot == nil {
			if q.SnapshotID != uuid.Nil {
				return ErrNotFound
			}
			return nil
		}
		view, e := domain.ProjectPNProgress215(snapshot.Payload, department, filter)
		if errors.Is(e, domain.ErrPNProgramScope215) {
			return problem.Forbidden
		}
		if e != nil {
			return ErrDependency.WithDetail("Bản Mẫu5 đã lưu không đủ cấu trúc/số liệu hợp lệ").WithCause(e)
		}
		if view.Period.ID != p.ID || view.Period.Type != p.Type || view.Period.Year != p.Year || !view.Period.From.Equal(p.From) || !view.Period.To.Equal(p.To) || !view.Period.Cutoff.Equal(p.Cutoff) || view.InputHash != snapshot.InputHash {
			return ErrDependency.WithDetail("Bản Mẫu5 không khớp định danh kỳ hoặc mốc đầu vào")
		}
		out.Status = "snapshot"
		out.Snapshot = &PNSource215{ID: snapshot.ID, Version: snapshot.Version, CapturedAt: view.CapturedAt}
		out.Rows, out.Programs = view.Rows, view.Programs
		return nil
	})
	if err != nil {
		return PNProgress215{}, err
	}
	return out, nil
}

func (s *Service) PNProgressExcel215(ctx context.Context, department string, id uuid.UUID, q PNQuery215) ([]byte, string, error) {
	if q.SnapshotID == uuid.Nil {
		return nil, "", ErrInvalid.WithField("snapshot_id", "Chọn đúng bản Mẫu5 đang xem")
	}
	view, err := s.PNProgramProgress215(ctx, department, id, q)
	if err != nil {
		return nil, "", err
	}
	if view.Snapshot == nil {
		return nil, "", ErrDependency
	}
	// Local rendering is after the read transaction has committed; no exporter,
	// source refresh, new report number, publication or ledger write is involved.
	body, err := renderPNProgress215(view)
	if err != nil {
		return nil, "", ErrDependency.WithCause(err)
	}
	if len(body) > ArchiveMaxBytes {
		return nil, "", ErrDependency
	}
	p, _, err := port.RequireSession(ctx)
	if err != nil {
		return nil, "", err
	}
	_ = meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{Internal: true, SourceID: ids.New(), Channel: meter.ChannelReportExport, DataTenantID: p.TenantID, CounterpartyKind: meter.CounterpartyActor, CounterpartyRef: p.ActorRef, Product: "kn_khtc_report", Records: int64(len(view.Rows)), Bytes: int64(len(body)), OccurredAt: s.now(), ActorRef: p.ActorRef, TraceID: telemetry.TraceID(ctx)}, nil)
	return body, fmt.Sprintf("tien-do-%s-%s-v%d.xlsx", department, id, view.Snapshot.Version), nil
}
