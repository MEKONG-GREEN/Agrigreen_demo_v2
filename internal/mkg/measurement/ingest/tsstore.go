package ingest

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Lỗi ghi lô.
var (
	// ErrBatchConflict: đã có lô cùng (device_id, epoch, counter_from) nhưng khác payload_sha256 —
	// thiết bị hỏng hoặc bị giả (ADR-0019 §3): báo động, không ghi gì.
	ErrBatchConflict = errors.New("ingest: lô trùng khóa nhưng khác nội dung")
	// ErrCounterRegression: lô chồng lấn / lùi bộ đếm so với lô cuối của (device_id, epoch).
	ErrCounterRegression = errors.New("ingest: bộ đếm không đơn điệu")
)

// Stored là kết quả ghi một lô.
type Stored struct {
	BatchID    uuid.UUID
	CounterTo  int64
	ReceivedAt time.Time
	// Duplicate: gửi lặp idempotent — lô đã có, cùng payload_sha256, không ghi thêm.
	Duplicate bool
	// GapBefore: số bộ đếm bị thiếu giữa lô cuối trước đó và lô này (0 = liền mạch) — giám sát mất lô.
	GapBefore int64
}

// BatchStore ghi lô đã xác minh vào cụm ts (vai ingest_ts).
type BatchStore interface {
	Store(ctx context.Context, tenantID uuid.UUID, b edgewire.Batch, sha [32]byte, sig []byte) (Stored, error)
}

// WindowReader đọc số đo của một cửa sổ (vai ts_reader).
type WindowReader interface {
	ReadWindow(ctx context.Context, tenantID uuid.UUID, w port.Window) ([]WindowReading, error)
}

// TSStore hiện thực BatchStore + WindowReader trên cụm ts.
type TSStore struct {
	// Writer, Reader: pool cụm ts. cmd/ingest dựng bằng measurement.NewIngestTSStore (vai đăng nhập app_ingest,
	// mọi giao dịch SET LOCAL ROLE r_measurement — W2.6); kiểm thử có thể dùng vai cũ ingest_ts / ts_reader.
	Writer tenant.Beginner
	Reader tenant.Beginner // nil → dùng Writer
}

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// Store ghi reading_batch + raw_reading của lô trong MỘT giao dịch tenant (RLS).
// Idempotent theo (device_id, epoch, counter_from): lô đã có cùng payload_sha256 →
// Duplicate; khác → ErrBatchConflict; chồng lấn/lùi → ErrCounterRegression (trigger ts).
func (s *TSStore) Store(ctx context.Context, tenantID uuid.UUID, b edgewire.Batch, sha [32]byte, sig []byte) (Stored, error) {
	for attempt := 0; ; attempt++ {
		out, err := s.store(ctx, tenantID, b, sha, sig)
		// Hai bản gửi cùng lúc: bản sau vấp UNIQUE → đọc lại lô vừa commit.
		if pgCode(err) == "23505" && attempt == 0 {
			continue
		}
		return out, err
	}
}

func (s *TSStore) store(ctx context.Context, tenantID uuid.UUID, b edgewire.Batch, sha [32]byte, sig []byte) (Stored, error) {
	var out Stored
	err := tenant.WithTenantTx(ctx, s.Writer, tenantID, func(tx pgx.Tx) error {
		var oldSHA []byte
		err := tx.QueryRow(ctx, `SELECT id, payload_sha256, counter_to, received_at FROM ts_weigh.reading_batch
		  WHERE device_id = $1 AND epoch = $2 AND counter_from = $3`, b.DeviceID, b.Epoch, b.CounterFrom).
			Scan(&out.BatchID, &oldSHA, &out.CounterTo, &out.ReceivedAt)
		switch {
		case err == nil:
			if !bytes.Equal(oldSHA, sha[:]) || out.CounterTo != b.CounterTo {
				return ErrBatchConflict
			}
			out.Duplicate = true
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		var last *int64
		if err := tx.QueryRow(ctx, `SELECT max(counter_to) FROM ts_weigh.reading_batch WHERE device_id = $1 AND epoch = $2`,
			b.DeviceID, b.Epoch).Scan(&last); err != nil {
			return err
		}
		if last != nil && b.CounterFrom > *last+1 {
			out.GapBefore = b.CounterFrom - *last - 1
		} else if last == nil && b.CounterFrom > 1 {
			out.GapBefore = b.CounterFrom - 1
		}
		first, err := time.Parse(edgewire.DeviceTimeLayout, b.Readings[0].DeviceTime)
		if err != nil {
			return err
		}
		lastT, err := time.Parse(edgewire.DeviceTimeLayout, b.Readings[len(b.Readings)-1].DeviceTime)
		if err != nil {
			return err
		}
		out.BatchID, out.CounterTo = ids.New(), b.CounterTo
		if err := tx.QueryRow(ctx, `INSERT INTO ts_weigh.reading_batch
		  (id, tenant_id, device_id, epoch, counter_from, counter_to, reading_count, key_id, payload_sha256, signature,
		   device_time_first, device_time_last, ntp_offset_ms)
		  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING received_at`,
			out.BatchID, tenantID, b.DeviceID, b.Epoch, b.CounterFrom, b.CounterTo, len(b.Readings), b.KeyID, sha[:], sig,
			first, lastT, b.NTPOffsetMs).Scan(&out.ReceivedAt); err != nil {
			if pgCode(err) == "23514" && strings.Contains(err.Error(), "không đơn điệu") {
				return ErrCounterRegression
			}
			return err
		}
		n := len(b.Readings)
		counters, times, values, stables, units, raws := make([]int64, n), make([]time.Time, n), make([]string, n), make([]bool, n), make([]string, n), make([][]byte, n)
		for i, r := range b.Readings {
			t, err := time.Parse(edgewire.DeviceTimeLayout, r.DeviceTime)
			if err != nil {
				return err
			}
			counters[i], times[i], values[i], stables[i], units[i] = r.Counter, t, r.ValueKg, r.Stable, r.Unit
			if r.Raw != "" {
				if raws[i], err = base64.StdEncoding.DecodeString(r.Raw); err != nil {
					return err
				}
			}
		}
		tag, err := tx.Exec(ctx, `INSERT INTO ts_weigh.raw_reading
		  (tenant_id, batch_id, device_id, epoch, counter, device_time, value_kg, stable, unit, raw)
		  SELECT $1, $2, $3, $4, r.c, r.t, r.v::numeric(14,3), r.s, r.u, r.raw
		  FROM unnest($5::bigint[], $6::timestamptz[], $7::text[], $8::boolean[], $9::text[], $10::bytea[]) AS r(c, t, v, s, u, raw)`,
			tenantID, out.BatchID, b.DeviceID, b.Epoch, counters, times, values, stables, units, raws)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != int64(n) {
			return fmt.Errorf("ingest: ghi %d/%d số đo", tag.RowsAffected(), n)
		}
		return nil
	})
	return out, err
}

// ReadWindow đọc số đo của cửa sổ theo bộ đếm tăng dần, trong tenant (RLS): số đo
// của tenant khác không bao giờ lọt vào (thiếu → Gaps ở IntegrateV1).
func (s *TSStore) ReadWindow(ctx context.Context, tenantID uuid.UUID, w port.Window) ([]WindowReading, error) {
	pool := s.Reader
	if pool == nil {
		pool = s.Writer
	}
	if w.CounterTo-w.CounterFrom+1 > MaxWindowReadings {
		return nil, ErrWindowTooLarge
	}
	var out []WindowReading
	err := tenant.WithTenantTxOptions(ctx, pool, tenantID, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT counter, device_time, value_kg::text, stable, unit, raw, batch_id, received_at
		  FROM ts_weigh.raw_reading
		  WHERE device_id = $1 AND epoch = $2 AND counter BETWEEN $3 AND $4
		  ORDER BY counter, received_at`, w.DeviceID, w.Epoch, w.CounterFrom, w.CounterTo)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r WindowReading
			var v string
			if err := rows.Scan(&r.Counter, &r.DeviceTime, &v, &r.Stable, &r.Unit, &r.Raw, &r.BatchID, &r.ReceivedAt); err != nil {
				return err
			}
			if r.ValueKg, err = mass.ParseKg(v); err != nil {
				return err
			}
			r.DeviceTime, r.ReceivedAt = r.DeviceTime.UTC(), r.ReceivedAt.UTC()
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
