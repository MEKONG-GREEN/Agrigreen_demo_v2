package tenantuse

import (
	"context"
	"testing"

	_ "example.com/ag/internal/platform/party/port/porttest"
	"example.com/ag/internal/shared/tenant"
)

// Tệp kiểm thử được miễn.
func TestX(t *testing.T) {
	_ = tenant.WithID(context.Background(), [16]byte{1})
	_ = tenant.MintGrant([16]byte{1}, [16]byte{2})
	_ = withID
}
