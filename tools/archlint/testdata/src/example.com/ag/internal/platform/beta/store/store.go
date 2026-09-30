package store

type tx interface{ Exec(sql string) error }

func RetirePlot(t tx) error {
	return t.Exec("DELETE FROM pf_geo.plot WHERE id = $1") // want "ADR-0011"
}

func Wipe(t tx) error {
	return t.Exec(`TRUNCATE pf_geo.plot`) // want "ADR-0011"
}

func Retire(t tx) error {
	return t.Exec("UPDATE pf_geo.plot_current SET status = 'retired'")
}

// RebuildCurrent dựng lại bảng chiếu từ sổ (R-22): được xóa bảng chiếu.
func RebuildCurrent(t tx) error {
	return t.Exec("DELETE FROM pf_geo.plot_current")
}

func ClearProjections(t tx) error {
	for _, n := range []string{"a_current", "b_summary"} {
		if err := t.Exec("DELETE FROM pf_geo." + n); err != nil {
			return err
		}
	}
	return nil
}
