package db

type tx interface{ Exec(sql string) error }

// SetLocalRole: nơi duy nhất được viết SET LOCAL ROLE.
func SetLocalRole(t tx, role string) error { return t.Exec("SET LOCAL ROLE " + role) }

// Session: kể cả shared/db cũng không được SET ROLE mức phiên.
func Session(t tx) error { return t.Exec("SET ROLE r_x") } // want "ADR-0028"
