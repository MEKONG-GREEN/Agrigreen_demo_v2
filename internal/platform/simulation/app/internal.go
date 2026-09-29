package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// SnapshotBytes trả đúng byte ảnh chụp (JSON chuẩn) mà băm đã lưu tính trên đó —
// Internal API cho optimizer. Đọc lại từ jsonb rồi kiểm băm (toàn vẹn).
func (s *Service) SnapshotBytes(ctx context.Context, tid, simID uuid.UUID) ([]byte, string, error) {
	var raw []byte
	var sha string
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		stored, want, err := store.SnapshotPayload(ctx, tx, simID)
		if err != nil {
			return err
		}
		if stored == nil {
			return ErrSimulationNotFound.WithDetail("chưa có ảnh chụp")
		}
		_, b, err := domain.DecodeSnapshot(stored, want)
		if errors.Is(err, domain.ErrSnapshotTampered) {
			return ErrSnapshotIntegrity.WithCause(err)
		}
		raw, sha = b, want
		return err
	})
	return raw, sha, err
}

// Accepted là kết quả của một lần nộp.
type Accepted struct {
	Status   string // completed | failed
	Replayed bool   // đã nhận trước đó — trả lại trạng thái đã lưu (idempotent)
}

// AcceptResult nhận phong bì kết quả của optimizer (POST /internal/v1/simulations/{id}/result).
// Idempotent theo việc mô phỏng: nộp lại sau khi đã nhận → trả trạng thái đã lưu,
// không ghi thêm. Kết quả phải tính trên ĐÚNG ảnh chụp đã ghim (snapshot_sha256).
// "Chính thức được" do nền tảng quyết từ ảnh chụp (FN-01 BR-04), không tin optimizer.
func (s *Service) AcceptResult(ctx context.Context, tid, simID uuid.UUID, raw []byte) (Accepted, error) {
	env, err := domain.ParseEnvelope(raw)
	if err != nil {
		return Accepted{}, ErrResultInvalid.WithDetail(err.Error())
	}
	if env.SimulationID != nil && *env.SimulationID != simID.String() {
		return Accepted{}, ErrResultInvalid.WithField("simulation_id", "không khớp đường dẫn")
	}
	var summary domain.ResultSummary
	var sens domain.SensitivitySummary
	hasSens := len(env.Sensitivity) > 0 && string(env.Sensitivity) != "null"
	if env.Status == domain.StatusCompleted {
		if summary, err = domain.Summarize(env.Result); err != nil {
			return Accepted{}, ErrResultInvalid.WithDetail(err.Error())
		}
		if hasSens {
			if sens, err = domain.SummarizeSensitivity(env.Sensitivity); err != nil {
				return Accepted{}, ErrResultInvalid.WithDetail(err.Error())
			}
		}
	}
	sum := sha256.Sum256(raw)
	bodySHA := hex.EncodeToString(sum[:])
	var out Accepted
	err = s.tx(ctx, tid, func(tx pgx.Tx) error {
		sim, err := store.SimulationByID(ctx, tx, simID, true)
		if err != nil {
			return err
		}
		if sim == nil {
			return ErrSimulationNotFound
		}
		meta, err := store.SnapshotMetaBySimulation(ctx, tx, simID)
		if err != nil {
			return err
		}
		if meta == nil {
			return ErrNotAccepting.WithDetail("chưa có ảnh chụp")
		}
		if env.SnapshotSHA256 != meta.SHA256 {
			return ErrSnapshotMismatch
		}
		switch sim.Status {
		case domain.StatusCompleted, domain.StatusFailed:
			out = Accepted{Status: sim.Status, Replayed: true}
			return nil
		case domain.StatusRunning:
		default:
			return ErrNotAccepting.WithDetail("trạng thái: " + sim.Status)
		}
		now := s.now()
		if env.Status == domain.StatusFailed {
			code, detail := env.Error.Code, env.Error.Message
			if !strings.HasPrefix(code, "optimizer.") {
				code = "optimizer.failed"
			}
			if len(detail) > 500 {
				detail = detail[:500]
			}
			ch := store.Change{ID: ids.New(), TenantID: tid, ScenarioID: sim.ScenarioID, Kind: domain.ChangeSimulationFailed, SimulationID: &simID,
				EventTime: now, ActorRef: sim.RequestedBy, IdemKey: "failed:" + simID.String(), Payload: map[string]any{"error_code": code}}
			if err := store.AppendChange(ctx, tx, &ch); err != nil {
				return err
			}
			if err := store.Finish(ctx, tx, simID, domain.StatusFailed, &code, &detail, now); err != nil {
				return err
			}
			out = Accepted{Status: domain.StatusFailed}
			return enqueueCompleted(ctx, tx, *sim, domain.StatusFailed, &code, nil, false, now, "")
		}
		row := store.ResultRow{SimulationID: simID, SnapshotID: meta.ID, SnapshotSHA256: meta.SHA256, Payload: env.Result, PayloadSHA256: bodySHA,
			EngineVersion: env.EngineVersion, AlgorithmVersion: env.AlgorithmVersion, OfficialEligible: meta.AllParamsApproved, ReceivedAt: now}
		if err := store.InsertResult(ctx, tx, tid, row, summary); err != nil {
			return err
		}
		if hasSens {
			ss := sha256.Sum256(env.Sensitivity)
			if err := store.InsertSensitivity(ctx, tx, tid, simID, env.Sensitivity, hex.EncodeToString(ss[:]), sens, now); err != nil {
				return err
			}
		}
		ch := store.Change{ID: ids.New(), TenantID: tid, ScenarioID: sim.ScenarioID, Kind: domain.ChangeSimulationCompleted, SimulationID: &simID,
			EventTime: now, ActorRef: sim.RequestedBy, IdemKey: "result:" + simID.String(),
			Payload: map[string]any{"result_sha256": bodySHA, "engine_version": env.EngineVersion, "official_eligible": meta.AllParamsApproved}}
		if err := store.AppendChange(ctx, tx, &ch); err != nil {
			return err
		}
		if err := store.Finish(ctx, tx, simID, domain.StatusCompleted, nil, nil, now); err != nil {
			return err
		}
		out = Accepted{Status: domain.StatusCompleted}
		return enqueueCompleted(ctx, tx, *sim, domain.StatusCompleted, nil, &bodySHA, meta.AllParamsApproved, now, "")
	})
	return out, err
}
