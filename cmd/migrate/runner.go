package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/clusterenv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/migrator"
)

// verifyRunner (W7.1, ADR-0038): đối chiếu trạng thái của hai bộ chạy trên từng cụm — phiên bản golang-migrate
// (schema_migrations_shared/_<cụm>) và sổ theo module (<schema>.schema_migrations). Không sửa gì. Lệch (mã thoát
// 1): tệp v2 sẽ áp mà v1 coi là đã áp (golang-migrate bỏ qua im lặng số nhỏ hơn), lệch tổng kiểm, dòng sổ không
// có tệp, ghi trùng, golang-migrate bẩn. Xem docs/runbooks/migrate-runner-v2.md.
func verifyRunner(ctx context.Context) error {
	base, err := need("AG_DB_URL")
	if err != nil {
		return err
	}
	clusters, err := clusterenv.Selected()
	if err != nil {
		return err
	}
	bad := 0
	for _, c := range clusters {
		dsn, err := dbadmin.RoleURL(clusterenv.BaseURL(c, base), c, "", dbadmin.Migrator)
		if err != nil {
			return err
		}
		st, err := migrator.Inspect(ctx, dsn, c, migrator.Options{})
		if err != nil {
			return fmt.Errorf("%s: %w", c, err)
		}
		db := dbadmin.DatabaseName(c, "")
		dirs := make([]string, 0, len(st.V1))
		for d := range st.V1 {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)
		for _, d := range dirs {
			v := st.V1[d]
			fmt.Printf("%s: golang-migrate %s: có=%v phiên bản=%d bẩn=%v\n", db, d, v.Exists, v.Version, v.Dirty)
		}
		schemas := map[string]int{}
		for _, r := range st.Rows {
			schemas[r.Schema]++
		}
		fmt.Printf("%s: sổ v2: %d dòng trong %d schema; kế hoạch %d tệp\n", db, len(st.Rows), len(schemas), len(st.Plan.Files))
		r := st.Recon
		if len(st.Rows) == 0 {
			fmt.Printf("%s: chưa chuyển sang v2 — lần chạy v2 đầu ghi sổ %d tệp đã áp (không chạy lại), áp %d tệp\n", db, len(r.Import), len(r.Apply))
		} else if len(r.Import) > 0 {
			fmt.Printf("%s: golang-migrate đã áp %d tệp sau lần v2 cuối — v2 sẽ ghi sổ, không chạy lại\n", db, len(r.Import))
		}
		for _, f := range r.Apply {
			fmt.Printf("%s: chờ áp %s (%s)\n", db, f.Key(), f.Owner)
		}
		for _, f := range migrator.SkippedByV1(r.Apply, st.V1) {
			bad++
			fmt.Printf("LỆCH %s: %s số %06d ≤ phiên bản golang-migrate %d — v1 bỏ qua im lặng, v2 sẽ áp\n", db, f.Key(), f.Version, st.V1[f.Dir].Version)
		}
		for _, k := range r.Drift {
			bad++
			fmt.Printf("LỆCH %s: %s tổng kiểm khác sổ (tệp đã áp bị sửa — R-24)\n", db, k)
		}
		for _, k := range r.Unknown {
			bad++
			fmt.Printf("LỆCH %s: dòng sổ %s không còn tệp\n", db, k)
		}
		for _, k := range r.Duplicate {
			bad++
			fmt.Printf("LỆCH %s: %s ghi ở hơn một sổ\n", db, k)
		}
		for _, p := range r.Problems {
			bad++
			fmt.Printf("LỆCH %s: %s\n", db, p)
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d lệch giữa bộ chạy v1 và v2", bad)
	}
	fmt.Println("bộ chạy migration: v1 và v2 khớp")
	return nil
}
