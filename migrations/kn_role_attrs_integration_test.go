package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// W11.07 (pf 000142): bảng thuộc tính vai, ủy quyền có hạn, đơn vị nội bộ — RLS cách ly tenant, sổ chỉ ghi
// thêm (r_accounts không UPDATE/DELETE sổ, không DELETE bảng chiếu), ràng buộc có hạn của ủy quyền ở DB.
func TestW1107_RoleAttrTablesIsolationAndLedger(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, unitFor("r_accounts", migrations.PF))
	m := db.Module("pf/accounts")
	ta, tb := ids.New(), ids.New()
	asg, unit, deleg, iu := ids.New(), ids.New(), ids.New(), ids.New()
	actor, grantee := ids.New(), ids.New()
	now := time.Now().UTC()

	run := func(tid uuid.UUID, fn func(tx pgx.Tx) error) error {
		return db.WithModuleTenantTx(ctx, pool, m, tid, fn)
	}
	mustRun := func(tid uuid.UUID, what string, fn func(tx pgx.Tx) error) {
		t.Helper()
		if err := run(tid, fn); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	insertDeleg := func(tx pgx.Tx, id uuid.UUID, from, to time.Time, grantor uuid.UUID) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.delegation_event (id, tenant_id, stream_key, delegation_id, action, grantor_ref,
		    grantee_ref, unit_ref, department_code, power, valid_from, valid_to, reason, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, current_setting('app.tenant_id')::uuid, $9, $2, 'granted', $3, $4, $5, 'trong_trot', 'mo_lai_ky',
		    $6, $7, 'ủy quyền mở lại kỳ khi đi công tác', now(), $3, $8, 'accounts.kn-attrs/1')`,
			ids.New(), id, grantor, grantee, unit, from, to, "k-"+ids.New().String(), "deleg:"+id.String())
		return err
	}

	mustRun(ta, "ghi tenant A", func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO pf_accounts.assignment_attr_event (id, tenant_id, stream_key, assignment_id, user_ref, role,
		    province_group, duties, internal_unit_code, after, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, $6, $3, $4, 'kn_tinh', 'lanh_dao_phong', '{qtnv_bao_gia}', 'trong_trot', '{}', now(), $4, $5,
		    'accounts.kn-attrs/1')`, ids.New(), ta, asg, actor, "k-"+ids.New().String(), "asgattr:"+asg.String()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO pf_accounts.assignment_attr_current (tenant_id, assignment_id, user_ref, role, province_group,
		    duties, internal_unit_code, last_event_id, last_seq, updated_by, updated_at)
		  VALUES ($1, $2, $3, 'kn_tinh', 'lanh_dao_phong', '{qtnv_bao_gia}', 'trong_trot', $4, 1, $3, now())`, ta, asg, actor, ids.New()); err != nil {
			return err
		}
		if err := insertDeleg(tx, deleg, now, now.Add(7*24*time.Hour), actor); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.internal_unit (tenant_id, id, org_ref, code, kind, title, last_event_id, last_seq, updated_by)
		  VALUES ($1, $2, $3, 'tram_1', 'tram', 'Trạm 1', $4, 1, $5)`, ta, iu, unit, ids.New(), actor)
		return err
	})

	// Tenant B không thấy dòng của A (p_tenant).
	mustRun(tb, "đọc tenant B", func(tx pgx.Tx) error {
		for _, q := range []string{"pf_accounts.assignment_attr_event", "pf_accounts.assignment_attr_current", "pf_accounts.delegation_event",
			"pf_accounts.internal_unit"} {
			var n int
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+q+" WHERE tenant_id = $1", ta).Scan(&n); err != nil {
				return err
			}
			if n != 0 {
				t.Errorf("%s: tenant B thấy %d dòng của tenant A", q, n)
			}
		}
		return nil
	})
	// Tenant B không ghi được dòng mang tenant A (WITH CHECK).
	err := run(tb, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.assignment_attr_current (tenant_id, assignment_id, user_ref, role, last_event_id,
		    last_seq, updated_by, updated_at) VALUES ($1, $2, $3, 'kn_xa', $4, 1, $3, now())`, ta, ids.New(), actor, ids.New())
		return err
	})
	if code := pgCode(err); code != "42501" {
		t.Errorf("ghi chéo tenant: muốn 42501, có %v", err)
	}

	// Sổ chỉ ghi thêm; bảng chiếu không DELETE.
	for _, q := range []string{
		"UPDATE pf_accounts.assignment_attr_event SET duties = '{}'",
		"DELETE FROM pf_accounts.assignment_attr_event",
		"UPDATE pf_accounts.delegation_event SET valid_to = valid_to + interval '1 day'",
		"DELETE FROM pf_accounts.delegation_current",
		"DELETE FROM pf_accounts.assignment_attr_current",
		"DELETE FROM pf_accounts.internal_unit",
		"UPDATE pf_accounts.internal_unit_event SET kind = 'trai'",
	} {
		err := run(ta, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, q); return err })
		if code := pgCode(err); code != "42501" {
			t.Errorf("%s: muốn 42501, có %v", q, err)
		}
	}

	// Ủy quyền phải có hạn (≤ 366 ngày ở DB), không tự ủy quyền; nhóm/cờ chỉ Cấp Tỉnh, cơ sở/hộ chỉ Cấp Xã.
	for name, fn := range map[string]func(tx pgx.Tx) error{
		"quá 1 năm":         func(tx pgx.Tx) error { return insertDeleg(tx, ids.New(), now, now.Add(400*24*time.Hour), actor) },
		"hết trước bắt đầu": func(tx pgx.Tx) error { return insertDeleg(tx, ids.New(), now, now.Add(-time.Hour), actor) },
		"tự ủy quyền":       func(tx pgx.Tx) error { return insertDeleg(tx, ids.New(), now, now.Add(time.Hour), grantee) },
		"nhóm ở Cấp Xã": func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.assignment_attr_event (id, tenant_id, stream_key, assignment_id, user_ref, role,
			    province_group, after, event_time, actor_ref, idem_key, rule_version)
			  VALUES ($1, current_setting('app.tenant_id')::uuid, 'asgattr:x', $2, $3, 'kn_xa', 'tong_hop', '{}', now(), $3, $4, 'r')`,
				ids.New(), ids.New(), actor, "k-"+ids.New().String())
			return err
		},
		"cơ sở ở Cấp Tỉnh": func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO pf_accounts.assignment_attr_event (id, tenant_id, stream_key, assignment_id, user_ref, role,
			    facility_refs, after, event_time, actor_ref, idem_key, rule_version)
			  VALUES ($1, current_setting('app.tenant_id')::uuid, 'asgattr:y', $2, $3, 'kn_tinh', ARRAY[$2]::uuid[], '{}', now(), $3, $4, 'r')`,
				ids.New(), ids.New(), actor, "k-"+ids.New().String())
			return err
		},
	} {
		if code := pgCode(run(ta, fn)); code != "23514" {
			t.Errorf("%s: muốn 23514 (CHECK), có %s", name, code)
		}
	}
}
