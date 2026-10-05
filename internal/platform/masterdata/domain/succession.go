package domain

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

// LegacyBasis là căn cứ ghi cho quan hệ kế thừa suy từ successor_codes (pf 000002) —
// cùng chuỗi migration pf 000089 điền.
const LegacyBasis = "successor_codes (pf 000002)"

// ErrRollBackward: ngày đích trước ngày nguồn — RollForward chỉ đi xuôi thời gian.
var ErrRollBackward = errors.New("ngày đích phải không trước ngày nguồn")

// EdgesFunc trả các quan hệ kế thừa có hiệu lực từ phiên bản u (đã đóng): mỗi cặp
// (tiền thân, kế thừa) một dòng, dòng 'official' thay dòng 'pending_c9' cùng cặp.
type EdgesFunc func(u Unit) ([]port.Succession, error)

// LegacyEdges suy quan hệ kế thừa từ successor_codes khi bảng kế thừa chưa có dòng
// cho phiên bản u (dữ liệu ghi thẳng trước pf 000089): tỉ lệ chưa biết, căn cứ
// 'pending_c9'. Mã kế thừa không có hiệu lực tại valid_to bị bỏ qua (như ResolveCurrent).
func LegacyEdges(u Unit, at FindFunc) ([]port.Succession, error) {
	if u.ValidTo == nil {
		return nil, nil
	}
	var out []port.Succession
	for _, s := range u.Successors {
		next, err := at(s, *u.ValidTo)
		if err != nil {
			return nil, err
		}
		if next == nil {
			continue
		}
		out = append(out, port.Succession{
			PredecessorCode: u.Code, PredecessorValidFrom: u.ValidFrom,
			SuccessorCode: next.Code, SuccessorValidFrom: next.ValidFrom, EffectiveOn: *u.ValidTo,
			LegalBasis: LegacyBasis, BasisStatus: port.BasisPendingC9,
		})
	}
	return out, nil
}

// PreferOfficial giữ một dòng mỗi cặp (tiền thân, kế thừa): 'official' thắng 'pending_c9'.
func PreferOfficial(rows []port.Succession) []port.Succession {
	type key struct {
		pc, sc string
		pf, sf time.Time
	}
	idx := map[key]int{}
	var out []port.Succession
	for _, r := range rows {
		k := key{r.PredecessorCode, r.SuccessorCode, r.PredecessorValidFrom, r.SuccessorValidFrom}
		if i, ok := idx[k]; ok {
			if r.BasisStatus == port.BasisOfficial {
				out[i] = r
			}
			continue
		}
		idx[k] = len(out)
		out = append(out, r)
	}
	return out
}

type versionKey struct {
	code string
	from time.Time
}

// SuccessorChain đi theo chuỗi kế thừa từ phiên bản start, trả mọi quan hệ có
// EffectiveOn ≤ to (theo EffectiveOn, mã tiền thân, mã kế thừa).
func SuccessorChain(start Unit, to time.Time, at FindFunc, edges EdgesFunc) ([]port.Succession, error) {
	seen := map[versionKey]bool{}
	var out []port.Succession
	queue := []Unit{start}
	for hops := 0; len(queue) > 0; hops++ {
		if hops > maxSuccessorHops*8 {
			return nil, fmt.Errorf("masterdata: chuỗi kế thừa của %s quá dài hoặc có vòng", start.Code)
		}
		u := queue[0]
		queue = queue[1:]
		k := versionKey{u.Code, u.ValidFrom}
		if seen[k] {
			continue
		}
		seen[k] = true
		if u.ValidTo == nil || u.ValidTo.After(to) {
			continue
		}
		es, err := edges(u)
		if err != nil {
			return nil, err
		}
		for _, e := range es {
			out = append(out, e)
			next, err := at(e.SuccessorCode, e.EffectiveOn)
			if err != nil {
				return nil, err
			}
			if next != nil {
				queue = append(queue, *next)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.EffectiveOn.Equal(b.EffectiveOn) {
			return a.EffectiveOn.Before(b.EffectiveOn)
		}
		if a.PredecessorCode != b.PredecessorCode {
			return a.PredecessorCode < b.PredecessorCode
		}
		return a.SuccessorCode < b.SuccessorCode
	})
	return out, nil
}

// RollForward ánh xạ phiên bản start sang các mã có hiệu lực ngày target. Tỉ lệ nhân
// dọc đường đi: AreaShare nếu có; 1 nếu tiền thân chỉ có một kế thừa (sáp nhập trọn);
// chưa biết nếu tách mà chưa có tỉ lệ. Đơn vị bị bãi bỏ không kế thừa không đóng góp.
func RollForward(start Unit, target time.Time, at FindFunc, edges EdgesFunc) ([]port.RolledUnit, error) {
	if start.ValidAt(target) {
		one := decimal.NewFromInt(1)
		return []port.RolledUnit{{Code: start.Code, Share: &one}}, nil
	}
	if start.ValidTo == nil || start.ValidTo.After(target) {
		return nil, ErrRollBackward
	}
	type acc struct {
		share     decimal.Decimal
		unknown   bool
		estimated bool
		pending   bool
	}
	out := map[string]*acc{}
	var walk func(u Unit, share *decimal.Decimal, estimated, pending bool, depth int) error
	walk = func(u Unit, share *decimal.Decimal, estimated, pending bool, depth int) error {
		if depth > maxSuccessorHops {
			return fmt.Errorf("masterdata: chuỗi kế thừa của %s quá dài hoặc có vòng", start.Code)
		}
		if u.ValidAt(target) {
			a := out[u.Code]
			if a == nil {
				a = &acc{}
				out[u.Code] = a
			}
			if share == nil {
				a.unknown = true
			} else {
				a.share = a.share.Add(*share)
			}
			a.estimated = a.estimated || estimated
			a.pending = a.pending || pending
			return nil
		}
		if u.ValidTo == nil || u.ValidTo.After(target) {
			return nil
		}
		es, err := edges(u)
		if err != nil {
			return err
		}
		for _, e := range es {
			next, err := at(e.SuccessorCode, e.EffectiveOn)
			if err != nil {
				return err
			}
			if next == nil {
				continue
			}
			var s *decimal.Decimal
			est := estimated
			switch {
			case e.AreaShare != nil:
				if share != nil {
					v := share.Mul(*e.AreaShare)
					s = &v
				}
				est = est || e.AreaShare.LessThan(decimal.NewFromInt(1))
			case len(es) == 1:
				s = share
			default:
				est = true
			}
			if err := walk(*next, s, est, pending || e.BasisStatus != port.BasisOfficial, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	one := decimal.NewFromInt(1)
	if err := walk(start, &one, false, false, 0); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, port.ErrUnitNotFound
	}
	codes := make([]string, 0, len(out))
	for c := range out {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	res := make([]port.RolledUnit, 0, len(codes))
	for _, c := range codes {
		a := out[c]
		r := port.RolledUnit{Code: c, Estimated: a.estimated || a.unknown, Pending: a.pending}
		if !a.unknown {
			v := a.share
			r.Share = &v
		}
		res = append(res, r)
	}
	return res, nil
}
