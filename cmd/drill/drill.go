package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objsync"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/opsjobs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Định dạng tệp.
const (
	DBManifestFormat = "ag.drill-db-manifest.v1"
	ReportFormat     = "ag.restore-drill-report.v1"
)

// Tên kiểm (ổn định — dùng trong báo cáo và cảnh báo).
const (
	CheckRowCounts = "row_counts"
	CheckParamHash = "param_set_hash"
	CheckChains    = "verify_chains"
	CheckBalance   = "balance_recompute"
	CheckObjects   = "object_checksums"
	CheckAnchors   = "anchor_signature"
)

// Trạng thái của một kiểm.
const (
	StatusPass = "pass"
	StatusFail = "fail"
	StatusSkip = "skip"
)

// paramTable là sổ tham số đã phê duyệt — nguồn của "băm bộ tham số" (mục 22).
const paramTable = "pf_masterdata.param_approved"

// DBManifest là ảnh chụp lúc sao lưu để so sau phục hồi.
type DBManifest struct {
	Format     string                     `json:"format"`
	CapturedAt time.Time                  `json:"captured_at"`
	Clusters   map[string]ClusterManifest `json:"clusters"`
}

// ClusterManifest là ảnh chụp của một cụm.
type ClusterManifest struct {
	Database         string           `json:"database"`
	Tables           map[string]int64 `json:"tables"`
	ParamSetHash     string           `json:"param_set_hash,omitempty"`
	ParamRows        int64            `json:"param_rows,omitempty"`
	LatestAnchorDate string           `json:"latest_anchor_date,omitempty"`
	Chains           int              `json:"chains"`
}

// Config là cấu hình một lần chụp/kiểm.
type Config struct {
	AdminURL     string // URL quản trị cụm (superuser hoặc vai BYPASSRLS chỉ đọc); path bị thay bằng database của cụm
	Suffix       string // hậu tố database (pgtest); rỗng ở môi trường thật
	Clusters     []migrations.Cluster
	Store        objstore.Store    // kho chính ĐÃ PHỤC HỒI (manifest neo, đối tượng) — nil: bỏ qua 2 kiểm
	AnchorPub    ed25519.PublicKey // khóa công khai TIN CẬY của manifest neo (kênh riêng)
	Objects      *objsync.Manifest // manifest kiểm kê kho đối tượng lúc sao lưu — nil: bỏ qua
	IgnoreTables map[string]bool   // bảng được phép lệch số dòng (vd job dọn dẹp chạy sau sao lưu)
	SampleRuns   int               // số lần cân đối mới nhất được tính lại (mặc định 20)
	Now          func() time.Time
}

func (c Config) clusters() []migrations.Cluster {
	if len(c.Clusters) == 0 {
		return migrations.Clusters
	}
	return c.Clusters
}

func (c Config) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

// CheckResult là kết quả một kiểm.
type CheckResult struct {
	Name    string   `json:"name"`
	Cluster string   `json:"cluster,omitempty"`
	Status  string   `json:"status"`
	Detail  string   `json:"detail"`
	Issues  []string `json:"issues,omitempty"` // tối đa 50 dòng — chỉ tên bảng, mã tham chiếu, băm; không có dữ liệu cá nhân
}

// Report là báo cáo diễn tập (JSON).
type Report struct {
	Format     string        `json:"format"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
	CapturedAt time.Time     `json:"manifest_captured_at"`
	OK         bool          `json:"ok"`
	Strict     bool          `json:"strict"`
	Pass       int           `json:"pass"`
	Fail       int           `json:"fail"`
	Skip       int           `json:"skip"`
	Checks     []CheckResult `json:"checks"`
}

// Find trả kết quả theo tên và cụm (cụm rỗng = kiểm không theo cụm).
func (r Report) Find(name, cluster string) (CheckResult, bool) {
	for _, c := range r.Checks {
		if c.Name == name && c.Cluster == cluster {
			return c, true
		}
	}
	return CheckResult{}, false
}

func (r *Report) add(c CheckResult) {
	if len(c.Issues) > 50 {
		c.Issues = append(c.Issues[:50], fmt.Sprintf("… và %d dòng khác", len(c.Issues)-50))
	}
	r.Checks = append(r.Checks, c)
}

func (r *Report) finish(strict bool) {
	r.Strict = strict
	r.Pass, r.Fail, r.Skip = 0, 0, 0
	for _, c := range r.Checks {
		switch c.Status {
		case StatusPass:
			r.Pass++
		case StatusFail:
			r.Fail++
		default:
			r.Skip++
		}
	}
	r.OK = r.Fail == 0 && (!strict || r.Skip == 0)
}

func openCluster(ctx context.Context, cfg Config, c migrations.Cluster) (*pgxpool.Pool, string, error) {
	u, err := url.Parse(cfg.AdminURL)
	if err != nil {
		return nil, "", errors.New("AG_PG_ADMIN_URL không hợp lệ")
	}
	name := dbadmin.DatabaseName(c, cfg.Suffix)
	u.Path = "/" + name
	p, err := db.Open(ctx, u.String(), db.Options{AppName: "drill", MaxConns: 4})
	if err != nil {
		return nil, name, fmt.Errorf("mở %s: %w", name, err)
	}
	return p, name, nil
}

// Capture chụp số dòng mọi bảng, băm bộ tham số và ngày neo mới nhất của mọi cụm.
// Chạy NGAY TRƯỚC khi sao lưu, sau khi đã dừng tiến trình ghi.
func Capture(ctx context.Context, cfg Config) (DBManifest, error) {
	m := DBManifest{Format: DBManifestFormat, CapturedAt: cfg.now(), Clusters: map[string]ClusterManifest{}}
	for _, c := range cfg.clusters() {
		pool, name, err := openCluster(ctx, cfg, c)
		if err != nil {
			return m, err
		}
		cm, err := captureCluster(ctx, pool)
		pool.Close()
		if err != nil {
			return m, fmt.Errorf("%s: %w", name, err)
		}
		cm.Database = name
		m.Clusters[string(c)] = cm
	}
	return m, nil
}

func captureCluster(ctx context.Context, pool *pgxpool.Pool) (ClusterManifest, error) {
	var cm ClusterManifest
	err := readOnly(ctx, pool, func(tx pgx.Tx) error {
		var err error
		if cm.Tables, err = rowCounts(ctx, tx); err != nil {
			return err
		}
		if cm.ParamSetHash, cm.ParamRows, err = paramSetHash(ctx, tx); err != nil {
			return err
		}
		var d *time.Time
		if err := tx.QueryRow(ctx, "SELECT max(business_date) FROM shared.chain_anchor").Scan(&d); err != nil {
			return err
		}
		if d != nil {
			cm.LatestAnchorDate = d.Format(time.DateOnly)
		}
		return tx.QueryRow(ctx, "SELECT count(*) FROM shared.chain_head").Scan(&cm.Chains)
	})
	return cm, err
}

// readOnly chạy fn trong giao dịch REPEATABLE READ chỉ đọc: một ảnh nhất quán.
func readOnly(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, fn)
}

// rowCounts đếm dòng mọi bảng thường/bảng cha phân vùng (không đếm riêng phân vùng con).
func rowCounts(ctx context.Context, tx pgx.Tx) (map[string]int64, error) {
	rows, err := tx.Query(ctx, `SELECT format('%I.%I', n.nspname, c.relname)
	  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	  WHERE c.relkind IN ('r', 'p') AND NOT c.relispartition
	    AND n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
	    AND n.nspname NOT LIKE 'pg\_temp%' AND n.nspname NOT LIKE 'pg\_toast\_temp%'
	  ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(tables))
	for _, t := range tables {
		var n int64
		// t do format('%I.%I') của catalog trả về — đã trích dẫn an toàn.
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+t).Scan(&n); err != nil {
			return nil, fmt.Errorf("đếm %s: %w", t, err)
		}
		out[t] = n
	}
	return out, nil
}

// paramSetHash là SHA-256 của dạng chuẩn mọi phiên bản tham số ĐÃ DUYỆT
// (tenant, khóa, phiên bản, giá trị, đơn vị, hiệu lực, văn bản nguồn, bản bù,
// row_hash) theo thứ tự (tenant, khóa, seq). Cụm không có sổ tham số → "".
func paramSetHash(ctx context.Context, tx pgx.Tx) (string, int64, error) {
	var reg *string
	if err := tx.QueryRow(ctx, "SELECT to_regclass($1)::text", paramTable).Scan(&reg); err != nil {
		return "", 0, err
	}
	if reg == nil {
		return "", 0, nil
	}
	rows, err := tx.Query(ctx, `SELECT tenant_id::text, param_key, version, coalesce(value_num::text, '∅'), coalesce(value_text, '∅'),
	    unit, effective_from, source_doc, coalesce(supersedes_id::text, '∅'), row_hash
	  FROM `+paramTable+` ORDER BY tenant_id, param_key, seq`)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	h := sha256.New()
	var n int64
	for rows.Next() {
		var tid, key, vnum, vtext, unit, src, sup string
		var ver int
		var eff time.Time
		var rh []byte
		if err := rows.Scan(&tid, &key, &ver, &vnum, &vtext, &unit, &eff, &src, &sup, &rh); err != nil {
			return "", 0, err
		}
		// Giá trị số chuẩn hóa qua decimal để "1.50000000" và "1.5" cho cùng băm.
		if vnum != "∅" {
			if d, err := decimal.NewFromString(vnum); err == nil {
				vnum = d.String()
			}
		}
		_, _ = fmt.Fprintf(h, "%s\x1f%s\x1f%d\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%s\x1f%x\n",
			tid, key, ver, vnum, vtext, unit, eff.UTC().Format(time.RFC3339Nano), src, sup, rh)
		n++
	}
	if err := rows.Err(); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// Check chạy các kiểm của mục 22 trên môi trường đã phục hồi.
func Check(ctx context.Context, cfg Config, want DBManifest, strict bool) (Report, error) {
	rep := Report{Format: ReportFormat, StartedAt: cfg.now(), CapturedAt: want.CapturedAt, Checks: []CheckResult{}}
	if want.Format != DBManifestFormat {
		return rep, errors.New("manifest DB sai định dạng")
	}
	for _, c := range cfg.clusters() {
		cn := string(c)
		pool, name, err := openCluster(ctx, cfg, c)
		if err != nil {
			rep.add(CheckResult{Name: CheckRowCounts, Cluster: cn, Status: StatusFail, Detail: "không mở được database " + name})
			continue
		}
		wm, ok := want.Clusters[cn]
		if !ok {
			rep.add(CheckResult{Name: CheckRowCounts, Cluster: cn, Status: StatusFail, Detail: "manifest không có cụm này"})
			pool.Close()
			continue
		}
		var got ClusterManifest
		if err := readOnly(ctx, pool, func(tx pgx.Tx) error {
			var err error
			if got.Tables, err = rowCounts(ctx, tx); err != nil {
				return err
			}
			got.ParamSetHash, got.ParamRows, err = paramSetHash(ctx, tx)
			return err
		}); err != nil {
			pool.Close()
			return rep, fmt.Errorf("%s: %w", name, err)
		}
		rep.add(compareCounts(cn, wm.Tables, got.Tables, cfg.IgnoreTables))
		if wm.ParamSetHash != "" || got.ParamSetHash != "" {
			rep.add(compareParams(cn, wm, got))
		}
		rep.add(checkChains(ctx, cn, pool))
		if c == migrations.PF {
			rep.add(checkBalance(ctx, cn, pool, cfg.SampleRuns))
		}
		rep.add(checkAnchors(ctx, cn, pool, cfg, wm))
		pool.Close()
	}
	rep.add(checkObjects(ctx, cfg))
	rep.FinishedAt = cfg.now()
	rep.finish(strict)
	return rep, nil
}

func compareCounts(cluster string, want, got map[string]int64, ignore map[string]bool) CheckResult {
	r := CheckResult{Name: CheckRowCounts, Cluster: cluster}
	names := map[string]bool{}
	for k := range want {
		names[k] = true
	}
	for k := range got {
		names[k] = true
	}
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var total int64
	for _, k := range keys {
		w, inW := want[k]
		g, inG := got[k]
		total += g
		switch {
		case ignore[k]:
		case !inG:
			r.Issues = append(r.Issues, fmt.Sprintf("%s: thiếu bảng (lúc sao lưu có %d dòng)", k, w))
		case !inW:
			r.Issues = append(r.Issues, fmt.Sprintf("%s: bảng không có trong manifest (%d dòng)", k, g))
		case w != g:
			r.Issues = append(r.Issues, fmt.Sprintf("%s: %d dòng, lúc sao lưu %d", k, g, w))
		}
	}
	if len(r.Issues) > 0 {
		r.Status, r.Detail = StatusFail, fmt.Sprintf("%d/%d bảng lệch số dòng", len(r.Issues), len(keys))
		return r
	}
	r.Status, r.Detail = StatusPass, fmt.Sprintf("%d bảng, %d dòng khớp manifest", len(keys), total)
	return r
}

func compareParams(cluster string, want, got ClusterManifest) CheckResult {
	r := CheckResult{Name: CheckParamHash, Cluster: cluster}
	if want.ParamSetHash == got.ParamSetHash && want.ParamRows == got.ParamRows {
		r.Status, r.Detail = StatusPass, fmt.Sprintf("%d phiên bản đã duyệt, sha256=%s", got.ParamRows, got.ParamSetHash)
		return r
	}
	r.Status = StatusFail
	r.Detail = fmt.Sprintf("băm bộ tham số %s (%d dòng) khác lúc sao lưu %s (%d dòng)", got.ParamSetHash, got.ParamRows, want.ParamSetHash, want.ParamRows)
	return r
}

func checkChains(ctx context.Context, cluster string, pool *pgxpool.Pool) CheckResult {
	r := CheckResult{Name: CheckChains, Cluster: cluster}
	cs, err := opsjobs.VerifyAllChains(ctx, pool)
	if err != nil {
		r.Status, r.Detail = StatusFail, "không chạy được verify_chain: "+err.Error()
		return r
	}
	var rows int64
	for _, c := range cs {
		rows += c.RowsChecked
		if c.OK {
			continue
		}
		seq := "?"
		if c.BrokenAtSeq != nil {
			seq = fmt.Sprint(*c.BrokenAtSeq)
		}
		r.Issues = append(r.Issues, fmt.Sprintf("%s tenant=%s stream=%s seq=%s: %s", c.Ledger, c.TenantID, c.StreamKey, seq, c.Reason))
	}
	if len(r.Issues) > 0 {
		r.Status, r.Detail = StatusFail, fmt.Sprintf("%d/%d chuỗi đứt", len(r.Issues), len(cs))
		return r
	}
	r.Status, r.Detail = StatusPass, fmt.Sprintf("%d chuỗi, %d dòng hợp lệ", len(cs), rows)
	return r
}

// checkBalance tính lại (FN-09, hàm thuần của module) các dòng của những lần
// cân đối mới nhất từ đầu vào đã lưu và so từng cột với kết quả đã lưu.
func checkBalance(ctx context.Context, cluster string, pool *pgxpool.Pool, sample int) CheckResult {
	r := CheckResult{Name: CheckBalance, Cluster: cluster}
	if sample <= 0 {
		sample = 20
	}
	type run struct {
		id, tenant  uuid.UUID
		th          string
		count       int
		counts      string
		season      string
		ruleVersion string
	}
	var runs []run
	checked := 0
	err := readOnly(ctx, pool, func(tx pgx.Tx) error {
		var reg *string
		if err := tx.QueryRow(ctx, "SELECT to_regclass('pf_mechanization.balance_run')::text").Scan(&reg); err != nil || reg == nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, tenant_id, thresholds::text, result_count, status_counts::text,
		    season_code || '-' || season_year, rule_version
		  FROM (SELECT DISTINCT ON (tenant_id, season_code, season_year) *
		          FROM pf_mechanization.balance_run ORDER BY tenant_id, season_code, season_year, seq DESC) x
		  ORDER BY event_time DESC, id LIMIT $1`, sample)
		if err != nil {
			return err
		}
		for rows.Next() {
			var x run
			if err := rows.Scan(&x.id, &x.tenant, &x.th, &x.count, &x.counts, &x.season, &x.ruleVersion); err != nil {
				rows.Close()
				return err
			}
			runs = append(runs, x)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, x := range runs {
			if x.ruleVersion != mechanization.RuleVersion {
				r.Issues = append(r.Issues, fmt.Sprintf("run %s: rule_version %s khác quy tắc hiện hành %s — không tính lại", x.id, x.ruleVersion, mechanization.RuleVersion))
				continue
			}
			n, statuses, issues, err := recomputeRun(ctx, tx, x.id, x.tenant, []byte(x.th))
			if err != nil {
				return err
			}
			checked += n
			for _, s := range issues {
				r.Issues = append(r.Issues, fmt.Sprintf("run %s (%s): %s", x.id, x.season, s))
			}
			if n != x.count {
				r.Issues = append(r.Issues, fmt.Sprintf("run %s: %d dòng kết quả, result_count %d", x.id, n, x.count))
			}
			if !sameCounts(x.counts, statuses) {
				r.Issues = append(r.Issues, fmt.Sprintf("run %s: status_counts đã lưu %s khác đếm lại %v", x.id, x.counts, statuses))
			}
		}
		return nil
	})
	switch {
	case err != nil:
		r.Status, r.Detail = StatusFail, "không tính lại được: "+err.Error()
	case len(runs) == 0:
		r.Status, r.Detail = StatusSkip, "chưa có lần cân đối nào để tính lại"
	case len(r.Issues) > 0:
		r.Status, r.Detail = StatusFail, fmt.Sprintf("%d sai lệch trên %d dòng của %d lần cân đối", len(r.Issues), checked, len(runs))
	default:
		r.Status, r.Detail = StatusPass, fmt.Sprintf("tính lại %d dòng của %d lần cân đối: khớp kết quả đã lưu", checked, len(runs))
	}
	return r
}

func recomputeRun(ctx context.Context, tx pgx.Tx, runID, tenant uuid.UUID, th []byte) (int, map[string]int, []string, error) {
	rows, err := tx.Query(ctx, `SELECT seq, cooperative_ref, machine_type, area_ha::text, norm_value::text, capacity, machines_total,
	    required::text, required_whole, coverage_pct::text, gap, status
	  FROM pf_mechanization.balance_result WHERE tenant_id = $1 AND run_id = $2 ORDER BY seq`, tenant, runID)
	if err != nil {
		return 0, nil, nil, err
	}
	defer rows.Close()
	n := 0
	statuses := map[string]int{}
	var issues []string
	for rows.Next() {
		var seq int64
		var coop uuid.UUID
		var mt, status string
		var area, norm, req, pct *string
		var capacity, total int
		var whole, gap *int
		if err := rows.Scan(&seq, &coop, &mt, &area, &norm, &capacity, &total, &req, &whole, &pct, &gap, &status); err != nil {
			return 0, nil, nil, err
		}
		n++
		statuses[status]++
		in := mechanization.RecomputeInput{Capacity: capacity, MachinesTotal: total, ThresholdsJSON: th}
		if in.AreaHa, err = dec(area); err != nil {
			return 0, nil, nil, err
		}
		if in.NormValue, err = dec(norm); err != nil {
			return 0, nil, nil, err
		}
		got, err := mechanization.Recompute(in)
		if err != nil {
			return 0, nil, nil, err
		}
		savedReq, err := dec(req)
		if err != nil {
			return 0, nil, nil, err
		}
		savedPct, err := dec(pct)
		if err != nil {
			return 0, nil, nil, err
		}
		var diff []string
		if got.Status != status {
			diff = append(diff, fmt.Sprintf("status %s ≠ %s", status, got.Status))
		}
		if !eqDec(savedReq, got.Required) {
			diff = append(diff, "required")
		}
		if !eqDec(savedPct, got.CoveragePct) {
			diff = append(diff, "coverage_pct")
		}
		if !eqInt(whole, got.RequiredWhole) {
			diff = append(diff, "required_whole")
		}
		if !eqInt(gap, got.Gap) {
			diff = append(diff, "gap")
		}
		if len(diff) > 0 {
			issues = append(issues, fmt.Sprintf("seq %d HTX %s %s: %s", seq, coop, mt, strings.Join(diff, ", ")))
		}
	}
	return n, statuses, issues, rows.Err()
}

func sameCounts(saved string, got map[string]int) bool {
	var m map[string]int
	if err := json.Unmarshal([]byte(saved), &m); err != nil {
		return false
	}
	for k, v := range m {
		if got[k] != v {
			return false
		}
	}
	for k, v := range got {
		if m[k] != v {
			return false
		}
	}
	return true
}

func dec(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func eqDec(a, b *decimal.Decimal) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func eqInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// checkAnchors: neo mới nhất trong shared.chain_anchor phải trùng manifest ký
// số trong kho (chữ ký Ed25519 bằng khóa công khai tin cậy), và mỗi đầu chuỗi
// đã neo phải trùng row_hash của dòng seq tương ứng trong bảng sổ.
func checkAnchors(ctx context.Context, cluster string, pool *pgxpool.Pool, cfg Config, want ClusterManifest) CheckResult {
	r := CheckResult{Name: CheckAnchors, Cluster: cluster}
	var latest *time.Time
	var heads int
	if err := pool.QueryRow(ctx, "SELECT max(business_date), (SELECT count(*) FROM shared.chain_head) FROM shared.chain_anchor").Scan(&latest, &heads); err != nil {
		r.Status, r.Detail = StatusFail, err.Error()
		return r
	}
	date := want.LatestAnchorDate
	if date == "" && latest != nil {
		date = latest.Format(time.DateOnly)
	}
	switch {
	case date == "" && heads == 0 && want.Chains == 0:
		r.Status, r.Detail = StatusPass, "cụm chưa có chuỗi nào — không có gì để neo"
		return r
	case date == "":
		r.Status, r.Detail = StatusSkip, "chưa có neo nào (job anchor chưa chạy trước khi sao lưu)"
		return r
	case latest == nil || latest.Format(time.DateOnly) != date:
		r.Status = StatusFail
		r.Detail = fmt.Sprintf("neo mới nhất sau phục hồi %v khác lúc sao lưu %s", latest, date)
		return r
	case cfg.Store == nil || len(cfg.AnchorPub) == 0:
		r.Status, r.Detail = StatusSkip, "thiếu kho đối tượng hoặc khóa công khai neo (AG_ANCHOR_PUBLIC_KEY)"
		return r
	}
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		r.Status, r.Detail = StatusFail, fmt.Sprintf("ngày neo %q không hợp lệ: %v", date, err)
		return r
	}
	key := opsjobs.ManifestKey(cluster, clock.Date{Year: d.Year(), Month: d.Month(), Day: d.Day()})
	rc, _, err := cfg.Store.Get(ctx, key)
	if err != nil {
		r.Status, r.Detail = StatusFail, "không đọc được manifest neo "+key+": "+err.Error()
		return r
	}
	body, err := io.ReadAll(io.LimitReader(rc, 64<<20))
	_ = rc.Close()
	if err != nil {
		r.Status, r.Detail = StatusFail, err.Error()
		return r
	}
	m, err := opsjobs.VerifyManifest(body, cfg.AnchorPub)
	if err != nil {
		r.Status, r.Detail = StatusFail, key+": "+err.Error()
		return r
	}
	if m.Cluster != cluster || m.BusinessDate != date {
		r.Status, r.Detail = StatusFail, fmt.Sprintf("%s: manifest của cụm %s ngày %s", key, m.Cluster, m.BusinessDate)
		return r
	}

	type head struct {
		seq  int64
		hash string
	}
	signed := map[string]head{}
	for _, e := range m.Entries {
		signed[e.Ledger+"|"+e.TenantID.String()+"|"+e.StreamKey] = head{e.LastSeq, e.LastHash}
	}
	tables, err := opsjobs.LedgerTables(ctx, pool)
	if err != nil {
		r.Status, r.Detail = StatusFail, err.Error()
		return r
	}
	byLedger := map[string]string{}
	for _, t := range tables {
		byLedger[t.Ledger] = t.Table
	}
	rows, err := pool.Query(ctx, `SELECT ledger, tenant_id, stream_key, last_seq, last_hash FROM shared.chain_anchor
	  WHERE business_date = $1 ORDER BY ledger, tenant_id, stream_key`, date)
	if err != nil {
		r.Status, r.Detail = StatusFail, err.Error()
		return r
	}
	type anchored struct {
		ledger, stream string
		tenant         uuid.UUID
		seq            int64
		hash           []byte
	}
	var dbRows []anchored
	for rows.Next() {
		var a anchored
		if err := rows.Scan(&a.ledger, &a.tenant, &a.stream, &a.seq, &a.hash); err != nil {
			rows.Close()
			r.Status, r.Detail = StatusFail, err.Error()
			return r
		}
		dbRows = append(dbRows, a)
	}
	rows.Close()
	for _, a := range dbRows {
		k := a.ledger + "|" + a.tenant.String() + "|" + a.stream
		s, ok := signed[k]
		delete(signed, k)
		switch {
		case !ok:
			r.Issues = append(r.Issues, fmt.Sprintf("%s tenant=%s stream=%s: có trong chain_anchor, không có trong manifest ký số", a.ledger, a.tenant, a.stream))
			continue
		case s.seq != a.seq || s.hash != hex.EncodeToString(a.hash):
			r.Issues = append(r.Issues, fmt.Sprintf("%s tenant=%s stream=%s: chain_anchor seq %d khác manifest seq %d/băm", a.ledger, a.tenant, a.stream, a.seq, s.seq))
			continue
		}
		t := byLedger[a.ledger]
		if t == "" {
			r.Issues = append(r.Issues, a.ledger+": không tìm thấy bảng sổ")
			continue
		}
		var rh []byte
		// t do format('%I.%I') của catalog trả về — đã trích dẫn an toàn.
		err := pool.QueryRow(ctx, "SELECT row_hash FROM "+t+" WHERE tenant_id = $1 AND stream_key = $2 AND seq = $3", a.tenant, a.stream, a.seq).Scan(&rh)
		if err != nil || !bytes.Equal(rh, a.hash) {
			r.Issues = append(r.Issues, fmt.Sprintf("%s tenant=%s stream=%s seq=%d: dòng sổ không trùng băm đã neo", a.ledger, a.tenant, a.stream, a.seq))
		}
	}
	for k := range signed {
		r.Issues = append(r.Issues, k+": có trong manifest ký số, thiếu trong chain_anchor")
	}
	if len(r.Issues) > 0 {
		r.Status, r.Detail = StatusFail, fmt.Sprintf("%s (%s): %d sai lệch", key, date, len(r.Issues))
		return r
	}
	r.Status, r.Detail = StatusPass, fmt.Sprintf("%s: chữ ký hợp lệ (key_id %s), %d đầu chuỗi khớp chain_anchor và bảng sổ", key, m.KeyID, len(dbRows))
	return r
}

func checkObjects(ctx context.Context, cfg Config) CheckResult {
	r := CheckResult{Name: CheckObjects}
	if cfg.Objects == nil || cfg.Store == nil {
		r.Status, r.Detail = StatusSkip, "thiếu manifest kiểm kê kho đối tượng hoặc kho đã phục hồi"
		return r
	}
	probs, err := objsync.Verify(ctx, cfg.Store, *cfg.Objects)
	if err != nil {
		r.Status, r.Detail = StatusFail, err.Error()
		return r
	}
	for _, p := range probs {
		r.Issues = append(r.Issues, p.Key+": "+p.Reason)
	}
	if len(probs) > 0 {
		r.Status, r.Detail = StatusFail, fmt.Sprintf("%d/%d đối tượng sai checksum hoặc thiếu", len(probs), cfg.Objects.Count)
		return r
	}
	r.Status, r.Detail = StatusPass, fmt.Sprintf("%d đối tượng, %d byte: SHA-256 khớp manifest", cfg.Objects.Count, cfg.Objects.TotalBytes)
	return r
}
