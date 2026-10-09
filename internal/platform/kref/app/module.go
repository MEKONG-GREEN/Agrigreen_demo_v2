// Package app là phần cài đặt của pf/kref (xem tài liệu gói gốc internal/platform/kref).
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: mọi giao dịch của module chạy SET LOCAL ROLE r_kref (ADR-0028). Module mới không có vai đăng
// nhập cũ <module>_app: pool là vai đăng nhập của đơn vị (app_worker), là thành viên r_kref.
var DBModule = db.Module("pf/kref")

// Consumer là tên consumer (hàng đợi + khóa inbox) của worker (khai ở port để outbox-publisher khai hàng đợi
// mà không ghép module).
const Consumer = port.Consumer

// Sự kiện worker tiêu thụ (schema ở contracts/events/, chủ là module nguồn).
const (
	EventOrgCreated        = port.EventOrgCreated
	EventOrgMoved          = port.EventOrgMoved
	EventOrgDeactivated    = port.EventOrgDeactivated
	EventOrgReactivated    = port.EventOrgReactivated
	EventAdminUnitChanged  = port.EventAdminUnitChanged
	EventShareGrantApprove = port.EventShareGrantApprove
	EventShareGrantRevoke  = port.EventShareGrantRevoke
)

// Events là danh sách sự kiện của consumer pf.kref.
var Events = port.Events

// ErrNotYetProjected: sự kiện tham chiếu Nút chưa có trong k_ref (sự kiện tới sai thứ tự, hoặc Nút có trước
// W2.5 khi chưa chạy Rebuild). Handler trả lỗi → sự kiện được giao lại; vẫn thiếu → DLQ (chạy Rebuild).
var ErrNotYetProjected = errors.New("kref: Nút chưa có trong k_ref")

// Module là pf/kref đã nối.
type Module struct {
	db  tenant.Beginner
	clk clock.Clock
}

// New tạo module trên pool vai đăng nhập của đơn vị (app_worker; kiểm thử: bất kỳ vai là thành viên r_kref).
func New(b tenant.Beginner, clk clock.Clock) *Module {
	return &Module{db: db.ForModule(b, DBModule), clk: clk}
}

// Handler là inbox.Handler của consumer pf.kref. inbox.Process đã mở giao dịch tenant (pool phải bọc
// db.ForModule(pool, DBModule) — cmd/worker đặt `module: kref.DBModule`).
func (m *Module) Handler() inbox.Handler {
	return func(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
		rel, err := m.apply(ctx, tx, env)
		if err != nil || rel == "" {
			return err
		}
		at := env.RecordedAt
		if at.IsZero() {
			at = env.OccurredAt
		}
		recordLag(ctx, rel, m.clk.Now().Sub(at))
		return nil
	}
}

// ---------------------------------------------------------------------------
// Payload (giải mã JSON theo contracts/events — không import port của module nguồn: pf/kref là lõi dưới cùng)
// ---------------------------------------------------------------------------

type orgSnapshotV1 struct {
	OrgRef        uuid.UUID  `json:"org_ref"`
	Kind          string     `json:"kind"`
	Code          string     `json:"code"`
	AdminUnitCode string     `json:"admin_unit_code"`
	ParentRef     *uuid.UUID `json:"parent_ref"`
	Active        *bool      `json:"active"`
	Seq           int64      `json:"seq"`
}

type adminUnitChangedV1 struct {
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	Level          string   `json:"level"`
	ParentCode     *string  `json:"parent_code"`
	ValidFrom      string   `json:"valid_from"`
	ValidTo        *string  `json:"valid_to"`
	SuccessorCodes []string `json:"successor_codes"`
}

type shareGrantV1 struct {
	GrantID   uuid.UUID `json:"grant_id"`
	Grantee   uuid.UUID `json:"grantee_tenant_id"`
	Kinds     []string  `json:"kinds"`
	ValidFrom time.Time `json:"valid_from"`
	ValidTo   time.Time `json:"valid_to"`
	Seq       int64     `json:"seq"`
	// Điều khoản v2 (W4.1) — tùy chọn, vắng ở cấp phép trước v2.
	GrantClass   string     `json:"grant_class"`
	ModuleTable  string     `json:"module_table"`
	ScopeNodeRef *uuid.UUID `json:"scope_node_ref"`
	PurposeCode  string     `json:"purpose_code"`
	BasisRef     string     `json:"basis_ref"`
}

func (p shareGrantV1) grant() port.ShareGrant {
	g := port.ShareGrant{GrantID: p.GrantID, Grantee: p.Grantee, Kinds: sortedStrings(p.Kinds), ValidFrom: p.ValidFrom, ValidTo: p.ValidTo,
		Seq: p.Seq, Class: p.GrantClass, ModuleTable: p.ModuleTable, Purpose: p.PurposeCode, Basis: p.BasisRef}
	if p.ScopeNodeRef != nil {
		g.ScopeNode = *p.ScopeNodeRef
	}
	return g
}

func badPayload(env outbox.Envelope, err error) error {
	return fmt.Errorf("kref: payload %s không hợp lệ: %w", env.EventType, err)
}

// apply áp một sự kiện; trả quan hệ bị đổi ("" = sự kiện không thuộc k_ref).
func (m *Module) apply(ctx context.Context, tx pgx.Tx, env outbox.Envelope) (string, error) {
	tid := env.TenantID
	switch env.EventType {
	case EventOrgCreated, EventOrgMoved, EventOrgDeactivated, EventOrgReactivated:
		var p orgSnapshotV1
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return "", badPayload(env, err)
		}
		if p.OrgRef == uuid.Nil || p.Seq < 1 {
			return "", badPayload(env, errors.New("thiếu org_ref/seq"))
		}
		if err := lockTenant(ctx, tx, tid); err != nil {
			return "", err
		}
		switch env.EventType {
		case EventOrgCreated, EventOrgMoved:
			if p.Kind == "" || p.Code == "" || p.Active == nil {
				return "", badPayload(env, errors.New("thiếu kind/code/active"))
			}
			o := port.Org{Ref: p.OrgRef, Kind: p.Kind, Code: p.Code, AdminUnitCode: p.AdminUnitCode, Active: *p.Active, Seq: p.Seq}
			if p.ParentRef != nil {
				o.ParentRef = *p.ParentRef
			}
			if err := applyOrgSnapshot(ctx, tx, tid, o); err != nil {
				return "", err
			}
			// Cây Nút đổi (Nút mới, nhánh chuyển) → dựng lại share_scope của mọi cấp phép của đơn vị chủ, cùng giao dịch.
			return port.RelNodeClosure, rebuildShareScope(ctx, tx, tid, uuid.Nil)
		default:
			return port.RelOrg, applyOrgStatus(ctx, tx, tid, p.OrgRef, env.EventType == EventOrgReactivated, p.Seq)
		}
	case EventAdminUnitChanged:
		var p adminUnitChangedV1
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return "", badPayload(env, err)
		}
		u, err := p.unit()
		if err != nil {
			return "", badPayload(env, err)
		}
		return port.RelAdminUnit, upsertAdminUnit(ctx, tx, tid, u)
	case EventShareGrantApprove, EventShareGrantRevoke:
		var p shareGrantV1
		if err := json.Unmarshal(env.Payload, &p); err != nil {
			return "", badPayload(env, err)
		}
		if p.GrantID == uuid.Nil || p.Seq < 1 {
			return "", badPayload(env, errors.New("thiếu grant_id/seq"))
		}
		if err := lockTenant(ctx, tx, tid); err != nil {
			return "", err
		}
		var err error
		if env.EventType == EventShareGrantRevoke {
			err = clearShareGrant(ctx, tx, tid, p.GrantID, p.Seq)
		} else {
			err = upsertShareGrant(ctx, tx, tid, p.grant())
		}
		if err != nil {
			return "", err
		}
		// share_scope của cấp phép dựng lại từ share_grant_active trong CÙNG giao dịch (W4.2): thu hồi xong là
		// đơn vị xem hết đọc được ở câu kế tiếp, kể cả trên kết nối tái sử dụng (biến app.* là SET LOCAL).
		return port.RelShareScope, rebuildShareScope(ctx, tx, tid, p.GrantID)
	}
	return "", nil
}

func (p adminUnitChangedV1) unit() (port.AdminUnit, error) {
	if p.Code == "" || p.Level == "" {
		return port.AdminUnit{}, errors.New("thiếu code/level")
	}
	from, err := time.Parse(time.DateOnly, p.ValidFrom)
	if err != nil {
		return port.AdminUnit{}, err
	}
	u := port.AdminUnit{Code: p.Code, ValidFrom: from, Name: p.Name, Level: p.Level, SuccessorCodes: sortedStrings(p.SuccessorCodes)}
	if p.ParentCode != nil {
		u.ParentCode = *p.ParentCode
	}
	if p.ValidTo != nil {
		to, err := time.Parse(time.DateOnly, *p.ValidTo)
		if err != nil {
			return port.AdminUnit{}, err
		}
		u.ValidTo = &to
	}
	return u, nil
}

// ---------------------------------------------------------------------------
// Áp cộng dồn
// ---------------------------------------------------------------------------

// lockTenant tuần tự hóa mọi lần ghi k_ref của một tenant (nhiều bản worker, Rebuild chạy song song).
func lockTenant(ctx context.Context, tx pgx.Tx, tid uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('k_ref:' || $1::text, 0))", tid.String())
	return err
}

func nullUUID(u uuid.UUID) *string {
	if u == uuid.Nil {
		return nil
	}
	s := u.String()
	return &s
}

func nullText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// applyOrgSnapshot áp ảnh chụp sau sự việc của một Nút (created/moved): thêm Nút mới kèm bao đóng, hoặc cập
// nhật Nút đã có (seq lớn hơn) và — khi đổi cha — dựng lại bao đóng của cả nhánh trong cùng giao dịch.
func applyOrgSnapshot(ctx context.Context, tx pgx.Tx, tid uuid.UUID, o port.Org) error {
	var curParent *uuid.UUID
	var curSeq int64
	err := tx.QueryRow(ctx, `SELECT parent_ref, source_seq FROM k_ref.org WHERE tenant_id = $1 AND ref = $2 FOR UPDATE`,
		tid, o.Ref).Scan(&curParent, &curSeq)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if exists && curSeq >= o.Seq {
		return nil // đã áp bản mới hơn (giao lại / sai thứ tự)
	}
	if o.ParentRef != uuid.Nil {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM k_ref.org WHERE tenant_id = $1 AND ref = $2)`, tid, o.ParentRef).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: cha %s của %s", ErrNotYetProjected, o.ParentRef, o.Ref)
		}
	}
	if !exists {
		if _, err := tx.Exec(ctx, `INSERT INTO k_ref.org (tenant_id, ref, kind, code, admin_unit_code, parent_ref, active, source_seq)
		  VALUES ($1, $2, $3, $4, $5, $6::uuid, $7, $8)`,
			tid, o.Ref, o.Kind, o.Code, nullText(o.AdminUnitCode), nullUUID(o.ParentRef), o.Active, o.Seq); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO k_ref.node_closure (tenant_id, ancestor, descendant, depth)
		  SELECT $1::uuid, $2::uuid, $2::uuid, 0
		  UNION ALL
		  SELECT $1::uuid, c.ancestor, $2::uuid, c.depth + 1 FROM k_ref.node_closure c
		   WHERE c.tenant_id = $1::uuid AND $3::uuid IS NOT NULL AND c.descendant = $3::uuid`,
			tid, o.Ref, nullUUID(o.ParentRef))
		return err
	}
	prev := uuid.Nil
	if curParent != nil {
		prev = *curParent
	}
	if prev != o.ParentRef && o.ParentRef != uuid.Nil {
		var inside bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM k_ref.node_closure
		    WHERE tenant_id = $1 AND ancestor = $2 AND descendant = $3)`, tid, o.Ref, o.ParentRef).Scan(&inside); err != nil {
			return err
		}
		if inside {
			return fmt.Errorf("kref: Nút %s không thể nằm dưới nhánh con %s của nó", o.Ref, o.ParentRef)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE k_ref.org SET kind = $3, code = $4, admin_unit_code = $5, parent_ref = $6::uuid, active = $7,
	    source_seq = $8, synced_at = now()
	  WHERE tenant_id = $1 AND ref = $2`,
		tid, o.Ref, o.Kind, o.Code, nullText(o.AdminUnitCode), nullUUID(o.ParentRef), o.Active, o.Seq); err != nil {
		return err
	}
	if prev == o.ParentRef {
		return nil
	}
	return rebuildSubtreeClosure(ctx, tx, tid, o.Ref, o.ParentRef)
}

// rebuildSubtreeClosure dựng lại bao đóng của nhánh gốc x sau khi x đổi cha sang parent (uuid.Nil = thành
// gốc): bỏ mọi cặp (tổ tiên ngoài nhánh, hậu duệ trong nhánh), thêm (tổ tiên của cha mới, hậu duệ trong nhánh).
// Cặp trong nội bộ nhánh giữ nguyên. Một giao dịch — không có trạng thái nửa vời (ADR-0029 §3).
func rebuildSubtreeClosure(ctx context.Context, tx pgx.Tx, tid, x, parent uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM k_ref.node_closure c
	  WHERE c.tenant_id = $1
	    AND c.descendant IN (SELECT s.descendant FROM k_ref.node_closure s WHERE s.tenant_id = $1 AND s.ancestor = $2)
	    AND c.ancestor NOT IN (SELECT s.descendant FROM k_ref.node_closure s WHERE s.tenant_id = $1 AND s.ancestor = $2)`,
		tid, x); err != nil {
		return err
	}
	if parent == uuid.Nil {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO k_ref.node_closure (tenant_id, ancestor, descendant, depth)
	  SELECT $1::uuid, a.ancestor, s.descendant, a.depth + s.depth + 1
	    FROM k_ref.node_closure a
	    JOIN k_ref.node_closure s ON s.tenant_id = $1 AND s.ancestor = $2
	   WHERE a.tenant_id = $1 AND a.descendant = $3`, tid, x, parent)
	return err
}

// applyOrgStatus áp vô hiệu hóa / kích hoạt lại (payload không mang cha: chỉ đổi trạng thái).
func applyOrgStatus(ctx context.Context, tx pgx.Tx, tid, ref uuid.UUID, active bool, seq int64) error {
	tag, err := tx.Exec(ctx, `UPDATE k_ref.org SET active = $3, source_seq = $4, synced_at = now()
	  WHERE tenant_id = $1 AND ref = $2 AND source_seq < $4`, tid, ref, active, seq)
	if err != nil || tag.RowsAffected() > 0 {
		return err
	}
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM k_ref.org WHERE tenant_id = $1 AND ref = $2)`, tid, ref).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotYetProjected, ref)
	}
	return nil // đã có bản mới hơn
}

// upsertAdminUnit thêm phiên bản hoặc ghi nhận đóng hiệu lực (valid_to + kế thừa). Đóng thắng mở: sự kiện
// "created" tới muộn không mở lại phiên bản đã đóng.
func upsertAdminUnit(ctx context.Context, tx pgx.Tx, tid uuid.UUID, u port.AdminUnit) error {
	var to *string
	if u.ValidTo != nil {
		s := u.ValidTo.Format(time.DateOnly)
		to = &s
	}
	_, err := tx.Exec(ctx, `INSERT INTO k_ref.admin_unit (tenant_id, code, valid_from, valid_to, name, level, parent_code, successor_codes)
	  VALUES ($1, $2, $3::date, $4::date, $5, $6, $7, $8::text[])
	  ON CONFLICT (tenant_id, code, valid_from) DO UPDATE SET
	    valid_to        = coalesce(EXCLUDED.valid_to, k_ref.admin_unit.valid_to),
	    successor_codes = CASE WHEN EXCLUDED.valid_to IS NOT NULL THEN EXCLUDED.successor_codes ELSE k_ref.admin_unit.successor_codes END,
	    name = EXCLUDED.name, level = EXCLUDED.level, parent_code = EXCLUDED.parent_code, synced_at = now()`,
		tid, u.Code, u.ValidFrom.Format(time.DateOnly), to, u.Name, u.Level, nullText(u.ParentCode), u.SuccessorCodes)
	return err
}

func upsertShareGrant(ctx context.Context, tx pgx.Tx, tid uuid.UUID, g port.ShareGrant) error {
	_, err := tx.Exec(ctx, `INSERT INTO k_ref.share_grant_active (tenant_id, grant_id, grantee_tenant_id, kinds, valid_from, valid_to, source_seq,
	    grant_class, module_table, scope_node_ref, purpose_code, basis_ref)
	  VALUES ($1, $2, $3, $4::text[], $5, $6, $7, $8, $9, $10::uuid, $11, $12)
	  ON CONFLICT (tenant_id, grant_id) DO UPDATE SET grantee_tenant_id = EXCLUDED.grantee_tenant_id, kinds = EXCLUDED.kinds,
	    valid_from = EXCLUDED.valid_from, valid_to = EXCLUDED.valid_to, source_seq = EXCLUDED.source_seq,
	    grant_class = EXCLUDED.grant_class, module_table = EXCLUDED.module_table, scope_node_ref = EXCLUDED.scope_node_ref,
	    purpose_code = EXCLUDED.purpose_code, basis_ref = EXCLUDED.basis_ref, synced_at = now()
	  WHERE k_ref.share_grant_active.source_seq < EXCLUDED.source_seq`,
		tid, g.GrantID, g.Grantee, g.Kinds, g.ValidFrom.UTC(), g.ValidTo.UTC(), g.Seq,
		nullText(g.Class), nullText(g.ModuleTable), nullUUID(g.ScopeNode), nullText(g.Purpose), nullText(g.Basis))
	return err
}

// shareTableArgs trả ánh xạ tên logic → khóa share_scope (modreg.ShareTable) dạng hai mảng song song.
func shareTableArgs() (logical, keys []string) {
	m := modreg.ShareTables()
	for l := range m {
		logical = append(logical, l)
	}
	slices.Sort(logical)
	for _, l := range logical {
		keys = append(keys, m[l])
	}
	return logical, keys
}

// rebuildShareScope dựng lại k_ref.share_scope của đơn vị chủ tid từ share_grant_active × node_closure (kiến
// trúc §17.3, ADR-0030 mục 3): mỗi cấp phép intra_platform đang hiệu lực sinh một dòng cho mỗi hậu duệ (kể cả
// chính nó) của scope_node_ref, khóa bảng = modreg.ShareTable(module_table). grant = uuid.Nil → mọi cấp phép
// của tid (cây Nút đổi, dựng lại toàn bộ). Gọi trong giao dịch đã khóa tenant (lockTenant): xóa + chèn là một
// đơn vị — đơn vị xem không bao giờ thấy trạng thái nửa vời. Cấp phép mà Nút phạm vi chưa được chiếu (sự kiện
// tới sai thứ tự) sinh 0 dòng (đóng) — sự kiện Nút đến sau dựng lại đủ. module_table không còn khai trong
// `shares` (module bỏ share) → 0 dòng (đóng).
func rebuildShareScope(ctx context.Context, tx pgx.Tx, tid, grant uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM k_ref.share_scope WHERE owner_tenant = $1 AND ($2::uuid IS NULL OR grant_id = $2::uuid)`,
		tid, nullUUID(grant)); err != nil {
		return err
	}
	logical, keys := shareTableArgs()
	_, err := tx.Exec(ctx, `INSERT INTO k_ref.share_scope (viewer_tenant, purpose, module_table, owner_tenant, node_id, grant_id, valid_from, valid_to)
	  SELECT g.grantee_tenant_id, g.purpose_code, m.key, g.tenant_id, c.descendant, g.grant_id, g.valid_from, g.valid_to
	    FROM k_ref.share_grant_active g
	    JOIN unnest($3::text[], $4::text[]) AS m(logical, key) ON m.logical = g.module_table
	    JOIN k_ref.node_closure c ON c.tenant_id = g.tenant_id AND c.ancestor = g.scope_node_ref
	   WHERE g.tenant_id = $1 AND g.grant_class = 'intra_platform' AND g.purpose_code IS NOT NULL
	     AND ($2::uuid IS NULL OR g.grant_id = $2::uuid)`,
		tid, nullUUID(grant), logical, keys)
	return err
}

// clearShareGrant bỏ cấp phép đã thu hồi khỏi bản chiếu (k_ref chỉ giữ cấp phép đang hiệu lực).
func clearShareGrant(ctx context.Context, tx pgx.Tx, tid, grant uuid.UUID, seq int64) error {
	_, err := tx.Exec(ctx, `DELETE FROM k_ref.share_grant_active WHERE tenant_id = $1 AND grant_id = $2 AND source_seq < $3`, tid, grant, seq)
	return err
}

// ---------------------------------------------------------------------------
// Dựng lại toàn bộ
// ---------------------------------------------------------------------------

// RebuildResult tổng kết một lần dựng lại.
type RebuildResult struct {
	Tenants int
	Rows    map[string]int // theo quan hệ, cộng mọi tenant
}

// Rebuild dựng lại k_ref của mọi tenant (src.Tenants) từ nguồn. Mỗi tenant một giao dịch: đọc nguồn trước
// (vai của module nguồn), kiểm cây (Project), rồi xóa + chèn dưới r_kref; bao đóng tính bằng truy vấn đệ quy
// trong DB. Lỗi ở một tenant dừng ngay (tenant đã dựng giữ kết quả, tenant lỗi giữ trạng thái cũ).
func (m *Module) Rebuild(ctx context.Context, src port.Sources) (RebuildResult, error) {
	if src.Orgs == nil || src.ShareGrants == nil || src.AdminUnits == nil || src.Tenants == nil {
		return RebuildResult{}, errors.New("kref: Rebuild cần đủ nguồn Orgs, ShareGrants, AdminUnits, Tenants")
	}
	tenants, err := src.Tenants(ctx)
	if err != nil {
		return RebuildResult{}, err
	}
	res := RebuildResult{Rows: map[string]int{}}
	for _, tid := range tenants {
		p, err := m.RebuildTenant(ctx, src, tid)
		if err != nil {
			return res, fmt.Errorf("kref: dựng lại tenant %s: %w", tid, err)
		}
		res.Tenants++
		for r, d := range p.Digests() {
			res.Rows[r] += d.Rows
		}
	}
	return res, nil
}

// Expected đọc nguồn của một tenant và trả bản chiếu mong đợi (tính trong Go, không chạm k_ref).
func Expected(ctx context.Context, src port.Sources, tid uuid.UUID) (Projection, error) {
	orgs, err := src.Orgs.OrgNodes(ctx, tid)
	if err != nil {
		return Projection{}, err
	}
	units, err := src.AdminUnits.AdminUnitVersions(ctx, tid)
	if err != nil {
		return Projection{}, err
	}
	grants, err := src.ShareGrants.ActiveShareGrants(ctx, tid)
	if err != nil {
		return Projection{}, err
	}
	return Project(orgs, units, grants)
}

// RebuildTenant dựng lại k_ref của một tenant; trả bản chiếu mong đợi đã dùng.
func (m *Module) RebuildTenant(ctx context.Context, src port.Sources, tid uuid.UUID) (Projection, error) {
	want, err := Expected(ctx, src, tid)
	if err != nil {
		return Projection{}, err
	}
	err = tenant.WithTenantTx(ctx, m.db, tid, func(tx pgx.Tx) error { return rebuildTenantTx(ctx, tx, tid, want) })
	return want, err
}

func rebuildTenantTx(ctx context.Context, tx pgx.Tx, tid uuid.UUID, p Projection) error {
	if err := lockTenant(ctx, tx, tid); err != nil {
		return err
	}
	// share_scope: dẫn xuất của share_grant_active × node_closure — dựng lại ở cuối (rebuildShareScope).
	for _, t := range []string{"k_ref.node_closure", "k_ref.org", "k_ref.admin_unit", "k_ref.share_grant_active"} {
		if _, err := tx.Exec(ctx, "DELETE FROM "+t+" WHERE tenant_id = $1", tid); err != nil {
			return err
		}
	}
	var refs, kinds, codes, units, parents []string
	var actives []bool
	var seqs []int64
	for _, o := range p.Orgs {
		refs, kinds, codes = append(refs, o.Ref.String()), append(kinds, o.Kind), append(codes, o.Code)
		units, parents = append(units, o.AdminUnitCode), append(parents, ref(o.ParentRef))
		actives, seqs = append(actives, o.Active), append(seqs, o.Seq)
	}
	if len(refs) > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO k_ref.org (tenant_id, ref, kind, code, admin_unit_code, parent_ref, active, source_seq)
		  SELECT $1::uuid, r::uuid, k, c, nullif(u, ''), nullif(p, '')::uuid, a, s
		    FROM unnest($2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::boolean[], $8::bigint[]) AS t(r, k, c, u, p, a, s)`,
			tid, refs, kinds, codes, units, parents, actives, seqs); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO k_ref.node_closure (tenant_id, ancestor, descendant, depth)
		  WITH RECURSIVE t(ancestor, descendant, depth) AS (
		    SELECT o.ref, o.ref, 0 FROM k_ref.org o WHERE o.tenant_id = $1
		    UNION ALL
		    SELECT o.parent_ref, t.descendant, t.depth + 1
		      FROM t JOIN k_ref.org o ON o.tenant_id = $1 AND o.ref = t.ancestor
		     WHERE o.parent_ref IS NOT NULL AND t.depth < 64)
		  SELECT $1::uuid, ancestor, descendant, depth FROM t`, tid); err != nil {
			return err
		}
	}
	var ucodes, froms, tos, names, levels, pcodes, succ []string
	for _, u := range p.AdminUnits {
		to := ""
		if u.ValidTo != nil {
			to = u.ValidTo.Format(time.DateOnly)
		}
		ucodes, froms, tos = append(ucodes, u.Code), append(froms, u.ValidFrom.Format(time.DateOnly)), append(tos, to)
		names, levels, pcodes = append(names, u.Name), append(levels, u.Level), append(pcodes, u.ParentCode)
		succ = append(succ, strings.Join(u.SuccessorCodes, ","))
	}
	if len(ucodes) > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO k_ref.admin_unit (tenant_id, code, valid_from, valid_to, name, level, parent_code, successor_codes)
		  SELECT $1::uuid, c, f::date, nullif(t, '')::date, n, l, nullif(p, ''), coalesce(string_to_array(nullif(s, ''), ','), '{}'::text[])
		    FROM unnest($2::text[], $3::text[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[]) AS x(c, f, t, n, l, p, s)`,
			tid, ucodes, froms, tos, names, levels, pcodes, succ); err != nil {
			return err
		}
	}
	for _, g := range p.ShareGrants {
		if err := upsertShareGrant(ctx, tx, tid, g); err != nil {
			return err
		}
	}
	return rebuildShareScope(ctx, tx, tid, uuid.Nil)
}

// ---------------------------------------------------------------------------
// Đọc lại bản chiếu (diễn tập, kiểm thử)
// ---------------------------------------------------------------------------

// Snapshot đọc toàn bộ k_ref của một tenant dưới vai r_kref.
func (m *Module) Snapshot(ctx context.Context, tid uuid.UUID) (Projection, error) {
	var p Projection
	err := tenant.WithTenantTx(ctx, m.db, tid, func(tx pgx.Tx) error {
		var e error
		p, e = snapshotTx(ctx, tx, tid)
		return e
	})
	return p, err
}

func snapshotTx(ctx context.Context, tx pgx.Tx, tid uuid.UUID) (Projection, error) {
	var p Projection
	rows, err := tx.Query(ctx, `SELECT ref, kind, code, coalesce(admin_unit_code, ''),
	    coalesce(parent_ref, '00000000-0000-0000-0000-000000000000'::uuid), active, source_seq
	  FROM k_ref.org WHERE tenant_id = $1`, tid)
	if err != nil {
		return p, err
	}
	p.Orgs, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (port.Org, error) {
		var o port.Org
		return o, r.Scan(&o.Ref, &o.Kind, &o.Code, &o.AdminUnitCode, &o.ParentRef, &o.Active, &o.Seq)
	})
	if err != nil {
		return p, err
	}
	rows, err = tx.Query(ctx, `SELECT ancestor, descendant, depth FROM k_ref.node_closure WHERE tenant_id = $1`, tid)
	if err != nil {
		return p, err
	}
	p.Closure, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ClosureRow, error) {
		var c ClosureRow
		return c, r.Scan(&c.Ancestor, &c.Descendant, &c.Depth)
	})
	if err != nil {
		return p, err
	}
	rows, err = tx.Query(ctx, `SELECT code, valid_from, valid_to, name, level, coalesce(parent_code, ''), successor_codes
	  FROM k_ref.admin_unit WHERE tenant_id = $1`, tid)
	if err != nil {
		return p, err
	}
	p.AdminUnits, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (port.AdminUnit, error) {
		var u port.AdminUnit
		return u, r.Scan(&u.Code, &u.ValidFrom, &u.ValidTo, &u.Name, &u.Level, &u.ParentCode, &u.SuccessorCodes)
	})
	if err != nil {
		return p, err
	}
	rows, err = tx.Query(ctx, `SELECT grant_id, grantee_tenant_id, kinds, valid_from, valid_to, source_seq,
	    coalesce(grant_class, ''), coalesce(module_table, ''), coalesce(scope_node_ref, '00000000-0000-0000-0000-000000000000'::uuid),
	    coalesce(purpose_code, ''), coalesce(basis_ref, '')
	  FROM k_ref.share_grant_active WHERE tenant_id = $1`, tid)
	if err != nil {
		return p, err
	}
	p.ShareGrants, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (port.ShareGrant, error) {
		var g port.ShareGrant
		return g, r.Scan(&g.GrantID, &g.Grantee, &g.Kinds, &g.ValidFrom, &g.ValidTo, &g.Seq, &g.Class, &g.ModuleTable, &g.ScopeNode,
			&g.Purpose, &g.Basis)
	})
	if err != nil {
		return p, err
	}
	rows, err = tx.Query(ctx, `SELECT viewer_tenant, purpose, module_table, node_id, grant_id, valid_from, valid_to
	  FROM k_ref.share_scope WHERE owner_tenant = $1`, tid)
	if err != nil {
		return p, err
	}
	p.ShareScope, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (port.ShareScopeRow, error) {
		var s port.ShareScopeRow
		return s, r.Scan(&s.Viewer, &s.Purpose, &s.ModuleTable, &s.Node, &s.Grant, &s.ValidFrom, &s.ValidTo)
	})
	if err != nil {
		return p, err
	}
	p.normalize()
	return p, nil
}

// ---------------------------------------------------------------------------
// Tìm tenant
// ---------------------------------------------------------------------------

// ProjectedTenants liệt kê tenant đang có dòng trong k_ref (pool ag_ops_jobs — BYPASSRLS, chỉ cột
// tenant_id, pf 000092): tenant đã mất hết nguồn vẫn được dựng lại (thành rỗng).
func ProjectedTenants(pool *pgxpool.Pool) func(ctx context.Context) ([]uuid.UUID, error) {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, `SELECT tenant_id FROM k_ref.org UNION SELECT tenant_id FROM k_ref.admin_unit
		  UNION SELECT tenant_id FROM k_ref.share_grant_active`)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
}

// UnionTenants gộp (không trùng, đã sắp) kết quả của nhiều hàm tìm tenant.
func UnionTenants(fns ...func(ctx context.Context) ([]uuid.UUID, error)) func(ctx context.Context) ([]uuid.UUID, error) {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		var out []uuid.UUID
		for _, f := range fns {
			ids, err := f(ctx)
			if err != nil {
				return nil, err
			}
			for _, id := range ids {
				if !slices.Contains(out, id) {
					out = append(out, id)
				}
			}
		}
		slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		return out, nil
	}
}
