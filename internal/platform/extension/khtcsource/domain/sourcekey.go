// Package domain là quy tắc thuần của bộ đọc nguồn module KHTC (W11.64 — BRD KHTC v2.2 BR-DL-02…04, BR-DL-09, BR-DL-17;
// D4 KH4): khóa nguồn module của hạng mục Kế hoạch năm, số của hạng mục từ ảnh chụp port KHTCSources (ADR-0053 §7) —
// lũy kế của nguồn (D18), trong kỳ = lũy kế − gốc (docs/uat/khtc-week-mapping.md Q4–Q7), giá min/max toàn tỉnh (D22),
// tách đã/chưa xác nhận (D2) — lịch sử thay đổi so với ảnh chụp trước và "Nguồn đã thay đổi" (so figures_hash, không so
// status_hash). Không I/O, không time.Now (R-12).
package domain

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
)

// RuleVersion là phiên bản quy tắc ghi vào sổ ảnh chụp.
const RuleVersion = "khtc-source-2026.10"

// Cách lấy số của khóa nguồn.
const (
	// ModeCumulative: lũy kế của nguồn (D18), chuỗi lũy kế theo NĂM của khóa kỳ nguồn — "lũy kế từ đầu năm" (CNTS
	// A.I.3): khóa kỳ nguồn sang năm mới → gốc 0.
	ModeCumulative = "cum"
	// ModeSeason: lũy kế của nguồn theo vụ (Trồng trọt A.I.1) — không tự về 0 khi sang năm (vụ vắt năm, golden G05B).
	ModeSeason = "season"
	// ModeSum: số phát sinh — cộng chính xác các kỳ nguồn quy vào kỳ (số phiếu giá, kỳ ngày).
	ModeSum = "sum"
	// ModePrice: giá "từ – đến" = min/max toàn tỉnh của mặt hàng ở bảng giá đã duyệt (D22, Mẫu 1 A.III).
	ModePrice = "price"
)

// Modes là các cách lấy số viết được ở đầu khóa nguồn.
var Modes = []string{ModeCumulative, ModeSeason, ModeSum, ModePrice}

// Mã biểu Báo giá viết thường (port.PriceForm*).
var (
	formPriceBoard   = strings.ToLower(port.PriceFormBoard)
	formPriceTickets = strings.ToLower(port.PriceFormTickets)
)

// SourceKey là khóa nguồn module của một hạng mục Kế hoạch năm (W11.61 source_key, BR-DL-10):
//
//	[<cách lấy>:]<mã phòng>:<mã biểu>:[<chỉ tiêu>:]<ô>
//
// viết thường (khớp ràng buộc ^[a-z][a-z0-9_.:-]{1,99}$ của W11.61). Ví dụ:
//
//	chan_nuoi_thuy_san:bm-04:san_luong_luy_ke      — lũy kế từ đầu năm của CNTS (cum mặc định)
//	season:trong_trot:pl01:lua_thu_dong:f          — ô F của chỉ tiêu lua_thu_dong, lũy kế vụ
//	thong_tin_truyen_thong:price-board:mh00001     — giá từ – đến của mặt hàng MH00001 (price mặc định với PRICE-BOARD)
//	thong_tin_truyen_thong:price-tickets:tickets_collected — số phiếu giá (sum mặc định với PRICE-TICKETS)
//
// Ô được so khớp không phân biệt hoa thường với khóa ô của port ("<chỉ tiêu>/<ô>" hoặc "<ô>").
type SourceKey struct {
	Raw        string
	Mode       string
	Department string
	Form       string // viết thường
	Cell       string // khóa ô viết thường ("<chỉ tiêu>/<ô>" hoặc "<ô>"); ModePrice: mã mặt hàng
}

var (
	deptRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
	formRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	cellRe = regexp.MustCompile(`^[a-z0-9_][a-z0-9_.-]{0,63}$`)
)

// ErrSourceKey: khóa nguồn module không đúng dạng.
var ErrSourceKey = errors.New("khóa nguồn module: [cum|season|sum|price:]<mã phòng>:<mã biểu>:[<chỉ tiêu>:]<ô>")

// ParseSourceKey đọc khóa nguồn module. Cách lấy mặc định: PRICE-BOARD → price (một đoạn = mã mặt hàng), PRICE-TICKETS →
// sum, biểu khác → cum.
func ParseSourceKey(raw string) (SourceKey, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	segs := strings.Split(s, ":")
	k := SourceKey{Raw: s}
	if len(segs) > 0 && slices.Contains(Modes, segs[0]) {
		k.Mode, segs = segs[0], segs[1:]
	}
	if len(segs) < 3 || len(segs) > 4 || !deptRe.MatchString(segs[0]) || !formRe.MatchString(segs[1]) {
		return SourceKey{}, ErrSourceKey
	}
	for _, c := range segs[2:] {
		if !cellRe.MatchString(c) {
			return SourceKey{}, ErrSourceKey
		}
	}
	k.Department, k.Form, k.Cell = segs[0], segs[1], strings.Join(segs[2:], "/")
	switch {
	case k.Mode == "" && k.Form == formPriceBoard:
		k.Mode = ModePrice
	case k.Mode == "" && k.Form == formPriceTickets:
		k.Mode = ModeSum
	case k.Mode == "":
		k.Mode = ModeCumulative
	}
	if (k.Mode == ModePrice) != (k.Form == formPriceBoard) || (k.Mode == ModePrice && len(segs) != 3) {
		return SourceKey{}, ErrSourceKey
	}
	return k, nil
}

// cellOf trả giá trị ô k của bản đồ ô (so khớp không phân biệt hoa thường).
func cellOf(m map[string]string, k string) (string, bool) {
	if v, ok := m[k]; ok {
		return v, true
	}
	for c, v := range m {
		if strings.EqualFold(c, k) {
			return v, true
		}
	}
	return "", false
}
