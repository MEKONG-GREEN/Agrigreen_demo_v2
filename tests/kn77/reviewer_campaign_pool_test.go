package kn77_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReviewerCampaignPublishPoolBoundary(t *testing.T) {
	f := newAggFx(t, facilityDir{}, func(fm *forms.Module, kn *extension.Module) {
		kn.UseCNTSCampaigns(extension.CNTSCampaignDeps{Forms: fm.Publisher(), Control: fm.Control(), Figures: fm.Figures()})
	})
	if e := tenant.WithTenantTx(context.Background(), pgtest.Get(t).AdminPool(t, migrations.PF), f.tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), `INSERT INTO k_ref.org(tenant_id,ref,kind,code,admin_unit_code,active,source_seq) VALUES($1,$2,'ttkn_province','REVIEW89','89',true,1)`, f.tid, f.node)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	st, body := f.call(http.MethodPost, "/v1/extension/cnts/period-forms/install", authz.KNAdmin, nil)
	f.ok(st, body, nil)
	pool := pgtest.Get(t).Pool(t, migrations.PF, dbadmin.AppPlatform)
	for _, free := range []int{2, 1} {
		input := map[string]any{"name": fmt.Sprintf("Reviewer pool %d", free), "goal": "Kiểm giao dịch", "field": "thuy_san", "template_code": "CNTS-BM17", "scope_mode": "communes", "communes": []string{"X1", "X2"}, "from_date": "2026-10-01", "to_date": "2026-10-05", "due_at": "2026-10-09T03:00:00Z"}
		st, raw := f.campaignCall(http.MethodPost, "/v1/extension/cnts/campaigns", authz.KNTinh, false, input)
		var cv campaignView
		f.ok(st, raw, &cv)
		t.Run(fmt.Sprintf("free_connections_%d", free), func(t *testing.T) {
			max := int(pool.Config().MaxConns)
			if max < 2 {
				t.Fatal("control requires capacity >=2")
			}
			var held []*pgxpool.Conn
			defer func() {
				for _, c := range held {
					c.Release()
				}
			}()
			acq, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for i := 0; i < max-free; i++ {
				c, e := pool.Acquire(acq)
				if e != nil {
					t.Fatal(e)
				}
				held = append(held, c)
			}
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			req := httptest.NewRequest(http.MethodPost, "/v1/extension/cnts/campaigns/"+cv.ID+"/publish", bytes.NewReader([]byte("null"))).WithContext(ctx)
			k, _ := json.Marshal(authz.KNContext{Role: authz.KNTinh, Department: aggDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1", "X2"}}})
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Test-KN", string(k))
			req.Header.Set("X-Test-Actor", ids.New().String())
			req.Header.Set(idempotency.Header, "review-"+ids.New().String())
			w := httptest.NewRecorder()
			wait := pool.Stat().EmptyAcquireWaitTime()
			f.eng.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("campaign publish should not need a second free connection (free=%d wait=%v): HTTP%d %s", free, pool.Stat().EmptyAcquireWaitTime()-wait, w.Code, w.Body.String())
			}
		})
	}
}
