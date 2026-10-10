package keycloak

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// Kiểu sự kiện đăng nhập đọc cho tab "Lịch sử đăng nhập" (FN-AD-04.04).
var loginEventTypes = []string{"LOGIN", "LOGIN_ERROR", "LOGOUT", "CODE_TO_TOKEN_ERROR"}

func msTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// UserSessions gọi GET /users/{id}/sessions (vai realm-management view-users).
func (c *Client) UserSessions(ctx context.Context, id uuid.UUID) ([]port.Session, error) {
	var reps []struct {
		ID         string            `json:"id"`
		IPAddress  string            `json:"ipAddress"`
		Start      int64             `json:"start"`
		LastAccess int64             `json:"lastAccess"`
		Clients    map[string]string `json:"clients"`
	}
	if _, err := c.do(ctx, "đọc phiên đăng nhập", http.MethodGet, userPath(id)+"/sessions", nil, nil, &reps); err != nil {
		return nil, err
	}
	out := make([]port.Session, 0, len(reps))
	for _, r := range reps {
		s := port.Session{ID: r.ID, IPAddress: r.IPAddress, Start: msTime(r.Start), LastAccess: msTime(r.LastAccess), Clients: []string{}}
		for _, cl := range r.Clients {
			s.Clients = append(s.Clients, cl)
		}
		slices.Sort(s.Clients)
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b port.Session) int { return b.LastAccess.Compare(a.LastAccess) })
	return out, nil
}

// LoginEvents gọi GET /events?user=…&type=… (vai realm-management view-events; realm eventsEnabled).
// Keycloak trả mới nhất trước.
func (c *Client) LoginEvents(ctx context.Context, id uuid.UUID, max int) ([]port.LoginEvent, error) {
	if max <= 0 || max > 200 {
		max = 50
	}
	q := url.Values{"user": {id.String()}, "max": {strconv.Itoa(max)}, "first": {"0"}}
	for _, t := range loginEventTypes {
		q.Add("type", t)
	}
	var reps []struct {
		Time      int64  `json:"time"`
		Type      string `json:"type"`
		ClientID  string `json:"clientId"`
		IPAddress string `json:"ipAddress"`
		Error     string `json:"error"`
	}
	if _, err := c.do(ctx, "đọc lịch sử đăng nhập", http.MethodGet, "/events", q, nil, &reps); err != nil {
		return nil, err
	}
	out := make([]port.LoginEvent, 0, len(reps))
	for _, r := range reps {
		out = append(out, port.LoginEvent{Time: msTime(r.Time), Type: r.Type, IPAddress: r.IPAddress, Client: r.ClientID, Error: r.Error})
	}
	return out, nil
}

var _ port.SessionReader = (*Client)(nil)

// ---------------------------------------------------------------------------
// Memory (kiểm thử, chạy thử tại chỗ)
// ---------------------------------------------------------------------------

// RecordLogin giả lập một lần đăng nhập: ok → thêm phiên + sự kiện LOGIN; sai → sự kiện LOGIN_ERROR.
func (m *Memory) RecordLogin(id uuid.UUID, ip, client string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	if m.sessions == nil {
		m.sessions = map[uuid.UUID][]port.Session{}
		m.events = map[uuid.UUID][]port.LoginEvent{}
	}
	if ok {
		m.sessions[id] = append(m.sessions[id], port.Session{ID: uuid.NewString(), IPAddress: ip, Start: now, LastAccess: now, Clients: []string{client}})
		m.events[id] = append(m.events[id], port.LoginEvent{Time: now, Type: "LOGIN", IPAddress: ip, Client: client})
		return
	}
	m.events[id] = append(m.events[id], port.LoginEvent{Time: now, Type: "LOGIN_ERROR", IPAddress: ip, Client: client, Error: "invalid_user_credentials"})
}

// UserSessions thực hiện port.SessionReader.
func (m *Memory) UserSessions(_ context.Context, id uuid.UUID) ([]port.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.get(id); err != nil {
		return nil, err
	}
	out := slices.Clone(m.sessions[id])
	slices.Reverse(out)
	return out, nil
}

// LoginEvents thực hiện port.SessionReader (mới nhất trước).
func (m *Memory) LoginEvents(_ context.Context, id uuid.UUID, max int) ([]port.LoginEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.get(id); err != nil {
		return nil, err
	}
	out := slices.Clone(m.events[id])
	slices.Reverse(out)
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out, nil
}

var _ port.SessionReader = (*Memory)(nil)
