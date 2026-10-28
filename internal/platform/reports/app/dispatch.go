package app

// Báo cáo Chi cục KN v7.7 (FN-TI-02.06; kn-v7.7-plan P4 mục 10): danh bạ người nhận (hộp thư đơn vị, Q-09), xuất tệp
// toàn tỉnh từ LẦN TỔNG HỢP GẦN NHẤT của pf/extension (Excel đúng bố cục mẫu, PDF; Google Sheet TẮT mặc định —
// ADR-0026), gửi email kèm tệp qua SMTP công vụ ở worker; vượt giới hạn đính kèm → chỉ gửi liên kết; gửi lỗi → gửi
// lại; mọi lần xuất đi qua điểm kiểm soát DLP (egress, kind "dispatch"). Không có bước trình duyệt (BRD).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/dispatch"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/form"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/render"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// EgressDispatch là đường xuất "Báo cáo Chi cục" qua điểm kiểm soát.
const EgressDispatch = "dispatch"

// Mã lỗi báo cáo Chi cục (docs/errors.md).
var (
	ErrDispatchNotFound   = problem.New(http.StatusNotFound, "reports.dispatch_not_found", "Không tìm thấy lần xuất/gửi báo cáo Chi cục")
	ErrRecipientNotFound  = problem.New(http.StatusNotFound, "reports.dispatch_recipient_not_found", "Không tìm thấy người nhận")
	ErrRecipientDuplicate = problem.New(http.StatusConflict, "reports.dispatch_recipient_duplicate", "Địa chỉ đã có trong danh bạ")
	ErrDispatchNoData     = problem.New(http.StatusConflict, "reports.dispatch_no_data", "Kỳ chưa có dữ liệu tổng hợp — bấm Cập nhật ở Báo cáo tổng hợp các xã")
	ErrGSheetDisabled     = problem.New(http.StatusConflict, "reports.gsheet_disabled", "Tạo link Google Sheet đang tắt (ADR-0026 chưa được chấp nhận)")
	ErrDispatchSending    = problem.New(http.StatusConflict, "reports.dispatch_sending", "Đang gửi — chờ kết quả trước khi gửi lại")
	ErrNoRecipients       = problem.New(http.StatusUnprocessableEntity, "reports.dispatch_no_recipients", "Chưa chọn người nhận Chi cục")
	ErrValveBlocked       = problem.New(http.StatusUnprocessableEntity, "reports.gsheet_valve_blocked", "Van chặn: mẫu có trường không được phép đưa ra ngoài lãnh thổ")
)

// sessionActor keeps authenticated identity separate from the registered scope.
type sessionActor struct {
	p        authz.Principal
	scope    port.SessionActor
	province string
}

func (s *Service) resolveSessionActor(ctx context.Context, write bool) (sessionActor, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return sessionActor{}, problem.Unauthenticated
	}
	if s.ResolveSession == nil {
		return sessionActor{}, problem.Forbidden
	}
	operation := port.SessionReadDispatch
	if write {
		operation = port.SessionWriteDispatch
	}
	scope, handled, err := s.ResolveSession(ctx, p, operation)
	if err != nil {
		return sessionActor{}, err
	}
	if !handled || !scope.Allowed || scope.AllowsDepartment == nil || (!scope.AllDepartments && (scope.Department == "" || !scope.AllowsDepartment(scope.Department))) || (scope.Province == "" && !scope.AllProvinces) {
		return sessionActor{}, problem.Forbidden
	}
	return sessionActor{p: p, scope: scope, province: scope.Province}, nil
}

func (a sessionActor) dept() string {
	if a.scope.AllDepartments {
		return ""
	}
	return a.scope.Department
}
func (a sessionActor) allows(province, dept string) bool {
	return a.scope.AllowsDepartment != nil && a.scope.AllowsDepartment(dept) && (a.province == "" || a.province == province)
}
func (a sessionActor) onBehalf() *uuid.UUID {
	if a.scope.OnBehalfOf == nil || *a.scope.OnBehalfOf == uuid.Nil {
		return nil
	}
	v := *a.scope.OnBehalfOf
	return &v
}

// ---------------------------------------------------------------------------
// Danh bạ người nhận Chi cục
// ---------------------------------------------------------------------------

// RecipientInput là yêu cầu thêm người nhận.
type RecipientInput struct {
	Label     string
	Address   string `pii:"true"`
	Kind      string
	IsDefault bool
}

// ListDispatchRecipients trả danh bạ (Cấp Tỉnh thấy đủ địa chỉ; Cấp Quốc gia thấy địa chỉ đã che).
func (s *Service) ListDispatchRecipients(ctx context.Context, province string) ([]store.DispatchRecipient, bool, error) {
	a, err := s.resolveSessionActor(ctx, false)
	if err != nil {
		return nil, false, err
	}
	if a.province != "" {
		province = a.province
	}
	if province == "" {
		return nil, false, problem.Validation.WithField("province", "chọn tỉnh")
	}
	dept := a.scope.Department
	var out []store.DispatchRecipient
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var err error
		out, err = store.Recipients(ctx, tx, province, dept, false)
		return err
	})
	masked := !a.scope.UnmaskRecipients
	if masked {
		for i := range out {
			if out[i].Address != nil {
				m := dispatch.Mask(*out[i].Address)
				out[i].Address = &m
			}
		}
	}
	return out, masked, err
}

// AddDispatchRecipient thêm hộp thư đơn vị của Chi cục (người nhận/CC).
func (s *Service) AddDispatchRecipient(ctx context.Context, in RecipientInput, idem string) (store.DispatchRecipient, error) {
	a, err := s.resolveSessionActor(ctx, true)
	if err != nil {
		return store.DispatchRecipient{}, err
	}
	in.Label, in.Address = strings.TrimSpace(in.Label), strings.ToLower(strings.TrimSpace(in.Address))
	switch {
	case len([]rune(in.Label)) < 1 || len([]rune(in.Label)) > 200:
		return store.DispatchRecipient{}, problem.Validation.WithField("label", "bắt buộc, tối đa 200 ký tự")
	case !dispatch.ValidAddress(in.Address):
		return store.DispatchRecipient{}, problem.Validation.WithField("address", "địa chỉ hộp thư không hợp lệ")
	case in.Kind != dispatch.KindTo && in.Kind != dispatch.KindCC:
		return store.DispatchRecipient{}, problem.Validation.WithField("kind", "to | cc")
	}
	now := s.now()
	r := store.DispatchRecipient{ID: ids.New(), Department: a.scope.Department, Province: a.province, Label: in.Label, Address: &in.Address, Kind: in.Kind,
		IsDefault: in.IsDefault, Active: true, CreatedBy: a.p.ActorRef, CreatedAt: now, UpdatedAt: now}
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		existing, err := store.Recipients(ctx, tx, r.Province, r.Department, true)
		if err != nil {
			return err
		}
		for _, x := range existing {
			if x.Address != nil && strings.EqualFold(*x.Address, in.Address) {
				return ErrRecipientDuplicate
			}
		}
		if err := store.InsertRecipient(ctx, tx, a.p.TenantID, r); err != nil {
			return err
		}
		after, _ := json.Marshal(map[string]any{"label": r.Label, "kind": r.Kind, "is_default": r.IsDefault, "active": true})
		return store.InsertRecipientEvent(ctx, tx, a.p.TenantID, store.RecipientEvent{ID: ids.New(), RecipientID: r.ID, Action: "added",
			Department: r.Department, Province: r.Province, After: after, EventTime: now, ActorRef: a.p.ActorRef, IdemKey: idemOr(idem, "recipient")})
	})
	return r, err
}

// DeactivateDispatchRecipient ngừng dùng một người nhận (không xóa, lý do bắt buộc).
func (s *Service) DeactivateDispatchRecipient(ctx context.Context, id uuid.UUID, why, idem string) (store.DispatchRecipient, error) {
	a, err := s.resolveSessionActor(ctx, true)
	if err != nil {
		return store.DispatchRecipient{}, err
	}
	rsn, err := reason(why, true)
	if err != nil {
		return store.DispatchRecipient{}, err
	}
	now := s.now()
	var out store.DispatchRecipient
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		r, err := store.RecipientByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if r == nil || !a.allows(r.Province, r.Department) {
			return ErrRecipientNotFound
		}
		before, _ := json.Marshal(map[string]any{"label": r.Label, "kind": r.Kind, "is_default": r.IsDefault, "active": r.Active})
		r.Active, r.IsDefault, r.UpdatedAt = false, false, now
		if err := store.UpdateRecipient(ctx, tx, *r); err != nil {
			return err
		}
		after, _ := json.Marshal(map[string]any{"label": r.Label, "kind": r.Kind, "is_default": false, "active": false})
		out = *r
		return store.InsertRecipientEvent(ctx, tx, a.p.TenantID, store.RecipientEvent{ID: ids.New(), RecipientID: r.ID, Action: "deactivated",
			Department: r.Department, Province: r.Province, Before: before, After: after, Reason: rsn, EventTime: now, ActorRef: a.p.ActorRef,
			IdemKey: idemOr(idem, "recipient")})
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Xuất tệp
// ---------------------------------------------------------------------------

// DispatchInput là yêu cầu "Xuất file".
type DispatchInput struct {
	PeriodID  uuid.UUID
	FormCodes []string // rỗng = mọi biểu đã tổng hợp của kỳ
	Formats   []string // xlsx | pdf | gsheet
}

// DispatchView là lần xuất/gửi kèm tệp và nhật ký.
type DispatchView struct {
	Dispatch store.Dispatch
	Files    []store.DispatchFile
	Events   []store.DispatchEvent
}

// gridRows sắp dòng xã theo khu vực, sau mỗi khu vực là dòng "Cộng khu vực", cuối là "Toàn tỉnh" (đúng mẫu).
func gridRows(v port.ConsolidationView) []render.GridRow {
	var regions []string
	byRegion := map[string][]port.ConsolidationRow{}
	for _, r := range v.Rows {
		if _, ok := byRegion[r.Region]; !ok {
			regions = append(regions, r.Region)
		}
		byRegion[r.Region] = append(byRegion[r.Region], r)
	}
	sort.SliceStable(regions, func(i, j int) bool {
		if (regions[i] == "") != (regions[j] == "") {
			return regions[j] == ""
		}
		return regions[i] < regions[j]
	})
	var out []render.GridRow
	for _, reg := range regions {
		for _, r := range byRegion[reg] {
			out = append(out, render.GridRow{Kind: render.GridCommune, Cells: r.Cells})
		}
		for _, t := range v.Totals {
			if t.Kind == port.TotalRegion && t.Region == reg {
				out = append(out, render.GridRow{Kind: render.GridRegion, Cells: t.Cells})
			}
		}
	}
	for _, t := range v.Totals {
		if t.Kind == port.TotalProvince {
			out = append(out, render.GridRow{Kind: render.GridProvince, Cells: t.Cells})
		}
	}
	return out
}

// CreateDispatch dựng tệp toàn tỉnh của kỳ từ lần tổng hợp gần nhất ("số trong file = số tổng hợp của kỳ").
func (s *Service) CreateDispatch(ctx context.Context, in DispatchInput, idem string) (DispatchView, error) {
	a, err := s.resolveSessionActor(ctx, true)
	if err != nil {
		return DispatchView{}, err
	}
	profile, err := s.dispatchProfile()
	if err != nil {
		return DispatchView{}, err
	}
	if s.Consolidations == nil {
		return DispatchView{}, ErrDependency.WithDetail("chưa nối lần tổng hợp (pf/extension)")
	}
	if s.Store == nil {
		return DispatchView{}, ErrDependency.WithDetail("chưa cấu hình kho đối tượng")
	}
	if len(in.Formats) == 0 {
		in.Formats = []string{dispatch.FormatXLSX}
	}
	for _, f := range in.Formats {
		if !slices.Contains(dispatch.Formats, f) {
			return DispatchView{}, problem.Validation.WithField("formats", "xlsx | pdf | gsheet")
		}
	}
	if slices.Contains(in.Formats, dispatch.FormatGSheet) {
		if !s.GSheet.Enabled {
			return DispatchView{}, ErrGSheetDisabled
		}
		if s.GSheetPub == nil {
			return DispatchView{}, ErrDependency.WithDetail("chưa kết nối tài khoản Google của TTKN tỉnh")
		}
	}
	tid := a.p.TenantID
	info, err := s.Consolidations.Period(ctx, tid, in.PeriodID)
	if errors.Is(err, port.ErrConsolidationNotFound) {
		return DispatchView{}, ErrDispatchNoData
	}
	if err != nil {
		return DispatchView{}, err
	}
	if !a.allows(info.ProvinceCode, info.DepartmentCode) {
		return DispatchView{}, ErrOutOfScope
	}
	views, err := s.Consolidations.LatestRuns(ctx, tid, in.PeriodID, in.FormCodes)
	if err != nil {
		return DispatchView{}, err
	}
	if len(views) == 0 {
		return DispatchView{}, ErrDispatchNoData
	}
	now := s.now()
	d := store.Dispatch{ID: ids.New(), Department: info.DepartmentCode, Province: info.ProvinceCode, PeriodID: info.ID, PeriodLabel: info.Label,
		Formats: in.Formats, Status: dispatch.StatusExported, CreatedBy: a.p.ActorRef, CreatedAt: now, UpdatedAt: now}
	type built struct {
		f    store.DispatchFile
		data []byte
	}
	var files []built
	var gsheets []dispatch.Sheet
	for _, v := range views {
		d.RunIDs = append(d.RunIDs, v.RunID)
		d.Forms = append(d.Forms, v.FormCode)
		if v.Provisional {
			d.Provisional = true
		}
		for _, m := range v.MissingNames {
			if !slices.Contains(d.Missing, m) {
				d.Missing = append(d.Missing, m)
			}
		}
		fv, err := Forms{Svc: s}.FormVersion(ctx, tid, v.VersionID)
		if err != nil {
			return DispatchView{}, err
		}
		def := form.Layout(fv.Definition, form.LayoutOptions{PeriodLabels: info.SlotLabels, Season: strings.ToUpper(info.SeasonLabel)})
		rows := gridRows(v)
		status := "Đã chốt"
		if v.Provisional {
			status = "Tạm tính"
		}
		meta := render.GridMeta{Title: "BÁO CÁO TỔNG HỢP CÁC XÃ — " + v.FormCode + " " + fv.Name, Provisional: v.Provisional, Missing: v.MissingNames, At: now,
			Lines: []string{"Kỳ: " + info.Label + " (" + info.RangeLabel + ")", "Vụ/đợt: " + info.SeasonLabel, "Trạng thái: " + status,
				"Lần cập nhật: " + v.At.In(clock.Zone()).Format("15:04 02/01/2006")}}
		for _, f := range in.Formats {
			var data []byte
			var ext string
			switch f {
			case dispatch.FormatXLSX:
				ext = "xlsx"
				if data, err = render.FormGridXLSX(def, rows, meta); err != nil {
					return DispatchView{}, err
				}
			case dispatch.FormatPDF:
				ext = "pdf"
				if data, err = render.PDF(render.GridDataset(def, rows, meta), now, render.PDFOptions{}); err != nil {
					return DispatchView{}, err
				}
			case dispatch.FormatGSheet:
				cells := make([]map[string]string, 0, len(rows))
				for _, r := range rows {
					cells = append(cells, r.Cells)
				}
				sh, err := dispatch.Valve(def, cells)
				if err != nil {
					return DispatchView{}, ErrValveBlocked.WithDetail(err.Error())
				}
				gsheets = append(gsheets, sh)
				continue
			}
			sum := sha256.Sum256(data)
			files = append(files, built{f: store.DispatchFile{ID: ids.New(), DispatchID: d.ID, Format: f, FormCode: v.FormCode,
				FileName: dispatch.FileName(v.FormCode, info.Label, ext), SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(data)), CreatedAt: now}, data: data})
		}
	}
	for i := range files {
		files[i].f.ObjectKey = "reports/" + tid.String() + "/dispatch/" + d.ID.String() + "/" + files[i].f.ID.String() + "." + files[i].f.Format
		ct := "application/pdf"
		if files[i].f.Format == dispatch.FormatXLSX {
			ct = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		}
		if _, err := s.Store.Put(ctx, files[i].f.ObjectKey, bytes.NewReader(files[i].data), files[i].f.SizeBytes, files[i].f.SHA256, ct); err != nil {
			return DispatchView{}, err
		}
	}
	var links []string
	if len(gsheets) > 0 {
		u, err := s.GSheetPub.Publish(ctx, "AgriGreen "+info.Label, gsheets, s.GSheet.ShareMode, nil)
		if err != nil {
			return DispatchView{}, err
		}
		links = append(links, u)
	}
	err = s.tx(ctx, tid, func(tx pgx.Tx) error {
		if err := store.InsertDispatch(ctx, tx, tid, d); err != nil {
			return err
		}
		for _, b := range files {
			if err := s.egress(ctx, tx, Egress{Kind: EgressDispatch, TenantID: tid, ActorRef: a.p.ActorRef, TemplateKey: dispatchFileKey(profile, b.f.FormCode),
				Format: b.f.Format, JobID: d.ID}); err != nil {
				return err
			}
			if err := store.InsertDispatchFile(ctx, tx, tid, b.f); err != nil {
				return err
			}
		}
		for _, u := range links {
			if err := s.egress(ctx, tx, Egress{Kind: EgressDispatch, TenantID: tid, ActorRef: a.p.ActorRef, TemplateKey: profile.SheetTemplateKey, Format: "gsheet", JobID: d.ID}); err != nil {
				return err
			}
			if err := store.InsertGSheetLink(ctx, tx, tid, ids.New(), d.ID, strings.Join(d.Forms, ","), u, s.GSheet.ShareMode, now); err != nil {
				return err
			}
		}
		fl := make([]map[string]any, 0, len(files))
		for _, b := range files {
			fl = append(fl, map[string]any{"file_id": b.f.ID, "form_code": b.f.FormCode, "format": b.f.Format, "sha256": b.f.SHA256, "size_bytes": b.f.SizeBytes})
		}
		payload, _ := json.Marshal(map[string]any{"period_id": d.PeriodID, "period_label": d.PeriodLabel, "run_ids": d.RunIDs, "forms": d.Forms,
			"files": fl, "gsheet_links": len(links), "provisional": d.Provisional})
		ev := store.DispatchEvent{ID: ids.New(), DispatchID: d.ID, Action: "exported", Payload: payload, Department: d.Department, Province: d.Province,
			OnBehalfOf: a.onBehalf(), EventTime: now, ActorRef: a.p.ActorRef, IdemKey: idemOr(idem, "export")}
		return store.InsertDispatchEvent(ctx, tx, tid, &ev)
	})
	if err != nil {
		return DispatchView{}, err
	}
	return s.GetDispatch(ctx, d.ID)
}

// GetDispatch trả lần xuất/gửi kèm tệp và nhật ký.
func (s *Service) GetDispatch(ctx context.Context, id uuid.UUID) (DispatchView, error) {
	a, err := s.resolveSessionActor(ctx, false)
	if err != nil {
		return DispatchView{}, err
	}
	var v DispatchView
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		d, err := store.DispatchByID(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if d == nil || !a.allows(d.Province, d.Department) {
			return ErrDispatchNotFound
		}
		v.Dispatch = *d
		if v.Files, err = store.DispatchFiles(ctx, tx, id); err != nil {
			return err
		}
		v.Events, err = store.DispatchEvents(ctx, tx, id)
		return err
	})
	return v, err
}

// ListDispatches trả lịch sử xuất/gửi.
func (s *Service) ListDispatches(ctx context.Context, province string, periodID, before *uuid.UUID, limit int) ([]store.Dispatch, error) {
	a, err := s.resolveSessionActor(ctx, false)
	if err != nil {
		return nil, err
	}
	if a.province != "" {
		province = a.province
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []store.Dispatch
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var err error
		out, err = store.Dispatches(ctx, tx, province, a.dept(), periodID, before, limit)
		return err
	})
	return out, err
}

// DispatchFileURL cấp URL tải một tệp ("chỉ tải file, không gửi") — qua điểm kiểm soát (download).
func (s *Service) DispatchFileURL(ctx context.Context, dispatchID, fileID uuid.UUID) (string, error) {
	a, err := s.resolveSessionActor(ctx, false)
	if err != nil {
		return "", err
	}
	if s.Store == nil {
		return "", ErrDependency.WithDetail("chưa cấu hình kho đối tượng")
	}
	var key string
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		d, err := store.DispatchByID(ctx, tx, dispatchID, false)
		if err != nil {
			return err
		}
		if d == nil || !a.allows(d.Province, d.Department) {
			return ErrDispatchNotFound
		}
		fs, err := store.DispatchFiles(ctx, tx, dispatchID)
		if err != nil {
			return err
		}
		for _, f := range fs {
			if f.ID == fileID {
				key = f.ObjectKey
				profile, err := s.dispatchProfile()
				if err != nil {
					return err
				}
				return s.egress(ctx, tx, Egress{Kind: EgressDownload, TenantID: a.p.TenantID, ActorRef: a.p.ActorRef, TemplateKey: dispatchFileKey(profile, f.FormCode),
					Format: f.Format, JobID: dispatchID})
			}
		}
		return ErrDispatchNotFound
	})
	if err != nil {
		return "", err
	}
	return s.Store.PresignGet(ctx, key, DownloadTTL)
}

// SendInput là yêu cầu "Gửi email".
type SendInput struct {
	RecipientIDs []uuid.UUID // rỗng = người nhận mặc định (lần trước)
	CCIDs        []uuid.UUID
	Subject      string
	Body         string
}

// SendDispatch xếp gửi email (worker gửi SMTP, 202). Gửi lại khi "Gửi lỗi" hoặc đã gửi.
func (s *Service) SendDispatch(ctx context.Context, id uuid.UUID, in SendInput, idem string) (DispatchView, error) {
	a, err := s.resolveSessionActor(ctx, true)
	if err != nil {
		return DispatchView{}, err
	}
	now := s.now()
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		d, err := store.DispatchByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if d == nil || !a.allows(d.Province, d.Department) {
			return ErrDispatchNotFound
		}
		// P0-WORKER-REPORTS (#128): "Đang gửi" chỉ chặn khi còn trong hạn nhận việc (worker đang giữ, hoặc sự kiện mới xếp
		// chưa ai nhận). Quá hạn (worker chết giữa chừng / sự kiện vào DLQ) → TIẾP TỤC CÙNG LƯỢT: giữ số lượt để dấu
		// recipient_mailed của người đã nhận vẫn khớp — không gửi trùng.
		resume := false
		// Review #128: "Gửi lại" sau "Gửi lỗi" là lượt mới (attempts+1) nhưng TIẾP TỤC VÒNG GỬI cũ — dấu người đã nhận
		// (khóa theo vòng, không theo lượt) vẫn giữ nên người đã nhận không nhận lần hai. Vòng mới chỉ bắt đầu sau "sent".
		continuesRound := d.Status == dispatch.StatusFailed
		if d.Status == dispatch.StatusSending {
			if !s.sendingStale(*d, now) {
				return ErrDispatchSending
			}
			resume = true
		}
		all, err := store.Recipients(ctx, tx, d.Province, d.Department, true)
		if err != nil {
			return err
		}
		byID := map[uuid.UUID]store.DispatchRecipient{}
		for _, r := range all {
			byID[r.ID] = r
		}
		to, cc := in.RecipientIDs, in.CCIDs
		if len(to) == 0 && len(cc) == 0 {
			for _, r := range all {
				if r.IsDefault && r.Kind == dispatch.KindTo {
					to = append(to, r.ID)
				} else if r.IsDefault && r.Kind == dispatch.KindCC {
					cc = append(cc, r.ID)
				}
			}
		}
		if len(to) == 0 {
			return ErrNoRecipients
		}
		for _, x := range append(slices.Clone(to), cc...) {
			if _, ok := byID[x]; !ok {
				return ErrRecipientNotFound.WithField("recipient_ids", x.String())
			}
		}
		profile, err := s.dispatchProfile()
		if err != nil {
			return err
		}
		subj := strings.TrimSpace(in.Subject)
		if subj == "" {
			subj = dispatch.Subject(profile.SubjectDepartment, d.PeriodLabel, strings.Join(d.Forms, ", "), d.Provisional)
		}
		if strings.ContainsAny(subj, "\r\n") || len([]rune(subj)) > 300 {
			return problem.Validation.WithField("subject", "tối đa 300 ký tự, không xuống dòng")
		}
		body := strings.TrimSpace(in.Body)
		if len([]rune(body)) > 5000 {
			return problem.Validation.WithField("body", "tối đa 5000 ký tự")
		}
		attempt := d.Attempts + 1
		if resume {
			attempt = d.Attempts
		}
		d.Status, d.Recipients, d.CC, d.Subject, d.Attempts, d.LastError, d.UpdatedAt = dispatch.StatusSending, to, cc, &subj, attempt, nil, now
		d.ClaimUntil = nil // UpdateDispatchSend nhả việc quá hạn — lượt mới xếp khóa tới updated_at + hạn.
		if body != "" {
			d.Body = &body
		} else {
			d.Body = nil
		}
		if err := store.UpdateDispatchSend(ctx, tx, *d); err != nil {
			return err
		}
		for _, x := range append(slices.Clone(to), cc...) {
			r := byID[x]
			r.LastUsedAt, r.UpdatedAt = &now, now
			// "email Chi cục lưu làm mặc định": người nhận vừa dùng thành mặc định.
			r.IsDefault = true
			if err := store.UpdateRecipient(ctx, tx, r); err != nil {
				return err
			}
		}
		payload, _ := json.Marshal(map[string]any{"recipient_ids": to, "cc_ids": cc, "attempt": d.Attempts, "subject": subj, "resumed": resume,
			"continues_round": continuesRound})
		ev := store.DispatchEvent{ID: ids.New(), DispatchID: d.ID, Action: "send_requested", Payload: payload, Department: d.Department, Province: d.Province,
			OnBehalfOf: a.onBehalf(), EventTime: now, ActorRef: a.p.ActorRef, IdemKey: idemOr(idem, "send")}
		if err := store.InsertDispatchEvent(ctx, tx, a.p.TenantID, &ev); err != nil {
			return err
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: a.p.TenantID, Type: port.EventDispatchRequested, SchemaVersion: 1, OccurredAt: now,
			Producer: port.Producer, Payload: port.DispatchRequestedV1{DispatchID: d.ID.String(), Attempt: d.Attempts, RequestedBy: a.p.ActorRef.String(),
				RequestedAt: ts(now)}})
	})
	if err != nil {
		return DispatchView{}, err
	}
	return s.GetDispatch(ctx, id)
}

// HandleDispatchRequested là inbox.Handler của consumer port.DispatchConsumer: gửi email Chi cục kèm tệp (hoặc chỉ
// liên kết khi vượt giới hạn). Lỗi SMTP không làm hỏng consumer (ghi "Gửi lỗi" để người dùng gửi lại).
//
// P0-WORKER-REPORTS (#128) — gửi "đúng một lần" trong thực tế:
//  1. NHẬN VIỆC: một câu UPDATE … WHERE status='sending' AND attempts=$n AND (chưa ai giữ hoặc hạn giữ đã qua), giao
//     dịch riêng commit TRƯỚC khi gửi — worker thứ hai (giao lặp, nhiều bản worker, "Gửi lại" sau hạn) không gửi song song.
//  2. Mỗi người nhận: gia hạn việc + đọc mới dấu recipient_mailed (giao dịch riêng) → mất quyền giữ thì dừng, đã có dấu
//     thì bỏ qua; gửi; ghi dấu ngay (giao dịch riêng, không theo ctx bị hủy).
//  3. Kết quả sent/failed ghi có điều kiện còn giữ đúng token rồi nhả việc (giao dịch riêng): giao dịch inbox hủy sau đó
//     → giao lại thấy "sent" → không gửi lại.
//
// P0-POOL-SHARED-2 (#143): toàn bộ lượt gửi (đọc, nhận việc, đọc kho, SMTP, ghi dấu, ghi kết quả — mỗi bước một giao
// dịch ngắn riêng) chạy NGOÀI giao dịch inbox qua txdeps.Get: inbox.Process hủy giao dịch inbox, chạy lượt gửi khi KHÔNG
// giữ kết nối nào, rồi chạy lại handler — lần này chỉ trả kết quả đã nhớ và commit dòng inbox. Trước đây giao dịch inbox
// mở suốt lượt gửi (giữ một kết nối trong khi chờ kết nối thứ hai cho giao dịch riêng, và trong lúc SMTP/kho chạy).
func (s *Service) HandleDispatchRequested(ctx context.Context, _ pgx.Tx, env outbox.Envelope) error {
	var p port.DispatchRequestedV1
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return err
	}
	id, err := uuid.Parse(p.DispatchID)
	if err != nil {
		return err
	}
	if s.DB == nil {
		return ErrDependency.WithDetail("chưa nối pool DB của pf/reports (worker)")
	}
	_, err = txdeps.Get(ctx, "reports.dispatch:"+env.EventID.String(), func(ctx context.Context) (struct{}, error) {
		return struct{}{}, s.sendDispatch(ctx, env, p, id)
	})
	return err
}

// sendDispatch là một lượt gửi email Chi cục (ngoài giao dịch inbox; mỗi bước DB là giao dịch ngắn riêng).
func (s *Service) sendDispatch(ctx context.Context, env outbox.Envelope, p port.DispatchRequestedV1, id uuid.UUID) error {
	tid := env.TenantID
	var d *store.Dispatch
	if err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		d, err = store.DispatchByID(ctx, tx, id, false)
		return err
	}); err != nil || d == nil {
		return err
	}
	if d.Status != dispatch.StatusSending || d.Attempts != p.Attempt {
		return nil // đã xử lý hoặc đã có lần gửi lại mới hơn
	}
	profile, err := s.dispatchProfile()
	if err != nil {
		return err
	}
	now := s.now()
	lease := s.sendLease()
	token := ids.New()
	var claimed bool
	round, sentAttempt := 0, 0
	var all []store.DispatchRecipient
	var files []store.DispatchFile
	if err := s.tx(ctx, tid, func(claimTx pgx.Tx) error {
		var err error
		claimed, err = store.ClaimDispatch(ctx, claimTx, d.ID, d.Attempts, token, now.Add(lease), now)
		if err != nil || !claimed {
			return err
		}
		// Đọc lại sau khi nhận: "Gửi lại" tiếp tục lượt có thể vừa đổi người nhận/tiêu đề.
		fresh, err := store.DispatchByID(ctx, claimTx, id, false)
		if err != nil {
			return err
		}
		if fresh != nil {
			d = fresh
		}
		// Vòng gửi = số lần đã "sent" (dòng sent chỉ ghi khi còn giữ việc): dấu người nhận khóa theo vòng (review #128).
		if round, err = store.CountDispatchEvents(ctx, claimTx, d.ID, "sent"); err != nil {
			return err
		}
		// Lượt của vòng gần nhất đã "sent": dấu dạng cũ chỉ có giá trị cho các lượt SAU đó (vòng chưa hoàn tất).
		if sentAttempt, err = store.LastSentAttempt(ctx, claimTx, d.ID); err != nil {
			return err
		}
		// Danh bạ và tệp đã xuất đọc cùng giao dịch nhận việc (trước đây đọc ở giao dịch inbox).
		if all, err = store.Recipients(ctx, claimTx, d.Province, d.Department, false); err != nil {
			return err
		}
		files, err = store.DispatchFiles(ctx, claimTx, d.ID)
		return err
	}); err != nil {
		return err
	}
	if !claimed {
		// Worker khác đang giữ lượt này (hoặc lượt vừa xong): không gửi song song. Worker đó chết → hết hạn giữ → "Gửi lại".
		s.log().Info("lượt gửi báo cáo Chi cục đang do worker khác giữ", "event", "reports.dispatch.claim_busy", "dispatch_id", d.ID,
			"attempt", d.Attempts, "outcome", "skipped")
		return nil
	}
	// finish ghi kết quả + sổ trong giao dịch riêng, chỉ khi còn giữ việc; ctx hủy (dừng worker) không bỏ dở ghi kết quả.
	finish := func(res store.Dispatch, ev store.DispatchEvent) error {
		fctx := context.WithoutCancel(ctx)
		return s.tx(fctx, tid, func(ftx pgx.Tx) error {
			ok, err := store.FinishDispatchClaim(fctx, ftx, res, token)
			if err != nil || !ok {
				return err // !ok: đã mất quyền giữ — worker nhận lại sẽ ghi kết quả
			}
			return store.InsertDispatchEvent(fctx, ftx, tid, &ev)
		})
	}
	fail := func(msg string) error {
		res := *d
		res.Status, res.LastError, res.UpdatedAt = dispatch.StatusFailed, &msg, now
		payload, _ := json.Marshal(map[string]any{"attempt": d.Attempts, "error": msg})
		ev := store.DispatchEvent{ID: ids.New(), DispatchID: d.ID, Action: "failed", Payload: payload, Department: d.Department, Province: d.Province,
			EventTime: now, ActorRef: d.CreatedBy, IdemKey: "dispatch-failed:" + env.EventID.String()}
		s.log().Warn("gửi báo cáo Chi cục lỗi", "event", "reports.dispatch.failed", "dispatch_id", d.ID, "outcome", "error")
		return finish(res, ev)
	}
	if s.Mailer == nil {
		return fail("chưa cấu hình SMTP công vụ (AG_SMTP_*)")
	}
	addr := map[uuid.UUID]string{}
	for _, r := range all {
		if r.Address != nil && r.Active {
			addr[r.ID] = *r.Address
		}
	}
	var cc []string
	for _, x := range d.CC {
		if a, ok := addr[x]; ok {
			cc = append(cc, a)
		}
	}
	limitMB, err := s.attachmentLimit(ctx, now)
	if err != nil {
		return err
	}
	var total int64
	for _, f := range files {
		total += f.SizeBytes
	}
	linkOnly := total > int64(limitMB)*1024*1024
	var atts []port.Attachment
	ttl := 0
	if linkOnly {
		// A4: liên kết do api-platform phục vụ (hạn reports.dispatch_link_ttl_hours, ghi sổ truy cập) thay URL ký
		// sẵn 15 phút của kho đối tượng; mỗi người nhận To một bộ liên kết (cấp trong vòng gửi bên dưới).
		if strings.TrimSpace(s.LinkDownloadBase) == "" {
			return fail("chưa cấu hình URL tải liên kết (AG_REPORT_LINK_BASE)")
		}
		if ttl, err = s.linkTTL(ctx, now); err != nil {
			return err
		}
	}
	// Đính kèm chỉ khi Σ kích thước (siêu dữ liệu) ≤ giới hạn; đọc kho cũng giới hạn cùng mức — đối tượng lớn hơn
	// siêu dữ liệu ghi (thay ngoài luồng, hỏng) không được nạp không giới hạn vào bộ nhớ worker.
	attachMax := int64(limitMB) * 1024 * 1024
	for _, f := range files {
		if linkOnly {
			continue
		}
		if s.Store == nil {
			return fail("chưa cấu hình kho đối tượng")
		}
		rc, _, err := s.Store.Get(ctx, f.ObjectKey)
		if err != nil {
			return fail("không đọc được tệp đã xuất")
		}
		data, err := io.ReadAll(io.LimitReader(rc, attachMax+1))
		_ = rc.Close()
		if err != nil {
			return fail("không đọc được tệp đã xuất")
		}
		if int64(len(data)) > attachMax {
			return fail("tệp đã xuất lớn hơn kích thước đã ghi (vượt giới hạn đính kèm)")
		}
		ct := "application/pdf"
		if f.Format == dispatch.FormatXLSX {
			ct = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		}
		atts = append(atts, port.Attachment{Name: f.FileName, ContentType: ct, Data: data})
	}
	bodyFor := func(links []string) string {
		body := ""
		if d.Body != nil {
			body = *d.Body
		}
		if body == "" {
			return dispatch.BodyFor(profile, d.PeriodLabel, "", d.Forms, d.Provisional, d.Missing, links)
		}
		if len(links) > 0 {
			body += "\n\n" + dispatch.BodyFor(profile, d.PeriodLabel, "", d.Forms, false, nil, links)
		}
		return body
	}
	subj := ""
	if d.Subject != nil {
		subj = *d.Subject
	}
	// Email là tác dụng phụ không hoàn tác được: liên kết tải và dấu "đã gửi" của từng người nhận ghi ở GIAO DỊCH RIÊNG,
	// commit quanh mỗi lần gửi — giao lại / worker nhận lại bỏ qua người đã nhận, liên kết trong email đã gửi vẫn mở được.
	sent := 0
	for _, x := range d.Recipients {
		to, ok := addr[x]
		if !ok {
			continue
		}
		key := mailedKey(d.ID, round, x)
		held, mailed := false, false
		if err := s.tx(ctx, tid, func(rtx pgx.Tx) error {
			var err error
			if held, err = store.RenewDispatchClaim(ctx, rtx, d.ID, token, s.now().Add(lease)); err != nil || !held {
				return err
			}
			if mailed, err = store.HasDispatchEvent(ctx, rtx, d.ID, key); err != nil || mailed {
				return err
			}
			// Dấu dạng cũ (theo lượt, trước review #128) của MỌI lượt thuộc vòng chưa hoàn tất (sau lượt "sent" gần nhất, tới
			// lượt đang chạy): lượt lỗi một phần trước nâng cấp rồi "Gửi lại" (lượt mới) vẫn không gửi trùng (review Codex
			// #128 @59a4ec8f). Dấu của lượt ≤ lượt đã "sent" thuộc vòng cũ — vòng mới chủ động vẫn gửi mọi người.
			mailed, err = store.HasAnyDispatchEvent(ctx, rtx, d.ID, legacyMailedKeys(d.ID, sentAttempt, d.Attempts, x))
			return err
		}); err != nil {
			return err
		}
		// Hạn của lời gọi SMTP tính từ lúc vừa gia hạn (giờ thực — deadline của ctx là giờ thực), ngắn hơn hạn nhận việc
		// một biên an toàn: SMTP treo bị cắt TRƯỚC khi worker khác được nhận lại (review #128), còn kịp ghi dấu.
		sendBy := time.Now().Add(lease - dispatch.SendMargin(lease))
		if !held {
			s.log().Warn("mất quyền giữ lượt gửi báo cáo Chi cục (quá hạn, worker khác đã nhận) — dừng", "event", "reports.dispatch.claim_lost",
				"dispatch_id", d.ID, "attempt", d.Attempts, "outcome", "skipped")
			return nil
		}
		if mailed {
			sent++
			continue
		}
		var links []string
		if linkOnly {
			if err := s.tx(ctx, tid, func(ltx pgx.Tx) error {
				links = links[:0]
				for _, f := range files {
					l, err := s.issueLink(ctx, ltx, tid, *d, f, x, ttl, now)
					if err != nil {
						return err
					}
					links = append(links, l)
				}
				return nil
			}); err != nil {
				return err
			}
		}
		sctx, cancel := context.WithDeadline(ctx, sendBy)
		err := s.Mailer.Send(sctx, port.Message{To: to, CC: cc, Subject: subj, Text: bodyFor(links), Attachments: atts})
		timedOut := errors.Is(sctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
		cancel()
		if err != nil {
			if timedOut {
				s.log().Warn("SMTP quá hạn của lượt gửi báo cáo Chi cục — cắt trước khi hết hạn nhận việc", "event", "reports.dispatch.send_timeout",
					"dispatch_id", d.ID, "attempt", d.Attempts, "outcome", "error")
				return fail("máy chủ thư không trả lời kịp (quá hạn gửi)")
			}
			return fail("máy chủ thư từ chối hoặc không kết nối được")
		}
		sent++
		payload, _ := json.Marshal(map[string]any{"attempt": d.Attempts, "recipient_id": x, "links": len(links)})
		ev := store.DispatchEvent{ID: ids.New(), DispatchID: d.ID, Action: ActionRecipientMailed, Payload: payload, Department: d.Department,
			Province: d.Province, EventTime: now, ActorRef: d.CreatedBy, IdemKey: key}
		// Email đã đi: ghi dấu kể cả khi ctx vừa bị hủy (dừng worker / mất kết nối broker).
		mctx := context.WithoutCancel(ctx)
		if err := s.tx(mctx, tid, func(mtx pgx.Tx) error { return store.InsertDispatchEvent(mctx, mtx, tid, &ev) }); isUniqueViolation(err) {
			// Worker khác đã gửi và ghi dấu cùng người nhận (SMTP của worker này sống quá hạn nhận việc): dấu đã có — không
			// lỗi (NACK/giao lại không gỡ được email đã đi), nhưng ghi nhận lần gửi trùng để vận hành đếm.
			s.log().Error("gửi trùng email Chi cục tới người đã có dấu (worker khác đã nhận lại lượt)", "event", "reports.dispatch.duplicate_send",
				"dispatch_id", d.ID, "attempt", d.Attempts, "recipient_ref", x, "outcome", "duplicate")
		} else if err != nil {
			// Email đã đi nhưng chưa ghi được dấu: lần nhận lại có thể gửi trùng người nhận này.
			s.log().Error("đã gửi email Chi cục nhưng không ghi được dấu người nhận", "event", "reports.dispatch.mark_failed",
				"dispatch_id", d.ID, "recipient_ref", x, "outcome", "error", "error", err.Error())
			return err
		}
	}
	if sent == 0 {
		return fail("không còn người nhận đang dùng")
	}
	res := *d
	res.Status, res.SentAt, res.LinkOnly, res.UpdatedAt = dispatch.StatusSent, &now, linkOnly, now
	payload, _ := json.Marshal(map[string]any{"attempt": d.Attempts, "recipients": sent, "cc": len(cc), "attachments": len(atts), "link_only": linkOnly})
	ev := store.DispatchEvent{ID: ids.New(), DispatchID: d.ID, Action: "sent", Payload: payload, Department: d.Department, Province: d.Province,
		EventTime: now, ActorRef: d.CreatedBy, IdemKey: "dispatch-sent:" + env.EventID.String()}
	if err := finish(res, ev); err != nil {
		return err
	}
	s.log().Info("đã gửi báo cáo Chi cục", "event", "reports.dispatch.sent", "dispatch_id", d.ID, "recipients", sent, "link_only", linkOnly, "outcome", "ok")
	return nil
}

// sendLease là hạn nhận việc gửi (P0-WORKER-REPORTS).
func (s *Service) sendLease() time.Duration {
	if s.DispatchLease > 0 {
		return s.DispatchLease
	}
	return dispatch.DefaultSendLease
}

// sendingStale: lần gửi "Đang gửi" đã quá hạn nhận việc — worker giữ đã quá hạn, hoặc chưa ai nhận sau một hạn kể từ
// lúc xếp gửi (sự kiện mất/vào DLQ).
func (s *Service) sendingStale(d store.Dispatch, now time.Time) bool {
	return !s.resumableAt(d).After(now)
}

// resumableAt là thời điểm lần gửi "Đang gửi" bắt đầu được "Gửi lại" (tiếp tục cùng lượt): hạn nhận việc của worker đang
// giữ, hoặc updated_at + hạn khi chưa ai nhận. Cùng một nguồn cho SendDispatch (409 reports.dispatch_sending) và DTO đọc.
func (s *Service) resumableAt(d store.Dispatch) time.Time {
	if d.ClaimUntil != nil {
		return *d.ClaimUntil
	}
	return d.UpdatedAt.Add(s.sendLease())
}

// ResendState là trạng thái nút "Gửi email/Gửi lại" tính tại máy chủ lúc đọc (review #128): CanSend = POST /send hiện
// được chấp nhận (không 409 reports.dispatch_sending); ResumableAt chỉ có khi "Đang gửi" — mốc từ đó được tiếp tục.
type ResendState struct {
	CanSend     bool
	ResumableAt *time.Time
}

// DispatchResendState tính ResendState của một lần xuất/gửi theo đồng hồ dịch vụ, cùng điều kiện với SendDispatch.
func (s *Service) DispatchResendState(d store.Dispatch) ResendState {
	if d.Status != dispatch.StatusSending {
		return ResendState{CanSend: true}
	}
	at := s.resumableAt(d)
	return ResendState{CanSend: !at.After(s.now()), ResumableAt: &at}
}

// ActionRecipientMailed là dòng sổ dispatch_event "đã gửi email tới một người nhận" của một lượt gửi (migration 000086).
const ActionRecipientMailed = "recipient_mailed"

// mailedKey là idem_key của dấu đã gửi: theo (lần gửi, VÒNG gửi, người nhận). Vòng = số lần đã "sent" — các lượt "Gửi
// lại" sau "Gửi lỗi"/quá hạn cùng vòng giữ dấu (không gửi lại người đã nhận); "Gửi lại" sau "sent" là vòng mới (review #128).
func mailedKey(dispatchID uuid.UUID, round int, recipient uuid.UUID) string {
	return fmt.Sprintf("dispatch-mailed:%s:r%d:%s", dispatchID, round, recipient)
}

// legacyMailedKey là dạng khóa cũ (theo lượt attempts) — chỉ đọc, cho lượt dở dang ghi trước review #128.
func legacyMailedKey(dispatchID uuid.UUID, attempt int, recipient uuid.UUID) string {
	return fmt.Sprintf("dispatch-mailed:%s:%d:%s", dispatchID, attempt, recipient)
}

// legacyMailedKeys: khóa dạng cũ của các lượt (afterAttempt, upTo] — các lượt của vòng gửi chưa hoàn tất.
func legacyMailedKeys(dispatchID uuid.UUID, afterAttempt, upTo int, recipient uuid.UUID) []string {
	var keys []string
	for a := max(afterAttempt+1, 1); a <= upTo; a++ {
		keys = append(keys, legacyMailedKey(dispatchID, a, recipient))
	}
	return keys
}

// isUniqueViolation: lỗi trùng khóa (23505) — idem sổ đã có dòng.
func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

// DispatchHistory hiện thực extension/port.DispatchHistory (kỳ đã gửi Chi cục → cảnh báo khi mở lại kỳ).
type DispatchHistory struct{ Svc *Service }

// SentForPeriod cho biết đã gửi thành công báo cáo Chi cục của kỳ.
func (h DispatchHistory) SentForPeriod(ctx context.Context, tid, periodID uuid.UUID) (bool, error) {
	var ok bool
	err := h.Svc.tx(ctx, tid, func(tx pgx.Tx) error {
		var err error
		ok, err = store.SentForPeriod(ctx, tx, periodID)
		return err
	})
	return ok, err
}

var _ port.DispatchHistory = DispatchHistory{}

func decimalInt(n int64) decimal.Decimal { return decimal.NewFromInt(n) }
