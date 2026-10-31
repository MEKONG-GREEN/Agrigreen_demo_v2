package hrm

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

func appraisalDrafts(ctx context.Context, acc port.Access, dir port.NotifyDirectory, tid uuid.UUID, payload json.RawMessage, at time.Time) ([]notifyport.NotificationDraft, error) {
	var p port.AppraisalChangedV1
	if err := decodePayload(port.EventAppraisalChanged, payload, &p); err != nil {
		return nil, err
	}
	if p.NoticeID == uuid.Nil || p.PeriodRef == uuid.Nil || p.NodeRef == uuid.Nil || p.ActorRef == uuid.Nil || p.Year < 2020 || p.Year > 2100 || p.Quarter < 0 || p.Quarter > 4 ||
		!slices.Contains([]string{"self", "manager", "summary"}, p.Step) ||
		!slices.Contains([]string{"open", "self_submit", "proxy_self", "send", "return", "reopen", "remind"}, p.Action) || len(p.Targets) > 500 || len(p.Units) > 5000 {
		return nil, fmt.Errorf("hrm: payload đánh giá thiếu/sai tham chiếu, kỳ hoặc bước")
	}
	if len(p.Targets) == 0 && len(p.Units) == 0 && !p.Center {
		return nil, fmt.Errorf("hrm: payload đánh giá thiếu đối tượng nhận")
	}
	for _, target := range p.Targets {
		if target == uuid.Nil {
			return nil, fmt.Errorf("hrm: mã người nhận rỗng")
		}
	}
	for _, unit := range p.Units {
		if strings.TrimSpace(unit) == "" || len(unit) > 100 {
			return nil, fmt.Errorf("hrm: đơn vị nhận không hợp lệ")
		}
	}
	if _, err := time.Parse(time.RFC3339, p.DueAt); err != nil {
		return nil, fmt.Errorf("hrm: payload đánh giá có hạn không hợp lệ")
	}
	refs := slices.Clone(p.Targets)
	if len(p.Units) > 0 || p.Center {
		if dir == nil {
			return nil, errNoNotifyDirectory
		}
	}
	if len(p.Units) > 0 {
		units := slices.Clone(p.Units)
		slices.Sort(units)
		key := "hrm.appraisal.managers:" + tid.String() + ":" + p.NodeRef.String() + ":" + strings.Join(units, ",")
		found, err := txdeps.Get(ctx, key, func(ctx context.Context) (map[string][]uuid.UUID, error) {
			return dir.UnitManagers(ctx, tid, p.NodeRef, units)
		})
		if err != nil {
			return nil, err
		}
		for _, unit := range units {
			refs = append(refs, found[unit]...)
		}
	}
	if p.Center {
		for _, role := range acc.ReadBy {
			found, err := roleHolders(ctx, dir, tid, role, acc.Department)
			if err != nil {
				return nil, err
			}
			refs = append(refs, found...)
		}
		for _, role := range acc.ManageBy {
			found, err := roleHolders(ctx, dir, tid, role, "")
			if err != nil {
				return nil, err
			}
			refs = append(refs, found...)
		}
	}
	label := fmt.Sprintf("năm %d", p.Year)
	if p.Quarter > 0 {
		label = fmt.Sprintf("quý %d/%d", p.Quarter, p.Year)
	}
	title := "Phiếu đánh giá " + label
	body := "Đợt đánh giá đã có bước cần thực hiện. Mở phiếu/đề xuất được phân công để kiểm tra."
	switch p.Action {
	case "open":
		body = "Đợt đã mở. Hãy hoàn tất phiếu tự đánh giá được phát hành cho mình."
	case "remind":
		title = "Nhắc hoàn tất đánh giá " + label
	case "return", "reopen":
		title = "Phiếu đánh giá cần điều chỉnh " + label
	}
	subject := p.PeriodRef
	link := "/kn/nhan-su/danh-gia/dot/" + p.PeriodRef.String()
	if p.StaffRef != nil && *p.StaffRef != uuid.Nil {
		link += "/phieu/" + p.StaffRef.String()
	}
	template := notifyport.NotificationDraft{Kind: port.KindAppraisal, Severity: notifyport.SeverityNormal, Title: title, Body: body,
		DeepLink: link, SubjectRef: &subject, CreatedAt: at.UTC()}
	return draftsFor(template, refs, p.ActorRef), nil
}
