# Staging — giống production về cấu hình (mục 21), dữ liệu ẩn danh/tổng hợp. Chỗ giữ — chờ chọn nhà cung cấp.
environment      = "staging"
provider_name    = "chua-chon"
provider_country = "VN"
region           = "vn-south-1"
secondary_region = "vn-north-1"
domain           = "staging.agrigreen.example.vn"
network_cidr     = "10.30.0.0/16"
admin_vpn_cidrs  = ["198.51.100.128/25"]
ot_vpn_cidrs     = ["198.51.100.0/25"]

node_pools = {
  "edge"     = { zone = "edge", flavor = "chờ-chọn-2c4g", min_nodes = 1, max_nodes = 2, disk_gb = 50, dedicated = true }
  "app-pf"   = { zone = "app-pf", flavor = "chờ-chọn-8c16g", min_nodes = 3, max_nodes = 10, disk_gb = 80, dedicated = true }
  "app-mkg"  = { zone = "app-mkg", flavor = "chờ-chọn-8c16g", min_nodes = 3, max_nodes = 8, disk_gb = 80, dedicated = true }
  "identity" = { zone = "identity", flavor = "chờ-chọn-4c8g", min_nodes = 3, max_nodes = 3, disk_gb = 80, dedicated = true }
  "data"     = { zone = "data", flavor = "chờ-chọn-8c32g", min_nodes = 3, max_nodes = 6, disk_gb = 100, dedicated = true }
  "ops"      = { zone = "ops", flavor = "chờ-chọn-4c16g", min_nodes = 1, max_nodes = 2, disk_gb = 100, dedicated = true }
}

buckets = {
  "objects"      = { purpose = "tệp (staging)", versioning = true, object_lock = false, retention_days = 0, replicate = false }
  "anchor"       = { purpose = "manifest neo (staging)", versioning = true, object_lock = true, retention_days = 90, replicate = false }
  "pg-backup"    = { purpose = "pgBackRest (staging)", versioning = true, object_lock = true, retention_days = 14, replicate = false }
  "vault-backup" = { purpose = "pgBackRest vault (staging)", versioning = true, object_lock = true, retention_days = 14, replicate = false }
  "tofu-state"   = { purpose = "trạng thái OpenTofu", versioning = true, object_lock = false, retention_days = 0, replicate = false }
}

# Phase K (G5, ADR-0025): partner.<domain> + chứng thư. Bật bằng cấu hình (R-01) khi khách chốt bên thứ ba đầu tiên.
phase_k = false
