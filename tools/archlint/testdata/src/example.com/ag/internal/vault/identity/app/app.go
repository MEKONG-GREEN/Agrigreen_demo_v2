// Vault là vùng chủ quyền riêng: không import platform/ hay mkg/ (mục 4, RB-3).
package app

import (
	_ "example.com/ag/internal/mkg/gamma/port"     // want "RB-3"
	_ "example.com/ag/internal/platform/beta/port" // want "RB-3"
	_ "example.com/ag/internal/shared/clock"
	_ "example.com/ag/internal/vault/identity/store"
)
