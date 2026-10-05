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
	// ShareScopeRows: số dòng k_ref.share_scope có viewer_tenant = tenant. Chưa có worker dựng (W4.2) nên
	// bản chiếu mong đợi luôn 0.
	ShareScopeRows int
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
	}
	p.normalize()
	return p, nil
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
			g.ValidFrom.UTC().Format(time.RFC3339Nano), g.ValidTo.UTC().Format(time.RFC3339Nano)}, "|"))
	}
	scope := make([]string, p.ShareScopeRows)
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
