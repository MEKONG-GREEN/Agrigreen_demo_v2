package ingest

// Tổng kiểm theo NGÀY của luồng song phương (R-INT-10, ADR-0033 §4, ADR-0034; W5.7) — tạm trên sổ đệm
// ingest_staging cho tới khi W5.6 nối sổ song phương thật (pf_coop/pf_cropcycle) và feed của cặp.
//
// Ngày = ngày nghiệp vụ (giờ Việt Nam) lúc cổng NHẬN lô (event_time). Mỗi ngày:
//
//	record_count = số bản ghi đã nhận của hợp đồng trong ngày (tenant của bên gọi)
//	sha256       = SHA-256 của các dòng "<business_key>\t<contract_version>\t<payload_sha256>\n" sắp theo
//	               (business_key theo byte, contract_version) — payload_sha256 = SHA-256 của JSON chuẩn bản ghi
//	               (khớp sổ đệm). Ngày không có bản ghi: 0 và SHA-256 của chuỗi rỗng.
//
// Bên kia của cặp tính cùng công thức trên bản ghi của mình để đối soát.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ChecksumAlgorithm định danh công thức tổng kiểm ngày (trả trong phản hồi).
const ChecksumAlgorithm = "bilateral_day.v1"

// MaxChecksumDays là số ngày tối đa mỗi lời gọi.
const MaxChecksumDays = 31

// PairRecord là một bản ghi song phương tham gia tổng kiểm ngày.
type PairRecord struct {
	BusinessKey     string
	ContractVersion int
	PayloadSHA256   string
}

// DayDigest tính (số bản ghi, sha256 hex) của một ngày theo ChecksumAlgorithm.
func DayDigest(recs []PairRecord) (int64, string) {
	rs := append([]PairRecord(nil), recs...)
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].BusinessKey != rs[j].BusinessKey {
			return rs[i].BusinessKey < rs[j].BusinessKey
		}
		return rs[i].ContractVersion < rs[j].ContractVersion
	})
	h := sha256.New()
	for _, r := range rs {
		h.Write([]byte(r.BusinessKey + "\t" + strconv.Itoa(r.ContractVersion) + "\t" + r.PayloadSHA256 + "\n"))
	}
	return int64(len(rs)), hex.EncodeToString(h.Sum(nil))
}

// DayChecksum là tổng kiểm một ngày.
type DayChecksum struct {
	Date        string
	RecordCount int64
	SHA256      string
}

// DailyChecksums tính tổng kiểm từng ngày [from, to] (ngày nghiệp vụ) của hợp đồng song phương contract trên sổ
// đệm của tenant đối tác partner (RLS). db là pool bọc vai r_apiclient.
func DailyChecksums(ctx context.Context, db tenant.Beginner, partner uuid.UUID, contract string, from, to clock.Date) ([]DayChecksum, error) {
	start, end := clock.StartOfBusinessDate(from), clock.StartOfBusinessDate(to).Add(24*time.Hour)
	byDay := map[string][]PairRecord{}
	err := tenant.WithTenantTx(ctx, db, partner, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT business_key, contract_version, payload_sha256, event_time FROM pf_apiclient.ingest_staging
		  WHERE contract = $1::text AND event_time >= $2::timestamptz AND event_time < $3::timestamptz`, contract, start, end)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				r  PairRecord
				at time.Time
			)
			if err := rows.Scan(&r.BusinessKey, &r.ContractVersion, &r.PayloadSHA256, &at); err != nil {
				return err
			}
			d := clock.BusinessDate(at).String()
			byDay[d] = append(byDay[d], r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	var out []DayChecksum
	for t := start; t.Before(end); t = t.Add(24 * time.Hour) {
		d := clock.BusinessDate(t).String()
		n, sum := DayDigest(byDay[d])
		out = append(out, DayChecksum{Date: d, RecordCount: n, SHA256: sum})
	}
	return out, nil
}
