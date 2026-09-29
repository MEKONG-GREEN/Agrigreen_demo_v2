package keycloak

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
)

// emulator giả lập các endpoint Keycloak Admin REST mà Client dùng (định dạng dây
// như Keycloak 26), lưu dữ liệu trong một Memory để kiểm thử hợp đồng hai bản.
type emulator struct {
	t    *testing.T
	mem  *Memory
	srv  *httptest.Server
	mu   sync.Mutex
	tok  string
	nTok int
}

const (
	emuRealm  = "agrigreen"
	emuClient = "agrigreen-accounts"
	emuSecret = "s3cr3t"
)

func newEmulator(t *testing.T) *emulator {
	e := &emulator{t: t, mem: NewMemory()}
	mux := http.NewServeMux()
	base := "/admin/realms/" + emuRealm
	mux.HandleFunc("POST /realms/"+emuRealm+"/protocol/openid-connect/token", e.token)
	mux.HandleFunc("GET "+base+"/users", e.auth(e.find))
	mux.HandleFunc("POST "+base+"/users", e.auth(e.create))
	mux.HandleFunc("GET "+base+"/users/{id}", e.auth(e.get))
	mux.HandleFunc("PUT "+base+"/users/{id}", e.auth(e.put))
	mux.HandleFunc("DELETE "+base+"/users/{id}", e.auth(e.del))
	mux.HandleFunc("GET "+base+"/users/{id}/role-mappings/realm", e.auth(e.roles))
	mux.HandleFunc("GET "+base+"/users/{id}/role-mappings/realm/available", e.auth(e.available))
	mux.HandleFunc("POST "+base+"/users/{id}/role-mappings/realm", e.auth(e.addRoles))
	mux.HandleFunc("DELETE "+base+"/users/{id}/role-mappings/realm", e.auth(e.delRoles))
	mux.HandleFunc("GET "+base+"/users/{id}/credentials", e.auth(e.creds))
	mux.HandleFunc("PUT "+base+"/users/{id}/execute-actions-email", e.auth(e.actions))
	mux.HandleFunc("POST "+base+"/users/{id}/logout", e.auth(e.logout))
	mux.HandleFunc("GET "+base+"/attack-detection/brute-force/users/{id}", e.auth(e.bruteForce))
	mux.HandleFunc("DELETE "+base+"/attack-detection/brute-force/users/{id}", e.auth(e.clearBruteForce))
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e
}

func (e *emulator) client(t *testing.T) *Client {
	c, err := New(Config{BaseURL: e.srv.URL, Realm: emuRealm, ClientID: emuClient, ClientSecret: emuSecret, HTTP: e.srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// revoke làm token hiện tại hết hiệu lực (Keycloak trả 401).
func (e *emulator) revoke() {
	e.mu.Lock()
	e.tok = ""
	e.mu.Unlock()
}

func (e *emulator) tokens() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.nTok
}

func (e *emulator) token(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	_ = r.ParseForm()
	if !ok || id != emuClient || secret != emuSecret || r.Form.Get("grant_type") != "client_credentials" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized_client"}`))
		return
	}
	e.mu.Lock()
	e.nTok++
	e.tok = "tok-" + strconv.Itoa(e.nTok)
	tok := e.tok
	e.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": tok, "expires_in": 300, "token_type": "Bearer"})
}

func (e *emulator) auth(h func(w http.ResponseWriter, r *http.Request, id uuid.UUID)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		want := e.tok
		e.mu.Unlock()
		if want == "" || r.Header.Get("Authorization") != "Bearer "+want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var id uuid.UUID
		if s := r.PathValue("id"); s != "" {
			var err error
			if id, err = uuid.Parse(s); err != nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
		}
		h(w, r, id)
	}
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, port.ErrUserNotFound), errors.Is(err, port.ErrRoleNotFound):
		w.WriteHeader(http.StatusNotFound)
	case errors.Is(err, port.ErrUserExists):
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"errorMessage":"User exists with same username"}`))
	case errors.Is(err, port.ErrEmailFailed):
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errorMessage":"Failed to send execute actions email"}`))
	default:
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func toRep(u port.User) userRep {
	attrs := map[string][]string{}
	if u.TenantID != uuid.Nil {
		attrs[port.AttrTenantID] = []string{u.TenantID.String()}
	}
	if u.OrgRef != uuid.Nil {
		attrs[port.AttrOrgRef] = []string{u.OrgRef.String()}
	}
	if u.Phone != "" {
		attrs[port.AttrPhone] = []string{u.Phone}
	}
	return userRep{
		ID: u.ID.String(), Username: u.Username, Email: u.Email, FirstName: u.FirstName, LastName: u.LastName,
		Enabled: u.Enabled, EmailVerified: u.EmailVerified, Attributes: attrs, RequiredActions: u.RequiredActions,
		CreatedTimestamp: u.CreatedAt.UnixMilli(),
	}
}

func (e *emulator) find(w http.ResponseWriter, r *http.Request, _ uuid.UUID) {
	q := port.Query{Attributes: map[string]string{}}
	v := r.URL.Query()
	for _, kv := range strings.Fields(v.Get("q")) {
		k, val, _ := strings.Cut(kv, ":")
		q.Attributes[k] = val
	}
	if v.Get("exact") == "true" {
		q.Username, q.Email = v.Get("username"), v.Get("email")
	}
	q.First, _ = strconv.Atoi(v.Get("first"))
	q.Max, _ = strconv.Atoi(v.Get("max"))
	users, err := e.mem.FindUsers(r.Context(), q)
	if err != nil {
		fail(w, err)
		return
	}
	reps := make([]userRep, 0, len(users))
	for _, u := range users {
		reps = append(reps, toRep(u))
	}
	_ = json.NewEncoder(w).Encode(reps)
}

func (e *emulator) create(w http.ResponseWriter, r *http.Request, _ uuid.UUID) {
	var rep userRep
	if err := json.NewDecoder(r.Body).Decode(&rep); err != nil || !rep.Enabled {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	nu := port.NewUser{Username: rep.Username, Email: rep.Email, FirstName: rep.FirstName, LastName: rep.LastName,
		Phone: first(rep.Attributes, port.AttrPhone), RequiredActions: rep.RequiredActions}
	nu.TenantID, _ = uuid.Parse(first(rep.Attributes, port.AttrTenantID))
	nu.OrgRef, _ = uuid.Parse(first(rep.Attributes, port.AttrOrgRef))
	id, err := e.mem.CreateUser(r.Context(), nu)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Location", e.srv.URL+"/admin/realms/"+emuRealm+"/users/"+id.String())
	w.WriteHeader(http.StatusCreated)
}

func (e *emulator) get(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	u, err := e.mem.GetUser(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(toRep(u))
}

// put nhận biểu diễn ĐẦY ĐỦ; thiếu username/email coi như lỗi (như user profile của Keycloak 26).
func (e *emulator) put(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var rep userRep
	if err := json.NewDecoder(r.Body).Decode(&rep); err != nil || rep.Username == "" || rep.Email == "" || first(rep.Attributes, port.AttrTenantID) == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	if err := e.mem.SetEnabled(ctx, id, rep.Enabled); err != nil {
		fail(w, err)
		return
	}
	org, _ := uuid.Parse(first(rep.Attributes, port.AttrOrgRef))
	_ = e.mem.SetOrgRef(ctx, id, org)
	_ = e.mem.AddRequiredActions(ctx, id, rep.RequiredActions)
	w.WriteHeader(http.StatusNoContent)
}

func (e *emulator) del(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if err := e.mem.DeleteUser(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func roleReps(names []string) []roleRep {
	out := make([]roleRep, 0, len(names))
	for _, n := range names {
		out = append(out, roleRep{ID: "id-" + n, Name: n})
	}
	return out
}

func (e *emulator) roles(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	have, err := e.mem.RealmRoles(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(roleReps(have))
}

func (e *emulator) available(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	have, err := e.mem.RealmRoles(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	var avail []string
	for _, n := range e.mem.realm {
		if !slices.Contains(have, n) {
			avail = append(avail, n)
		}
	}
	_ = json.NewEncoder(w).Encode(roleReps(avail))
}

func decodeRoles(r *http.Request) ([]string, bool) {
	var reps []roleRep
	if err := json.NewDecoder(r.Body).Decode(&reps); err != nil {
		return nil, false
	}
	var names []string
	for _, rr := range reps {
		if rr.ID != "id-"+rr.Name { // Keycloak cần id đúng của vai
			return nil, false
		}
		names = append(names, rr.Name)
	}
	return names, true
}

func (e *emulator) addRoles(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	names, ok := decodeRoles(r)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := e.mem.AddRealmRoles(r.Context(), id, names); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (e *emulator) delRoles(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	names, ok := decodeRoles(r)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := e.mem.RemoveRealmRoles(r.Context(), id, names); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (e *emulator) creds(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	has, err := e.mem.HasOTP(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	out := []map[string]string{{"type": "password"}}
	if has {
		out = append(out, map[string]string{"type": "otp"})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (e *emulator) actions(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var acts []string
	if err := json.NewDecoder(r.Body).Decode(&acts); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	life, _ := strconv.Atoi(r.URL.Query().Get("lifespan"))
	if err := e.mem.ExecuteActionsEmail(r.Context(), id, acts, secondsDur(life)); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (e *emulator) logout(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if err := e.mem.Logout(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// bruteForce: GET /attack-detection/brute-force/users/{id} như Keycloak 26 (enabled giữ nguyên).
func (e *emulator) bruteForce(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	locked, err := e.mem.BruteForceLocked(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"disabled": locked, "numFailures": 5, "lastIPFailure": "n/a"})
}

func (e *emulator) clearBruteForce(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if err := e.mem.ClearBruteForce(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
