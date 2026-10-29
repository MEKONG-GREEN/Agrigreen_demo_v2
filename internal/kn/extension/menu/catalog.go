// Package menu là cây menu của ứng dụng Khuyến nông do KN sở hữu (KNSPLIT-2-MENU-KN, ADR-0055): dựng
// partyport.MenuBuilder và đăng ký route ↔ module từ CHÍNH các nguồn WH-A `internal/kn/*/menus/*.json`
// (cùng nguồn scripts/gen-module-menus.mjs biên dịch thành cây compatibility của pf/party). Không có bản
// sao thứ hai của cây: gói này biên dịch cùng các tài liệu JSON theo đúng ngữ nghĩa của bộ sinh (thứ tự,
// gộp, thay thế có kiểm `before`) rồi dựng menu theo đối tượng × phòng ban × chế độ.
package menu

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Version là phiên bản bảng menu (client lưu đệm theo phiên bản). Đổi menu = đổi nguồn JSON VÀ tăng
// phiên bản ở đây cùng bản compatibility (party domain.MenuVersion) cho tới khi #53 gỡ cầu nối.
const Version = "kn.menu/v9"

// tableNames là các bảng mà bộ sinh chấp nhận (scripts/gen-module-menus.mjs compileMenus).
var tableNames = []string{"trongTrotMenus", "genericMenus", "adminDeptItems", "AdminOverview", "AdminSystem", "defaultRoutes",
	"contextDefaultRoutes", "MenuProfiles", "khtcReportProfiles", "KHTCReportItem", "KHTCReportAdminItem", "SharedGroup",
	"sharedByAudience", "deptBusiness", "deptMenus", "adminDeptBusiness", "deptProfileByCode", "menuModules", "menuDeptModules"}

var navigationRoute = regexp.MustCompile(`^/[^/\\?#\r\n][^\\?#\r\n]*$|^/$`)

// ErrSource là lỗi nguồn menu (tài liệu JSON hỏng, trùng, thay thế cũ hoặc thiếu bảng).
var ErrSource = errors.New("kn menu: invalid source")

type document struct {
	ID           string                     `json:"id"`
	Order        json.Number                `json:"order"`
	Tables       map[string]json.RawMessage `json:"tables"`
	Replacements []struct {
		Path   []any `json:"path"`
		Before any   `json:"before"`
		After  any   `json:"after"`
	} `json:"replacements"`
	order int64
}

func decodeAny(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(out)
}

// Compile gộp các tài liệu nguồn theo ngữ nghĩa của bộ sinh và trả bảng đã gộp (map JSON chung).
func Compile(sources ...[]byte) (map[string]any, error) {
	docs := make([]document, 0, len(sources))
	ids := map[string]bool{}
	for _, raw := range sources {
		var doc document
		if err := decodeAny(raw, &doc); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrSource, err)
		}
		order, err := strconv.ParseInt(doc.Order.String(), 10, 64)
		if doc.ID == "" || ids[doc.ID] || err != nil {
			return nil, fmt.Errorf("%w: menu registration needs a unique id and integer order", ErrSource)
		}
		ids[doc.ID] = true
		doc.order = order
		docs = append(docs, doc)
	}
	slices.SortStableFunc(docs, func(a, b document) int {
		if a.order != b.order {
			if a.order < b.order {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	tables := map[string]any{}
	for _, doc := range docs {
		keys := make([]string, 0, len(doc.Tables))
		for key := range doc.Tables {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			if !slices.Contains(tableNames, key) {
				return nil, fmt.Errorf("%w: unknown menu table %s", ErrSource, key)
			}
			var value any
			if err := decodeAny(doc.Tables[key], &value); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrSource, err)
			}
			previous, ok := tables[key]
			merged, err := merge(previous, ok, value, key)
			if err != nil {
				return nil, err
			}
			tables[key] = merged
		}
		for _, patch := range doc.Replacements {
			if err := replace(tables, patch.Path, patch.Before, patch.After); err != nil {
				return nil, err
			}
		}
	}
	if err := validate(tables); err != nil {
		return nil, err
	}
	return tables, nil
}

func clone(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = clone(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = clone(x)
		}
		return out
	}
	return v
}

func identifier(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case map[string]any:
		if key, ok := v["Key"].(string); ok {
			return key
		}
	}
	return ""
}

func merge(target any, present bool, source any, location string) (any, error) {
	if !present {
		return clone(source), nil
	}
	targetList, targetIsList := target.([]any)
	sourceList, sourceIsList := source.([]any)
	if targetIsList && sourceIsList {
		seen := map[string]bool{}
		for _, item := range targetList {
			if id := identifier(item); id != "" {
				seen[id] = true
			}
		}
		for _, item := range sourceList {
			id := identifier(item)
			if id != "" && seen[id] {
				return nil, fmt.Errorf("%w: duplicate menu item %s/%s; use a tested replacement", ErrSource, location, id)
			}
			if id != "" {
				seen[id] = true
			}
		}
		return append(slices.Clone(targetList), clone(sourceList).([]any)...), nil
	}
	targetMap, targetIsMap := target.(map[string]any)
	sourceMap, sourceIsMap := source.(map[string]any)
	if targetIsMap && sourceIsMap {
		keys := make([]string, 0, len(sourceMap))
		for key := range sourceMap {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			previous, ok := targetMap[key]
			merged, err := merge(previous, ok, sourceMap[key], location+"/"+key)
			if err != nil {
				return nil, err
			}
			targetMap[key] = merged
		}
		return targetMap, nil
	}
	return nil, fmt.Errorf("%w: duplicate menu value %s", ErrSource, location)
}

// step trả phần tử con theo khóa (đối tượng) hoặc chỉ số (mảng); ok = false khi không có.
func step(parent any, key any) (any, bool) {
	switch p := parent.(type) {
	case map[string]any:
		name, ok := pathKey(key)
		if !ok {
			return nil, false
		}
		v, ok := p[name]
		return v, ok
	case []any:
		i, ok := pathIndex(key)
		if !ok || i < 0 || i >= len(p) {
			return nil, false
		}
		return p[i], true
	}
	return nil, false
}

func pathKey(key any) (string, bool) {
	switch k := key.(type) {
	case string:
		return k, true
	case json.Number:
		return k.String(), true
	}
	return "", false
}

func pathIndex(key any) (int, bool) {
	text, ok := pathKey(key)
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(text)
	return i, err == nil
}

func replace(tables map[string]any, path []any, before, after any) error {
	if len(path) == 0 {
		return fmt.Errorf("%w: stale menu replacement: %v", ErrSource, path)
	}
	var parent any = tables
	for _, key := range path[:len(path)-1] {
		next, ok := step(parent, key)
		if !ok || next == nil {
			return fmt.Errorf("%w: missing replacement path: %v", ErrSource, path)
		}
		parent = next
	}
	last := path[len(path)-1]
	current, _ := step(parent, last)
	if !reflect.DeepEqual(current, before) {
		return fmt.Errorf("%w: stale menu replacement: %v", ErrSource, path)
	}
	switch p := parent.(type) {
	case map[string]any:
		name, _ := pathKey(last)
		p[name] = clone(after)
	case []any:
		i, _ := pathIndex(last)
		p[i] = clone(after)
	default:
		return fmt.Errorf("%w: missing replacement path: %v", ErrSource, path)
	}
	return nil
}

func validate(tables map[string]any) error {
	profiles, _ := tables["MenuProfiles"].([]any)
	business, _ := tables["deptBusiness"].(map[string]any)
	full, _ := tables["deptMenus"].(map[string]any)
	known := map[string]bool{}
	for _, p := range profiles {
		name, _ := p.(string)
		known[name] = true
		if name != "trong_trot" && name != "generic" && business[name] == nil && full[name] == nil {
			return fmt.Errorf("%w: profile has no registered business menu: %s", ErrSource, name)
		}
	}
	defaults, _ := tables["defaultRoutes"].(map[string]any)
	contextual, _ := tables["contextDefaultRoutes"].(map[string]any)
	for profile, roles := range contextual {
		if !known[profile] {
			return fmt.Errorf("%w: navigation profile is not registered: %s", ErrSource, profile)
		}
		byRole, _ := roles.(map[string]any)
		for audience, route := range byRole {
			text, ok := route.(string)
			if defaults[audience] == nil || !ok || !navigationRoute.MatchString(text) {
				return fmt.Errorf("%w: invalid navigation route: %s/%s", ErrSource, profile, audience)
			}
		}
	}
	return nil
}

// SourceID trả id của một tài liệu nguồn menu.
func SourceID(raw []byte) (string, error) {
	var doc struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || doc.ID == "" {
		return "", fmt.Errorf("%w: menu source without id", ErrSource)
	}
	return doc.ID, nil
}
