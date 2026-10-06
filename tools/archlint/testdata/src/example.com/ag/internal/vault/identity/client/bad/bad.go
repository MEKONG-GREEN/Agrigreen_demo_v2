// client/ của vault là hợp đồng: không kéo lớp trong hay driver DB (mục 8).
package bad

import (
	_ "example.com/ag/internal/vault/identity/store" // want "mục 8"
	_ "github.com/jackc/pgx/v5"                      // want "mục 8"
)
