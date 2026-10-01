# Dev — cụm dùng chung nhỏ, dữ liệu tổng hợp. Chỗ giữ — chờ chọn nhà cung cấp.
environment      = "dev"
provider_name    = "chua-chon"
provider_country = "VN"
region           = "vn-south-1"
secondary_region = "vn-south-1"
domain           = "agrigreen.dev.example.vn"
network_cidr     = "10.40.0.0/16"
admin_vpn_cidrs  = ["198.51.100.128/25"]
ot_vpn_cidrs     = []

node_pools = {
  "app-pf" = { zone = "app-pf", flavor = "chờ-chọn-4c8g", min_nodes = 2, max_nodes = 3, disk_gb = 50, dedicated = false }
  "data"   = { zone = "data", flavor = "chờ-chọn-4c16g", min_nodes = 1, max_nodes = 2, disk_gb = 100, dedicated = false }
}

buckets = {
  "objects"    = { purpose = "tệp (dev)", versioning = true, object_lock = false, retention_days = 0, replicate = false }
  "anchor"     = { purpose = "manifest neo (dev)", versioning = true, object_lock = false, retention_days = 0, replicate = false }
  "tofu-state" = { purpose = "trạng thái OpenTofu", versioning = true, object_lock = false, retention_days = 0, replicate = false }
}

# Phase K (G5, ADR-0025): partner.<domain> + chứng thư. Bật bằng cấu hình (R-01) khi khách chốt bên thứ ba đầu tiên.
phase_k = false
