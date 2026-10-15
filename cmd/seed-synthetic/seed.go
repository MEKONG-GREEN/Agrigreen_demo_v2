package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"time"
)

// Stats đếm bản ghi theo loại: created = mới tạo, existing = đã có từ lần chạy trước.
type Stats struct {
	Created  map[string]int
	Existing map[string]int
}

func newStats() *Stats { return &Stats{Created: map[string]int{}, Existing: map[string]int{}} }

func (s *Stats) add(kind string, created bool) {
	if created {
		s.Created[kind]++
	} else {
		s.Existing[kind]++
	}
}

// seeder thực thi Plan qua API.
type seeder struct {
	api   *api
	log   *slog.Logger
	seed  uint64
	scale string
	st    *Stats
}

// key sinh Idempotency-Key tất định (8–200 ký tự [A-Za-z0-9._:-]).
func (s *seeder) key(kind string, parts ...any) string {
	k := fmt.Sprintf("syn-%d-%s-%s", s.seed, s.scale, kind)
	for _, p := range parts {
		k += fmt.Sprintf("-%v", p)
	}
	if len(k) > 200 {
		k = k[:200]
	}
	return k
}

// isConflict: đã tồn tại từ lần chạy trước (mã, số seri, id tự sinh…).
func isConflict(err error, codes ...string) (resp, bool) {
	var es *errStatus
	if !errors.As(err, &es) || es.r.status != http.StatusConflict {
		return resp{}, false
	}
	for _, c := range codes {
		if es.r.code() == c {
			return es.r, true
		}
	}
	return resp{}, false
}

// loadUnits đọc tỉnh và xã hiện hành từ pf/masterdata (Application API).
func (s *seeder) loadUnits(ctx context.Context) ([]Unit, map[string][]Unit, error) {
	provs, err := s.api.list(ctx, appAPI, "/v1/masterdata/admin-units")
	if err != nil {
		return nil, nil, err
	}
	var out []Unit
	communes := map[string][]Unit{}
	for _, p := range provs {
		code := str(p, "code")
		out = append(out, Unit{Code: code, Name: str(p, "name")})
		children, err := s.api.list(ctx, appAPI, "/v1/masterdata/admin-units?parent="+url.QueryEscape(code))
		if err != nil {
			return nil, nil, err
		}
		for _, c := range children {
			if str(c, "level") == "commune" {
				communes[code] = append(communes[code], Unit{Code: str(c, "code"), Name: str(c, "name"), Parent: code})
			}
		}
		sort.Slice(communes[code], func(i, j int) bool { return communes[code][i].Code < communes[code][j].Code })
	}
	return out, communes, nil
}

// org tạo tổ chức; mã đã có → tìm lại ref theo mã.
func (s *seeder) org(ctx context.Context, kind, code, name, unit, parent string) (string, error) {
	body := map[string]any{"kind": kind, "code": code, "name": name}
	if unit != "" {
		body["admin_unit_code"] = unit
	}
	if parent != "" {
		body["parent_ref"] = parent
	}
	r, err := s.api.expect(ctx, adminAPI, http.MethodPost, "/v1/orgs", s.key("org", code), body, http.StatusCreated, http.StatusOK)
	if _, ok := isConflict(err, "party.org_code_taken"); ok {
		orgs, lerr := s.api.list(ctx, appAPI, "/v1/orgs?kind="+kind)
		if lerr != nil {
			return "", lerr
		}
		for _, o := range orgs {
			if str(o, "code") == code {
				s.st.add("org", false)
				return str(o, "ref"), nil
			}
		}
		return "", err
	}
	if err != nil {
		return "", err
	}
	s.st.add("org", r.isNew())
	return str(obj(r.body, "org"), "ref"), nil
}

// Run thực thi kế hoạch. Thứ tự: dữ liệu chủ → cây tổ chức → HTX/nông hộ/thửa →
// danh bạ, nhiệm vụ, báo cáo → CGH → cân đối.
func (s *seeder) Run(ctx context.Context, sc Scale, today time.Time) (Plan, error) {
	if _, err := s.api.expect(ctx, adminAPI, http.MethodPost, "/v1/masterdata/seed", s.key("masterdata"), map[string]any{}, http.StatusOK); err != nil {
		return Plan{}, fmt.Errorf("nạp dữ liệu chủ: %w", err)
	}
	provs, communes, err := s.loadUnits(ctx)
	if err != nil {
		return Plan{}, err
	}
	plan := BuildPlan(s.seed, sc, provs, communes, today)
	if len(plan.Provinces) == 0 {
		return plan, errors.New("masterdata không có tỉnh/xã ĐBSCL hiện hành")
	}
	nat, err := s.org(ctx, "ttkn_national", "SYN-TTKNQG", "TTKN Quốc gia (dữ liệu tổng hợp)", "", "")
	if err != nil {
		return plan, err
	}
	for _, pp := range plan.Provinces {
		if err := s.province(ctx, plan, pp, nat); err != nil {
			return plan, err
		}
	}
	for _, season := range []string{"DX", "HT"} {
		r, err := s.api.expect(ctx, adminAPI, http.MethodPost, "/v1/mechanization/balance-runs", s.key("balance", season, plan.SeasonYear),
			map[string]any{"season_code": season, "season_year": plan.SeasonYear}, http.StatusCreated, http.StatusOK)
		if err != nil {
			return plan, fmt.Errorf("cân đối %s %d: %w", season, plan.SeasonYear, err)
		}
		s.st.add("balance_run", r.isNew())
		s.log.Info("cân đối cung–cầu máy", "event", "seed.balance", "season", season, "season_year", plan.SeasonYear,
			"status_counts", obj(obj(r.body, "run"), "status_counts"), "outcome", "ok")
	}
	return plan, nil
}

func (s *seeder) province(ctx context.Context, plan Plan, pp ProvincePlan, nat string) error {
	prov, err := s.org(ctx, "ttkn_province", "SYN-TTKN-"+pp.Code, "TTKN "+pp.Name+" (tổng hợp)", pp.Code, nat)
	if err != nil {
		return err
	}
	for _, st := range pp.Staff {
		if err := s.staff(ctx, st, pp.Code); err != nil {
			return err
		}
	}
	for ci, cp := range pp.Communes {
		team, err := s.org(ctx, "extension_team", fmt.Sprintf("SYN-TKN-%s", asciiSlug(cp.Code)), "Tổ KNCĐ "+cp.Name+" (tổng hợp)", cp.Code, prov)
		if err != nil {
			return err
		}
		for _, st := range cp.Staff {
			if err := s.staff(ctx, st, cp.Code); err != nil {
				return err
			}
		}
		var coopRefs []string
		for k, coop := range cp.Coops {
			ref, err := s.coop(ctx, plan, cp, coop, team)
			if err != nil {
				return fmt.Errorf("HTX %d xã %s: %w", k+1, cp.Code, err)
			}
			coopRefs = append(coopRefs, ref)
		}
		if err := s.tasks(ctx, cp, coopRefs); err != nil {
			return err
		}
		for _, pr := range cp.PestReports {
			r, err := s.api.expect(ctx, appAPI, http.MethodPost, "/v1/extension/field-reports", s.key("pest", pr.ID), map[string]any{
				"id": pr.ID.String(), "kind": "growth_pest", "commune_code": cp.Code, "period": plan.Period, "growth_stage": pr.Stage,
				"pest_code": pr.Pest, "severity": pr.Severity, "affected_area_ha": pr.AffectedAreaHa, "urgent": pr.Urgent,
			}, http.StatusCreated, http.StatusOK)
			if _, ok := isConflict(err, "extension.id_reused"); ok {
				s.st.add("field_report", false)
				continue
			}
			if err != nil {
				return err
			}
			s.st.add("field_report", r.isNew())
		}
		s.log.Info("đã sinh xã", "event", "seed.commune", "province", pp.Code, "commune", cp.Code, "index", ci+1, "outcome", "ok")
	}
	return nil
}

func (s *seeder) staff(ctx context.Context, st StaffPlan, unit string) error {
	r, err := s.api.expect(ctx, appAPI, http.MethodPost, "/v1/extension/staff", s.key("staff", st.ActorRef), map[string]any{
		"actor_ref": st.ActorRef.String(), "unit_code": unit, "role_title": st.Title, "display_name": st.Name,
	}, http.StatusOK, http.StatusCreated)
	if err == nil {
		s.st.add("staff", !r.replayed)
	}
	return err
}

func (s *seeder) coop(ctx context.Context, plan Plan, cp CommunePlan, c CoopPlan, team string) (string, error) {
	r, err := s.api.expect(ctx, adminAPI, http.MethodPost, "/v1/htx-anchors", s.key("coop", c.TaxCode), map[string]any{
		"created_by": "mkg", "mst": c.TaxCode,
		"new_cooperative": map[string]any{"name": c.Name, "admin_unit_code": cp.Code, "parent_ref": team,
			"representative": map[string]any{"full_name": c.Rep.FullName, "phone": c.Rep.Phone},
			"profile": map[string]any{"area_ha": c.AreaHa, "established_on": c.Established,
				"location": map[string]any{"lat": fmt.Sprintf("%.7f", c.Location.Lat), "lon": fmt.Sprintf("%.7f", c.Location.Lon)}},
		},
	}, http.StatusCreated, http.StatusOK)
	var ref string
	if cr, ok := isConflict(err, "cooperative.htx_anchor_mst_taken"); ok {
		ref = str(cr.body, "existing_org_ref")
		if ref == "" && str(cr.body, "existing_anchor_id") != "" {
			prior, readErr := s.api.expect(ctx, adminAPI, http.MethodGet, "/v1/htx-anchors/"+str(cr.body, "existing_anchor_id"), "", nil, http.StatusOK)
			if readErr != nil {
				return "", readErr
			}
			ref = str(prior.body, "org_ref")
		}
		s.st.add("cooperative", false)
	} else if err != nil {
		return "", err
	} else {
		ref = str(r.body, "org_ref")
		s.st.add("cooperative", r.isNew())
	}
	if ref == "" {
		return "", errors.New("không lấy được mã HTX")
	}
	for hi, h := range c.Households {
		id := map[string]any{"full_name": h.FullName, "phone": h.Phone, "address": h.Address}
		if h.NationalID != "" {
			id["national_id"] = h.NationalID
		}
		hr, err := s.api.expect(ctx, appAPI, http.MethodPost, "/v1/households", s.key("hh", ref, hi), map[string]any{
			"cooperative_ref": ref, "commune_code": cp.Code, "cultivated_area_ha": h.CultivatedHa, "member_since": h.MemberSince, "identity": id,
		}, http.StatusCreated, http.StatusOK)
		if err != nil {
			return "", fmt.Errorf("nông hộ %d: %w", hi+1, err)
		}
		dup, _ := hr.body["duplicate"].(bool)
		s.st.add("household", !dup && hr.isNew())
		href := str(obj(hr.body, "household"), "household_ref")
		for _, pl := range h.Plots {
			ring := make([][]float64, len(pl.Ring))
			for i, pt := range pl.Ring {
				ring[i] = []float64{pt[0], pt[1]}
			}
			pr, err := s.api.expect(ctx, appAPI, http.MethodPost, "/v1/plots", s.key("plot", pl.Ref), map[string]any{
				"plot_ref": pl.Ref.String(), "name": pl.Name, "commune_code": cp.Code, "cooperative_ref": ref, "household_ref": href,
				"geometry": map[string]any{"type": "Polygon", "coordinates": [][][]float64{ring}}, "gps_accuracy_m": "3.0", "source": "kn_web",
			}, http.StatusCreated, http.StatusOK)
			if _, ok := isConflict(err, "geo.plot_exists"); ok {
				s.st.add("plot", false)
				continue
			}
			if err != nil {
				return "", fmt.Errorf("thửa %s: %w", pl.Name, err)
			}
			s.st.add("plot", pr.isNew())
		}
	}
	if err := s.mechanization(ctx, plan, c, ref); err != nil {
		return "", err
	}
	for _, sw := range c.Sowing {
		r, err := s.api.expect(ctx, appAPI, http.MethodPost, "/v1/extension/field-reports", s.key("sow", sw.ID), map[string]any{
			"id": sw.ID.String(), "kind": "sowing_progress", "commune_code": cp.Code, "cooperative_ref": ref, "period": plan.Period,
			"season_code": "DX", "variety_code": sw.Variety, "sown_area_ha": sw.AreaHa, "sown_on": plan.SownOn,
		}, http.StatusCreated, http.StatusOK)
		if _, ok := isConflict(err, "extension.id_reused"); ok {
			s.st.add("field_report", false)
			continue
		}
		if err != nil {
			return "", fmt.Errorf("báo cáo gieo sạ: %w", err)
		}
		s.st.add("field_report", r.isNew())
	}
	return ref, nil
}

func (s *seeder) mechanization(ctx context.Context, plan Plan, c CoopPlan, coop string) error {
	r, err := s.api.expect(ctx, adminAPI, http.MethodPost, "/v1/mechanization/owners", s.key("owner", coop), map[string]any{
		"owner_type": "thanh_vien_htx", "cooperative_ref": coop, "name": c.Owner.FullName, "phone": c.Owner.Phone,
	}, http.StatusCreated, http.StatusOK)
	var ownerID string
	if cr, ok := isConflict(err, "mechanization.owner_duplicate"); ok {
		code := str(cr.body, "existing_code")
		owners, lerr := s.api.list(ctx, appAPI, "/v1/mechanization/owners?include_inactive=true&cooperative_ref="+coop)
		if lerr != nil {
			return lerr
		}
		for _, o := range owners {
			if str(o, "code") == code {
				ownerID = str(o, "id")
			}
		}
		s.st.add("owner", false)
	} else if err != nil {
		return fmt.Errorf("chủ máy: %w", err)
	} else {
		ownerID = str(obj(r.body, "owner"), "id")
		s.st.add("owner", r.isNew())
	}
	if ownerID == "" {
		return errors.New("không lấy được mã chủ máy")
	}
	for _, m := range c.Machines {
		mr, err := s.api.expect(ctx, adminAPI, http.MethodPost, "/v1/mechanization/machines", s.key("machine", m.Serial), map[string]any{
			"cooperative_ref": coop, "machine_type": m.Type, "owner_id": ownerID, "brand": m.Brand, "model": m.Model, "serial_no": m.Serial,
			"year_made": m.YearMade, "fuel": m.Fuel, "power_hp": m.PowerHP, "owned_since": m.OwnedSince, "status": m.Status,
		}, http.StatusCreated, http.StatusOK)
		if _, ok := isConflict(err, "mechanization.serial_taken"); ok {
			s.st.add("machine", false)
			continue
		}
		if err != nil {
			return fmt.Errorf("máy %s: %w", m.Serial, err)
		}
		s.st.add("machine", mr.isNew())
	}
	for _, sa := range c.SeasonAreas {
		ar, err := s.api.expect(ctx, adminAPI, http.MethodPost, "/v1/mechanization/season-areas", s.key("area", coop, sa.Season, plan.SeasonYear), map[string]any{
			"cooperative_ref": coop, "season_code": sa.Season, "season_year": plan.SeasonYear, "area_ha": sa.AreaHa, "source": "manual",
		}, http.StatusCreated, http.StatusOK)
		if err != nil {
			return fmt.Errorf("diện tích vụ %s: %w", sa.Season, err)
		}
		s.st.add("season_area", ar.isNew())
	}
	return nil
}

func (s *seeder) tasks(ctx context.Context, cp CommunePlan, coops []string) error {
	for _, tp := range cp.Tasks {
		body := map[string]any{"id": tp.ID.String(), "commune_code": cp.Code, "category": tp.Category, "priority": tp.Priority, "description": tp.Description}
		if tp.CoopIndex >= 0 && tp.CoopIndex < len(coops) {
			body["cooperative_ref"] = coops[tp.CoopIndex]
		}
		if len(cp.Staff) > 0 {
			body["assignee_ref"] = cp.Staff[int(tp.ID[15])%len(cp.Staff)].ActorRef.String()
		}
		r, err := s.api.expect(ctx, appAPI, http.MethodPost, "/v1/extension/tasks", s.key("task", tp.ID), body, http.StatusCreated, http.StatusOK)
		if _, ok := isConflict(err, "extension.id_reused"); ok {
			s.st.add("task", false)
			continue
		}
		if err != nil {
			return fmt.Errorf("nhiệm vụ: %w", err)
		}
		s.st.add("task", r.isNew())
		if tp.Resolve && r.isNew() {
			_, err := s.api.expect(ctx, appAPI, http.MethodPost, "/v1/extension/tasks/"+tp.ID.String()+"/status", s.key("resolve", tp.ID),
				map[string]any{"action": "resolved", "note": "Đã hướng dẫn trực tiếp tại ruộng (dữ liệu tổng hợp)"}, http.StatusOK)
			if err != nil {
				return fmt.Errorf("đóng nhiệm vụ: %w", err)
			}
			s.st.add("task_resolved", true)
		}
	}
	return nil
}
