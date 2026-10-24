package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
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

// reportsWorker dựng pf/reports cho worker một lần — LÚC KHỞI ĐỘNG consumer (prepare, P0-WORKER-REPORTS #128): kho
// đối tượng, cổng đọc của pf/extension, pf/mechanization và pf/cropcycle (sản lượng HTX — HTX US-DASH-02/03) — mỗi
// module bằng vai DB riêng (R-05) — cùng bộ gửi SMTP (AG_SMTP_*). Thiếu cấu hình bắt buộc (DB, kho, tham số) → worker
// không khởi động (trước đây module dựng với DB nil, panic giữa lúc gửi email Chi cục → gửi trùng, kẹt "Đang gửi").
type reportsWorker struct {
	once sync.Once
	mod  *reports.Module
	err  error
}

func (w *reportsWorker) module(ctx context.Context, log *slog.Logger) (*reports.Module, error) {
	w.once.Do(func() {
		w.mod, w.err = buildReportsWorker(ctx, log)
		if w.err == nil {
			w.err = w.mod.RequireWorkerDeps()
		}
	})
	return w.mod, w.err
}

func buildReportsWorker(ctx context.Context, log *slog.Logger) (*reports.Module, error) {
	base := os.Getenv("AG_DB_URL")
	if base == "" {
		return nil, errors.New("thiếu AG_DB_URL")
	}
	// W2.6: một pool vai app_worker cho mọi module nguồn (mỗi module tự bọc db.ForModule — r_<module>).
	dsn, err := dbadmin.RoleURL(base, migrations.PF, "", dbadmin.AppWorker)
	if err != nil {
		return nil, err
	}
	pool, err := db.Open(context.WithoutCancel(ctx), dsn, db.OptionsFromEnv("worker-reports"))
	if err != nil {
		return nil, err
	}
	m, err := buildReportsModule(ctx, log, pool)
	if err != nil {
		pool.Close()
	}
	return m, err
}

// buildReportsModule nối pf/reports của worker trên pool vai app_worker đã mở (kiểm thử khởi động dùng pool pgtest).
func buildReportsModule(ctx context.Context, log *slog.Logger, pool *pgxpool.Pool) (*reports.Module, error) {
	store, err := objstore.FromEnv(ctx)
	if err != nil {
		return nil, err
	}
	clk := clock.System{}
	md := masterdata.New(pool, clk, masterdata.Deps{Params: []mdport.ParamRegistration{extension.MasterdataParams()}, Catalogs: []mdport.CatalogRegistration{extension.MasterdataCatalogs()}})
	pty := party.New(pool, clk, party.Deps{AdminUnits: md.AdminUnits()})
	knUnits := extension.NewPublishedAdminUnits(pool)
	knOrgs := extension.NewPublishedOrganizations(pool)
	kn := extension.New(pool, clk, extension.Deps{
		Scope: pty.ScopeResolver(), Orgs: knOrgs, Units: knUnits, Params: md.Params(), Catalogs: md.Catalogs(),
	})
	mech := mechanization.New(pool, clk, mechanization.Deps{
		Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Params: md.Params(), Catalogs: md.Catalogs(), AdminUnits: md.AdminUnits(),
	})
	// W3.3: báo cáo sản lượng HTX đọc yield_summary ở pf_coop (pf/cooperative, r_cooperative).
	coop := cooperative.New(pool, clk, cooperative.Deps{Scope: pty.ScopeResolver(), Orgs: pty.Orgs(), Params: md.Params(), Catalogs: md.Catalogs()})
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
	lease, err := reports.SendLeaseFromEnv(os.Getenv)
	if err != nil {
		return nil, err
	}
	// Review #128: consumer gửi Chi cục dùng hạn xử lý mặc định của inbox; hạn nhận việc phải dài hơn hạn đó + biên.
	if err := reports.ValidateSendLease(lease, inbox.DefaultHandlerTimeout); err != nil {
		return nil, err
	}
	// P0-WORKER-REPORTS (#128): pool THẬT (trước đây nil → panic khi ghi dấu người nhận, giao lại gửi trùng 6 email).
	return reports.New(pool, clk, reports.Deps{FormSeeds: []repport.FormSeedRegistration{extension.ReportFormSeeds()}, Builders: extension.ReportBuilders(extension.ReportBuilderDeps{Data: kn.ReportData(), Orgs: knOrgs, Units: knUnits, Catalogs: md.Catalogs(), Clock: clk}), Catalog: []repport.CatalogRegistration{extension.ReportCatalog()}, Policies: []repport.Policy{extension.ReportPolicy()},
		ResolveSession: extension.ReportSessionResolver(knOrgs),
		Scope:          pty.ScopeResolver(), Orgs: pty.Orgs(), Units: md.AdminUnits(), Params: md.Params(), Catalogs: md.Catalogs(), Store: store,
		Mechanization: mech.ReportData(), CropCycle: coop.ReportData(), Directory: dir, Mailer: mailer, LinkBase: os.Getenv("AG_WEB_OPS_URL"), Log: log,
		LinkDownloadBase: os.Getenv("AG_REPORT_LINK_BASE"), // A4: liên kết tải Chi cục do api-platform phục vụ
		DispatchLease:    lease,
	}), nil
}

// reportsConsumers là consumer dựng tệp (pf.reports.export.requested) và gửi
// email lịch (pf.reports.export.completed), cùng pool r_reports.
func reportsConsumers(log *slog.Logger) []consumer {
	w := &reportsWorker{}
	wrap := func(pick func(*reports.Module) inbox.Handler) inbox.Handler {
		return func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) (err error) {
			m, err := w.module(ctx, log)
			if err != nil {
				log.Error("chưa dựng được pf/reports ở worker", "event", env.EventType, "event_id", env.EventID, "outcome", "error", "error", err.Error())
				return err
			}
			// P0-WORKER-REPORTS (#128): panic trong handler → lỗi của thông điệp (NACK, lùi, DLQ), không làm sập worker.
			defer func() {
				if r := recover(); r != nil {
					log.Error("handler pf/reports panic", "event", env.EventType, "event_id", env.EventID, "outcome", "error", "panic", fmt.Sprint(r))
					err = fmt.Errorf("pf/reports: handler panic: %v", r)
				}
			}()
			return pick(m)(ctx, tx, env)
		}
	}
	// Dựng + kiểm phụ thuộc khi khởi động consumer (fail fast), không đợi sự kiện đầu tiên.
	prepare := func(ctx context.Context) error {
		_, err := w.module(ctx, log)
		return err
	}
	return []consumer{
		{prepare: prepare, name: repport.ExportConsumer, cluster: migrations.PF, module: reports.DBModule, events: []string{repport.EventExportRequested},
			handler: wrap(func(m *reports.Module) inbox.Handler { return m.ExportHandler() })},
		{prepare: prepare, name: repport.DeliveryConsumer, cluster: migrations.PF, module: reports.DBModule, events: []string{repport.EventExportCompleted},
			handler: wrap(func(m *reports.Module) inbox.Handler { return m.DeliveryHandler() })},
		// KN v7.7 P4: gửi email báo cáo Chi cục kèm tệp (SMTP công vụ AG_SMTP_*; thiếu → "Gửi lỗi", gửi lại được).
		{prepare: prepare, name: repport.DispatchConsumer, cluster: migrations.PF, module: reports.DBModule, events: []string{repport.EventDispatchRequested},
			handler: wrap(func(m *reports.Module) inbox.Handler { return m.DispatchHandler() })},
	}
}
