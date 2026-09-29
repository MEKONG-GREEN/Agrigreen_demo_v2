package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Giới hạn trang pull.
const (
	DefaultPullLimit = 500
	MaxPullLimit     = 1000
)

// PullResult là một trang thay đổi chiều xuống.
type PullResult struct {
	Changes    []port.Change
	NextCursor string
	HasMore    bool
	ServerTime time.Time
	// Reset = true: phạm vi của người gọi đã đổi so với lúc phát con trỏ — pull
	// bắt đầu lại từ đầu; máy thay toàn bộ dữ liệu tham chiếu bằng kết quả mới.
	Reset bool
}

// Pull trả thay đổi chiều xuống theo phạm vi phân công của người gọi.
//
// Con trỏ là mờ, có chữ ký HMAC và gắn với (tenant, thiết bị): sửa hoặc dùng
// con trỏ của thiết bị khác → sync.cursor_invalid. Các entity chia đều hạn mức
// trang theo vòng (round-robin) để một entity nhiều thay đổi không chặn entity
// khác; phần hạn mức entity dùng không hết chia lại cho entity còn dữ liệu.
func (s *Service) Pull(ctx context.Context, deviceHeader, cursor string, limit int) (PullResult, error) {
	return s.PullEntitiesOnly(ctx, deviceHeader, cursor, limit, nil)
}

// PullEntitiesOnly như Pull nhưng chỉ đọc các entity trong only (rỗng = tất cả) — máy
// nhận lệnh sync_now kèm "entities" (KN US-SYNC-02: đồng bộ ngay một nhóm dữ liệu).
// Con trỏ giữ nguyên vị trí của entity không được đọc, nên lượt pull đầy đủ sau đó
// không mất thay đổi nào. Entity lạ → 422.
func (s *Service) PullEntitiesOnly(ctx context.Context, deviceHeader, cursor string, limit int, only []string) (PullResult, error) {
	p, err := principal(ctx)
	if err != nil {
		return PullResult{}, err
	}
	dev, err := s.checkDevice(ctx, p, deviceHeader)
	if err != nil {
		return PullResult{}, err
	}
	var filter map[string]bool
	if len(only) > 0 {
		known := s.PullEntities()
		filter = map[string]bool{}
		for _, e := range only {
			if !slices.Contains(known, e) {
				return PullResult{}, problem.Validation.WithField("entities", "không có entity "+e)
			}
			filter[e] = true
		}
	}
	if limit <= 0 {
		limit = DefaultPullLimit
	}
	if limit > MaxPullLimit {
		limit = MaxPullLimit
	}
	scope, err := s.scope(ctx)
	if err != nil {
		return PullResult{}, err
	}
	v := view{scope: scope}
	fp := domain.ScopeFingerprint(scope.All, scope.Units)
	if scope.Empty() && p.Has(accport.RoleFarmer) {
		// Nông dân: token không có org_ref → phạm vi pf/party rỗng. Đi đường riêng theo
		// nông hộ liên kết (chỉ entity của FarmerSource), không suy phạm vi xã.
		if v.farmer, err = s.farmerScope(ctx, p); err != nil {
			return PullResult{}, err
		}
		if v.farmer != nil {
			fp = farmerFingerprint(*v.farmer)
		}
	}
	now := s.Clock.Now()
	cur := domain.NewCursor(p.TenantID, dev.ID, fp)
	res := PullResult{ServerTime: now, Changes: []port.Change{}}
	if cursor != "" {
		c, err := domain.DecodeCursor(s.CursorKey, cursor)
		if err != nil || c.Tenant != p.TenantID || c.Device != dev.ID {
			return PullResult{}, ErrCursorInvalid
		}
		if c.Scope == fp {
			cur = c
		} else {
			res.Reset = true
		}
	}
	if !scope.Empty() || v.farmer != nil {
		res.Changes, res.HasMore, err = s.collect(ctx, v, &cur, limit, now, filter)
		if err != nil {
			return PullResult{}, err
		}
	}
	if res.NextCursor, err = cur.Encode(s.CursorKey); err != nil {
		return PullResult{}, err
	}
	return res, nil
}

// collect chạy vòng chia hạn mức. has_more = còn entity đã dùng đủ phần của mình
// (có thể còn dữ liệu) hoặc chưa được hỏi ở vòng cuối.
func (s *Service) collect(ctx context.Context, v view, cur *domain.Cursor, limit int, now time.Time, filter map[string]bool) ([]port.Change, bool, error) {
	out := []port.Change{}
	var active []pager
	for _, pg := range s.pagers {
		if filter == nil || filter[pg.entity()] {
			active = append(active, pg)
		}
	}
	remaining := limit
	for remaining > 0 && len(active) > 0 {
		share, extra := remaining/len(active), remaining%len(active)
		next := active[:0:0]
		for i, pg := range active {
			q := share
			if i < extra {
				q++
			}
			if q == 0 || remaining <= 0 {
				next = append(next, pg)
				continue
			}
			ch, exhausted, err := pg.fetch(ctx, v, cur, q, now)
			if err != nil {
				return nil, false, err
			}
			out = append(out, ch...)
			remaining -= len(ch)
			if !exhausted {
				next = append(next, pg)
			}
		}
		active = next
	}
	return out, len(active) > 0, nil
}

// view là phạm vi của một lượt pull: phạm vi cán bộ (scope, theo pf/party) hoặc phạm vi
// nông dân (farmer ≠ nil, theo nông hộ liên kết) — không bao giờ cả hai.
type view struct {
	scope  authz.Scope
	farmer *port.FarmerScope
}

// farmerScope tính phạm vi nông dân từ liên kết tài khoản ↔ nông hộ (pf/accounts). Chưa
// nối pf/accounts → nil (thiết bị nông dân không nhận gì — mặc định chặn).
func (s *Service) farmerScope(ctx context.Context, p authz.Principal) (*port.FarmerScope, error) {
	if s.Farmers == nil {
		return nil, nil
	}
	links, err := s.Farmers.HouseholdsForSubject(ctx, p.ActorRef)
	if err != nil {
		return nil, ErrDependency.WithDetail("không tra được nông hộ của tài khoản").WithCause(err)
	}
	fs := &port.FarmerScope{Households: []uuid.UUID{}, Cooperatives: []uuid.UUID{}, Communes: []string{}}
	for _, l := range links {
		if !slices.Contains(fs.Households, l.HouseholdRef) {
			fs.Households = append(fs.Households, l.HouseholdRef)
		}
		if l.CooperativeRef != uuid.Nil && !slices.Contains(fs.Cooperatives, l.CooperativeRef) {
			fs.Cooperatives = append(fs.Cooperatives, l.CooperativeRef)
		}
		if l.CommuneCode != "" && !slices.Contains(fs.Communes, l.CommuneCode) {
			fs.Communes = append(fs.Communes, l.CommuneCode)
		}
	}
	byStr := func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) }
	slices.SortFunc(fs.Households, byStr)
	slices.SortFunc(fs.Cooperatives, byStr)
	slices.Sort(fs.Communes)
	return fs, nil
}

// farmerFingerprint: đổi nông hộ liên kết (liên kết mới, hủy liên kết) → con trỏ cũ reset.
func farmerFingerprint(fs port.FarmerScope) string {
	keys := make([]string, 0, len(fs.Households)+len(fs.Cooperatives)+len(fs.Communes))
	for _, h := range fs.Households {
		keys = append(keys, "hh:"+h.String())
	}
	for _, c := range fs.Cooperatives {
		keys = append(keys, "htx:"+c.String())
	}
	for _, c := range fs.Communes {
		keys = append(keys, "xa:"+c)
	}
	return "farmer:" + domain.ScopeFingerprint(false, keys)
}

// pager đọc một entity chiều xuống, cập nhật vị trí trong cur.
type pager interface {
	entity() string
	// fetch trả tối đa limit thay đổi; exhausted = chắc chắn không còn gì ở trang này.
	fetch(ctx context.Context, v view, cur *domain.Cursor, limit int, now time.Time) ([]port.Change, bool, error)
}

type sourcePager struct {
	src    port.Source
	ent    string
	farmer bool // entity nằm trong FarmerEntities của Source
}

func (p sourcePager) entity() string { return p.ent }

func (p sourcePager) fetch(ctx context.Context, v view, cur *domain.Cursor, limit int, _ time.Time) ([]port.Change, bool, error) {
	after := cur.Last[p.ent]
	var (
		ch   []port.Change
		last int64
		err  error
	)
	if v.farmer != nil {
		if !p.farmer {
			return nil, true, nil // entity không phục vụ nông dân → đóng
		}
		ch, last, err = p.src.(port.FarmerSource).FarmerChanges(ctx, *v.farmer, p.ent, after, limit)
	} else {
		ch, last, err = p.src.Changes(ctx, v.scope, p.ent, after, limit)
	}
	var pr *problem.Problem
	if errors.As(err, &pr) && pr.Status == http.StatusForbidden {
		// Người gọi không có vai đọc entity này (vd nông dân với entity task): bỏ qua entity,
		// các entity khác vẫn đồng bộ — như pubkey.
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	for i := range ch {
		if ch[i].Entity == "" {
			ch[i].Entity = p.ent
		}
	}
	if len(ch) > 0 && last > after {
		cur.Last[p.ent] = last
	}
	return ch, len(ch) < limit, nil
}

// pubkeyPager giao khóa công khai X25519 của hộ thuộc địa bàn (entity pubkey).
//
// Kho định danh không có cursor tăng dần cho khóa, nên mỗi lượt giao TOÀN BỘ tập
// khóa của phạm vi, sắp theo ref; lượt mới bắt đầu khi dấu vân tay tập khóa đổi
// (hộ mới, hộ bị xóa định danh, phạm vi đổi). Kết thúc lượt là bản ghi đánh dấu
// id = 00000000-0000-0000-0000-000000000000, op = tombstone: máy xóa mọi khóa
// có version nhỏ hơn version của lượt. Phạm vi toàn quốc (All) không nhận khóa
// — thiết bị hiện trường luôn gắn một địa bàn cụ thể.
type pubkeyPager struct{ svc *Service }

func (pubkeyPager) entity() string { return port.EntityPubKey }

type pubkeyData struct {
	Ref       uuid.UUID `json:"ref"`
	AreaCode  string    `json:"area_code"`
	PublicKey []byte    `json:"public_key"` // base64 chuẩn
}

func (p pubkeyPager) fetch(ctx context.Context, v view, cur *domain.Cursor, limit int, now time.Time) ([]port.Change, bool, error) {
	if p.svc.Vault == nil {
		return nil, true, nil
	}
	var (
		keys []client.PubKey
		err  error
	)
	switch {
	case v.farmer != nil:
		// Nông dân: CHỈ khóa của các nông hộ liên kết (HTX US-LOG-01, R-40 — máy mã hóa ảnh
		// bằng khóa hộ mình), qua HouseholdPubKeys của kho (vai farmer, không cần vault_pubkey_sync).
		keys, err = householdKeys(ctx, p.svc.Vault, v.farmer.Households)
	case v.scope.All || len(v.scope.Units) == 0:
		return nil, true, nil
	default:
		keys, err = p.svc.Vault.PubKeys(ctx, v.scope.Units)
	}
	if err != nil {
		var pr *problem.Problem
		if errors.As(err, &pr) && pr.Status == http.StatusForbidden {
			return nil, true, nil // người gọi không có vai tải khóa — không chặn dữ liệu tham chiếu khác
		}
		return nil, false, err
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Ref.String() < keys[j].Ref.String() })
	fp := keysFingerprint(keys)
	pk := cur.PK
	if pk == nil || pk.FP != fp {
		ver := now.UnixMilli()
		if pk != nil && ver <= pk.Ver {
			ver = pk.Ver + 1
		}
		pk = &domain.PubKeyCursor{FP: fp, Ver: ver}
	}
	cur.PK = pk
	if pk.Done {
		return nil, true, nil
	}
	var out []port.Change
	for _, k := range keys {
		if len(out) == limit {
			return out, false, nil
		}
		if k.Ref.String() <= pk.After {
			continue
		}
		data, err := json.Marshal(pubkeyData{Ref: k.Ref, AreaCode: k.AreaCode, PublicKey: k.PublicKey})
		if err != nil {
			return nil, false, err
		}
		out = append(out, port.Change{Entity: port.EntityPubKey, ID: k.Ref, Version: pk.Ver, Op: port.OpUpsert, Data: data})
		pk.After = k.Ref.String()
	}
	if len(out) == limit {
		return out, false, nil
	}
	out = append(out, port.Change{Entity: port.EntityPubKey, ID: uuid.Nil, Version: pk.Ver, Op: port.OpTombstone})
	pk.Done = true
	return out, true, nil
}

// householdKeys đọc khóa theo lô MaxPubKeyRefs ref; không có nông hộ → tập rỗng (lượt
// vẫn kết thúc bằng bản ghi đánh dấu để máy xóa khóa cũ).
func householdKeys(ctx context.Context, vc client.Client, refs []uuid.UUID) ([]client.PubKey, error) {
	out := []client.PubKey{}
	for batch := range slices.Chunk(refs, client.MaxPubKeyRefs) {
		keys, err := vc.HouseholdPubKeys(ctx, batch)
		if err != nil {
			return nil, err
		}
		out = append(out, keys...)
	}
	return out, nil
}

func keysFingerprint(keys []client.PubKey) string {
	h := sha256.New()
	for _, k := range keys {
		h.Write(k.Ref[:])
		h.Write([]byte(k.AreaCode))
		h.Write([]byte{0})
		h.Write(k.PublicKey)
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}
