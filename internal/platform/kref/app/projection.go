package app

// Phép chiếu thuần (không DB) từ nguồn sang k_ref — dùng làm bản dựng ĐỘC LẬP để so khi diễn tập
// (ADR-0029 §5: closure tính bằng Go đi theo con trỏ cha, còn Rebuild tính bằng truy vấn đệ quy trong DB) và
// làm khóa so sánh "dựng lại = cộng dồn" trong kiểm thử.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
)

// ClosureRow là một cặp (tổ tiên, hậu duệ, độ sâu) của k_ref.node_closure; (x, x, 0) cho chính Nút.
type ClosureRow struct {
	Ancestor   uuid.UUID
	Descendant uuid.UUID
	Depth      int
}

// Projection là nội dung k_ref của MỘT tenant (đã sắp theo khóa chính).
type Projection struct {
	Orgs        []port.Org
	Closure     []ClosureRow
	AdminUnits  []port.AdminUnit
	ShareGrants []port.ShareGrant
	// ShareScope: dòng k_ref.share_scope có owner_tenant = tenant (W4.2) — cấp phép intra_platform × hậu duệ
	// của Nút phạm vi (ShareScope tính trong Go, Rebuild/worker tính bằng SQL — so được khi diễn tập).
	ShareScope []port.ShareScopeRow
}

// Digest là số dòng và băm SHA-256 của một quan hệ.
type Digest struct {
	Rows int    `json:"rows"`
	Hash string `json:"sha256"`
}

// maxDepth chặn vòng (dữ liệu nguồn hỏng): cây thật có 3–4 cấp.
const maxDepth = 64

// Closure tính bao đóng của cây từ con trỏ cha. Nút có cha không thuộc tập → lỗi (nguồn thiếu Nút);
// vòng → lỗi.
func Closure(orgs []port.Org) ([]ClosureRow, error) {
	parent := make(map[uuid.UUID]uuid.UUID, len(orgs))
	for _, o := range orgs {
		parent[o.Ref] = o.ParentRef
	}
	var out []ClosureRow
	for _, o := range orgs {
		out = append(out, ClosureRow{Ancestor: o.Ref, Descendant: o.Ref, Depth: 0})
		cur, depth := o.ParentRef, 1
		for cur != uuid.Nil {
			if _, ok := parent[cur]; !ok {
				return nil, fmt.Errorf("kref: Nút %s có cha %s không thuộc cây", o.Ref, cur)
			}
			if depth > maxDepth || cur == o.Ref {
				return nil, fmt.Errorf("kref: vòng trong cây tại Nút %s", o.Ref)
			}
			out = append(out, ClosureRow{Ancestor: cur, Descendant: o.Ref, Depth: depth})
			cur, depth = parent[cur], depth+1
		}
	}
	sortClosure(out)
	return out, nil
}

// Project dựng bản chiếu mong đợi của một tenant từ dữ liệu nguồn.
func Project(orgs []port.Org, units []port.AdminUnit, grants []port.ShareGrant) (Projection, error) {
	cl, err := Closure(orgs)
	if err != nil {
		return Projection{}, err
	}
	p := Projection{
		Orgs: slices.Clone(orgs), Closure: cl, AdminUnits: slices.Clone(units), ShareGrants: slices.Clone(grants),
		ShareScope: ShareScope(grants, cl),
	}
	p.normalize()
	return p, nil
}

// ShareScope tính dòng k_ref.share_scope của một đơn vị chủ (ADR-0030 mục 3, kiến trúc §17.3): mỗi cấp phép
// intra_platform có mục đích và module_table khai trong `shares` (modreg.ShareTable) sinh một dòng cho mỗi hậu duệ
// (kể cả chính nó) của Nút phạm vi trong bao đóng cl. Cấp phép partner / trước v2 không sinh dòng.
func ShareScope(grants []port.ShareGrant, cl []ClosureRow) []port.ShareScopeRow {
	var out []port.ShareScopeRow
	for _, g := range grants {
		key, ok := modreg.ShareTable(g.ModuleTable)
		if g.Class != port.GrantClassIntraPlatform || g.Purpose == "" || g.ScopeNode == uuid.Nil || !ok {
			continue
		}
		for _, c := range cl {
			if c.Ancestor == g.ScopeNode {
				out = append(out, port.ShareScopeRow{Viewer: g.Grantee, Purpose: g.Purpose, ModuleTable: key, Node: c.Descendant,
					Grant: g.GrantID, ValidFrom: g.ValidFrom, ValidTo: g.ValidTo})
			}
		}
	}
	sortShareScope(out)
	return out
}

func sortShareScope(rows []port.ShareScopeRow) {
	slices.SortFunc(rows, func(a, b port.ShareScopeRow) int {
		return strings.Compare(shareScopeLine(a), shareScopeLine(b))
	})
}

func shareScopeLine(r port.ShareScopeRow) string {
	return strings.Join([]string{r.Viewer.String(), r.Purpose, r.ModuleTable, r.Node.String(), r.Grant.String(),
		r.ValidFrom.UTC().Format(time.RFC3339Nano), r.ValidTo.UTC().Format(time.RFC3339Nano)}, "|")
}

func sortClosure(rows []ClosureRow) {
	slices.SortFunc(rows, func(a, b ClosureRow) int {
		if c := strings.Compare(a.Ancestor.String(), b.Ancestor.String()); c != 0 {
			return c
		}
		return strings.Compare(a.Descendant.String(), b.Descendant.String())
	})
}

// normalize sắp mọi quan hệ theo khóa chính và chuẩn hóa mảng, để hai bản chiếu so được bằng băm.
func (p *Projection) normalize() {
	slices.SortFunc(p.Orgs, func(a, b port.Org) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	sortClosure(p.Closure)
	for i := range p.AdminUnits {
		p.AdminUnits[i].ValidFrom = dayUTC(p.AdminUnits[i].ValidFrom)
		if vt := p.AdminUnits[i].ValidTo; vt != nil {
			d := dayUTC(*vt)
			p.AdminUnits[i].ValidTo = &d
		}
		p.AdminUnits[i].SuccessorCodes = sortedStrings(p.AdminUnits[i].SuccessorCodes)
	}
	slices.SortFunc(p.AdminUnits, func(a, b port.AdminUnit) int {
		if c := strings.Compare(a.Code, b.Code); c != 0 {
			return c
		}
		return a.ValidFrom.Compare(b.ValidFrom)
	})
	for i := range p.ShareGrants {
		p.ShareGrants[i].Kinds = sortedStrings(p.ShareGrants[i].Kinds)
	}
	slices.SortFunc(p.ShareGrants, func(a, b port.ShareGrant) int { return strings.Compare(a.GrantID.String(), b.GrantID.String()) })
	sortShareScope(p.ShareScope)
}

func sortedStrings(xs []string) []string {
	out := slices.Clone(xs)
	if out == nil {
		out = []string{}
	}
	slices.Sort(out)
	return out
}

func dayUTC(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func ref(u uuid.UUID) string {
	if u == uuid.Nil {
		return ""
	}
	return u.String()
}

func digest(lines []string) Digest {
	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	return Digest{Rows: len(lines), Hash: hex.EncodeToString(h.Sum(nil))}
}

// Digests trả số dòng + băm theo quan hệ. Không băm seq/thời điểm đồng bộ: đó là siêu dữ liệu thứ tự, không
// phải nội dung (đổi tên tổ chức tăng seq ở sổ mà không đổi gì trong k_ref).
func (p Projection) Digests() map[string]Digest {
	q := p
	q.Orgs, q.Closure = slices.Clone(p.Orgs), slices.Clone(p.Closure)
	q.AdminUnits, q.ShareGrants = slices.Clone(p.AdminUnits), slices.Clone(p.ShareGrants)
	q.ShareScope = slices.Clone(p.ShareScope)
	q.normalize()
	var orgs, cl, units, grants []string
	for _, o := range q.Orgs {
		orgs = append(orgs, strings.Join([]string{o.Ref.String(), o.Kind, o.Code, o.AdminUnitCode, ref(o.ParentRef), fmt.Sprint(o.Active)}, "|"))
	}
	for _, c := range q.Closure {
		cl = append(cl, fmt.Sprintf("%s|%s|%d", c.Ancestor, c.Descendant, c.Depth))
	}
	for _, u := range q.AdminUnits {
		vt := ""
		if u.ValidTo != nil {
			vt = u.ValidTo.Format(time.DateOnly)
		}
		units = append(units, strings.Join([]string{u.Code, u.ValidFrom.Format(time.DateOnly), vt, u.Name, u.Level, u.ParentCode,
			strings.Join(u.SuccessorCodes, ",")}, "|"))
	}
	for _, g := range q.ShareGrants {
		grants = append(grants, strings.Join([]string{g.GrantID.String(), g.Grantee.String(), strings.Join(g.Kinds, ","),
			g.ValidFrom.UTC().Format(time.RFC3339Nano), g.ValidTo.UTC().Format(time.RFC3339Nano),
			g.Class, g.ModuleTable, ref(g.ScopeNode), g.Purpose, g.Basis}, "|"))
	}
	scope := make([]string, 0, len(q.ShareScope))
	for _, r := range q.ShareScope {
		scope = append(scope, shareScopeLine(r))
	}
	return map[string]Digest{
		port.RelOrg: digest(orgs), port.RelNodeClosure: digest(cl), port.RelAdminUnit: digest(units),
		port.RelShareGrantActive: digest(grants), port.RelShareScope: digest(scope),
	}
}

// Diff trả các quan hệ mà hai bản chiếu khác nhau (theo thứ tự port.Relations).
func Diff(a, b Projection) []string {
	da, db := a.Digests(), b.Digests()
	var out []string
	for _, r := range port.Relations {
		if da[r] != db[r] {
			out = append(out, r)
		}
	}
	return out
}
