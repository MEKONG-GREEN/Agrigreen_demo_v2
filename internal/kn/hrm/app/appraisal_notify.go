package app

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func appraisalNotice(p AppraisalPeriod, c appraisalActor, action, step string, id uuid.UUID) port.AppraisalChangedV1 {
	due := p.SelfDueAt
	if step == "manager" {
		due = p.ManagerDueAt
	} else if step == "summary" {
		due = p.SummaryDueAt
	}
	return port.AppraisalChangedV1{NoticeID: id, PeriodRef: p.ID, NodeRef: p.NodeRef, Year: p.Year, Quarter: p.Quarter,
		Action: action, Step: step, DueAt: due.UTC().Format(time.RFC3339), ActorRef: c.ref, Targets: []uuid.UUID{}, Units: []string{}}
}

func appraisalManagerAudience(n *port.AppraisalChangedV1, m domain.AppraisalMember) {
	if m.SummaryKind == "MS06" || m.ManagementUnit == "" {
		n.Center = true // MS06 hoặc cảnh báo thiếu HS37: HCTH xử lý, không đoán người quản lý khác
	} else {
		n.Units = append(n.Units, m.ManagementUnit)
	}
}

// enqueueAppraisalNotice chỉ INSERT outbox trong cùng TX. Không gọi directory, vault hoặc kênh gửi ở đây.
func (s *Service) enqueueAppraisalNotice(ctx context.Context, tx pgx.Tx, c appraisalActor, n port.AppraisalChangedV1) error {
	seen := map[uuid.UUID]bool{uuid.Nil: true}
	targets := []uuid.UUID{}
	for _, r := range n.Targets {
		if !seen[r] {
			seen[r] = true
			targets = append(targets, r)
		}
	}
	slices.SortFunc(targets, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	slices.Sort(n.Units)
	n.Units = slices.Compact(n.Units)
	if len(targets) == 0 && len(n.Units) == 0 && !n.Center {
		return nil
	}
	for offset := 0; offset < len(targets) || offset == 0; offset += 500 {
		x := n
		x.Targets = targets[offset:min(offset+500, len(targets))]
		if offset > 0 {
			x.Units, x.Center = []string{}, false
		}
		if err := enqueueEvent(ctx, tx, c.tenant, s.Clock.Now(), port.EventAppraisalChanged, x); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) appraisalEntryNotice(ctx context.Context, tx pgx.Tx, c appraisalActor, p AppraisalPeriod, row store.AppraisalEvent, e domain.AppraisalEntry) error {
	step := "manager"
	switch row.Action {
	case "self_submit", "proxy_self", "reopen":
	case "return":
		step = e.ReturnedTo
	case "send":
		step = "summary"
	default:
		return nil
	}
	n := appraisalNotice(p, c, row.Action, step, row.ID)
	staff := e.Member.StaffID
	n.StaffRef = &staff
	if step == "self" {
		if e.Member.AccountRef != nil {
			n.Targets = append(n.Targets, *e.Member.AccountRef)
		} else {
			appraisalManagerAudience(&n, e.Member) // nhập thay có scan; không giả đã gửi OTP khách
		}
	} else if step == "summary" {
		n.Center = true
	} else {
		appraisalManagerAudience(&n, e.Member)
	}
	return s.enqueueAppraisalNotice(ctx, tx, c, n)
}

func (s *Service) remindAppraisal(ctx context.Context, tx pgx.Tx, c appraisalActor, p AppraisalPeriod, step, _ string) error {
	if step != "self" && step != "manager" && step != "summary" {
		return problem.Validation.WithField("step", "chọn self, manager hoặc summary")
	}
	rows, err := store.AppraisalEntrySummaries(ctx, tx, p.ID, false)
	if err != nil {
		return err
	}
	n := appraisalNotice(p, c, "remind", step, ids.New())
	for _, row := range rows {
		x, e := appraisalItem(row, c)
		if e != nil {
			return e
		}
		if x.Member.ExcludedReason != "" || x.Status == domain.AppraisalClosed {
			continue
		}
		switch step {
		case "self":
			if x.Status != domain.AppraisalNotStarted && !(x.Status == domain.AppraisalChangeRequested && x.ReturnedTo == "self") {
				continue
			}
			if x.Member.AccountRef != nil {
				n.Targets = append(n.Targets, *x.Member.AccountRef)
			} else {
				appraisalManagerAudience(&n, x.Member)
			}
		case "manager":
			if x.Status != domain.AppraisalSelfSubmitted && !(x.Status == domain.AppraisalChangeRequested && x.ReturnedTo == "manager") {
				continue
			}
			appraisalManagerAudience(&n, x.Member)
		case "summary":
			if x.Status == domain.AppraisalSent {
				continue
			}
			appraisalManagerAudience(&n, x.Member)
		}
	}
	if len(n.Targets) == 0 && len(n.Units) == 0 && !n.Center {
		return port.ErrAppraisalBlocked.WithDetail("không còn phiếu phù hợp để nhắc")
	}
	return s.enqueueAppraisalNotice(ctx, tx, c, n)
}
