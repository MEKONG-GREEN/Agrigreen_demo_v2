package closedfig

// KHTCSources (ADR-0053 §7): port duy nhất phía KHTC — theo từng kỳ KHTC, số đã chốt của từng phòng nguồn đã quy đổi
// sang lịch KHTC (crosswalk §6), giữ danh tính kỳ nguồn, cờ chốt, nguồn gốc (băm) và tách confirmed/unconfirmed (D2).
// Nguồn: họ closed_figures/v1 (Trồng trọt — pf_extension_pub; CNTS — pf_forms_pub) và Báo giá (pf_price_pub.v1_board_daily,
// v1_ticket_week — W11.76) đọc trong CÙNG một giao dịch chỉ đọc của tenant (một mốc nhất quán cho ảnh chụp "Cập nhật số
// liệu" của W11.64). View giá chưa có (trước W11.76) → phòng Báo giá Unavailable, không lỗi.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	rcdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// PriceDepartment là phòng chủ dữ liệu Báo giá (BRD Báo giá v1.3).
const PriceDepartment = partyport.DeptThongTinTruyenThong

// priceCalendarVersion: Báo giá theo lịch v2 mặc định (D8 — phòng Báo giá không có lịch riêng; W11.76 ReportWeekOf).
var priceCalendarVersion = rcdomain.CalendarVersionDefault

// priceProfile là bên dùng cap/price của KN.
const priceProfile = "kn"

// priceLookback: số ngày đọc lùi trước kỳ đầu để lấy "ngày đã duyệt gần nhất" (D22) khi kỳ không có ngày duyệt.
const priceLookback = 62

// PriceBoardRow là một dòng của pf_price_pub.v1_board_daily (cột dùng — hợp đồng published, pf 000152 — W11.76).
type PriceBoardRow struct {
	BoardDate      time.Time
	BoardVersion   int64
	ApprovedAt     time.Time
	Reopened       bool
	ItemCode       string
	PriceMin       string
	PriceMax       string
	PriceAvg       *string
	PointsReported int
}

// PriceTicketRow là một dòng của pf_price_pub.v1_ticket_week.
type PriceTicketRow struct {
	WeekKey          string
	WeekFrom, WeekTo time.Time
	PointCode        string
	CommuneCode      string
	TicketsCollected int
	DaysScheduled    int
	DaysApproved     int
	WeekClosed       bool
}

// Periods thực hiện port.KHTCSources.
func (r Reader) Periods(ctx context.Context, tid uuid.UUID, q port.KHTCQuery) ([]port.KHTCPeriod, error) {
	t, tc, err := normTarget(q.Target)
	if err != nil {
		return nil, err
	}
	rule := tc.Rule
	first, err := rule.PeriodOf(t.PeriodType, q.From, nil)
	if err != nil {
		return nil, err
	}
	last, err := rule.PeriodOf(t.PeriodType, q.To, nil)
	if err != nil {
		return nil, err
	}
	wFrom, wTo := first.From, last.To
	var figs []port.ClosedFigure
	priceOK := false
	err = tenant.WithTenantTxOptions(ctx, r.DB, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		lb := wFrom.AddDate(0, 0, -priceLookback)
		fam, err := readFamily(ctx, tx, tid, port.ClosedFiguresQuery{Province: q.Province, From: &wFrom, To: &wTo})
		if err != nil {
			return err
		}
		figs = fam
		if !wants(q.Departments, PriceDepartment) {
			return nil
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM (VALUES (to_regclass('pf_price_pub.v1_board_daily')),
		    (to_regclass('pf_price_pub.v1_ticket_week'))) v(c) WHERE c IS NOT NULL`).Scan(&n); err != nil {
			return err
		}
		if n != 2 {
			return nil
		}
		priceOK = true
		boards, tickets, err := readPrice(ctx, tx, lb, wTo, wFrom)
		if err != nil {
			return err
		}
		figs = append(figs, PriceFigures(q.Province, boards, tickets)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := applyHTXAt(ctx, r.HTX, figs, q.ConfirmationAt); err != nil {
		return nil, err
	}
	var keep []port.ClosedFigure
	for _, f := range figs {
		if wants(q.Departments, f.DepartmentCode) {
			keep = append(keep, f)
		}
	}
	conv, err := Convert(keep, t)
	if err != nil {
		return nil, err
	}
	return Assemble(conv, t, tc, wFrom, wTo, q.Departments, priceOK, wants(q.Departments, PriceDepartment)), nil
}

func wants(depts []string, d string) bool { return len(depts) == 0 || slices.Contains(depts, d) }

func readPrice(ctx context.Context, tx pgx.Tx, boardFrom, to, weekFrom time.Time) ([]PriceBoardRow, []PriceTicketRow, error) {
	rows, err := tx.Query(ctx, `SELECT board_date, board_version::bigint, approved_at, board_reopened, item_code, price_min::text,
	    price_max::text, price_avg::text, points_reported FROM pf_price_pub.v1_board_daily
	   WHERE profile = $1 AND board_date BETWEEN $2 AND $3 ORDER BY board_date, item_code`, priceProfile, boardFrom, to)
	if err != nil {
		return nil, nil, err
	}
	boards, err := pgx.CollectRows(rows, pgx.RowToStructByPos[PriceBoardRow])
	if err != nil {
		return nil, nil, err
	}
	rows, err = tx.Query(ctx, `SELECT week_key, week_from, week_to, point_code, commune_code, tickets_collected, days_scheduled,
	    days_approved, week_closed FROM pf_price_pub.v1_ticket_week
	   WHERE profile = $1 AND week_to BETWEEN $2 AND $3 ORDER BY week_to, point_code`, priceProfile, weekFrom, to)
	if err != nil {
		return nil, nil, err
	}
	tickets, err := pgx.CollectRows(rows, pgx.RowToStructByPos[PriceTicketRow])
	return boards, tickets, err
}

// PriceFigures chuyển dòng giá về dạng kỳ nguồn của họ closed_figures/v1 (hàm thuần): mỗi ngày bảng giá đã duyệt là
// một kỳ ngày biểu PRICE-BOARD (dòng = mặt hàng; đã chốt = ngày không đang mở duyệt; SnapshotNo = phiên bản bảng giá);
// mỗi tuần báo giá là một kỳ tuần biểu PRICE-TICKETS (dòng = xã báo giá; đã chốt = mọi xã week_closed). Lịch Báo giá là
// lịch kỳ hệ thống (W11.03, v2). Giá dùng Last (bảng giá ngày duyệt cuối của kỳ KHTC — D22), số phiếu dùng Sum.
func PriceFigures(province string, boards []PriceBoardRow, tickets []PriceTicketRow) []port.ClosedFigure {
	var out []port.ClosedFigure
	base := func(pt, key string, from, to time.Time, form string) port.ClosedFigure {
		f, t := from, to
		return port.ClosedFigure{DepartmentCode: PriceDepartment, ProvinceCode: province, CalendarID: PriceDepartment, CalendarVersion: &priceCalendarVersion,
			PeriodType: pt, PeriodKey: key, PeriodFrom: &f, PeriodTo: &t, PeriodLabel: key, Source: "price", SourceModule: "pf/price",
			SourcePeriodID: uuid.NewSHA1(uuid.NameSpaceURL, []byte("pf/price:"+form+":"+key)), FormCode: form,
			ConfirmationStatus: port.ConfirmationConfirmed}
	}
	for i := 0; i < len(boards); {
		j := i
		for j < len(boards) && boards[j].BoardDate.Equal(boards[i].BoardDate) {
			j++
		}
		day := boards[i].BoardDate
		f := base(rcdomain.PeriodDay, day.Format(time.DateOnly), day, day, port.PriceFormBoard)
		closed := true
		var rows []map[string]any
		ver, at := boards[i].BoardVersion, boards[i].ApprovedAt
		for _, b := range boards[i:j] {
			closed = closed && !b.Reopened
			cells := map[string]string{"price_min": b.PriceMin, "price_max": b.PriceMax, "points_reported": fmt.Sprint(b.PointsReported)}
			if b.PriceAvg != nil {
				cells["price_avg"] = *b.PriceAvg
			}
			// indicator = mã mặt hàng: ô "<mặt hàng>/price_min" — min/max toàn tỉnh từng mặt hàng (D22), không cộng lẫn mặt hàng.
			rows = append(rows, map[string]any{"key": b.ItemCode, "unit": b.ItemCode, "unit_kind": "price_item", "indicator": b.ItemCode, "cells": cells})
		}
		f.FormClosed, f.PeriodClosed, f.KeyClosed, f.KeyClosedAll = closed, closed, closed, closed
		f.SnapshotNo, f.ClosedAt = &ver, &at
		setRows(&f, rows)
		out = append(out, f)
		i = j
	}
	for i := 0; i < len(tickets); {
		j := i
		for j < len(tickets) && tickets[j].WeekKey == tickets[i].WeekKey {
			j++
		}
		w := tickets[i]
		f := base(rcdomain.PeriodWeek, w.WeekKey, w.WeekFrom, w.WeekTo, port.PriceFormTickets)
		closed := true
		var rows []map[string]any
		for _, x := range tickets[i:j] {
			closed = closed && x.WeekClosed
			rows = append(rows, map[string]any{"key": x.PointCode, "unit": x.PointCode, "unit_kind": "price_point", "parent": x.CommuneCode,
				"cells": map[string]string{"tickets_collected": fmt.Sprint(x.TicketsCollected), "days_scheduled": fmt.Sprint(x.DaysScheduled),
					"days_approved": fmt.Sprint(x.DaysApproved)}})
		}
		f.FormClosed, f.PeriodClosed, f.KeyClosed, f.KeyClosedAll = closed, closed, closed, closed
		setRows(&f, rows)
		out = append(out, f)
		i = j
	}
	return out
}

// setRows gắn rows (thêm confirmation) và băm số/băm trạng thái như view của họ closed_figures/v1.
func setRows(f *port.ClosedFigure, rows []map[string]any) {
	core, _ := json.Marshal(rows)
	fh := sha256.Sum256(core)
	fs := hex.EncodeToString(fh[:])
	var st []string
	for _, r := range rows {
		r["confirmation"] = port.ConfirmationConfirmed
		st = append(st, fmt.Sprint(r["key"])+"="+port.ConfirmationConfirmed)
	}
	sh := sha256.Sum256([]byte(strings.Join(st, ";")))
	ss := hex.EncodeToString(sh[:])
	f.Rows, _ = json.Marshal(rows)
	f.FiguresHash, f.StatusHash = &fs, &ss
	n := len(rows)
	f.UnitsTotal, f.UnitsWithData = &n, &n
	f.Totals = json.RawMessage("[]")
}

// Assemble dựng kỳ KHTC từ kết quả quy đổi (hàm thuần): mọi kỳ đích trong [wFrom, wTo] (kể cả kỳ không có số), mỗi kỳ
// đủ các phòng (yêu cầu hoặc có số trong khoảng; Báo giá luôn có khi được yêu cầu/không lọc — Unavailable nếu view chưa
// có). Báo giá không có ngày duyệt trong kỳ → PRICE-BOARD lấy Last của kỳ đích gần nhất trước đó (Fallback, D22).
func Assemble(conv []port.TargetFigure, t port.CrosswalkTarget, tc rcdomain.Calendar, wFrom, wTo time.Time,
	depts []string, priceOK, priceWanted bool) []port.KHTCPeriod {
	deptSet := map[string]bool{}
	for _, d := range depts {
		deptSet[d] = true
	}
	byKey := map[string]map[string][]port.TargetFigure{}
	var boards []port.TargetFigure // PRICE-BOARD theo thứ tự kỳ đích (cả trước khoảng — để lấy ngày duyệt gần nhất)
	for _, f := range conv {
		if f.FormCode == port.PriceFormBoard {
			boards = append(boards, f)
		}
		if f.TargetTo.Before(wFrom) || f.TargetFrom.After(wTo) {
			continue
		}
		deptSet[f.DepartmentCode] = true
		if byKey[f.TargetKey] == nil {
			byKey[f.TargetKey] = map[string][]port.TargetFigure{}
		}
		byKey[f.TargetKey][f.DepartmentCode] = append(byKey[f.TargetKey][f.DepartmentCode], f)
	}
	if priceWanted {
		deptSet[PriceDepartment] = true
	}
	names := make([]string, 0, len(deptSet))
	for d := range deptSet {
		names = append(names, d)
	}
	sort.Strings(names)
	var out []port.KHTCPeriod
	for d := wFrom; !d.After(wTo); {
		span, err := tc.Rule.PeriodOf(t.PeriodType, d, nil)
		if err != nil {
			break
		}
		p := port.KHTCPeriod{RuleVersion: t.Version, TargetCalendarID: tc.ID, TargetCalendarVersion: tc.Version,
			TargetRule: tc.Rule.Tag(), TargetType: t.PeriodType, TargetKey: span.Key, TargetFrom: span.From, TargetTo: span.To, Closed: len(names) > 0}
		for _, dept := range names {
			forms := byKey[span.Key][dept]
			df := port.DepartmentFigures{DepartmentCode: dept, Forms: forms}
			if dept == PriceDepartment && !priceOK {
				df.Unavailable = true
			}
			if dept == PriceDepartment && priceOK && !slices.ContainsFunc(forms, func(f port.TargetFigure) bool { return f.FormCode == port.PriceFormBoard }) {
				for i := len(boards) - 1; i >= 0; i-- {
					if boards[i].TargetTo.Before(span.From) {
						fb := boards[i]
						fb.TargetKey, fb.TargetFrom, fb.TargetTo, fb.Sum, fb.SumConfirmed, fb.SumUnconfirmed = span.Key, span.From, span.To, nil, nil, nil
						df.Forms, df.Fallback = append(df.Forms, fb), true
						break
					}
				}
			}
			df.Missing = len(df.Forms) == 0 && !df.Unavailable
			df.Closed = !df.Unavailable // không kỳ nguồn nào quy vào (Missing) → đã chốt (KHTCQA, D17)
			mods := map[string]bool{}
			for _, f := range df.Forms {
				df.Closed = df.Closed && f.KeyClosed
				for _, s := range f.Sources {
					mods[s.SourceModule] = true
				}
			}
			ms := make([]string, 0, len(mods))
			for m := range mods {
				ms = append(ms, m)
			}
			sort.Strings(ms)
			df.SourceModule = strings.Join(ms, ",")
			if df.SourceModule == "" && dept == PriceDepartment {
				df.SourceModule = "pf/price"
			}
			p.Closed = p.Closed && df.Closed
			p.Departments = append(p.Departments, df)
		}
		out = append(out, p)
		d = span.To.AddDate(0, 0, 1)
	}
	return out
}

// MetricNames uses the existing versioned board product, under the same tenant RLS as figures.
func (r Reader) MetricNames(ctx context.Context, tid uuid.UUID, cutoff time.Time) (map[string]port.KHTCMetricName, error) {
	out := map[string]port.KHTCMetricName{}
	err := tenant.WithTenantTxOptions(ctx, r.DB, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT ON (item_code) item_code, item_name, unit_code FROM pf_price_pub.v1_board_daily
    WHERE profile=$1 AND board_date <= $2::date ORDER BY item_code, board_date DESC`, priceProfile, cutoff.Format(time.DateOnly))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var code string
			var name port.KHTCMetricName
			if err := rows.Scan(&code, &name.Label, &name.Unit); err != nil {
				return err
			}
			out[strings.ToLower(code)] = name
		}
		return rows.Err()
	})
	return out, err
}
