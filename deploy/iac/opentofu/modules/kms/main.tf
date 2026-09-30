# KMS trong lãnh thổ (mục 4 "Trình quản lý bí mật + KMS trong lãnh thổ", RB-7). Khóa không rời KMS;
# External Secrets đọc bí mật từ trình quản lý bí mật trong lãnh thổ (ClusterSecretStore agrigreen-vn-secrets).
variable "name" { type = string }
variable "region" { type = string }
variable "keys" { type = list(string) }
variable "tags" { type = map(string) }

resource "terraform_data" "key" {
  for_each = toset(var.keys)
  input = {
    name       = "${var.name}-${each.key}"
    region     = var.region
    rotation   = "365d"
    protection = "hsm"
    tags       = var.tags
  }
}

# chờ chọn nhà cung cấp:
# resource "<provider>_kms_key" "this" { for_each = toset(var.keys); name = "${var.name}-${each.key}"; rotation_days = 365 }

output "key_ids" { value = { for k, v in terraform_data.key : k => v.id } }
output "etcd_key_id" { value = terraform_data.key["etcd"].id }
output "cidrs" { value = [] } # chờ chọn nhà cung cấp
