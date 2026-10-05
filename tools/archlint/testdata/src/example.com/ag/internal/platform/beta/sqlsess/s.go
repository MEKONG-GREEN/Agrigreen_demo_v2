package sqlsess

type tx interface{ Exec(sql string) error }

func Bad(t tx) {
	_ = t.Exec("SET ROLE r_beta")                               // want "ADR-0028"
	_ = t.Exec("set search_path = pf_beta")                     // want "ADR-0028"
	_ = t.Exec(`SET statement_timeout TO '5s'`)                 // want "ADR-0028"
	_ = t.Exec("SET SESSION AUTHORIZATION r_beta")              // want "ADR-0028"
	_ = t.Exec("SET TIME ZONE 'UTC'")                           // want "ADR-0028"
	_ = t.Exec("RESET ROLE")                                    // want "ADR-0028"
	_ = t.Exec("SELECT 1; RESET ALL")                           // want "ADR-0028"
	_ = t.Exec("SELECT set_config('app.tenant_id', $1, false)") // want "set_config"
	_ = t.Exec("SET LOCAL ROLE r_beta")                         // want "R-DB-03"
}

func Good(t tx) {
	_ = t.Exec("SELECT set_config('app.tenant_id', $1, true)")
	_ = t.Exec("SET LOCAL statement_timeout = '5s'")
	_ = t.Exec("UPDATE pf_beta.x SET role = $1 WHERE id = $2")
	_ = t.Exec(`INSERT INTO pf_beta.x (a) VALUES ($1) ON CONFLICT (a) DO UPDATE SET role = excluded.role`)
}
