// Bản giả cho kiểm thử: được đúc bằng chứng.
package porttest

import "example.com/ag/internal/shared/tenant"

func Grant(grantee, grantor [16]byte) tenant.Grant { return tenant.MintGrant(grantee, grantor) }
