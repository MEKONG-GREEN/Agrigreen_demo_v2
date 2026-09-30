# Cụm Kubernetes được quản trị trong lãnh thổ (Phụ lục A G4). Yêu cầu tối thiểu với nhà cung cấp
# (R-81, R-82, mục 18): CNI có NetworkPolicy (Calico/Cilium), API server riêng chỉ mở cho VPN quản trị,
# etcd mã hóa bằng KMS trong lãnh thổ, nhật ký kiểm toán API ≥ 3 tháng trong lãnh thổ, Pod Security
# Admission, admission từ chối image chưa ký (mục 20), node không có IP công khai.
variable "name" { type = string }
variable "region" { type = string }
variable "kubernetes_version" { type = string }
variable "network_id" { type = string }
variable "control_subnet_id" { type = string }
variable "api_allowed_cidrs" { type = list(string) }
variable "kms_key_id" { type = string }
variable "tags" { type = map(string) }

resource "terraform_data" "cluster" {
  input = {
    name                = var.name
    region              = var.region
    version             = var.kubernetes_version
    network             = var.network_id
    private_api         = true
    api_allowed_cidrs   = var.api_allowed_cidrs
    cni_network_policy  = true
    etcd_encryption_key = var.kms_key_id
    audit_log_retention = "90d" # mục 18: tối thiểu 3 tháng, trong lãnh thổ
    pod_security        = "restricted"
    signed_images_only  = true
    tags                = var.tags
  }
}

# chờ chọn nhà cung cấp:
# resource "<provider>_kubernetes_cluster" "this" {
#   name = var.name; version = var.kubernetes_version; private_endpoint = true
#   network_policy = "calico"; encryption { kms_key_id = var.kms_key_id }
#   api_server_authorized_ranges = var.api_allowed_cidrs
# }

output "cluster_id" { value = terraform_data.cluster.id }
output "api_server_cidrs" { value = [] } # chờ chọn nhà cung cấp
