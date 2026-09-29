package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/store"
)

// addG3Sections thêm vào ảnh chụp (tại as_of, chỉ đọc sổ) các phần G3: liên kết
// nhật ký HTX, bằng chứng thu gom qua van, kết quả đối chiếu và ước tính CO2 kèm
// rule_version + phiên bản tham số. Chỉ khi bộ lọc không giới hạn loại minh chứng
// hay trạng thái duyệt (các bộ lọc đó thuộc minh chứng cán bộ); lọc theo phạm vi xã
// và mã vụ; khoảng event_from/event_to chỉ áp cho minh chứng cán bộ.
func addG3Sections(ctx context.Context, tx pgx.Tx, j domain.ExportJob, snap *domain.Snapshot) error {
	f := j.Filters
	if f.EvidenceType != "" || f.ReviewStatus != "" {
		return nil
	}
	asOf := j.RequestedAt
	seasons := map[uuid.UUID]*store.SeasonRow{}
	seasonCode := func(ref uuid.UUID) (string, error) {
		if s, ok := seasons[ref]; ok {
			if s == nil {
				return "", nil
			}
			return s.SeasonCode, nil
		}
		s, err := store.SeasonAt(ctx, tx, ref, &asOf)
		if err != nil {
			return "", err
		}
		seasons[ref] = s
		if s == nil {
			return "", nil
		}
		return s.SeasonCode, nil
	}
	keep := func(ref *uuid.UUID) (bool, error) {
		if f.SeasonCode == "" {
			return true, nil
		}
		if ref == nil {
			return false, nil
		}
		c, err := seasonCode(*ref)
		return c == f.SeasonCode, err
	}
	logs, err := store.FarmLogs(ctx, tx, store.FarmLogFilter{All: f.AllUnits, Units: f.AdminUnitCodes, AsOf: &asOf})
	if err != nil {
		return err
	}
	for _, l := range logs {
		if ok, err := keep(&l.SeasonRef); err != nil || !ok {
			if err != nil {
				return err
			}
			continue
		}
		snap.FarmLogs = append(snap.FarmLogs, domain.SnapshotFarmLog{
			LogRef: l.LogRef.String(), LogID: l.LogID.String(), LogVersion: l.LogVersion, SeasonRef: l.SeasonRef.String(),
			PlotRef: l.PlotRef.String(), CommuneCode: l.CommuneCode, ActivityCode: l.ActivityCode,
			LogEventTime: domain.FormatTime(l.LogEventTime), CatalogVersion: l.CatalogVersion, ReviewDecision: l.Decision,
			RowHash: store.HexHash(l.RowHash),
		})
	}
	cols, err := store.Collections(ctx, tx, store.CollectionFilter{All: f.AllUnits, Units: f.AdminUnitCodes, AsOf: &asOf})
	if err != nil {
		return err
	}
	for _, c := range cols {
		if ok, err := keep(c.SeasonRef); err != nil || !ok {
			if err != nil {
				return err
			}
			continue
		}
		snap.Collections = append(snap.Collections, domain.SnapshotCollection{
			EvidenceRef: c.EvidenceRef.String(), Revision: c.Revision, PlotRef: c.PlotRef.String(), SeasonRef: strPtr(c.SeasonRef),
			CollectedOn: c.CollectedOn.Format("2006-01-02"), Bucket: c.Bucket, BucketRule: c.BucketRule, RowHash: store.HexHash(c.RowHash),
		})
	}
	cmps, err := store.Comparisons(ctx, tx, store.ComparisonFilter{All: f.AllUnits, Units: f.AdminUnitCodes, AsOf: &asOf})
	if err != nil {
		return err
	}
	for _, c := range cmps {
		if ok, err := keep(&c.SeasonRef); err != nil || !ok {
			if err != nil {
				return err
			}
			continue
		}
		snap.Comparisons = append(snap.Comparisons, domain.SnapshotComparison{
			ID: c.ID.String(), EvidenceID: c.EvidenceID.String(), LogRef: c.LogRef.String(), LogID: c.LogID.String(),
			SeasonRef: c.SeasonRef.String(), Result: c.Result, Note: c.Note, ComparedBy: c.ActorRef.String(),
			EventTime: domain.FormatTime(c.EventTime), RowHash: store.HexHash(c.RowHash), SupersedesID: strPtr(c.SupersedesID),
		})
	}
	ests, err := store.Estimates(ctx, tx, store.EstimateFilter{All: f.AllUnits, Units: f.AdminUnitCodes, AsOf: &asOf})
	if err != nil {
		return err
	}
	for _, e := range ests {
		if f.SeasonCode != "" && e.SeasonCode != f.SeasonCode {
			continue
		}
		se := domain.SnapshotEstimate{
			ID: e.ID.String(), SeasonRef: e.SeasonRef.String(), PlotRef: e.PlotRef.String(), RuleVersion: e.RuleVersion,
			InputSHA256: e.InputSHA256, CountedTonnes: e.CountedTonnes.StringFixed(3), AvoidedBurningKgCO2e: e.AvoidedKg.StringFixed(3),
			TransportStatus: e.TransportStatus, Inputs: e.Inputs, Params: e.Params, Official: e.Official,
			MethodVersionID: strPtr(e.MethodVersionID), RowHash: store.HexHash(e.RowHash),
		}
		if e.TransportKg != nil {
			t := e.TransportKg.StringFixed(3)
			se.TransportKgCO2e = &t
		}
		snap.Estimates = append(snap.Estimates, se)
	}
	return nil
}
