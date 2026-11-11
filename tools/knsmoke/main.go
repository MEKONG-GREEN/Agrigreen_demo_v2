// Lệnh knsmoke dựng một tenant Khuyến nông MỚI trên stack đang chạy và kiểm khói "KN độc lập" (PLAN-KN-STANDALONE
// nguyên tắc 3, gói KN-STANDALONE-10B-PROFILE-CI): chỉ gọi Application API/Admin API thật bằng token của Keycloak realm
// kn — không SQL, không mock.
//
//  1. Tạo quản trị gốc của tenant mới trong realm kn, đăng nhập bằng mật khẩu + TOTP.
//  2. Seed qua Admin API: danh mục nền, cây TTKN, 5 phòng ban, tắt module ngoài KN, một tài khoản Cấp Tỉnh.
//  3. Đăng nhập tài khoản KN đó; kiểm module ngoài KN trả 403 module_not_enabled; với mỗi phòng: chọn ngữ cảnh, menu của
//     phòng trả về, vài FN đại diện đọc được.
//
// CHỈ dùng cho Local/Dev và CI (profile Compose kn — docs/dev/kn-standalone.md): cần client chỉ-dev kn-seed và tài khoản
// bootstrap của Keycloak. Mật khẩu, OTP, token không bao giờ được in.
//
//	KEYCLOAK_ADMIN_PASSWORD=… go run ./tools/knsmoke
//	-app-url (http://127.0.0.1:8080) -admin-url (http://127.0.0.1:8082) -kc-url (http://127.0.0.1:8180)
//	-kc-realm (kn) -kc-client (kn-seed) -province (91)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"
)

func run(args []string) error {
	fs := flag.NewFlagSet("knsmoke", flag.ContinueOnError)
	app := fs.String("app-url", "http://127.0.0.1:8080", "Application API")
	admin := fs.String("admin-url", "http://127.0.0.1:8082", "Admin API")
	kc := fs.String("kc-url", "http://127.0.0.1:8180", "Keycloak Local/CI")
	realm := fs.String("kc-realm", "kn", "realm của profile kn")
	client := fs.String("kc-client", "kn-seed", "client chỉ-dev có direct access grants")
	province := fs.String("province", "91", "mã tỉnh của TTKN tỉnh")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if env := os.Getenv("AG_ENV"); env == "production" || env == "prod" {
		return errors.New("knsmoke: AG_ENV=production — từ chối")
	}
	u, err := url.Parse(*kc)
	if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		return errors.New("knsmoke: chỉ chạy với Keycloak Local/CI (localhost)")
	}
	pw := os.Getenv("KEYCLOAK_ADMIN_PASSWORD")
	if pw == "" {
		return errors.New("knsmoke: thiếu KEYCLOAK_ADMIN_PASSWORD (deploy/compose/.env)")
	}
	hc := &http.Client{Timeout: 60 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return Run(ctx, Config{AppURL: *app, AdminURL: *admin, HTTP: hc, Out: os.Stdout, Province: *province,
		ID: &keycloakID{base: *kc, realm: *realm, client: *client, adminPassword: pw, hc: hc}})
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
	fmt.Println("PASS: kiểm khói KN độc lập")
}
