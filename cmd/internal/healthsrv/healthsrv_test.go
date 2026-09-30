package healthsrv

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// G4 (hạ tầng, mục 6b): /healthz luôn 200; /readyz 200 khi mọi kiểm tra đạt, 503 "<tên>_unavailable" khi một
// kiểm tra lỗi; -healthcheck trả mã thoát theo HTTP.
func TestHealthAndReadiness_G4(t *testing.T) {
	s := New("pf")
	var closed atomic.Bool
	s.Add("db", func(context.Context) error { return nil })
	s.Add("amqp", Closed(closed.Load))
	s.Info("consumers", 3)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	get := func(path string) (int, map[string]any) {
		r, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Body.Close() }()
		var m map[string]any
		_ = json.NewDecoder(r.Body).Decode(&m)
		return r.StatusCode, m
	}
	if c, m := get("/healthz"); c != 200 || m["status"] != "ok" || m["zone"] != "pf" {
		t.Fatalf("healthz: %d %v", c, m)
	}
	if c, m := get("/readyz"); c != 200 || m["status"] != "ready" || m["consumers"] == nil {
		t.Fatalf("readyz: %d %v", c, m)
	}
	closed.Store(true)
	if c, m := get("/readyz"); c != 503 || m["status"] != "amqp_unavailable" {
		t.Fatalf("readyz khi amqp đóng: %d %v", c, m)
	}
	if Healthcheck(srv.URL+"/healthz") != 0 || Healthcheck(srv.URL+"/readyz") != 1 || Healthcheck("http://127.0.0.1:1/healthz") != 1 {
		t.Fatal("healthcheck mã thoát")
	}
	s.Add("x", func(context.Context) error { return errors.New("x") })
	if c, _ := get("/readyz"); c != 503 {
		t.Fatal("kiểm tra lỗi phải 503")
	}
}

func TestStartListensOnAddr_G4(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	errc := make(chan error, 1)
	stop := New("mkg").Start(addr, ":0", nil, errc)
	defer stop()
	deadline := time.Now().Add(3 * time.Second)
	for Healthcheck("http://"+addr+"/healthz") != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("không nghe %s", addr)
		}
		select {
		case err := <-errc:
			t.Fatal(err)
		case <-time.After(50 * time.Millisecond):
		}
	}
}
