package ingest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// RuleV1 là phiên bản thuật toán tích phân TẠM (g3-conventions §8, chờ mẫu cân C-6):
// tịnh = trung vị các số đo `stable` của cửa sổ − bì. Ghim vào phiếu (R-70).
const RuleV1 = "weigh.integrate.v1"

// WindowDigestFormat là dòng đầu của dạng chuẩn cửa sổ (tách miền băm với lô edge).
const WindowDigestFormat = "agrigreen.weigh.window.v1"

// Lỗi tích phân (ngoài port.ErrWindowIncomplete).
var (
	// ErrNoStableReadings: cửa sổ đủ bộ đếm nhưng không có số đo ổn định nào.
	ErrNoStableReadings = errors.New("ingest: cửa sổ không có số đo ổn định")
	// ErrNonPositiveNet: trung vị ≤ bì (proto plant-weighing: tổng phải lớn hơn bì).
	ErrNonPositiveNet = errors.New("ingest: khối lượng tịnh phải lớn hơn 0 (tổng ≤ bì)")
	// ErrWindowCorrupt: số đo trùng bộ đếm / ngoài cửa sổ / khác thiết bị — lỗi toàn vẹn, không bao giờ xảy ra nếu trigger ts đúng.
	ErrWindowCorrupt = errors.New("ingest: cửa sổ số đo không toàn vẹn")
	// ErrWindowTooLarge: cửa sổ vượt MaxWindowReadings.
	ErrWindowTooLarge = errors.New("ingest: cửa sổ vượt giới hạn số đo")
)

// MaxWindowReadings giới hạn một cửa sổ tích phân (một lần cân xe/ghe ~ vài nghìn số đo).
const MaxWindowReadings = 200_000

// WindowReading là một dòng ts_weigh.raw_reading của cửa sổ.
type WindowReading struct {
	Counter    int64
	DeviceTime time.Time // chỉ thông tin — không dùng làm thứ tự (ADR-0019)
	ValueKg    mass.Kg
	Stable     bool
	Unit       string
	Raw        []byte
	BatchID    uuid.UUID
	ReceivedAt time.Time
}

// Result là kết quả tích phân chi tiết: port.Integration + received_at của số đo
// cuối cửa sổ (EventTime của phiếu, port.TicketDraft).
type Result struct {
	port.Integration
	LastReceivedAt time.Time
	TareKg         mass.Kg
}

// WindowDigest là SHA-256 (hex thường) của dạng chuẩn cửa sổ — tái dựng được từ cột
// ts_weigh.raw_reading bất kỳ lúc nào (tranh chấp):
//
//	agrigreen.weigh.window.v1
//	device_id=<device_id>
//	epoch=<epoch>
//	counter_from=<n>
//	counter_to=<n>
//	reading=<counter>;<device_time>;<value_kg>;<1|0>;<unit>;<raw base64>
//	... (mỗi số đo CÓ MẶT một dòng, theo counter tăng dần; cùng dạng dòng với lô edgewire)
//
// readings phải đã sắp theo counter.
func WindowDigest(w port.Window, readings []WindowReading) string {
	h := sha256.New()
	write := func(s string) { _, _ = h.Write([]byte(s)) }
	write(WindowDigestFormat + "\n")
	write("device_id=" + w.DeviceID + "\n")
	write("epoch=" + strconv.Itoa(w.Epoch) + "\n")
	write("counter_from=" + strconv.FormatInt(w.CounterFrom, 10) + "\n")
	write("counter_to=" + strconv.FormatInt(w.CounterTo, 10) + "\n")
	for _, r := range readings {
		stable := "0"
		if r.Stable {
			stable = "1"
		}
		raw := ""
		if len(r.Raw) > 0 {
			raw = base64.StdEncoding.EncodeToString(r.Raw)
		}
		write("reading=" + strconv.FormatInt(r.Counter, 10) + ";" + edgewire.FormatDeviceTime(r.DeviceTime) + ";" +
			r.ValueKg.String() + ";" + stable + ";" + r.Unit + ";" + raw + "\n")
	}
	return hex.EncodeToString(h.Sum(nil))
}

// IntegrateV1 là thuật toán weigh.integrate.v1 — hàm thuần, tất định:
//
//  1. readings (đã sắp theo counter) phải phủ ĐỦ w.CounterFrom..w.CounterTo, mỗi bộ đếm
//     đúng một lần; thiếu → port.ErrWindowIncomplete kèm Gaps (không bao giờ nội suy).
//  2. Chỉ lấy số đo stable = true. Không có → ErrNoStableReadings.
//  3. Gộp = trung vị value_kg của các số đo đó; số lượng chẵn → trung bình hai số giữa,
//     làm tròn nửa xa 0 về 3 chữ số (R-10).
//  4. Tịnh = gộp − bì; tịnh ≤ 0 → ErrNonPositiveNet (proto plant-weighing: tổng > bì).
//
// Đồng hồ thiết bị không tham gia (ADR-0019).
func IntegrateV1(w port.Window, readings []WindowReading, tare mass.Kg) (Result, error) {
	res := Result{TareKg: tare}
	res.Window = w
	res.RuleVersion = RuleV1
	if w.CounterTo < w.CounterFrom || w.CounterFrom < 1 {
		return res, ErrWindowCorrupt
	}
	if w.CounterTo-w.CounterFrom+1 > MaxWindowReadings {
		return res, ErrWindowTooLarge
	}
	if tare.NonNegative() != nil {
		return res, ErrWindowCorrupt
	}
	res.ReadingsTotal = len(readings)
	next := w.CounterFrom
	batches := map[uuid.UUID]bool{}
	var stable []decimal.Decimal
	for _, r := range readings {
		if r.Counter < next || r.Counter > w.CounterTo {
			return res, ErrWindowCorrupt // trùng, lùi hoặc ngoài cửa sổ
		}
		for ; next < r.Counter; next++ {
			res.Gaps = append(res.Gaps, next)
		}
		next = r.Counter + 1
		if !batches[r.BatchID] {
			batches[r.BatchID] = true
			res.BatchIDs = append(res.BatchIDs, r.BatchID)
		}
		if r.ReceivedAt.After(res.LastReceivedAt) {
			res.LastReceivedAt = r.ReceivedAt
		}
		if r.Stable {
			stable = append(stable, r.ValueKg.Decimal())
		}
	}
	for ; next <= w.CounterTo; next++ {
		res.Gaps = append(res.Gaps, next)
	}
	sort.Slice(res.BatchIDs, func(i, j int) bool { return res.BatchIDs[i].String() < res.BatchIDs[j].String() })
	res.ReadingsSHA256 = WindowDigest(w, readings)
	if len(res.Gaps) > 0 {
		return res, port.ErrWindowIncomplete
	}
	if len(stable) == 0 {
		return res, ErrNoStableReadings
	}
	sort.Slice(stable, func(i, j int) bool { return stable[i].LessThan(stable[j]) })
	n := len(stable)
	median := stable[n/2]
	if n%2 == 0 {
		median = stable[n/2-1].Add(stable[n/2]).Div(decimal.NewFromInt(2)).Round(mass.Scale)
	}
	gross, err := mass.ParseKg(median.StringFixed(mass.Scale))
	if err != nil {
		return res, err
	}
	net := gross.Sub(tare)
	if net.Cmp(mass.Kg{}) <= 0 {
		return res, ErrNonPositiveNet
	}
	res.NetKg = net
	res.ReadingsUsed = n
	return res, nil
}
