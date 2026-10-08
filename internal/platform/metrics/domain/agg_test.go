package domain

import (
	"errors"
	"testing"
)

func sampleAggJob() AggJob {
	return AggJob{
		ID: "cc.sample", Version: 1, Views: []string{"pf_cropcycle_pub.v1_harvest_declaration"},
		Query:    `SELECT h.commune_code, sum(h.mass_kg)::text FROM pf_cropcycle_pub.v1_harvest_declaration h GROUP BY 1`,
		Dims:     []string{"commune_code"},
		Measures: []string{"mass_kg"},
		Purpose:  "Tổng sản lượng theo xã",
		KMin:     10,
	}
}

// W6.4 (ADR-0031 §4): quy tắc danh mục job — dạng id/phiên bản/view _pub, câu SQL chỉ đọc view đã khai, k ≥ k sàn.
func TestValidateAggJob_W64(t *testing.T) {
	if err := ValidateAggJob(sampleAggJob(), KFloorDefault); err != nil {
		t.Fatalf("job mẫu: %v", err)
	}
	cases := []struct {
		name string
		mod  func(*AggJob)
		want error
	}{
		{"id sai dạng", func(j *AggJob) { j.ID = "Sample" }, ErrAggJobID},
		{"phiên bản 0", func(j *AggJob) { j.Version = 0 }, ErrAggVersion},
		{"không view", func(j *AggJob) { j.Views = nil }, ErrAggViews},
		{"view không _pub", func(j *AggJob) { j.Views = []string{"pf_cropcycle.harvest_declaration"} }, ErrAggViews},
		{"view trùng", func(j *AggJob) { j.Views = append(j.Views, j.Views[0]) }, ErrAggViews},
		{"đọc bảng nội bộ", func(j *AggJob) {
			j.Query = `SELECT commune_code, sum(mass_kg)::text FROM pf_cropcycle.harvest_declaration GROUP BY 1`
		}, ErrAggQuery},
		{"đọc view không khai", func(j *AggJob) {
			j.Query = `SELECT province_code, count(*)::text FROM pf_extension_pub.v1_field_report GROUP BY 1`
		}, ErrAggQuery},
		{"đọc k_ref", func(j *AggJob) { j.Query += ` UNION ALL SELECT tenant_id::text, '0' FROM k_ref.org` }, ErrAggQuery},
		{"tự đặt tenant", func(j *AggJob) { j.Query = `SELECT set_config('app.tenant_id', '', true), '0'` }, errAggTenantRefs},
		{"không số đo", func(j *AggJob) { j.Measures = nil }, ErrAggColumns},
		{"mục đích ngắn", func(j *AggJob) { j.Purpose = "ngắn" }, ErrAggPurpose},
		{"k dưới sàn", func(j *AggJob) { j.KMin = 5 }, ErrKBelowFloor},
	}
	for _, c := range cases {
		j := sampleAggJob()
		c.mod(&j)
		if err := ValidateAggJob(j, KFloorDefault); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, muốn %v", c.name, err, c.want)
		}
	}
	// k sàn cao hơn k của job (ngưỡng ký sau C-4) → job cũ không còn hợp lệ.
	if err := ValidateAggJob(sampleAggJob(), 15); !errors.Is(err, ErrKBelowFloor) {
		t.Errorf("k sàn 15: %v", err)
	}
}

// k áp = max(k job, k sàn, 2); CheckK từ chối k dưới sàn, sàn < 2 coi như 2.
func TestAggKFloor_W64(t *testing.T) {
	j := sampleAggJob()
	if got := KUsed(j, 5); got != 10 {
		t.Errorf("KUsed(sàn 5) = %d", got)
	}
	if got := KUsed(j, 20); got != 20 {
		t.Errorf("KUsed(sàn 20) = %d", got)
	}
	if err := CheckK(9, 10); !errors.Is(err, ErrKBelowFloor) {
		t.Errorf("CheckK(9, 10) = %v", err)
	}
	if err := CheckK(1, 0); !errors.Is(err, ErrKBelowFloor) {
		t.Errorf("CheckK(1, 0) = %v", err)
	}
	if err := CheckK(10, 10); err != nil {
		t.Errorf("CheckK(10, 10) = %v", err)
	}
}

// Băm định nghĩa: không phụ thuộc thứ tự view và khoảng trắng của SQL; đổi bất kỳ trường nào → băm khác.
func TestAggJobHash_W64(t *testing.T) {
	a, b := sampleAggJob(), sampleAggJob()
	b.Query = "  SELECT h.commune_code,\n\tsum(h.mass_kg)::text   FROM pf_cropcycle_pub.v1_harvest_declaration h GROUP BY 1 "
	if a.Hash() != b.Hash() || len(a.Hash()) != 64 {
		t.Fatalf("băm khác vì khoảng trắng: %s %s", a.Hash(), b.Hash())
	}
	for name, mod := range map[string]func(*AggJob){
		"version": func(j *AggJob) { j.Version = 2 }, "k": func(j *AggJob) { j.KMin = 11 },
		"query": func(j *AggJob) { j.Query += " HAVING true" }, "purpose": func(j *AggJob) { j.Purpose += "!" },
		"views": func(j *AggJob) { j.Views = append(j.Views, "pf_geo_pub.v1_plot_event") },
	} {
		c := sampleAggJob()
		mod(&c)
		if c.Hash() == a.Hash() {
			t.Errorf("đổi %s mà băm không đổi", name)
		}
	}
}

// Gộp qua tenant: cộng số đo, đếm tenant góp (một tenant nhiều dòng chỉ tính một), ẩn nhóm < k.
func TestAggAccumulatorSuppress_W64(t *testing.T) {
	acc := NewAggAccumulator(sampleAggJob())
	for i, tn := range []string{"t1", "t1", "t2", "t3"} {
		if err := acc.Add(tn, []string{"X1"}, []string{"1.5"}); err != nil {
			t.Fatal(i, err)
		}
	}
	if err := acc.Add("t1", []string{"X2"}, []string{"7"}); err != nil {
		t.Fatal(err)
	}
	if err := acc.Add("t1", []string{"X2"}, []string{"7", "8"}); err == nil {
		t.Error("số đo thừa phải lỗi")
	}
	groups, hidden := acc.Suppress(3)
	if len(groups) != 1 || hidden != 1 || groups[0].Dims[0] != "X1" || groups[0].Contributors != 3 ||
		groups[0].Measures[0].String() != "6" || acc.Rows != 5 {
		t.Fatalf("nhóm %+v, ẩn %d, dòng %d", groups, hidden, acc.Rows)
	}
}
