package app

// Khu vực theo tỉnh và gán khu vực cho Nút xã (FN-AD-04.02: "Khu vực là thuộc tính của Nút xã";
// báo cáo nhóm đúng theo Khu vực).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/seeddata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func regionSnapshot(r store.RegionRow) domain.RegionSnapshot {
	return domain.RegionSnapshot{ProvinceCode: r.ProvinceCode, Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, Active: r.Active,
		DeactivationReason: r.DeactivationReason}
}

// checkProvince kiểm mã tỉnh còn hiệu lực (masterdata).
func (k *KN) checkProvince(ctx context.Context, code string) error {
	if k.Reg == nil || k.Reg.Units == nil {
		return ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	u, err := k.Reg.Units.Get(ctx, code, k.now())
	if errors.Is(err, mdport.ErrUnitNotFound) || (err == nil && u.Level != mdport.LevelProvince) {
		return problem.Validation.WithField("province_code", "không có tỉnh còn hiệu lực với mã này")
	}
	return err
}

// RegionView là khu vực kèm số xã được gán.
type RegionView struct {
	store.RegionRow
	Communes int
}

// CreateRegion thêm khu vực cho tỉnh.
func (k *KN) CreateRegion(ctx context.Context, in port.Region, idem string) (store.RegionRow, bool, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return store.RegionRow{}, false, err
	}
	if idem == "" {
		return store.RegionRow{}, false, problem.IdempotencyRequired
	}
	r, err := domain.NormalizeRegion(in)
	if err != nil {
		return store.RegionRow{}, false, fieldProblem(err)
	}
	if err := k.checkProvince(ctx, r.ProvinceCode); err != nil {
		return store.RegionRow{}, false, err
	}
	var out store.RegionRow
	replayed := false
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, replayed, e = k.createRegionTx(ctx, tx, p, r, "region:create:"+idem)
		return e
	})
	return out, replayed, err
}

func (k *KN) createRegionTx(ctx context.Context, tx pgx.Tx, p authz.Principal, r port.Region, key string) (store.RegionRow, bool, error) {
	if prev, err := store.RegionEventByIdem(ctx, tx, key); err != nil || prev != nil {
		if err != nil {
			return store.RegionRow{}, false, err
		}
		cur, err := store.RegionByID(ctx, tx, prev.EntityID, false)
		if err != nil || cur == nil {
			return store.RegionRow{}, false, notFoundOr(err, ErrRegionNotFound)
		}
		return *cur, true, nil
	}
	if cur, err := store.RegionByCode(ctx, tx, r.ProvinceCode, r.Code); err != nil {
		return store.RegionRow{}, false, err
	} else if cur != nil {
		return store.RegionRow{}, false, ErrRegionCodeTaken
	}
	now := k.now()
	r.ID, r.Active = ids.New(), true
	row := store.RegionRow{Region: r, UpdatedBy: p.ActorRef, UpdatedAt: now}
	e := store.KNEvent{ID: ids.New(), TenantID: p.TenantID, EntityID: r.ID, ProvinceCode: r.ProvinceCode, Code: r.Code, Action: domain.RegionCreated,
		After: snapshotJSON(regionSnapshot(row)), EventTime: now, ActorRef: p.ActorRef, IdemKey: key}
	if err := ledger.Classify(store.InsertRegionEvent(ctx, tx, &e)); err != nil {
		return store.RegionRow{}, false, err
	}
	return row, false, store.PutRegion(ctx, tx, p.TenantID, row, e)
}

func (k *KN) writeRegion(ctx context.Context, id uuid.UUID, key, action string, reason *string,
	mutate func(tx pgx.Tx, cur store.RegionRow) (*store.RegionRow, error)) (store.RegionRow, bool, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return store.RegionRow{}, false, err
	}
	now := k.now()
	var out store.RegionRow
	replayed := false
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.RegionEventByIdem(ctx, tx, key); err != nil || prev != nil {
			if err != nil {
				return err
			}
			if prev.EntityID != id {
				return problem.IdempotencyConflict
			}
			cur, err := store.RegionByID(ctx, tx, id, false)
			if err != nil || cur == nil {
				return notFoundOr(err, ErrRegionNotFound)
			}
			out, replayed = *cur, true
			return nil
		}
		cur, err := store.RegionByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if cur == nil {
			return ErrRegionNotFound
		}
		next, err := mutate(tx, *cur)
		if err != nil || next == nil {
			out = *cur
			return err
		}
		next.UpdatedBy, next.UpdatedAt = p.ActorRef, now
		e := store.KNEvent{ID: ids.New(), TenantID: p.TenantID, EntityID: id, ProvinceCode: cur.ProvinceCode, Code: cur.Code, Action: action,
			Before: snapshotJSON(regionSnapshot(*cur)), After: snapshotJSON(regionSnapshot(*next)), Reason: reason, EventTime: now,
			ActorRef: p.ActorRef, IdemKey: key}
		if err := ledger.Classify(store.InsertRegionEvent(ctx, tx, &e)); err != nil {
			return err
		}
		out = *next
		return store.PutRegion(ctx, tx, p.TenantID, *next, e)
	})
	return out, replayed, err
}

// UpdateRegion sửa tên, thứ tự (PUT).
func (k *KN) UpdateRegion(ctx context.Context, id uuid.UUID, name string, sortOrder int) (store.RegionRow, error) {
	r, _, err := k.writeRegion(ctx, id, "region:put:"+ids.New().String(), domain.RegionUpdated, nil,
		func(_ pgx.Tx, cur store.RegionRow) (*store.RegionRow, error) {
			n, err := domain.NormalizeRegion(port.Region{ProvinceCode: cur.ProvinceCode, Code: cur.Code, Name: name, SortOrder: sortOrder})
			if err != nil {
				return nil, fieldProblem(err)
			}
			if n.Name == cur.Name && n.SortOrder == cur.SortOrder {
				return nil, nil
			}
			next := cur
			next.Name, next.SortOrder = n.Name, n.SortOrder
			return &next, nil
		})
	return r, err
}

// SetRegionActive ngừng dùng (lý do bắt buộc; chặn khi còn xã được gán) hoặc dùng lại khu vực.
func (k *KN) SetRegionActive(ctx context.Context, id uuid.UUID, active bool, reason, idem string) (store.RegionRow, bool, error) {
	if _, err := authz.Require(ctx, knAdminRoles...); err != nil {
		return store.RegionRow{}, false, err
	}
	if idem == "" {
		return store.RegionRow{}, false, problem.IdempotencyRequired
	}
	if !active {
		if err := domain.ValidateReason("reason", reason); err != nil {
			return store.RegionRow{}, false, fieldProblem(err)
		}
	}
	action, key := domain.RegionReactivated, "region:reactivate:"+idem
	if !active {
		action, key = domain.RegionDeactivated, "region:deactivate:"+idem
	}
	return k.writeRegion(ctx, id, key, action, optReason(reason), func(tx pgx.Tx, cur store.RegionRow) (*store.RegionRow, error) {
		if cur.Active == active {
			return nil, ErrRegionState
		}
		if !active {
			counts, err := store.RegionCommuneCounts(ctx, tx, cur.ProvinceCode)
			if err != nil {
				return nil, err
			}
			if n := counts[cur.Code]; n > 0 {
				return nil, ErrRegionInUse.With("communes", n)
			}
		}
		next := cur
		next.Active, next.DeactivationReason = active, ""
		if !active {
			next.DeactivationReason = strings.TrimSpace(reason)
		}
		return &next, nil
	})
}

// ListRegions liệt kê khu vực của tỉnh kèm số xã (Application API: mọi người đã xác thực; Admin API).
func (k *KN) ListRegions(ctx context.Context, province string, includeInactive bool) ([]RegionView, error) {
	p, err := authenticated(ctx)
	if err != nil {
		return nil, err
	}
	province = strings.TrimSpace(province)
	if province == "" {
		return nil, problem.Validation.WithField("province_code", "bắt buộc")
	}
	var out []RegionView
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		rs, err := store.ListRegions(ctx, tx, province, includeInactive)
		if err != nil {
			return err
		}
		counts, err := store.RegionCommuneCounts(ctx, tx, province)
		if err != nil {
			return err
		}
		for _, r := range rs {
			out = append(out, RegionView{RegionRow: r, Communes: counts[r.Code]})
		}
		return nil
	})
	if out == nil {
		out = []RegionView{}
	}
	return out, err
}

// GetRegion đọc một khu vực (Admin).
func (k *KN) GetRegion(ctx context.Context, id uuid.UUID) (RegionView, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return RegionView{}, err
	}
	var out RegionView
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		r, err := store.RegionByID(ctx, tx, id, false)
		if err != nil || r == nil {
			return notFoundOr(err, ErrRegionNotFound)
		}
		counts, err := store.RegionCommuneCounts(ctx, tx, r.ProvinceCode)
		out = RegionView{RegionRow: *r, Communes: counts[r.Code]}
		return err
	})
	return out, err
}

// RegionLog trả khung Log của khu vực.
func (k *KN) RegionLog(ctx context.Context, id uuid.UUID, after int64, limit int) ([]LogEntry, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return nil, err
	}
	var evs []store.KNEvent
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		if r, err := store.RegionByID(ctx, tx, id, false); err != nil || r == nil {
			return notFoundOr(err, ErrRegionNotFound)
		}
		var e error
		evs, e = store.RegionEvents(ctx, tx, id, after, clampLog(limit))
		return e
	})
	if err != nil {
		return nil, err
	}
	return k.toLog(ctx, evs, snapshotChanges), nil
}

// ProvinceRegions thực hiện port.Regions.
func (k *KN) ProvinceRegions(ctx context.Context, province string, includeInactive bool) ([]port.Region, error) {
	var out []port.Region
	err := tenant.FromCtxTx(ctx, k.DB, func(tx pgx.Tx) error {
		rs, err := store.ListRegions(ctx, tx, province, includeInactive)
		for _, r := range rs {
			out = append(out, r.Region)
		}
		return err
	})
	return out, err
}

// CommuneRegions thực hiện port.Regions: khu vực của mọi Nút xã đang hoạt động của tỉnh.
func (k *KN) CommuneRegions(ctx context.Context, province string) (map[string]port.CommuneRegion, error) {
	if k.Reg == nil || k.Reg.Units == nil {
		return nil, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	codes, err := k.Reg.Units.Descendants(ctx, province, k.now())
	if err != nil {
		return nil, err
	}
	var out map[string]port.CommuneRegion
	err = tenant.FromCtxTx(ctx, k.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.CommuneRegions(ctx, tx, codes)
		return e
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Gán khu vực cho Nút xã
// ---------------------------------------------------------------------------

// OrgRegionResult là kết quả gán khu vực.
type OrgRegionResult struct {
	OrgRef       uuid.UUID
	CommuneCode  string
	ProvinceCode string
	RegionCode   string // rỗng = chưa gán
	Action       string
	Replayed     bool
}

// provinceOf trả mã tỉnh của Nút xã: theo masterdata (cha của xã), dự phòng là đơn vị của Nút cha.
func (k *KN) provinceOf(ctx context.Context, tx pgx.Tx, o *store.OrgState) (string, error) {
	if k.Reg != nil && k.Reg.Units != nil {
		u, err := k.Reg.Units.Get(ctx, o.AdminUnitCode, k.now())
		if err == nil && u.ParentCode != "" {
			return u.ParentCode, nil
		}
	}
	parent, err := store.OrgByRef(ctx, tx, o.ParentRef, false)
	if err != nil || parent == nil {
		return "", notFoundOr(err, ErrRegionNode)
	}
	return parent.AdminUnitCode, nil
}

// AssignRegion gán / đổi / bỏ (regionCode rỗng) khu vực của một Nút xã. Đổi hoặc bỏ cần lý do.
func (k *KN) AssignRegion(ctx context.Context, orgRef uuid.UUID, regionCode, reason, idem string) (OrgRegionResult, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return OrgRegionResult{}, err
	}
	if idem == "" {
		return OrgRegionResult{}, problem.IdempotencyRequired
	}
	var out OrgRegionResult
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = k.assignRegionTx(ctx, tx, p, orgRef, strings.TrimSpace(regionCode), reason, "org-region:"+idem)
		return e
	})
	return out, err
}

func (k *KN) assignRegionTx(ctx context.Context, tx pgx.Tx, p authz.Principal, orgRef uuid.UUID, regionCode, reason, key string) (OrgRegionResult, error) {
	if prev, err := store.OrgRegionEventByIdem(ctx, tx, key); err != nil || prev != nil {
		if err != nil {
			return OrgRegionResult{}, err
		}
		if prev.EntityID != orgRef {
			return OrgRegionResult{}, problem.IdempotencyConflict
		}
		res := OrgRegionResult{OrgRef: orgRef, CommuneCode: prev.CommuneCode, ProvinceCode: prev.ProvinceCode, Action: prev.Action, Replayed: true}
		if prev.RegionCode != nil {
			res.RegionCode = *prev.RegionCode
		}
		return res, nil
	}
	o, err := store.OrgByRef(ctx, tx, orgRef, true)
	if err != nil {
		return OrgRegionResult{}, err
	}
	if o == nil {
		return OrgRegionResult{}, ErrOrgNotFound
	}
	if o.Kind != port.OrgExtensionTeam || !o.Active || o.AdminUnitCode == "" {
		return OrgRegionResult{}, ErrRegionNode
	}
	province, err := k.provinceOf(ctx, tx, o)
	if err != nil {
		return OrgRegionResult{}, err
	}
	if k.Reg != nil {
		if err := k.Reg.requireWriteScope(ctx, p, o.AdminUnitCode); err != nil {
			return OrgRegionResult{}, err
		}
	}
	cur, err := store.OrgRegionByOrg(ctx, tx, orgRef, true)
	if err != nil {
		return OrgRegionResult{}, err
	}
	var prevCode *string
	if cur != nil && cur.RegionCode != nil {
		prevCode = cur.RegionCode
	}
	action := domain.OrgRegionAssigned
	switch {
	case regionCode == "" && prevCode == nil:
		return OrgRegionResult{}, ErrRegionState.WithDetail("xã chưa được gán khu vực")
	case regionCode == "":
		action = domain.OrgRegionCleared
	case prevCode != nil && *prevCode == regionCode && cur.ProvinceCode == province:
		return OrgRegionResult{}, ErrRegionState.WithDetail("xã đã ở khu vực này")
	case prevCode != nil:
		action = domain.OrgRegionChanged
	}
	if action != domain.OrgRegionAssigned {
		if err := domain.ValidateReason("reason", reason); err != nil {
			return OrgRegionResult{}, fieldProblem(err)
		}
	}
	var newCode *string
	if regionCode != "" {
		r, err := store.RegionByCode(ctx, tx, province, regionCode)
		if err != nil {
			return OrgRegionResult{}, err
		}
		if r == nil || !r.Active {
			return OrgRegionResult{}, ErrRegionNode.WithField("region_code", "không có khu vực đang dùng với mã này trong tỉnh "+province)
		}
		newCode = &regionCode
	}
	return k.writeOrgRegion(ctx, tx, p, o, province, action, newCode, prevCode, optReason(reason), key)
}

func (k *KN) writeOrgRegion(ctx context.Context, tx pgx.Tx, p authz.Principal, o *store.OrgState, province, action string,
	newCode, prevCode *string, reason *string, key string) (OrgRegionResult, error) {
	now := k.now()
	e := store.KNEvent{ID: ids.New(), TenantID: p.TenantID, EntityID: o.Ref, CommuneCode: o.AdminUnitCode, ProvinceCode: province, Action: action,
		RegionCode: newCode, PrevRegionCode: prevCode, Reason: reason, EventTime: now, ActorRef: p.ActorRef, IdemKey: key}
	if err := ledger.Classify(store.InsertOrgRegionEvent(ctx, tx, &e)); err != nil {
		return OrgRegionResult{}, err
	}
	if err := store.PutOrgRegion(ctx, tx, p.TenantID, store.OrgRegion{OrgRef: o.Ref, CommuneCode: o.AdminUnitCode, ProvinceCode: province,
		RegionCode: newCode}, e); err != nil {
		return OrgRegionResult{}, err
	}
	res := OrgRegionResult{OrgRef: o.Ref, CommuneCode: o.AdminUnitCode, ProvinceCode: province, Action: action}
	if newCode != nil {
		res.RegionCode = *newCode
	}
	return res, nil
}

// clearRegionOnMove bỏ gán khu vực khi Nút xã chuyển sang tỉnh khác (khu vực thuộc tỉnh cũ) — bản ghi
// bù trong CÙNG giao dịch với sự kiện moved, lý do = lý do di chuyển.
func clearRegionOnMove(ctx context.Context, tx pgx.Tx, p authz.Principal, o port.Org, reason string, now time.Time, moveKey string) error {
	if o.Kind != port.OrgExtensionTeam {
		return nil
	}
	cur, err := store.OrgRegionByOrg(ctx, tx, o.Ref, true)
	if err != nil || cur == nil || cur.RegionCode == nil {
		return err
	}
	parent, err := store.OrgByRef(ctx, tx, o.ParentRef, false)
	if err != nil {
		return err
	}
	if parent != nil && parent.AdminUnitCode == cur.ProvinceCode && o.AdminUnitCode == cur.CommuneCode {
		return nil // vẫn cùng tỉnh, cùng xã: giữ khu vực
	}
	r := "Di chuyển Nút: " + strings.TrimSpace(reason)
	e := store.KNEvent{ID: ids.New(), TenantID: p.TenantID, EntityID: o.Ref, CommuneCode: cur.CommuneCode, ProvinceCode: cur.ProvinceCode,
		Action: domain.OrgRegionCleared, PrevRegionCode: cur.RegionCode, Reason: &r, EventTime: now, ActorRef: p.ActorRef,
		IdemKey: "org-region:move:" + moveKey}
	if err := ledger.Classify(store.InsertOrgRegionEvent(ctx, tx, &e)); err != nil {
		return err
	}
	return store.PutOrgRegion(ctx, tx, p.TenantID, store.OrgRegion{OrgRef: o.Ref, CommuneCode: cur.CommuneCode, ProvinceCode: cur.ProvinceCode}, e)
}

// OrgRegionLog trả lịch sử gán khu vực của một Nút xã.
func (k *KN) OrgRegionLog(ctx context.Context, orgRef uuid.UUID, after int64, limit int) ([]LogEntry, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return nil, err
	}
	var evs []store.KNEvent
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		evs, e = store.OrgRegionEvents(ctx, tx, orgRef, after, clampLog(limit))
		return e
	})
	if err != nil {
		return nil, err
	}
	return k.toLog(ctx, evs, func(e store.KNEvent) []domain.FieldChange {
		return []domain.FieldChange{{Field: "region_code", Old: e.PrevRegionCode, New: e.RegionCode}}
	}), nil
}

// ---------------------------------------------------------------------------
// Dữ liệu nền khu vực (tạm — Q-04)
// ---------------------------------------------------------------------------

type regionSeedFile struct {
	Format       string `json:"format"`
	ProvinceCode string `json:"province_code"`
	Status       string `json:"status"`
	Source       string `json:"source"`
	Regions      []struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		SortOrder int    `json:"sort_order"`
	} `json:"regions"`
	Assignments []struct {
		CommuneCode string `json:"commune_code"`
		RegionCode  string `json:"region_code"`
	} `json:"assignments"`
}

func regionSeeds() (map[string]regionSeedFile, error) {
	var f regionSeedFile
	dec := json.NewDecoder(bytes.NewReader(seeddata.RegionsAnGiang))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil || f.Format != "agrigreen.regions/v1" {
		return nil, fmt.Errorf("party: dữ liệu nhúng regions lỗi: %w", err)
	}
	return map[string]regionSeedFile{f.ProvinceCode: f}, nil
}

// RegionSeedResult tổng kết một lần nạp khu vực.
type RegionSeedResult struct {
	ProvinceCode    string
	RegionsCreated  int
	Assigned        int
	AlreadyAssigned int
	MissingNode     []string // mã xã chưa có Nút — chạy "bảo đảm Nút xã" trước
	Status          string
}

// SeedRegions nạp khu vực nhúng của tỉnh và gán cho Nút xã chưa có khu vực (một giao dịch, chạy lặp an toàn).
func (k *KN) SeedRegions(ctx context.Context, province, idem string) (RegionSeedResult, error) {
	p, err := authz.Require(ctx, knAdminRoles...)
	if err != nil {
		return RegionSeedResult{}, err
	}
	if idem == "" {
		return RegionSeedResult{}, problem.IdempotencyRequired
	}
	seeds, err := regionSeeds()
	if err != nil {
		return RegionSeedResult{}, err
	}
	f, ok := seeds[strings.TrimSpace(province)]
	if !ok {
		return RegionSeedResult{}, ErrNoSeed.WithDetail("chỉ có dữ liệu nền khu vực của tỉnh An Giang (mã 91)")
	}
	if err := k.checkProvince(ctx, f.ProvinceCode); err != nil {
		return RegionSeedResult{}, err
	}
	res := RegionSeedResult{ProvinceCode: f.ProvinceCode, Status: f.Status, MissingNode: []string{}}
	err = tenant.WithTenantTx(ctx, k.DB, p.TenantID, func(tx pgx.Tx) error {
		for _, r := range f.Regions {
			if cur, err := store.RegionByCode(ctx, tx, f.ProvinceCode, r.Code); err != nil {
				return err
			} else if cur != nil {
				continue
			}
			nr, err := domain.NormalizeRegion(port.Region{ProvinceCode: f.ProvinceCode, Code: r.Code, Name: r.Name, SortOrder: r.SortOrder})
			if err != nil {
				return err
			}
			if _, _, err := k.createRegionTx(ctx, tx, p, nr, "seed:region:"+f.ProvinceCode+":"+r.Code); err != nil {
				return err
			}
			res.RegionsCreated++
		}
		communes := make([]string, 0, len(f.Assignments))
		for _, a := range f.Assignments {
			communes = append(communes, a.CommuneCode)
		}
		nodes, err := store.ExtensionTeamUnits(ctx, tx, communes)
		if err != nil {
			return err
		}
		for _, a := range f.Assignments {
			ref, ok := nodes[a.CommuneCode]
			if !ok {
				res.MissingNode = append(res.MissingNode, a.CommuneCode)
				continue
			}
			o, err := store.OrgByRef(ctx, tx, ref, true)
			if err != nil {
				return err
			}
			if !o.Active {
				res.MissingNode = append(res.MissingNode, a.CommuneCode)
				continue
			}
			cur, err := store.OrgRegionByOrg(ctx, tx, ref, true)
			if err != nil {
				return err
			}
			if cur != nil && cur.RegionCode != nil {
				res.AlreadyAssigned++
				continue
			}
			code := a.RegionCode
			if _, err := k.writeOrgRegion(ctx, tx, p, o, f.ProvinceCode, domain.OrgRegionAssigned, &code, nil, nil,
				"seed:org-region:"+idem+":"+a.CommuneCode); err != nil {
				return err
			}
			res.Assigned++
		}
		return nil
	})
	return res, err
}
