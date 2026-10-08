package aggcall

import "example.com/ag/internal/shared/db"

type tx interface{ Exec(sql string) error }

// W6.4: ngoài shared/db không đổi vai bằng set_config, không nêu r_agg, không gọi WithAggTx.
func Bad(t tx) {
	_ = t.Exec("SELECT set_config('role', $1, true)") // want "R-DB-03"
	_ = t.Exec("GRANT r_agg TO app_x")                // want "R-DB-11"
	_ = db.WithAggTx(t)                               // want "R-DB-11"
}
