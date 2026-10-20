package app

// W11.46 (Q-057 "theo phòng (tham số)") — cấu hình lịch công tác theo phòng ban: quy tắc người duyệt, check-in GPS,
// ngưỡng, cờ lưu tọa độ (QĐ-8) và hạn lưu.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
)

// deptParam tra tham số theo phòng ban rồi tỉnh: "<k>:<phòng>:<tỉnh>", "<k>:<phòng>", "<k>:<tỉnh>", "<k>".
func (s *Service) deptParam(ctx context.Context, key, dept, province string, at time.Time) (params.ParamValue, bool, error) {
	if s.Params == nil {
		return params.ParamValue{}, false, nil
	}
	var keys []string
	if dept != "" {
		if province != "" {
			keys = append(keys, key+":"+dept+":"+province)
		}
		keys = append(keys, key+":"+dept)
	}
	if province != "" {
		keys = append(keys, key+":"+province)
	}
	keys = append(keys, key)
	for _, k := range keys {
		v, err := s.Params.At(ctx, k, at)
		if errors.Is(err, params.ErrParamNotFound) {
			continue
		}
		if err != nil {
			return params.ParamValue{}, false, err
		}
		return v, true, nil
	}
	return params.ParamValue{}, false, nil
}

func onOff(v params.ParamValue) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v.Text)) {
	case "on", "true", "1":
		return true, true
	case "off", "false", "0":
		return false, true
	}
	return false, false
}

// deptSettings điền phần cấu hình theo phòng ban. Mặc định: Chăn nuôi – Thủy sản duyệt "follower", bật check-in
// (BRD CNTS FN-12); phòng khác "levels", không check-in (Trồng trọt v7.7 Q-05: Trưởng phòng duyệt, không check-in).
func (s *Service) deptSettings(ctx context.Context, st *ScheduleSettings, dept, province string, at time.Time) error {
	st.Department = dept
	st.ApproverRule, st.Checkin = port.ApproverRuleLevels, false
	if dept == partyport.DeptChanNuoiThuySan {
		st.ApproverRule, st.Checkin = port.ApproverRuleFollower, true
	}
	st.CheckinFarM, st.CheckinAccM = domain.DefaultCheckinFarM, domain.DefaultCheckinAccuracyM
	st.StorePoint, st.RetentionDays = true, domain.DefaultCheckinRetentionDays
	if v, ok, err := s.deptParam(ctx, port.ParamScheduleApproverRule, dept, province, at); err != nil {
		return err
	} else if ok {
		if t := strings.TrimSpace(v.Text); t == port.ApproverRuleLevels || t == port.ApproverRuleFollower {
			st.ApproverRule = t
		}
	}
	for _, b := range []struct {
		key string
		dst *bool
	}{{port.ParamScheduleCheckin, &st.Checkin}, {port.ParamScheduleCheckinStorePoint, &st.StorePoint}} {
		v, ok, err := s.deptParam(ctx, b.key, dept, province, at)
		if err != nil {
			return err
		}
		if on, valid := onOff(v); ok && valid {
			*b.dst = on
		}
	}
	if v, ok, err := s.deptParam(ctx, port.ParamScheduleCheckinFarM, dept, province, at); err != nil {
		return err
	} else if ok && v.Value.IsPositive() {
		st.CheckinFarM = v.Value.InexactFloat64()
	}
	if v, ok, err := s.deptParam(ctx, port.ParamCNTSGPSAccuracyM, "", "", at); err != nil {
		return err
	} else if ok && v.Value.IsPositive() {
		st.CheckinAccM = v.Value.InexactFloat64()
	}
	if v, ok, err := s.deptParam(ctx, port.ParamScheduleCheckinRetentionDays, dept, province, at); err != nil {
		return err
	} else if ok && v.Value.IsPositive() {
		st.RetentionDays = min(int(v.Value.IntPart()), domain.MaxCheckinRetentionDays)
	}
	return nil
}

// mayApprove: người gọi (Cấp Tỉnh) có thẩm quyền duyệt theo quy tắc của phòng ban. "levels": cấp quản lý được duyệt
// (Q-05). "follower": người theo dõi được phân công (cờ nhiệm vụ) hoặc lãnh đạo phòng (FN-12 BR-04).
func (a knActor) mayApprove(st ScheduleSettings) bool {
	if st.ApproverRule != port.ApproverRuleFollower {
		return domain.ApproverLevelAllowed(a.MgmtLevel, st.ApproverLevels)
	}
	if k := a.ownedSession; k != nil {
		return !k.ReadOnly() && (k.Manager || k.HasDuty(port.SessionDutyScheduleFollower))
	}
	return accountsport.IsManager(a.MgmtLevel, "")
}

// followers trả người mang cờ "theo dõi lịch" của phòng ban (quy tắc follower); thiếu cổng → rỗng.
func (s *Service) followers(ctx context.Context, tid uuid.UUID, dept string, st ScheduleSettings) (map[uuid.UUID]bool, error) {
	out := map[uuid.UUID]bool{}
	if st.ApproverRule != port.ApproverRuleFollower || s.Holders == nil {
		return out, nil
	}
	refs, err := s.Holders.DutyHolders(ctx, tid, accountsport.KNTinh, dept, authz.DutyScheduleFollower)
	for _, r := range refs {
		out[r] = true
	}
	return out, err
}

// officerMayApprove là mayApprove cho danh bạ (người nhận thông báo, nhắc duyệt).
func officerMayApprove(o accountsport.KNOfficer, followers map[uuid.UUID]bool, st ScheduleSettings) bool {
	if st.ApproverRule != port.ApproverRuleFollower {
		return domain.ApproverLevelAllowed(o.MgmtLevel, st.ApproverLevels)
	}
	return followers[o.ActorRef] || accountsport.IsManager(o.MgmtLevel, "")
}
