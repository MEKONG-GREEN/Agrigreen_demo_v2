package handler_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func egress(ch port.Channel, dataTenant, counterparty uuid.UUID, records int64, at time.Time) port.Egress {
	return port.Egress{SourceID: ids.New(), Channel: ch, DataTenantID: dataTenant, CounterpartyKind: port.CounterpartyTenant,
		CounterpartyRef: counterparty, Product: "valve_collection", ContractVersion: 1, Records: records, Bytes: records * 100, OccurredAt: at}
}

// ADR-0024 mục 1–2 (L-01, L-07, L-09): mỗi kênh một dòng egress_event ở tenant chủ dữ liệu, idempotent
// theo (kênh, nguồn, bên nhận); hạn mức kênh là tham số có phiên bản (pf/masterdata); observe KHÔNG BAO
// GIỜ từ chối (kể cả vượt hạn mức, kể cả checkpoint lỗi); enforce từ chối đúng mã và không đếm phần bị
// từ chối.
func TestCheckpointObserveEnforce_ADR0024(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, port.ModeObserve)
	dt, recv := ids.New(), ids.New()
	at := oct

	// Mỗi kênh một dòng; giao lặp cùng nguồn → không thêm.
	for _, ch := range port.Channels {
		e := egress(ch, dt, recv, 10, at)
		for i := 0; i < 2; i++ {
			if err := port.Guard(ctx, f.mod.Checkpoint(), f.mod.Mode(), e, nil); err != nil {
				t.Fatalf("%s: %v", ch, err)
			}
		}
		if n := f.count(dt, `SELECT count(*) FROM pf_metering.egress_event WHERE channel = $1`, string(ch)); n != 1 {
			t.Fatalf("kênh %s: %d dòng (muốn 1, giao lặp không thêm)", ch, n)
		}
	}
	if n := f.count(ids.New(), `SELECT count(*) FROM pf_metering.egress_event`); n != 0 {
		t.Fatal("tenant khác không thấy sổ xuất")
	}

	// Hạn mức kênh valve = 25 (tham số đã duyệt). observe: vượt vẫn cho qua, ghi lý do.
	f.params.set(dt, port.EgressParam(port.ParamEgressDailyQuotaFmt, port.ChannelValve), "25")
	d, err := f.mod.Checkpoint().Pass(ctx, egress(port.ChannelValve, dt, recv, 20, at))
	if err != nil || !d.Allowed || d.Reason != "quota_exceeded" || d.DailyRecords != 30 || d.DailyQuota != 25 || d.Mode != port.ModeObserve {
		t.Fatalf("observe vượt hạn mức phải cho qua: %+v %v", d, err)
	}
	if err := port.Guard(ctx, f.mod.Checkpoint(), port.ModeObserve, egress(port.ChannelValve, dt, recv, 5, at), nil); err != nil {
		t.Fatalf("Guard observe: %v", err)
	}

	// enforce (Phase K): cùng tenant, cùng hạn mức → từ chối, không đếm.
	enf := metering.New(f.pool, f.clk, metering.Deps{Params: f.params, Mode: port.ModeEnforce})
	d, err = enf.Checkpoint().Pass(ctx, egress(port.ChannelValve, dt, recv, 1, at))
	if err != nil || d.Allowed || d.Reason != "quota_exceeded" || d.DailyRecords != 35 {
		t.Fatalf("enforce phải từ chối: %+v %v", d, err)
	}
	if err := port.Guard(ctx, enf.Checkpoint(), enf.Mode(), egress(port.ChannelValve, dt, recv, 1, at), nil); codeOf(err) != "metering.egress_quota_exceeded" {
		t.Fatalf("Guard enforce: %v", err)
	}
	if n := f.count(dt, `SELECT count(*) FROM pf_metering.egress_event WHERE decision = 'denied' AND mode = 'enforce'`); n != 2 {
		t.Fatalf("dòng bị từ chối vẫn ghi sổ: %d", n)
	}
	if n := f.count(dt, `SELECT records FROM pf_metering.egress_counter_current WHERE channel = 'valve'`); n != 35 {
		t.Fatalf("phần bị từ chối không đếm: %d", n)
	}
	// Kênh khác / ngày khác không bị ảnh hưởng.
	if err := port.Guard(ctx, enf.Checkpoint(), enf.Mode(), egress(port.ChannelValve, dt, recv, 20, at.Add(24*time.Hour)), nil); err != nil {
		t.Fatalf("ngày mới: %v", err)
	}

	// Checkpoint lỗi hạ tầng: observe → tiếp tục; enforce → metering.egress_unavailable.
	broken := metering.New(brokenDB{}, clock.NewFixed(at), metering.Deps{})
	if err := port.Guard(ctx, broken.Checkpoint(), port.ModeObserve, egress(port.ChannelMRVPackage, dt, recv, 1, at), nil); err != nil {
		t.Fatalf("observe + checkpoint lỗi phải tiếp tục: %v", err)
	}
	if err := port.Guard(ctx, broken.Checkpoint(), port.ModeEnforce, egress(port.ChannelMRVPackage, dt, recv, 1, at), nil); codeOf(err) != "metering.egress_unavailable" {
		t.Fatalf("enforce + checkpoint lỗi: %v", err)
	}
	if broken.Metrics().Count("egress_checkpoint_errors:mrv_package") != 2 {
		t.Fatal("thiếu metric ag_egress_checkpoint_errors")
	}
	if f.mod.Metrics().Count("egress_records:valve:allowed") == 0 || enf.Metrics().Count("egress_denied:valve") != 2 {
		t.Fatal("thiếu metric ag_egress_records / ag_egress_denied")
	}
	// Public API qua Ledger.Record luôn enforce: hạn mức kênh public_api của chủ dữ liệu → 429.
	f.params.set(dt, port.EgressParam(port.ParamEgressDailyQuotaFmt, port.ChannelPublicAPI), "15")
	client := ids.New()
	if _, err := f.mod.Ledger().Record(ctx, f.op, call(client, ids.New(), dt, at)); err != nil {
		t.Fatal(err)
	}
	rec, err := f.mod.Ledger().Record(ctx, f.op, call(client, ids.New(), dt, at.Add(time.Second)))
	if codeOf(err) != "metering.egress_quota_exceeded" || rec.Units != 0 {
		t.Fatalf("Public API luôn enforce: %+v %v", rec, err)
	}
	if n := f.count(f.op, `SELECT count(*) FROM pf_metering.usage_event WHERE status = 429 AND problem_code = 'metering.egress_quota_exceeded' AND data_tenant_id IS NULL`); n != 1 {
		t.Fatalf("dòng sổ sử dụng của lời gọi bị checkpoint từ chối: %d", n)
	}
}

// ADR-0024 mục 1.3 (L-08, E2E-G5-08): khối lượng ngày vượt ngưỡng tuyệt đối (khi < 7 ngày mẫu) → đúng MỘT
// cảnh báo + MỘT sự kiện pf.metering.egress.anomaly_detected trong ngày; đủ 7 ngày mẫu → so 3 × trung vị.
func TestEgressAnomalyFirstOfDayOnly_ADR0024(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, port.ModeObserve)
	dt, recv := ids.New(), ids.New()
	f.params.set(dt, port.EgressParam(port.ParamEgressAnomalyRecordsFmt, port.ChannelReportExport), "100")
	ch := port.ChannelReportExport
	for i, n := range []int64{60, 50, 70, 5} {
		d, err := f.mod.Checkpoint().Pass(ctx, egress(ch, dt, recv, n, oct.Add(time.Duration(i)*time.Minute)))
		if err != nil || !d.Allowed {
			t.Fatal(d, err)
		}
		if wantAnomaly := i >= 1; d.Anomaly != wantAnomaly {
			t.Fatalf("lần %d: anomaly = %v (ngày %d)", i, d.Anomaly, d.DailyRecords)
		}
	}
	if n := f.count(dt, `SELECT count(*) FROM pf_metering.egress_anomaly WHERE basis = 'absolute' AND threshold = 100 AND daily_records = 110`); n != 1 {
		t.Fatalf("muốn đúng 1 cảnh báo, có %d", n)
	}
	if n := f.count(dt, `SELECT count(*) FROM shared.outbox WHERE event_type = $1`, port.EventEgressAnomaly); n != 1 {
		t.Fatalf("muốn đúng 1 sự kiện, có %d", n)
	}
	var raw []byte
	f.in(dt, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT payload FROM shared.outbox WHERE event_type = $1`, port.EventEgressAnomaly).Scan(&raw)
	})
	var pl port.EgressAnomalyV1
	if err := json.Unmarshal(raw, &pl); err != nil || pl.Channel != "report_export" || pl.DailyRecords != 110 || pl.Basis != "absolute" ||
		pl.CounterpartyRef != recv.String() || pl.BusinessDate != "2026-10-15" || pl.Mode != "observe" {
		t.Fatalf("payload: %s %v", raw, err)
	}
	if f.mod.Metrics().Count("egress_anomalies:report_export") != 1 {
		t.Fatal("metric ag_egress_anomalies phải tăng đúng một lần")
	}

	// Bên nhận khác: 7 ngày lịch sử ~10/ngày → ngưỡng 3 × 10 = 30.
	recv2 := ids.New()
	day0 := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	for i := 0; i < 7; i++ {
		if _, err := f.mod.Checkpoint().Pass(ctx, egress(ch, dt, recv2, 10, day0.Add(time.Duration(i)*24*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	today := day0.Add(8 * 24 * time.Hour)
	if d, _ := f.mod.Checkpoint().Pass(ctx, egress(ch, dt, recv2, 30, today)); d.Anomaly {
		t.Fatal("30 = ngưỡng → chưa bất thường")
	}
	if d, _ := f.mod.Checkpoint().Pass(ctx, egress(ch, dt, recv2, 1, today)); !d.Anomaly {
		t.Fatal("31 > 3 × trung vị → bất thường")
	}
	if n := f.count(dt, `SELECT count(*) FROM pf_metering.egress_anomaly WHERE counterparty_ref = $1 AND basis = 'median' AND threshold = 30`, recv2); n != 1 {
		t.Fatalf("cảnh báo theo trung vị: %d", n)
	}
	r := f.as(dt, port.RoleMeteringViewer).do("GET", "/v1/metering/egress/anomalies", nil, "")
	if items, _ := r.body["items"].([]any); r.status != 200 || len(items) != 2 {
		t.Fatalf("GET anomalies: %s", r.raw)
	}
	if other := f.as(ids.New(), port.RoleMeteringViewer).do("GET", "/v1/metering/egress/anomalies", nil, ""); len(other.body["items"].([]any)) != 0 {
		t.Fatal("tenant khác không thấy cảnh báo (L-11)")
	}
	p := f.as(dt, port.RoleMeteringViewer).do("GET", "/v1/metering/params", nil, "")
	if p.status != 200 || len(p.body["items"].([]any)) < 15 {
		t.Fatalf("params: %s", p.raw)
	}
}

type projRow map[string]string

func (f *fixture) dumpProjections(tid uuid.UUID) map[string][]projRow {
	f.t.Helper()
	ctx := context.Background()
	out := map[string][]projRow{}
	f.in(tid, func(tx pgx.Tx) error {
		for table, q := range map[string]string{
			"usage": `SELECT client_id::text, product, window_kind, window_start::text, units::text, calls::text FROM pf_metering.usage_counter_current
			  ORDER BY 1, 2, 3, 4`,
			"egress": `SELECT channel, counterparty_kind, counterparty_ref::text, business_date::text, records::text, bytes::text, events::text
			  FROM pf_metering.egress_counter_current ORDER BY 1, 2, 3, 4`,
			"statement": `SELECT statement_id::text, status, coalesce(approved_by::text, ''), coalesce(approved_at::text, ''), requested_at::text,
			  coalesce(superseded_by::text, ''), coalesce(supersedes_id::text, ''), snapshot_sha256, coalesce(total_vnd::text, '')
			  FROM pf_metering.statement_current ORDER BY 1`,
		} {
			rows, err := tx.Query(ctx, q)
			if err != nil {
				return err
			}
			for rows.Next() {
				vals, err := rows.Values()
				if err != nil {
					rows.Close()
					return err
				}
				r := projRow{}
				for i, v := range vals {
					r[strconv.Itoa(i)] = v.(string)
				}
				out[table] = append(out[table], r)
			}
			rows.Close()
		}
		return nil
	})
	return out
}

// R-22 (M-04): dựng lại bảng chiếu (bộ đếm sử dụng, bộ đếm xuất, bảng kê hiện hành) chỉ từ sổ = trạng
// thái sống, kể cả lời gọi 429, dòng bị checkpoint từ chối và bảng kê bù.
func TestRebuildProjectionsEqualsLive_R22(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, port.ModeObserve)
	client, ct := ids.New(), ids.New()
	for i := 0; i < 4; i++ {
		c := call(client, ct, f.op, oct.Add(time.Duration(i)*20*time.Hour))
		if i == 3 {
			c.Product, c.Kind, c.Status, c.DataTenantID = "", port.CallMeta, 200, uuid.Nil
		}
		if _, err := f.mod.Ledger().Record(ctx, f.op, c); err != nil {
			t.Fatal(err)
		}
	}
	f.params.set(f.op, port.EgressParam(port.ParamEgressDailyQuotaFmt, port.ChannelPublicAPI), "5")
	if _, err := f.mod.Ledger().Record(ctx, f.op, call(client, ct, f.op, oct.Add(100*time.Hour))); codeOf(err) != "metering.egress_quota_exceeded" {
		t.Fatalf("muốn bị checkpoint từ chối: %v", err)
	}
	enf := metering.New(f.pool, f.clk, metering.Deps{Params: f.params, Mode: port.ModeEnforce})
	f.params.set(f.op, port.EgressParam(port.ParamEgressDailyQuotaFmt, port.ChannelMRVPackage), "3")
	_, _ = enf.Checkpoint().Pass(ctx, egress(port.ChannelMRVPackage, f.op, ids.New(), 9, oct))
	sid := f.draftAndApprove(client, "2026-10")
	corr := f.as(f.op, port.RoleBillingOfficer).do("POST", "/v1/metering/statements", map[string]any{"period": "2026-10", "client_id": client.String(),
		"supersedes_id": sid, "correction_reason": "Kiểm thử dựng lại bảng chiếu"}, "")
	want(t, corr, 202, "", "bù")
	f.runStatementJobs(f.op)

	live := f.dumpProjections(f.op)
	if len(live["usage"]) == 0 || len(live["egress"]) == 0 || len(live["statement"]) != 2 {
		t.Fatalf("thiếu dữ liệu: %v", live)
	}
	r, err := f.mod.RebuildTenant(ctx, f.op)
	if err != nil || r.Statements != 2 {
		t.Fatalf("rebuild: %+v %v", r, err)
	}
	if rebuilt := f.dumpProjections(f.op); !reflect.DeepEqual(live, rebuilt) {
		t.Fatalf("dựng lại ≠ sống:\nsống   %v\ndựng lại %v", live, rebuilt)
	}
}

// A5 (03/10/2026, ADR-0024 ghi chú triển khai): xuất NỘI BỘ (Egress.Internal) qua checkpoint thật ở enforce — vượt
// hạn mức vẫn ghi egress_event (mode = observe, decision = allowed, có lý do quota_exceeded), được đếm (cộng vào
// phát hiện bất thường), KHÔNG bị từ chối; cùng lúc kênh ngoài vẫn bị từ chối.
func TestCheckpointInternalObserveUnderEnforce_A5(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, port.ModeEnforce)
	dt, actor := ids.New(), ids.New()
	f.params.set(dt, port.EgressParam(port.ParamEgressDailyQuotaFmt, port.ChannelReportExport), "3")
	enf := metering.New(f.pool, f.clk, metering.Deps{Params: f.params, Mode: port.ModeEnforce})
	internal := func(records int64) port.Egress {
		return port.Egress{SourceID: ids.New(), Channel: port.ChannelReportExport, DataTenantID: dt, CounterpartyKind: port.CounterpartyActor,
			CounterpartyRef: actor, Product: "kn_plot_file", Records: records, OccurredAt: oct, ActorRef: actor, Internal: true}
	}
	for i := 0; i < 3; i++ {
		if err := port.Guard(ctx, enf.Checkpoint(), enf.Mode(), internal(2), nil); err != nil {
			t.Fatalf("lần %d: xuất nội bộ không được từ chối ở enforce: %v", i+1, err)
		}
	}
	d, err := enf.Checkpoint().Pass(ctx, internal(1))
	if err != nil || !d.Allowed || d.Mode != port.ModeObserve || d.Reason != "quota_exceeded" || d.DailyRecords != 7 {
		t.Fatalf("Pass nội bộ: %+v %v", d, err)
	}
	if n := f.count(dt, `SELECT count(*) FROM pf_metering.egress_event WHERE product = 'kn_plot_file' AND mode = 'observe' AND decision = 'allowed'`); n != 4 {
		t.Fatalf("dòng egress_event nội bộ: %d", n)
	}
	if n := f.count(dt, `SELECT records FROM pf_metering.egress_counter_current WHERE channel = 'report_export'`); n != 7 {
		t.Fatalf("xuất nội bộ phải được đếm: %d", n)
	}
	// Kênh ngoài cùng bên nhận/ngày: vẫn bị chặn ở enforce.
	ext := internal(1)
	ext.Internal = false
	if err := port.Guard(ctx, enf.Checkpoint(), enf.Mode(), ext, nil); codeOf(err) != "metering.egress_quota_exceeded" {
		t.Fatalf("kênh ngoài phải bị từ chối: %v", err)
	}
}
