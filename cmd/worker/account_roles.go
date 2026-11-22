package main

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Every worker consumer/job uses the same trusted realm policy as the API.
func workerAccounts(db tenant.Beginner, clk clock.Clock, deps accounts.Deps) (*accounts.Module, error) {
	roles, err := assembly.AccountRoleCatalog()
	if err != nil {
		return nil, err
	}
	deps.Roles = roles
	deps.UnitDelegations, err = extension.UnitDelegationPolicy()
	if err != nil {
		return nil, err
	}
	return accounts.New(db, clk, deps), nil
}
