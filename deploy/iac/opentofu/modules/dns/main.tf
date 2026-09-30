# DNS: bản ghi công khai tới LB Ingress (Biên/DMZ); bản ghi nội bộ (vùng VPN) cho ingest.ot.
# Nhà cung cấp DNS trong lãnh thổ — chờ chọn nhà cung cấp.
variable "domain" { type = string }
variable "records" { type = map(string) }
variable "ingress_ip" { type = string }
variable "private_records" { type = map(string) }
variable "tags" { type = map(string) }

locals {
  public  = { for h, kind in var.records : "${h}.${var.domain}" => (kind == "ingress" ? var.ingress_ip : kind) }
  private = { for h, ip in var.private_records : "${h}.${var.domain}" => ip }
}

resource "terraform_data" "zone" {
  input = {
    domain  = var.domain
    public  = local.public
    private = local.private
    dnssec  = true
    tags    = var.tags
  }
}

# chờ chọn nhà cung cấp:
# resource "<provider>_dns_record" "public"  { for_each = local.public;  name = each.key; type = "A"; value = each.value }
# resource "<provider>_dns_record" "private" { for_each = local.private; zone = "<vùng nội bộ VPN>"; ... }

output "fqdns" { value = keys(local.public) }
