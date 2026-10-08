package main

import (
	"context"
	"log/slog"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// apiclientKeyJobs dựng pf/apiclient cho job apiclientport.JobKeyExpiry: pool r_apiclient nối
// thẳng cụm pf, tenant đơn vị vận hành từ AG_PUBLIC_OPERATOR_TENANT. Không cần pepper (job chỉ ghi
// sự kiện expired, không phát hành khóa).
func apiclientKeyJobs(ctx context.Context, direct string, closeFn *func()) (*apiclient.Module, error) {
	op, err := apiclient.OperatorTenantFromEnv()
	if err != nil {
		return nil, err
	}
	pool, err := openPF(ctx, direct, closeFn)
	if err != nil {
		return nil, err
	}
	return apiclient.New(pool, clock.System{}, apiclient.Deps{OperatorTenant: op, Log: slog.Default()}), nil
}
