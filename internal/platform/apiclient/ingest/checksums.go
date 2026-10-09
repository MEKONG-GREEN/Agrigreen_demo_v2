package ingest

// Tổng kiểm theo NGÀY của luồng song phương (R-INT-10, ADR-0033 §4, ADR-0034; W5.7) — từ W5.6 trên SỔ SONG PHƯƠNG
// THẬT (pf_coop.bilateral_record qua port.BilateralLedger), không còn trên sổ đệm ingest_staging. Hình phản hồi giữ
// nguyên (integration.BilateralDayAlgorithm = "bilateral_day.v1").
//
// Ngày = ngày nghiệp vụ (giờ Việt Nam) của recorded_at mà sổ song phương ghi — cùng giá trị recorded_at của mục feed
// của cặp, nên bên đối tác tính lại được trên bản sao feed của mình. Mỗi ngày:
//
//	record_count = số bản ghi của hợp đồng trong ngày (mọi cặp của tenant bên gọi, hoặc một cặp)
//	sha256       = SHA-256 của các dòng "<business_key>\t<contract_version>\t<payload_sha256>\n" sắp theo
//	               (business_key theo byte, contract_version) — payload_sha256 = SHA-256 của JSON chuẩn bản ghi
//	               (integration.PayloadSHA256). Ngày không có bản ghi: 0 và SHA-256 của chuỗi rỗng.

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// ChecksumAlgorithm định danh công thức tổng kiểm ngày (trả trong phản hồi).
const ChecksumAlgorithm = integration.BilateralDayAlgorithm

// MaxChecksumDays là số ngày tối đa mỗi lời gọi.
const MaxChecksumDays = 31

// PairRecord là một bản ghi song phương tham gia tổng kiểm ngày.
type PairRecord = integration.BilateralDayRecord

// DayDigest tính (số bản ghi, sha256 hex) của một ngày theo ChecksumAlgorithm.
func DayDigest(recs []PairRecord) (int64, string) { return integration.BilateralDayDigest(recs) }

// DayChecksum là tổng kiểm một ngày.
type DayChecksum struct {
	Date        string
	RecordCount int64
	SHA256      string
}

// DailyChecksums tính tổng kiểm từng ngày [from, to] (ngày nghiệp vụ) của hợp đồng song phương contract trên sổ song
// phương, với mọi cặp của tenant đối tác partner (pair = Nil) hoặc một cặp.
func DailyChecksums(ctx context.Context, ledger port.BilateralLedger, partner uuid.UUID, contract string, pair uuid.UUID, from, to clock.Date) ([]DayChecksum, error) {
	if ledger == nil {
		return nil, port.ErrBilateralUnavailable
	}
	start, end := clock.StartOfBusinessDate(from), clock.StartOfBusinessDate(to).Add(24*time.Hour)
	recs, err := ledger.DayRecords(ctx, partner, contract, pair, start, end)
	if err != nil {
		return nil, err
	}
	byDay := map[string][]PairRecord{}
	for _, r := range recs {
		d := clock.BusinessDate(r.RecordedAt).String()
		byDay[d] = append(byDay[d], PairRecord{BusinessKey: r.BusinessKey, ContractVersion: r.ContractVersion, PayloadSHA256: r.PayloadSHA256})
	}
	var out []DayChecksum
	for t := start; t.Before(end); t = t.Add(24 * time.Hour) {
		d := clock.BusinessDate(t).String()
		n, sum := DayDigest(byDay[d])
		out = append(out, DayChecksum{Date: d, RecordCount: n, SHA256: sum})
	}
	return out, nil
}
