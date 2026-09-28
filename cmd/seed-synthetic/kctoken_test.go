package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Client chỉ-dev agrigreen-seed: password grant kèm OTP; lỗi không lộ thông tin đăng nhập.
func TestFetchTokenPasswordGrantWithOTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "password" || r.Form.Get("client_id") != "agrigreen-seed" || r.Form.Get("otp") != "123456" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"tok-1","expires_in":300}`))
	}))
	defer srv.Close()
	l := KCLogin{TokenURL: srv.URL, Username: "dev-admin", Password: "bi-mat-khong-in", OTP: "123456"}
	tok, err := fetchToken(context.Background(), srv.Client(), l)
	if err != nil || tok != "tok-1" {
		t.Fatalf("token: %q %v", tok, err)
	}
	l.OTP = "000000"
	_, err = fetchToken(context.Background(), srv.Client(), l)
	if err == nil || strings.Contains(err.Error(), "bi-mat-khong-in") || strings.Contains(err.Error(), "dev-admin") {
		t.Fatalf("lỗi phải có mã OAuth và không chứa thông tin đăng nhập: %v", err)
	}
}
