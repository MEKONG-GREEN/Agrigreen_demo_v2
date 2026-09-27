// Package migrations nhúng các tệp migration SQL viết tay (R-23).
//
// Thứ tự áp cho mỗi database: shared/ trước (bảng migration riêng
// schema_migrations_shared), rồi thư mục của cụm (pf/, mkg/, vault/).
package migrations

import "embed"

// FS chứa mọi tệp *.sql theo thư mục cụm.
//
//go:embed shared/*.sql pf/*.sql mkg/*.sql vault/*.sql
var FS embed.FS

// Cluster là một database đích.
type Cluster string

// Các cụm của ĐTK 1. Cụm ts (chuỗi thời gian) bổ sung ở G3.
const (
	PF    Cluster = "pf"
	MKG   Cluster = "mkg"
	Vault Cluster = "vault"
)

// Clusters liệt kê các cụm theo thứ tự áp.
var Clusters = []Cluster{PF, MKG, Vault}
