package handler_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type unitManagerRow struct {
	tid, actor      uuid.UUID
	unit, org       any
	role, level     string
	group, internal string
	depts           []string
	attrs, inactive bool
	from, to        time.Time
}

func seedUnitManagerRow(t *testing.T, b tenant.Beginner, now time.Time, r unitManagerRow) {
	t.Helper()
	if r.role == "" {
		r.role = "kn_tinh"
	}
	if r.depts == nil {
		r.depts = []string{"fixture_department"}
	}
	if r.from.IsZero() {
		r.from = now.Add(-time.Hour)
	}
	var to, level, group, internal any
	if !r.to.IsZero() {
		to = r.to
	}
	if r.level != "" {
		level = r.level
	}
	if r.group != "" {
		group = r.group
	}
	if r.internal != "" {
		internal = r.internal
	}
	aid, event, by := ids.New(), ids.New(), ids.New()
	err := tenant.WithTenantTx(context.Background(), b, r.tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO pf_accounts.role_assignment_current
          (tenant_id,assignment_id,user_ref,role,org_ref,unit_ref,department_codes,mgmt_level,valid_from,valid_to,active,last_event_id,last_seq,created_by,created_at,updated_by,updated_at)
          VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,$13,$14,$13,$14)`, r.tid, aid, r.actor, r.role, r.org, r.unit, r.depts, level, r.from, to, !r.inactive, event, by, now)
		if err != nil || !r.attrs {
			return err
		}
		_, err = tx.Exec(context.Background(), `INSERT INTO pf_accounts.assignment_attr_current
          (tenant_id,assignment_id,user_ref,role,province_group,duties,internal_unit_code,last_event_id,last_seq,updated_by,updated_at)
          VALUES($1,$2,$3,$4,$5,'{}',$6,$7,1,$8,$9)`, r.tid, aid, r.actor, r.role, group, internal, event, by, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// NOTIFY-HCTH (#48): lãnh đạo đơn vị nội bộ tại MỘT Nút cho người nhận "Nhắc phòng chưa lập" — cấp quản lý/nhóm lãnh đạo
// theo danh mục thuộc tính, không nhóm chỉ xem, Cấp Tỉnh, lần gán còn hiệu lực, đúng Nút (UnitRef ưu tiên, dự phòng
// OrgRef), đơn vị = thuộc tính đơn vị nội bộ hoặc phòng ban khi thuộc tính trống; giao dịch chỉ đọc r_accounts, không
// Principal; tenant ngữ cảnh khác → từ chối.
func TestUnitManagersSameNodeLeadersOnly(t *testing.T) {
	now := time.Date(2026, 10, 26, 2, 0, 0, 0, time.UTC)
	clk := &scopedDutyClock{at: now}
	f := newFixtureAt(t, clk)
	writer := db.ForModule(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), accounts.DBModule)
	obs := &scopedDutyObserver{Beginner: writer}
	reader := accounts.New(obs, clk, accounts.Deps{}).UnitManagers()
	a, b, foreign := ids.New(), ids.New(), ids.New()
	head, deputy, group, deptOnly, fallbackOrg := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	cn := ids.New()
	rows := []unitManagerRow{
		{tid: f.tenant, actor: head, unit: a, level: port.MgmtTruongPhong, attrs: true, internal: "trong_trot"},
		{tid: f.tenant, actor: deputy, unit: a, level: port.MgmtPhoTruongPhong, attrs: true, internal: "trong_trot"},
		{tid: f.tenant, actor: group, unit: a, attrs: true, group: port.GroupLanhDaoPhong, internal: "trong_trot"},
		// Không thuộc tính đơn vị nội bộ → đơn vị = phòng ban (như phiên).
		{tid: f.tenant, actor: deptOnly, unit: a, level: port.MgmtTruongPhong, depts: []string{"chan_nuoi"}},
		{tid: f.tenant, actor: fallbackOrg, org: a, unit: uuid.Nil, level: port.MgmtTruongPhong, attrs: true, internal: "chan_nuoi"},
		{tid: f.tenant, actor: cn, unit: a, level: port.MgmtTruongPhong, attrs: true, internal: "chan_nuoi"},
		// Không nhận: chuyên viên, Ban Giám đốc (chỉ xem), Cấp Xã, Nút khác, đơn vị khác, hết/chưa hiệu lực, thu hồi, tenant khác.
		{tid: f.tenant, actor: ids.New(), unit: a, level: port.MgmtChuyenVien, attrs: true, internal: "trong_trot"},
		{tid: f.tenant, actor: ids.New(), unit: a, attrs: true, group: port.GroupBanGiamDoc, internal: "trong_trot"},
		{tid: f.tenant, actor: ids.New(), unit: a, role: "kn_xa", level: port.MgmtTruongPhong, attrs: true, internal: "trong_trot"},
		{tid: f.tenant, actor: ids.New(), unit: b, level: port.MgmtTruongPhong, attrs: true, internal: "trong_trot"},
		{tid: f.tenant, actor: ids.New(), unit: a, level: port.MgmtTruongPhong, attrs: true, internal: "hcth"},
		{tid: f.tenant, actor: ids.New(), unit: a, level: port.MgmtTruongPhong, attrs: true, internal: "trong_trot", to: now},
		{tid: f.tenant, actor: ids.New(), unit: a, level: port.MgmtTruongPhong, attrs: true, internal: "trong_trot", from: now.Add(time.Hour)},
		{tid: f.tenant, actor: ids.New(), unit: a, level: port.MgmtTruongPhong, attrs: true, internal: "trong_trot", inactive: true},
		{tid: foreign, actor: ids.New(), unit: a, level: port.MgmtTruongPhong, attrs: true, internal: "trong_trot"},
		// Thuộc tính đơn vị nội bộ thắng phòng ban: lãnh đạo có thuộc tính "hcth" dù phòng ban là trong_trot → không vào trong_trot.
		{tid: f.tenant, actor: ids.New(), unit: a, level: port.MgmtTruongPhong, attrs: true, internal: "hcth", depts: []string{"trong_trot"}},
	}
	for _, r := range rows {
		seedUnitManagerRow(t, writer, now, r)
	}
	job := context.Background()
	obs.calls = 0
	got, err := reader.UnitManagers(job, f.tenant, a, []string{"trong_trot", "chan_nuoi", "trong_trot", "khong_co"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]uuid.UUID{"trong_trot": sortedScopedRefs(head, deputy, group), "chan_nuoi": sortedScopedRefs(deptOnly, fallbackOrg, cn)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lãnh đạo:\n got %v\nwant %v", got, want)
	}
	if obs.calls != 1 || !obs.readOnly || obs.role != "r_accounts" {
		t.Fatalf("giao dịch: %d readonly=%v role=%s", obs.calls, obs.readOnly, obs.role)
	}
	if _, ok := authz.FromContext(job); ok {
		t.Fatal("job có Principal")
	}
	if got, err := reader.UnitManagers(job, f.tenant, b, []string{"trong_trot"}); err != nil || len(got["trong_trot"]) != 1 {
		t.Fatalf("Nút B: %v %v", got, err)
	}
	// Tham số sai / tenant ngữ cảnh khác → lỗi (không trả rỗng ngầm).
	for _, tc := range []struct {
		ctx   context.Context
		tid   uuid.UUID
		node  uuid.UUID
		units []string
		want  error
	}{
		{job, f.tenant, uuid.Nil, []string{"trong_trot"}, port.ErrScopedDutyQuery},
		{job, f.tenant, a, nil, port.ErrScopedDutyQuery},
		{job, f.tenant, a, []string{" "}, port.ErrScopedDutyQuery},
		{job, uuid.Nil, a, []string{"trong_trot"}, tenant.ErrMissing},
		{tenant.WithID(job, foreign), f.tenant, a, []string{"trong_trot"}, port.ErrScopedDutyTenant},
		{authz.WithPrincipal(job, authz.Principal{ActorRef: ids.New(), TenantID: foreign}), f.tenant, a, []string{"trong_trot"}, port.ErrScopedDutyTenant},
	} {
		if _, err := reader.UnitManagers(tc.ctx, tc.tid, tc.node, tc.units); !errors.Is(err, tc.want) {
			t.Fatalf("lỗi %v, muốn %v", err, tc.want)
		}
	}
}
