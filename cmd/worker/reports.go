package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// reportsWorker dựng pf/reports cho worker một lần (sự kiện đầu tiên): kho đối
// tượng, cổng đọc của pf/extension, pf/mechanization và pf/cropcycle (sản lượng
// HTX — HTX US-DASH-02/03) — mỗi module bằng vai DB
// riêng (R-05) — cùng bộ gửi SMTP (AG_SMTP_*). Thiếu cấu hình → handler lỗi, sự
// kiện được giao lại rồi vào DLQ (R-62) — không mất yêu cầu.
type reportsWorker struct {
	once sync.Once
	mod  *reports.Module
	err  error
}

func (w *reportsWorker) module(ctx context.Context, log *slog.Logger) (*reports.Module, error) {
	w.once.Do(func() {
		w.mod, w.err = buildReportsWorker(ctx, log)
	})
	return w.mod, w.err
}

func buildReportsWorker(ctx context.Context, log *slog.Logger) (*reports.Module, error) {
	store, err := objstore.FromEnv(ctx)
	if err != nil {
		return nil, err
	}
	base := os.Getenv("AG_DB_URL")
	if base == "" {
		return nil, errors.New("thiếu AG_DB_URL")
	}
	open := func(role string) (*pgxpool.Pool, error) {
		dsn, err := dbadmin.RoleURL(base, migrations.PF, "", role)
		if err != nil {
			return nil, err
		}
		return db.Open(context.WithoutCancel(ctx), dsn, db.OptionsFromEnv("worker-reports"))
	}
	pools := map[string]*pgxpool.Pool{}
	for _, role := range []string{masterdata.DBRole, party.DBRole, extension.DBRole, mechanization.DBRole, cropcycle.DBRole} {
		p, err := open(role)
		if err != nil {
			for _, q := range pools {
				q.Close()
			}
			return nil, err
		}
		pools[role] = p
	}
	clk := clock.System{}
	md := masterdata.New(pools[masterdata.DBRole], clk)
	pty := party.New(pools[party.DBRole], clk, party.Deps{AdminUnits: md.AdminUnits()})
	kn := extension.New(pools[extension.DBRole], clk, extension.Deps{
		Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs(),
	})
	mech := mechanization.New(pools[mechanization.DBRole], clk, mechanization.Deps{
		Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Params: md.Params(), Catalogs: md.Catalogs(), AdminUnits: md.AdminUnits(),
	})
	cc := cropcycle.New(pools[cropcycle.DBRole], clk, cropcycle.Deps{Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Params: md.Params(), Catalogs: md.Catalogs()})
	// Email người nhận tra qua pf/accounts (Keycloak Admin REST, AG_KC_*); chưa cấu hình → bỏ gửi kèm cảnh báo.
	kc, err := accounts.DirectoryFromEnv()
	if err != nil {
		return nil, err
	}
	dir := accounts.New(nil, clk, accounts.Deps{Directory: kc}).MailDirectory()
	if dir == nil {
		log.Warn("chưa cấu hình AG_KC_* — lịch gửi báo cáo không tra được email người nhận", "event", "startup", "outcome", "skipped")
	}
	mailer, err := reports.SMTPFromEnv(clk)
	if err != nil {
		return nil, err
	}
	if mailer == nil {
		log.Warn("chưa cấu hình AG_SMTP_* — lịch gửi báo cáo sẽ bỏ gửi email", "event", "startup", "outcome", "skipped")
	}
	return reports.New(nil, clk, reports.Deps{
		Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs(), Store: store,
		Extension: kn.ReportData(), Mechanization: mech.ReportData(), CropCycle: cc.ReportData(), Directory: dir, Mailer: mailer, LinkBase: os.Getenv("AG_WEB_OPS_URL"), Log: log,
		LinkDownloadBase: os.Getenv("AG_REPORT_LINK_BASE"), // A4: liên kết tải Chi cục do api-platform phục vụ
	}), nil
}

// reportsConsumers là consumer dựng tệp (pf.reports.export.requested) và gửi
// email lịch (pf.reports.export.completed), cùng pool vai reports_app.
func reportsConsumers(log *slog.Logger) []consumer {
	w := &reportsWorker{}
	wrap := func(pick func(*reports.Module) inbox.Handler) inbox.Handler {
		return func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
			m, err := w.module(ctx, log)
			if err != nil {
				log.Error("chưa dựng được pf/reports ở worker", "event", env.EventType, "event_id", env.EventID, "outcome", "error", "error", err.Error())
				return err
			}
			return pick(m)(ctx, tx, env)
		}
	}
	return []consumer{
		{name: repport.ExportConsumer, cluster: migrations.PF, role: reports.DBRole, events: []string{repport.EventExportRequested},
			handler: wrap(func(m *reports.Module) inbox.Handler { return m.ExportHandler() })},
		{name: repport.DeliveryConsumer, cluster: migrations.PF, role: reports.DBRole, events: []string{repport.EventExportCompleted},
			handler: wrap(func(m *reports.Module) inbox.Handler { return m.DeliveryHandler() })},
		// KN v7.7 P4: gửi email báo cáo Chi cục kèm tệp (SMTP công vụ AG_SMTP_*; thiếu → "Gửi lỗi", gửi lại được).
		{name: repport.DispatchConsumer, cluster: migrations.PF, role: reports.DBRole, events: []string{repport.EventDispatchRequested},
			handler: wrap(func(m *reports.Module) inbox.Handler { return m.DispatchHandler() })},
	}
}
