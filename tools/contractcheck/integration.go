package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/publication"
)

// Hợp đồng cổng tích hợp (W5.1, R-INT-08, ADR-0033…0035): contracts/integration/.
//
//	(a) envelope/<tên>.v<N>.json: phong bì feed_page, ingest_batch đúng lược đồ (publication.Node);
//	(b) feeds/<tên>.v<N>.json, ingest/<tên>.v<N>.json: siêu dữ liệu đúng (integration.MetaProblems —
//	    người đề xuất ≠ người phê duyệt, kind khớp thư mục, flow/grant_class, k của tập tổng hợp,
//	    hai bên của luồng song phương), name/version trùng tên tệp, một tên chỉ một kind;
//	(c) "publication": <tên> trỏ hợp đồng van hợp lệ (lược đồ dùng nguyên, ADR-0033 §3);
//	    "replaces" trỏ hợp đồng có thật;
//	(d) entity_key / business_key / pair_key / effective_dating trỏ trường có thật của bản ghi;
//	(e) mỗi hợp đồng có examples/<tên>.v<N>.json khớp phong bì và hợp đồng; không mẫu mồ côi;
//	(f) -base: so với nhánh gốc chỉ được thêm trường tùy chọn — xóa, đổi kiểu, thêm required →
//	    phiên bản mới <tên>.v<N+1>.json chạy song song (R-INT-08).
const integrationDir = "integration"

var integrationFileRe = regexp.MustCompile(`^([a-z][a-z0-9_]{2,47})\.v([0-9]+)\.json$`)

func integrationRules(root, base string, pubs map[string]*publication.Contract) ([]string, error) {
	dir := filepath.Join(root, "contracts", integrationDir)
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var p []string
	add := func(f, format string, a ...any) { p = append(p, f+": "+fmt.Sprintf(format, a...)) }

	envs := map[string]*integration.Envelope{}
	envFiles, _ := filepath.Glob(filepath.Join(dir, "envelope", "*.json"))
	for _, f := range envFiles {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		e, err := integration.ParseEnvelope(raw)
		if err != nil {
			add(f, "%v", err)
			continue
		}
		bad := false
		for _, mp := range e.Problems() {
			add(f, "%s", mp)
			bad = true
		}
		if want := fmt.Sprintf("%s.v%d.json", e.Name, e.Version); filepath.Base(f) != want {
			add(f, "tên tệp phải là %s", want)
			bad = true
		}
		if base != "" {
			if s, err := parseSchema(raw); err == nil {
				p = append(p, compat(root, base, f, s)...)
			}
		}
		if !bad {
			envs[e.Name] = e
		}
	}

	lookup := func(name string) (*publication.Contract, error) {
		c, ok := pubs[name]
		if !ok {
			return nil, errors.New("không có hợp đồng van hợp lệ trong contracts/publication/")
		}
		return c, nil
	}
	kindOfName := map[string]string{}
	ids := map[string]bool{}
	for _, kind := range []string{integration.KindFeed, integration.KindIngest} {
		files, _ := filepath.Glob(filepath.Join(dir, integration.Dir(kind), "*.json"))
		for _, f := range files {
			m := integrationFileRe.FindStringSubmatch(filepath.Base(f))
			if m == nil {
				add(f, "tên tệp phải là <tên>.v<N>.json (R-INT-08)")
				continue
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			c, err := integration.Parse(raw)
			if err != nil {
				add(f, "%v", err)
				continue
			}
			for _, mp := range c.MetaProblems() {
				add(f, "%s", mp)
			}
			v, _ := strconv.Atoi(m[2])
			if c.Name != m[1] || c.Version != v {
				add(f, "name/version (%s v%d) phải trùng tên tệp", c.Name, c.Version)
			}
			if c.Kind != kind {
				add(f, "kind %q nằm trong thư mục %s/", c.Kind, integration.Dir(kind))
			}
			if k, ok := kindOfName[c.Name]; ok && k != kind {
				add(f, "tên %s đã dùng cho %s — feed và hợp đồng nạp phải khác tên", c.Name, k)
			}
			kindOfName[c.Name] = kind
			ids[c.ID()] = true
			for _, r := range c.Replaces {
				if _, err := os.Stat(filepath.Join(root, "contracts", filepath.FromSlash(r)+".json")); err != nil {
					add(f, "replaces %s: không có contracts/%s.json", r, r)
				}
			}
			if base != "" {
				if s, err := parseSchema(raw); err == nil {
					p = append(p, compat(root, base, f, s)...)
				}
			}
			if err := c.Resolve(lookup); err != nil {
				add(f, "%v", err)
				continue
			}
			for _, rp := range c.RecordProblems() {
				add(f, "%s", rp)
			}
			ex := filepath.Join(dir, "examples", c.ID()+".json")
			exRaw, err := os.ReadFile(ex)
			if errors.Is(err, os.ErrNotExist) {
				add(f, "thiếu mẫu examples/%s.json (R-INT-08: kiểm thử hợp đồng)", c.ID())
				continue
			}
			if err != nil {
				return nil, err
			}
			env, ok := envs[integration.EnvelopeName(kind)]
			if !ok {
				add(f, "thiếu phong bì envelope/%s.v1.json hợp lệ", integration.EnvelopeName(kind))
				continue
			}
			for _, ep := range c.CheckExample(exRaw, env) {
				add(ex, "%s", ep)
			}
		}
	}
	exFiles, _ := filepath.Glob(filepath.Join(dir, "examples", "*.json"))
	for _, f := range exFiles {
		if id := strings.TrimSuffix(filepath.Base(f), ".json"); !ids[id] {
			add(f, "mẫu không có hợp đồng %s hợp lệ tương ứng", id)
		}
	}
	sort.Strings(p)
	return p, nil
}
