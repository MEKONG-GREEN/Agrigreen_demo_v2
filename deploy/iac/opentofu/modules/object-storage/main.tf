# Kho đối tượng tương thích S3 TRONG LÃNH THỔ (RB-7, mục 22): versioning, khóa đối tượng (WORM) cho
# manifest neo và sao lưu, mã hóa bằng KMS trong lãnh thổ, sao chép sang vùng thứ hai trong lãnh thổ.
# Thông tin xác thực: ghi sao lưu TÁCH khỏi phục hồi; ứng dụng không có quyền xóa (mục 22).
variable "name" { type = string }
variable "region" { type = string }
variable "secondary_region" { type = string }
variable "buckets" {
  type = map(object({
    purpose        = string
    versioning     = bool
    object_lock    = bool
    retention_days = number
    replicate      = bool
  }))
}
variable "kms_key_ids" { type = map(string) }
variable "tags" { type = map(string) }

locals {
  bucket_names = { for k, _ in var.buckets : k => "${var.name}-${k}" }
  # Kho định danh: bucket sao lưu dùng khóa RIÊNG (mục 22).
  key_for = { for k, _ in var.buckets : k => (k == "vault-backup" ? var.kms_key_ids["backup-vault"] : (k == "pg-backup" ? var.kms_key_ids["backup-data"] : var.kms_key_ids["object-storage"])) }
}

resource "terraform_data" "bucket" {
  for_each = var.buckets
  input = {
    name           = local.bucket_names[each.key]
    region         = var.region
    purpose        = each.value.purpose
    versioning     = each.value.versioning
    object_lock    = each.value.object_lock
    retention_days = each.value.retention_days
    replicate_to   = each.value.replicate ? var.secondary_region : null
    kms_key        = local.key_for[each.key]
    block_public   = true
    tags           = var.tags
  }
  lifecycle {
    precondition {
      condition     = !each.value.object_lock || each.value.versioning
      error_message = "Khóa đối tượng cần versioning."
    }
  }
}

# chờ chọn nhà cung cấp:
# resource "<provider>_s3_bucket" "this" { for_each = var.buckets; name = local.bucket_names[each.key]
#   versioning = each.value.versioning; object_lock { mode = "COMPLIANCE", days = each.value.retention_days }
#   encryption { kms_key_id = local.key_for[each.key] }; replication { region = var.secondary_region } }
# resource "<provider>_s3_access_key" "app"            {}  # Read/Write/List, KHÔNG Delete
# resource "<provider>_s3_access_key" "backup_writer"  {}  # chỉ ghi sao lưu
# resource "<provider>_s3_access_key" "backup_restore" {}  # chỉ đọc — tách khỏi ghi (mục 22)

output "bucket_names" { value = local.bucket_names }
output "endpoint" { value = "chờ chọn nhà cung cấp" }
output "cidrs" { value = [] }
