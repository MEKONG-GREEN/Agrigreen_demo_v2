// Package clusterenv chọn cụm DB và URL gốc theo cụm cho các lệnh vận hành chạy
// trên nhiều cụm (migrate, outbox-publisher, lịch vận hành của worker).
//
// Tách đơn vị triển khai bằng cấu hình (RB-5, R-01; G4 Kubernetes, ADR-0008): mỗi vùng
// chạy bản riêng của lệnh, chỉ nối tới cụm của vùng mình, không cần sửa mã.
//
//	AG_CLUSTERS        danh sách cụm phân tách dấu phẩy (pf, mkg, vault, ts). Rỗng = mọi
//	                   cụm (giữ hành vi Compose ĐTK 1).
//	AG_<CỤM>_DB_URL    URL gốc (host, cổng, tham số) riêng của một cụm, vd AG_MKG_DB_URL.
//	                   Thiếu → dùng URL gốc chung của lệnh (AG_DB_URL / AG_DB_DIRECT_URL).
//	                   Tên kết thúc _URL nên guard RB-7 (internal/shared/residency) kiểm đích.
//
// Chỉ lấy host/cổng/tham số từ URL; vai và database do dbadmin.RoleURL dựng.
package clusterenv

import (
	"fmt"
	"os"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// EnvClusters là biến chọn cụm.
const EnvClusters = "AG_CLUSTERS"

// Selected đọc AG_CLUSTERS. Rỗng → migrations.Clusters (mọi cụm, đúng thứ tự áp).
// Kết quả giữ thứ tự của migrations.Clusters; tên lạ hoặc lặp → lỗi.
func Selected() ([]migrations.Cluster, error) { return Parse(os.Getenv(EnvClusters)) }

// Parse là phần thuần của Selected (kiểm thử được).
func Parse(v string) ([]migrations.Cluster, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return append([]migrations.Cluster(nil), migrations.Clusters...), nil
	}
	want := map[migrations.Cluster]bool{}
	for _, p := range strings.Split(v, ",") {
		c := migrations.Cluster(strings.ToLower(strings.TrimSpace(p)))
		if c == "" {
			continue
		}
		if !known(c) {
			return nil, fmt.Errorf("%s: cụm không hợp lệ %q (pf, mkg, vault, ts)", EnvClusters, string(c))
		}
		if want[c] {
			return nil, fmt.Errorf("%s: cụm %q lặp", EnvClusters, string(c))
		}
		want[c] = true
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("%s không có cụm nào", EnvClusters)
	}
	out := make([]migrations.Cluster, 0, len(want))
	for _, c := range migrations.Clusters {
		if want[c] {
			out = append(out, c)
		}
	}
	return out, nil
}

// Has cho biết c có trong danh sách.
func Has(cs []migrations.Cluster, c migrations.Cluster) bool {
	for _, x := range cs {
		if x == c {
			return true
		}
	}
	return false
}

// EnvBaseURL là tên biến URL gốc riêng của cụm c, vd AG_MKG_DB_URL.
func EnvBaseURL(c migrations.Cluster) string { return "AG_" + strings.ToUpper(string(c)) + "_DB_URL" }

// BaseURL trả AG_<CỤM>_DB_URL nếu có, ngược lại fallback.
func BaseURL(c migrations.Cluster, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(EnvBaseURL(c))); v != "" {
		return v
	}
	return fallback
}

func known(c migrations.Cluster) bool {
	for _, x := range migrations.Clusters {
		if x == c {
			return true
		}
	}
	return false
}
