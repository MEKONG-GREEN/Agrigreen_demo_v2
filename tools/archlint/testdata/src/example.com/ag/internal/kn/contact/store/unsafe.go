package store

func Remove() string       { return "DELETE FROM pf_contact.card" } // want "RB-1/ADR-0011: không xóa cứng"
func RebuildCards() string { return "DELETE FROM pf_contact.card" }
