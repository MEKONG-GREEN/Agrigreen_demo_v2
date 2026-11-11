package app

// Log (khung Log chuẩn: hiệu giữa hai ảnh chụp liên tiếp) và xuất tệp GeoJSON/KML một xã của thửa ruộng KN.

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knplot/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

func rawJSON(s string) json.RawMessage { return json.RawMessage(s) }

// LogChange là một trường đổi (cũ → mới; tên nông dân hiện bằng farmer_ref, không PII).
type LogChange struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// LogItem là một dòng Log.
type LogItem struct {
	ID           uuid.UUID
	Event        string
	ActorRef     uuid.UUID
	ActorDisplay *string
	At           time.Time
	Changes      []LogChange
	Reason       *string
}

// LogPage là một trang Log (mới nhất trước).
type LogPage struct {
	Items []LogItem
	Next  *uuid.UUID
}

func snapshot(v store.Version) [][2]any {
	str := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	ref := func(u *uuid.UUID) any {
		if u == nil {
			return nil
		}
		return u.String()
	}
	var dec any
	if v.DeclaredAreaHa != nil {
		dec = v.DeclaredAreaHa.StringFixed(4)
	}
	return [][2]any{{"commune_code", str(v.CommuneCode)}, {"cooperative_ref", ref(v.CooperativeRef)}, {"lot_name", str(v.LotName)},
		{"farmer_ref", ref(v.FarmerRef)}, {"note", str(v.Note)}, {"declared_area_ha", dec}, {"shape", str(v.Shape)},
		{"department_code", str(v.Department)}, {"boundary_id", v.BoundaryID.String()}, {"area_ha", v.AreaHa.StringFixed(4)}}
}

func diff(prev *store.Version, cur store.Version) []LogChange {
	now := snapshot(cur)
	var old [][2]any
	if prev != nil {
		old = snapshot(*prev)
	}
	out := []LogChange{}
	for i, f := range now {
		var o any
		if old != nil {
			o = old[i][1]
		}
		if o != f[1] {
			out = append(out, LogChange{Field: f[0].(string), Old: o, New: f[1]})
		}
	}
	return out
}

// History trả lịch sử thửa (ảnh chụp trên sổ kn_plot_version), mới nhất trước, phân trang theo id.
func (s *Service) History(ctx context.Context, ref, after uuid.UUID, limit int) (LogPage, error) {
	c, err := s.caller(ctx, false)
	if err != nil {
		return LogPage{}, err
	}
	if _, err := s.plot(ctx, c, ref); err != nil {
		return LogPage{}, err
	}
	var vs []store.Version
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) (e error) { vs, e = store.Versions(ctx, tx, ref); return }); err != nil {
		return LogPage{}, err
	}
	items := make([]LogItem, 0, len(vs))
	for i, v := range vs {
		var prev *store.Version
		if i > 0 {
			prev = &vs[i-1]
		}
		reason := v.Reason
		if reason == nil {
			reason = v.SupportReason
		}
		items = append(items, LogItem{ID: v.ID, Event: v.Event, ActorRef: v.ActorRef, At: v.RecordedAt, Changes: diff(prev, v), Reason: reason})
	}
	slices.Reverse(items)
	if after != uuid.Nil {
		for i, it := range items {
			if it.ID == after {
				items = items[i+1:]
				break
			}
		}
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	pg := LogPage{Items: items}
	if len(items) > limit {
		pg.Items = items[:limit]
		n := pg.Items[limit-1].ID
		pg.Next = &n
	}
	s.actorNames(ctx, pg.Items)
	return pg, nil
}

func (s *Service) actorNames(ctx context.Context, items []LogItem) {
	if s.Names == nil || len(items) == 0 {
		return
	}
	n := s.Names()
	if n == nil {
		return
	}
	refs := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		refs = append(refs, it.ActorRef)
	}
	m, err := n.DisplayNames(ctx, refs)
	if err != nil {
		return
	}
	for i := range items {
		if v, ok := m[items[i].ActorRef]; ok && v != "" {
			name := v
			items[i].ActorDisplay = &name
		}
	}
}

// ExportFile là tệp xuất cho màn xã.
type ExportFile struct {
	Name        string
	ContentType string
	Body        []byte
}

// Export xuất thửa của một xã trong phạm vi ra GeoJSON (FeatureCollection) hoặc KML.
func (s *Service) Export(ctx context.Context, commune, format string) (ExportFile, error) {
	c, err := s.caller(ctx, false)
	if err != nil {
		return ExportFile{}, err
	}
	commune = strings.TrimSpace(commune)
	if commune == "" {
		return ExportFile{}, problem.Validation.WithField("commune", "bắt buộc")
	}
	if !c.k.Scope.Allows(commune) {
		return ExportFile{}, ErrOutOfScope.WithDetail("xã ngoài phạm vi phụ trách")
	}
	if format == "" {
		format = "geojson"
	}
	if format != "geojson" && format != "kml" {
		return ExportFile{}, problem.Validation.WithField("format", "phải là geojson hoặc kml")
	}
	var rows []store.Plot
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) (e error) {
		rows, e = store.Export(ctx, tx, commune, c.readDept(), MaxExport)
		return
	}); err != nil {
		return ExportFile{}, err
	}
	labels := s.FarmerLabels(ctx, rows...)
	_ = meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{
		SourceID: ids.New(), Channel: meter.ChannelReportExport, DataTenantID: c.p.TenantID,
		CounterpartyKind: meter.CounterpartyActor, CounterpartyRef: c.p.ActorRef, Product: ProductKNPlotFile,
		Records: int64(len(rows)), OccurredAt: s.now(), ActorRef: c.p.ActorRef, TraceID: telemetry.TraceID(ctx), Internal: true,
	}, s.Log)
	base := "thua-ruong-" + commune
	if format == "kml" {
		return ExportFile{Name: base + ".kml", ContentType: "application/vnd.google-earth.kml+xml", Body: kmlDoc(commune, rows, labels)}, nil
	}
	feats := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		feats = append(feats, map[string]any{"type": "Feature", "id": r.Ref.String(), "geometry": rawJSON(r.GeoJSON), "properties": exportProps(r, labels)})
	}
	body, err := json.Marshal(map[string]any{"type": "FeatureCollection", "features": feats})
	if err != nil {
		return ExportFile{}, err
	}
	return ExportFile{Name: base + ".geojson", ContentType: "application/geo+json", Body: body}, nil
}

func exportProps(p store.Plot, labels map[uuid.UUID]string) map[string]any {
	opt := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	m := map[string]any{"ref": p.Ref.String(), "code": p.Code, "lot_name": opt(p.LotName), "commune_code": p.CommuneCode,
		"cooperative_ref": nil, "farmer_label": nil, "note": opt(p.Note), "area_ha": p.AreaHa.StringFixed(4), "declared_area_ha": nil,
		"shape": opt(p.Shape), "status": Status(p.Snapshot)}
	if p.CooperativeRef != nil {
		m["cooperative_ref"] = p.CooperativeRef.String()
	}
	if p.DeclaredAreaHa != nil {
		m["declared_area_ha"] = p.DeclaredAreaHa.StringFixed(4)
	}
	if p.FarmerRef != nil {
		if l, ok := labels[*p.FarmerRef]; ok {
			m["farmer_label"] = l
		}
	}
	return m
}

func xmlEsc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func kmlDoc(commune string, rows []store.Plot, labels map[uuid.UUID]string) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<kml xmlns="http://www.opengis.net/kml/2.2"><Document><name>` + xmlEsc("Thửa ruộng xã "+commune) + "</name>\n")
	for _, r := range rows {
		name := r.LotName
		if name == "" {
			name = r.Code
		}
		b.WriteString("<Placemark><name>" + xmlEsc(name) + "</name><ExtendedData>")
		props := exportProps(r, labels)
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if v, ok := props[k].(string); ok {
				b.WriteString(`<Data name="` + xmlEsc(k) + `"><value>` + xmlEsc(v) + "</value></Data>")
			}
		}
		b.WriteString("</ExtendedData>" + r.KML + "</Placemark>\n")
	}
	b.WriteString("</Document></kml>\n")
	return b.Bytes()
}
