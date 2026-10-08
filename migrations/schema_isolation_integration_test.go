package migrations_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Cổng W9.1 (ADR-0028 §4, R-DB-05, kiến trúc v4.1 §17): không vai nhóm r_<module> nào có quyền hiệu lực trên đối
// tượng thuộc schema của MODULE KHÁC, trừ k_ref (bản chiếu lõi, mọi r_* đọc) và schema công bố *_pub. Xét trên mọi
// cụm sau khi migrate tới head: USAGE/CREATE trên schema; mọi quyền trên bảng, khung nhìn, chuỗi số; quyền cột
// riêng; EXECUTE trên hàm. Quyền có được qua PUBLIC (vd hàm mặc định EXECUTE cho PUBLIC) không tính — đó không
// phải cấp quyền cho module; schema không thuộc module nào (shared, public, ops) ngoài phạm vi.
//
// Vi phạm đang tồn tại hợp lệ tạm thời ghi trong schema_isolation_exceptions.yaml (chỉ được co lại): mỗi mục
// có gói kế hoạch gỡ; cổng hỏng khi có vi phạm mới HOẶC ngoại lệ lỗi thời (không còn xảy ra).
func TestNoCrossModuleSchemaPrivileges_W91(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	exc := loadIsolationExceptions(t)

	owner := map[string]string{} // schema → module
	for _, e := range modreg.Catalog() {
		for _, s := range e.OwnsSchema {
			owner[s] = e.Name
		}
	}
	allowed := func(mod, schema string) bool {
		return owner[schema] == "" || owner[schema] == mod || schema == "k_ref" || strings.HasSuffix(schema, "_pub")
	}

	const q = `
		WITH s AS (SELECT oid, nspname FROM pg_namespace WHERE nspname = ANY($2::text[])),
		rel AS (SELECT c.oid, c.relkind, s.nspname, c.oid::regclass::text AS name
		          FROM pg_class c JOIN s ON s.oid = c.relnamespace
		         WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S'))
		SELECT nspname, 'schema ' || nspname, p FROM s, unnest(ARRAY['USAGE', 'CREATE']) p
		 WHERE has_schema_privilege($1, s.oid, p) AND NOT has_schema_privilege('public', s.oid, p)
		UNION ALL
		SELECT nspname, 'table ' || name, p FROM rel, unnest(ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER']) p
		 WHERE relkind <> 'S' AND has_table_privilege($1, oid, p) AND NOT has_table_privilege('public', oid, p)
		UNION ALL
		SELECT nspname, 'sequence ' || name, p FROM rel, unnest(ARRAY['USAGE', 'SELECT', 'UPDATE']) p
		 WHERE relkind = 'S' AND has_sequence_privilege($1, oid, p) AND NOT has_sequence_privilege('public', oid, p)
		UNION ALL
		SELECT DISTINCT nspname, 'column ' || name || '.' || a.attname, p FROM rel JOIN pg_attribute a ON a.attrelid = rel.oid,
		       unnest(ARRAY['SELECT', 'INSERT', 'UPDATE', 'REFERENCES']) p
		 WHERE relkind <> 'S' AND a.attnum > 0 AND NOT a.attisdropped
		   AND has_column_privilege($1, rel.oid, a.attnum, p) AND NOT has_table_privilege($1, rel.oid, p)
		   AND NOT has_column_privilege('public', rel.oid, a.attnum, p)
		UNION ALL
		SELECT s.nspname, 'function ' || f.oid::regprocedure::text, 'EXECUTE' FROM pg_proc f JOIN s ON s.oid = f.pronamespace
		 WHERE has_function_privilege($1, f.oid, 'EXECUTE') AND NOT has_function_privilege('public', f.oid, 'EXECUTE')
		ORDER BY 1, 2, 3`

	type hit struct{ cluster, role, schema, object, privs string }
	var hits []hit
	for _, c := range migrations.Clusters {
		admin := env.AdminPool(t, c)
		var present []string
		rows, err := admin.Query(ctx, `SELECT nspname FROM pg_namespace`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			present = append(present, s)
		}
		rows.Close()
		for _, g := range dbadmin.ClusterGroups(c) {
			var foreign []string
			for _, s := range present {
				if !allowed(g.Module, s) {
					foreign = append(foreign, s)
				}
			}
			rows, err := admin.Query(ctx, q, g.Role, foreign)
			if err != nil {
				t.Fatalf("%s %s: %v", c, g.Role, err)
			}
			byObj := map[string]*hit{}
			var order []string
			for rows.Next() {
				var schema, obj, p string
				if err := rows.Scan(&schema, &obj, &p); err != nil {
					t.Fatal(err)
				}
				h := byObj[obj]
				if h == nil {
					h = &hit{cluster: string(c), role: g.Role, schema: schema, object: obj}
					byObj[obj] = h
					order = append(order, obj)
				}
				h.privs = strings.TrimPrefix(h.privs+","+p, ",")
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			for _, o := range order {
				hits = append(hits, *byObj[o])
			}
		}
	}

	// PUBLIC không được có quyền trên schema/bảng/chuỗi số của module (ngoài k_ref, *_pub): quyền đó đến mọi vai,
	// kể cả vai đăng nhập đơn vị, vượt mọi phép kiểm r_* ở trên. Hàm không tính (EXECUTE mặc định cho PUBLIC;
	// cần USAGE schema mới gọi được).
	const qPublic = `
		SELECT 'schema ' || nspname || ' ' || p FROM pg_namespace s, unnest(ARRAY['USAGE', 'CREATE']) p
		 WHERE nspname = ANY($1::text[]) AND has_schema_privilege('public', s.oid, p)
		UNION ALL
		SELECT 'table ' || c.oid::regclass::text || ' ' || p
		  FROM pg_class c JOIN pg_namespace s ON s.oid = c.relnamespace,
		       unnest(ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER']) p
		 WHERE s.nspname = ANY($1::text[]) AND c.relkind IN ('r', 'p', 'v', 'm', 'f') AND has_table_privilege('public', c.oid, p)
		ORDER BY 1`
	var moduleSchemas []string
	for s := range owner {
		if !allowed("", s) {
			moduleSchemas = append(moduleSchemas, s)
		}
	}
	for _, c := range migrations.Clusters {
		rows, err := env.AdminPool(t, c).Query(ctx, qPublic, moduleSchemas)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var d string
			if err := rows.Scan(&d); err != nil {
				t.Fatal(err)
			}
			t.Errorf("[%s] PUBLIC có quyền trên đối tượng module (W9.1): %s", c, d)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}

	used := make([]bool, len(exc))
	var bad []string
	for _, h := range hits {
		i := slices.IndexFunc(exc, func(e isolationException) bool { return e.matches(h.cluster, h.role, h.schema, h.object) })
		if i >= 0 {
			used[i] = true
			continue
		}
		bad = append(bad, fmt.Sprintf("[%s] %s → %s (%s): %s", h.cluster, h.role, h.object, owner[h.schema], h.privs))
	}
	for _, b := range bad {
		t.Errorf("quyền chéo schema module (W9.1): %s", b)
	}
	for i, e := range exc {
		if !used[i] {
			t.Errorf("ngoại lệ lỗi thời (%s) không còn xảy ra — xóa khỏi schema_isolation_exceptions.yaml: %s", e.Owner, e)
		}
	}
	// Kiểm có nghĩa: phép đo thấy được quyền thật (r_content đọc bảng của mình) và quyền chéo nếu có.
	var own bool
	if err := env.AdminPool(t, migrations.PF).QueryRow(ctx,
		`SELECT has_table_privilege('r_content', 'pf_content.document', 'SELECT') AND NOT has_table_privilege('public', 'pf_content.document', 'SELECT')`).Scan(&own); err != nil {
		t.Fatal(err)
	}
	if !own {
		t.Fatal("r_content phải có SELECT pf_content.document (phép đo hỏng)")
	}
	t.Logf("W9.1 quyền chéo schema: %d đối tượng, %d mục ngoại lệ", len(hits), len(exc))
}

// isolationException cho phép tạm một nhóm quyền chéo. Object rỗng = mọi đối tượng của schema; object kết thúc
// bằng "*" là tiền tố. Cluster rỗng = mọi cụm.
type isolationException struct {
	Cluster string `yaml:"cluster"`
	Role    string `yaml:"role"`
	Schema  string `yaml:"schema"`
	Object  string `yaml:"object"`
	Reason  string `yaml:"reason"`
	Owner   string `yaml:"owner"`
}

func (e isolationException) matches(cluster, role, schema, object string) bool {
	if (e.Cluster != "" && e.Cluster != cluster) || e.Role != role || e.Schema != schema {
		return false
	}
	switch {
	case e.Object == "":
		return true
	case strings.HasSuffix(e.Object, "*"):
		return strings.HasPrefix(object, strings.TrimSuffix(e.Object, "*"))
	}
	return object == e.Object
}

func (e isolationException) String() string {
	o := e.Object
	if o == "" {
		o = e.Schema + ".*"
	}
	return fmt.Sprintf("%s → %s", e.Role, o)
}

var reIsoOwner = regexp.MustCompile(`^W[0-9]+\.[0-9]+$`)

// loadExceptions đọc tệp ngoại lệ YAML (chỉ được co lại) và kiểm định dạng: role, schema, reason, owner W<n>.<m>
// bắt buộc; không lặp; chỉ khóa đã biết.
func loadExceptions[T any](t *testing.T, file string, key func(T) string, check func(T) error) []T {
	t.Helper()
	data, err := os.ReadFile(file) //nolint:gosec // tệp trong repo
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Exceptions []T `yaml:"exceptions"`
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	seen := map[string]bool{}
	for i, e := range f.Exceptions {
		if err := check(e); err != nil {
			t.Fatalf("%s: mục %d: %v", file, i+1, err)
		}
		if seen[key(e)] {
			t.Fatalf("%s: mục %d lặp", file, i+1)
		}
		seen[key(e)] = true
	}
	return f.Exceptions
}

func loadIsolationExceptions(t *testing.T) []isolationException {
	return loadExceptions(t, "schema_isolation_exceptions.yaml",
		func(e isolationException) string { return e.Cluster + "|" + e.Role + "|" + e.Schema + "|" + e.Object },
		func(e isolationException) error {
			if e.Role == "" || e.Schema == "" || strings.TrimSpace(e.Reason) == "" || !reIsoOwner.MatchString(e.Owner) {
				return fmt.Errorf("role, schema, reason, owner (W<n>.<m>) bắt buộc: %+v", e)
			}
			return nil
		})
}
