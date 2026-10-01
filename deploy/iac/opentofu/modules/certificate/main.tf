# Chứng thư TLS công khai cho một tập hostname (G5: partner.<domain> của Public API, ADR-0022/0025).
# CA phát hành TRONG LÃNH THỔ (RB-7) — chờ chọn nhà cung cấp. Khóa riêng không rời trình quản lý bí mật:
# chứng thư + khóa được ghi vào <remotePrefix>/<secret_path> (vd agrigreen/prod/tls/partner, thuộc tính
# tls.crt, tls.key), External Secrets đồng bộ thành Secret agrigreen-partner-tls ở agrigreen-pf.
variable "name" { type = string }
variable "hostnames" { type = list(string) }
variable "secret_path" { type = string }
variable "tags" { type = map(string) }

resource "terraform_data" "certificate" {
  input = {
    name        = var.name
    hostnames   = var.hostnames
    key_type    = "ECDSA-P256"
    validity    = "397d" # trần CA/B Forum cho chứng thư công khai
    renew       = "30d"  # gia hạn trước 30 ngày; Traefik tự nạp Secret mới, không cần khởi động lại
    secret_path = var.secret_path
    tags        = var.tags
  }
}

# chờ chọn nhà cung cấp:
# resource "<provider>_certificate" "this" { domains = var.hostnames; ... }
# resource "<provider>_secret_version" "tls" { path = var.secret_path; data = { "tls.crt" = ..., "tls.key" = ... } }

output "id" { value = terraform_data.certificate.id }
output "hostnames" { value = var.hostnames }
