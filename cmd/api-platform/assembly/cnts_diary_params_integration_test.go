package assembly

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// KN-PARAMS-CNTS-DIARY (#84): gốc lắp ráp nối đăng ký KN cũ (giữ nguyên, đứng đầu) và đăng ký riêng tham số nhật ký CNTS
// (#85) vào pf/masterdata; hai đăng ký cùng nạp được (không trùng khóa, không xung đột tham số dựng sẵn).
func TestMasterdataParamRegistrationsIncludeCNTSDiary(t *testing.T) {
	regs := masterdataParamRegistrations()
	if len(regs) != 2 {
		t.Fatalf("muốn 2 đăng ký tham số (KN cũ + nhật ký CNTS): %d", len(regs))
	}
	if len(regs[0].Specs) != len(extension.MasterdataParams().Specs) || regs[0].Specs[0] != extension.MasterdataParams().Specs[0] {
		t.Fatal("đăng ký KN cũ phải giữ nguyên")
	}
	keys := map[string]bool{}
	for _, sp := range regs[1].Specs {
		keys[sp.Key] = true
	}
	if !keys[knport.ParamCNTSDiaryEditWindowDays] || !keys[knport.ParamCNTSDiaryGPSRetentionDays] {
		t.Fatalf("thiếu đăng ký nhật ký CNTS: %+v", regs[1].Specs)
	}
	// masterdata.New panic khi trùng/xung đột — dựng được là hợp lệ.
	_ = masterdata.New(nil, clock.System{}, masterdata.Deps{Params: regs, Catalogs: []mdport.CatalogRegistration{extension.MasterdataCatalogs()}})
}

// Trên DB thật: Build dựng pf/masterdata với đăng ký mới; nạp dữ liệu nền tenant → pf/extension đọc được hạn sửa/hạn lưu
// 7/365 qua Params; tenant chưa nạp → ErrParamNotFound (consumer dùng mặc định theo hợp đồng #85).
func TestBuildRegistersCNTSDiaryParams(t *testing.T) {
	env := pgtest.Get(t)
	mods, _, err := Build(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clock.System{}, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	tid, bare := ids.New(), ids.New()
	if _, err := mods.MasterData.Seed(context.Background(), tid); err != nil {
		t.Fatal(err)
	}
	now := clock.BusinessDay(clock.System{}.Now())
	for key, want := range map[string]string{knport.ParamCNTSDiaryEditWindowDays: "7", knport.ParamCNTSDiaryGPSRetentionDays: "365"} {
		v, err := mods.MasterData.Params().At(tenant.WithID(context.Background(), tid), key, now)
		if err != nil || v.Value.String() != want || v.Version != 1 || v.Unit != "ngày" {
			t.Fatalf("%s sau khi nạp dữ liệu nền: %+v %v", key, v, err)
		}
		if _, err := mods.MasterData.Params().At(tenant.WithID(context.Background(), bare), key, now); !errors.Is(err, params.ErrParamNotFound) {
			t.Fatalf("%s tenant chưa nạp: %v", key, err)
		}
	}
	// Tham số KN cũ vẫn nạp như trước.
	if v, err := mods.MasterData.Params().At(tenant.WithID(context.Background(), tid), knport.ParamCNTSLocationDistanceM, now); err != nil ||
		v.Value.String() != "200" {
		t.Fatalf("tham số KN cũ: %+v %v", v, err)
	}
}
