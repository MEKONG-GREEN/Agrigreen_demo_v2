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
	"time"

	"github.com/google/uuid"

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
	now := s.Clock.Now()
	fp := domain.ScopeFingerprint(scope.All, scope.Units)
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
	if !scope.Empty() {
		res.Changes, res.HasMore, err = s.collect(ctx, scope, &cur, limit, now, filter)
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
func (s *Service) collect(ctx context.Context, scope authz.Scope, cur *domain.Cursor, limit int, now time.Time, filter map[string]bool) ([]port.Change, bool, error) {
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
			ch, exhausted, err := pg.fetch(ctx, scope, cur, q, now)
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

// pager đọc một entity chiều xuống, cập nhật vị trí trong cur.
type pager interface {
	entity() string
	// fetch trả tối đa limit thay đổi; exhausted = chắc chắn không còn gì ở trang này.
	fetch(ctx context.Context, scope authz.Scope, cur *domain.Cursor, limit int, now time.Time) ([]port.Change, bool, error)
}

type sourcePager struct {
	src port.Source
	ent string
}

func (p sourcePager) entity() string { return p.ent }

func (p sourcePager) fetch(ctx context.Context, scope authz.Scope, cur *domain.Cursor, limit int, _ time.Time) ([]port.Change, bool, error) {
	after := cur.Last[p.ent]
	ch, last, err := p.src.Changes(ctx, scope, p.ent, after, limit)
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

func (p pubkeyPager) fetch(ctx context.Context, scope authz.Scope, cur *domain.Cursor, limit int, now time.Time) ([]port.Change, bool, error) {
	if p.svc.Vault == nil || scope.All || len(scope.Units) == 0 {
		return nil, true, nil
	}
	keys, err := p.svc.Vault.PubKeys(ctx, scope.Units)
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
