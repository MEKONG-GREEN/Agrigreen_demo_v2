variable "environment" {
  description = "Môi trường (mục 21): dev | staging | production."
  type        = string
  validation {
    condition     = contains(["dev", "staging", "production"], var.environment)
    error_message = "environment phải là dev, staging hoặc production."
  }
}

variable "provider_name" {
  description = "Nhà cung cấp đám mây trong lãnh thổ — chờ chọn nhà cung cấp (vd vnpt, viettel, fpt)."
  type        = string
  default     = "chua-chon"
}

variable "provider_country" {
  description = "Quốc gia của hạ tầng VÀ pháp nhân lưu trữ (RB-7, ADR-0007). Chỉ chấp nhận VN."
  type        = string
  default     = "VN"
  validation {
    condition     = var.provider_country == "VN"
    error_message = "RB-7: hạ tầng và pháp nhân lưu trữ phải ở Việt Nam."
  }
}

variable "region" {
  description = "Vùng/khu vực trung tâm dữ liệu của nhà cung cấp (trong lãnh thổ) — chờ chọn nhà cung cấp."
  type        = string
}

variable "secondary_region" {
  description = "Vùng thứ hai TRONG LÃNH THỔ cho bản sao kho đối tượng và sao lưu (mục 22)."
  type        = string
}

variable "domain" {
  description = "Tên miền công khai (chờ MKG chốt), vd agrigreen.example.vn."
  type        = string
}

variable "kubernetes_version" {
  description = "Phiên bản Kubernetes được quản trị (chart yêu cầu >= 1.29)."
  type        = string
  default     = "1.31"
}

variable "network_cidr" {
  description = "Dải mạng riêng của cụm. Mỗi vùng một subnet (R-81/R-82: vùng mạng tách riêng về logic)."
  type        = string
  default     = "10.20.0.0/16"
}

variable "admin_vpn_cidrs" {
  description = "Dải VPN quản trị + MFA (Quản trị, mục 17). API server chỉ mở cho dải này."
  type        = list(string)
  default     = []
}

variable "ot_vpn_cidrs" {
  description = "Dải VPN của vùng Biên OT (hub cân → ingest, mục 14/17)."
  type        = list(string)
  default     = []
}

# Nhóm node theo vùng. Nhãn agrigreen.vn/pool khớp scheduling.nodeSelectorByZone của chart Helm.
variable "node_pools" {
  description = "Nhóm node theo vùng: kích cỡ, số node, taint riêng. Khóa = nhãn agrigreen.vn/pool."
  type = map(object({
    zone      = string # edge | app-pf | app-mkg | identity | data | ops
    flavor    = string # loại máy của nhà cung cấp — chờ chọn nhà cung cấp
    min_nodes = number
    max_nodes = number
    disk_gb   = number
    dedicated = bool # true → taint agrigreen.vn/pool=<khóa>:NoSchedule (chỉ Pod của vùng)
  }))
}

variable "buckets" {
  description = "Bucket kho đối tượng. object_lock = WORM (manifest neo, sao lưu — mục 22)."
  type = map(object({
    purpose        = string
    versioning     = bool
    object_lock    = bool
    retention_days = number
    replicate      = bool # sao chép sang secondary_region (mục 22)
  }))
}

variable "dns_records" {
  description = "Bản ghi DNS công khai (hostname con → loại đích). ingest.ot chỉ ở DNS nội bộ VPN."
  type        = map(string)
  default = {
    "api"       = "ingress"
    "admin"     = "ingress"
    "ops"       = "ingress"
    "mkg"       = "ingress"
    "mkg-api"   = "ingress"
    "mkg-admin" = "ingress"
    "auth"      = "ingress"
  }
}

variable "tags" {
  description = "Nhãn chung gắn vào tài nguyên."
  type        = map(string)
  default     = { project = "agrigreen", gate = "g4" }
}
