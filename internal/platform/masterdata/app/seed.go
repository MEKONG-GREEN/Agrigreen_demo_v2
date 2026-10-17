package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/seeddata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// Tác nhân hệ thống của việc nạp dữ liệu nền. Không phải người: hai mã khác nhau
// để tham số nạp sẵn vẫn thỏa ràng buộc người duyệt ≠ người đặt (R-56) ở DB.
// Dữ liệu nền là giá trị đã có văn bản/BRD; muốn đổi thì đề xuất phiên bản mới qua
// quy trình duyệt hai người như mọi tham số khác.
var (
	SeedSetterRef   = uuid.MustParse("00000000-0000-7000-8000-00000000a001")
	SeedApproverRef = uuid.MustParse("00000000-0000-7000-8000-00000000a002")
)

// SeedResult tổng kết một lần nạp dữ liệu nền.
type SeedResult struct {
	Units   ImportResult `json:"admin_units"`
	Items   int          `json:"catalog_items_created"`
	Params  int          `json:"params_created"`
	Skipped int          `json:"params_skipped"`
}

type catalogFile struct {
	Format string `json:"format"`
	Source string `json:"source"`
	Items  []struct {
		Catalog string            `json:"catalog"`
		Code    string            `json:"code"`
		Name    string            `json:"name"`
		Attrs   map[string]string `json:"attrs"`
	} `json:"items"`
}

type paramFile struct {
	Format string           `json:"format"`
	Source string           `json:"source"`
	Params []port.ParamSeed `json:"params"`
}

func decodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// SeedForCaller nạp dữ liệu nền cho tenant của người gọi (Admin API; vai
// masterdata_admin hoặc platform_admin).
func (s *Service) SeedForCaller(ctx context.Context) (SeedResult, error) {
	p, err := authz.Require(ctx, port.RoleMasterdataAdmin, authz.RolePlatformAdmin)
	if err != nil {
		return SeedResult{}, err
	}
	return s.Seed(ctx, p.TenantID)
}

// Seed nạp dữ liệu nền nhúng (seeddata) cho tenant tid trong MỘT giao dịch: đơn vị
// hành chính ĐBSCL, danh mục mặc định, tham số CGH đã duyệt. Chạy lặp an toàn:
// đơn vị không đổi được bỏ qua, mục danh mục đã có giữ nguyên, khóa tham số đã có
// phiên bản nào thì không nạp nữa. Gốc lắp ráp gọi khi tạo tenant.
func (s *Service) Seed(ctx context.Context, tid uuid.UUID) (SeedResult, error) {
	var res SeedResult
	source, units, err := domain.ParseGazetteer(seeddata.AdminUnits)
	if err != nil {
		return res, fmt.Errorf("masterdata: dữ liệu nhúng admin_units lỗi: %w", err)
	}
	var cf catalogFile
	if err := decodeStrict(seeddata.Catalogs, &cf); err != nil {
		return res, fmt.Errorf("masterdata: dữ liệu nhúng catalogs lỗi: %w", err)
	}
	paramSeeds, err := s.parameterSeeds()
	if err != nil {
		return res, err
	}
	if cf.Format != domain.CatalogFormat {
		return res, fmt.Errorf("masterdata: dữ liệu nhúng sai định dạng (%q)", cf.Format)
	}
	now := s.Clock.Now()
	err = s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		if res.Units, e = s.importUnits(ctx, tx, tid, SeedSetterRef, source, units); e != nil {
			return e
		}
		for _, raw := range cf.Items {
			it, e := domain.Item{Catalog: raw.Catalog, Code: raw.Code, Name: raw.Name, Attrs: raw.Attrs}.Normalize()
			if e != nil {
				return fmt.Errorf("masterdata: mục nhúng %s/%s: %w", raw.Catalog, raw.Code, e)
			}
			if cur, e := store.Item(ctx, tx, it.Catalog, it.Code, true); e != nil {
				return e
			} else if cur != nil {
				continue
			}
			if domain.EffectiveDated(it.Catalog) {
				it.ValidFrom = SeedValidFrom
			}
			if e := checkRefs(ctx, tx, it, domain.DayOf(now)); e != nil {
				return e
			}
			it.Active, it.Revision, it.UpdatedBy, it.UpdatedAt = true, 1, SeedSetterRef, now
			c := &domain.Change{ID: ids.New(), Catalog: it.Catalog, Code: it.Code, Action: domain.ActionCreated, After: it.Snapshot(),
				Reason: "Nạp dữ liệu nền", EventTime: now, ActorRef: SeedSetterRef, IdemKey: "seed:catalog:" + it.Catalog + ":" + it.Code}
			if e := s.writeChange(ctx, tx, tid, c, it); e != nil {
				return e
			}
			res.Items++
		}
		for _, raw := range paramSeeds {
			created, e := s.seedParam(ctx, tx, tid, raw.Key, raw.Value, raw.Text, raw.Unit, raw.EffectiveFrom, raw.SourceDoc)
			if e != nil {
				return e
			}
			if created {
				res.Params++
			} else {
				res.Skipped++
			}
		}
		return nil
	})
	return res, err
}

func (s *Service) seedParam(ctx context.Context, tx pgx.Tx, tid uuid.UUID, key string, value, text *string, unit, eff, doc string) (bool, error) {
	if err := store.LockParamKey(ctx, tx, tid, key); err != nil {
		return false, err
	}
	if has, err := store.HasVersions(ctx, tx, key); err != nil || has {
		return false, err
	}
	effAt, err := domain.ParseInstant(eff)
	if err != nil {
		return false, fmt.Errorf("masterdata: tham số nhúng %s: effective_from: %w", key, err)
	}
	now := s.Clock.Now()
	approver := SeedApproverRef
	v := domain.ParamVersion{
		ID: ids.New(), TenantID: tid, Key: key, Version: 1, Text: text, Unit: strings.TrimSpace(unit), EffectiveFrom: effAt,
		SourceDoc: doc, Status: domain.StatusApproved, SetterRef: SeedSetterRef, SubmittedAt: &now, ApproverRef: &approver,
		DecidedAt: &now, IdemKey: "seed:param:" + key,
	}
	if value != nil {
		d, err := decimal.NewFromString(*value)
		if err != nil {
			return false, fmt.Errorf("masterdata: tham số nhúng %s: value: %w", key, err)
		}
		v.Value = &d
	}
	if err := s.validateParameterProposal(v); err != nil {
		return false, fmt.Errorf("masterdata: tham số nhúng %s: %w", key, err)
	}
	if err := store.InsertVersion(ctx, tx, &v); err != nil {
		return false, err
	}
	if _, err := s.recordApproved(ctx, tx, tid, v, nil, now); err != nil {
		return false, err
	}
	return true, nil
}
