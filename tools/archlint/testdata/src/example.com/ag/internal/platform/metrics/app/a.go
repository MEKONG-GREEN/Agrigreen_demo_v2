package app

import "example.com/ag/internal/shared/db"

type tx interface{ Exec(sql string) error }

// Bộ chạy job tổng hợp đã khai được gọi WithAggTx.
func Run(t tx) error { return db.WithAggTx(t) }
