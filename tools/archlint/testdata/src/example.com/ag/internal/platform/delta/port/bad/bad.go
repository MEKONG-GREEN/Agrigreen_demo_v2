// port/ chỉ interface + DTO (mục 8): domain/ thuần được phép; store/, app/, pgx thì không.
package bad

import (
	_ "example.com/ag/internal/platform/delta/app" // want "mục 8"
	_ "example.com/ag/internal/platform/delta/domain"
	_ "example.com/ag/internal/platform/delta/store" // want "mục 8"
	_ "example.com/ag/internal/shared/clock"
	_ "github.com/jackc/pgx/v5" // want "mục 8"
)
