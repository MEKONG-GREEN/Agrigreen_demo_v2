# Production trong lãnh thổ. Giá trị là CHỖ GIỮ — chờ chọn nhà cung cấp (loại máy, vùng, dải VPN).
environment      = "production"
provider_name    = "chua-chon"
provider_country = "VN"
region           = "vn-south-1" # chờ chọn nhà cung cấp
secondary_region = "vn-north-1" # vùng thứ hai TRONG LÃNH THỔ (mục 22)
domain           = "agrigreen.example.vn"
network_cidr     = "10.20.0.0/16"
admin_vpn_cidrs  = ["198.51.100.128/25"]
ot_vpn_cidrs     = ["198.51.100.0/25"]

# R-81: phương án ba node gộp chỉ cho UAT — production tách nhóm node theo vùng.
node_pools = {
  "edge"     = { zone = "edge", flavor = "chờ-chọn-4c8g", min_nodes = 2, max_nodes = 4, disk_gb = 50, dedicated = true }
  "app-pf"   = { zone = "app-pf", flavor = "chờ-chọn-8c16g", min_nodes = 3, max_nodes = 10, disk_gb = 80, dedicated = true }
  "app-mkg"  = { zone = "app-mkg", flavor = "chờ-chọn-8c16g", min_nodes = 3, max_nodes = 8, disk_gb = 80, dedicated = true }
  "identity" = { zone = "identity", flavor = "chờ-chọn-4c8g", min_nodes = 3, max_nodes = 3, disk_gb = 80, dedicated = true }
  "data"     = { zone = "data", flavor = "chờ-chọn-8c32g", min_nodes = 3, max_nodes = 6, disk_gb = 100, dedicated = true }
  "ops"      = { zone = "ops", flavor = "chờ-chọn-4c16g", min_nodes = 2, max_nodes = 4, disk_gb = 200, dedicated = true }
}

buckets = {
  "objects"      = { purpose = "tệp, ảnh mã hóa theo hộ, báo cáo (AG_S3_BUCKET)", versioning = true, object_lock = false, retention_days = 0, replicate = true }
  "anchor"       = { purpose = "manifest neo chuỗi băm ký số (mục 8.2)", versioning = true, object_lock = true, retention_days = 3650, replicate = true }
  "pg-backup"    = { purpose = "pgBackRest cụm pf/mkg/ts/iam (mục 22)", versioning = true, object_lock = true, retention_days = 35, replicate = true }
  "vault-backup" = { purpose = "pgBackRest cụm vault — khóa riêng (mục 22)", versioning = true, object_lock = true, retention_days = 35, replicate = true }
  "tofu-state"   = { purpose = "trạng thái OpenTofu", versioning = true, object_lock = false, retention_days = 0, replicate = true }
}
