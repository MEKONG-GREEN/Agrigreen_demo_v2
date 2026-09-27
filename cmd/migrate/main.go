// Lệnh migrate: dựng vai/database và chạy migration. Là job riêng, chạy TRƯỚC
// khi triển khai ứng dụng (mục 20).
//
//	migrate bootstrap   tạo/cập nhật vai và database — cần AG_PG_ADMIN_URL (quản trị cụm)
//	migrate up          áp migration cho mọi cụm bằng vai ag_migrator — cần AG_DB_URL
//	migrate verify      kiểm các view cổng (v_missing_immutability, v_missing_rls) phải rỗng
//
// Mật khẩu vai: AG_DB_PASSWORD_<VAI> hoặc AG_DB_APP_PASSWORD.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "dùng: migrate bootstrap|up|verify")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func need(k string) (string, error) {
	v := os.Getenv(k)
	if v == "" {
		return "", fmt.Errorf("thiếu biến môi trường %s", k)
	}
	return v, nil
}

func run(cmd string) error {
	ctx := context.Background()
	switch cmd {
	case "bootstrap":
		admin, err := need("AG_PG_ADMIN_URL")
		if err != nil {
			return err
		}
		return dbadmin.Bootstrap(ctx, admin, "")
	case "up":
		base, err := need("AG_DB_URL")
		if err != nil {
			return err
		}
		for _, c := range migrations.Clusters {
			if err := dbadmin.Migrate(base, c, ""); err != nil {
				return err
			}
			fmt.Println("đã áp migration:", dbadmin.DatabaseName(c, ""))
		}
		return nil
	case "verify":
		base, err := need("AG_DB_URL")
		if err != nil {
			return err
		}
		bad := 0
		for _, c := range migrations.Clusters {
			dsn, err := dbadmin.RoleURL(base, c, "", dbadmin.Migrator)
			if err != nil {
				return err
			}
			pool, err := db.Open(ctx, dsn, db.Options{AppName: "migrate-verify"})
			if err != nil {
				return err
			}
			for _, view := range []string{"shared.v_missing_immutability", "shared.v_missing_rls"} {
				rows, err := pool.Query(ctx, "SELECT schema_name, table_name, problem FROM "+view)
				if err != nil {
					pool.Close()
					return err
				}
				for rows.Next() {
					var s, t, p string
					if err := rows.Scan(&s, &t, &p); err != nil {
						rows.Close()
						pool.Close()
						return err
					}
					fmt.Printf("%s %s: %s.%s — %s\n", dbadmin.DatabaseName(c, ""), view, s, t, p)
					bad++
				}
				rows.Close()
			}
			pool.Close()
		}
		if bad > 0 {
			return fmt.Errorf("%d vi phạm", bad)
		}
		fmt.Println("cổng catalog: sạch")
		return nil
	}
	return fmt.Errorf("lệnh không hỗ trợ: %s", cmd)
}
