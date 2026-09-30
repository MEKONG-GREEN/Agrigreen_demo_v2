# Mạng theo vùng (mục 17, R-80..R-82): VPC riêng, một subnet mỗi vùng, không subnet dữ liệu/định danh
# nào có định tuyến Internet ra; cổng VPN quản trị và VPN Biên OT; địa chỉ LB cho Ingress (Biên/DMZ)
# và LB nội bộ cho ingest (chỉ dải OT VPN).
variable "name" { type = string }
variable "region" { type = string }
variable "network_cidr" { type = string }
variable "subnets" { type = map(string) }
variable "admin_vpn_cidrs" { type = list(string) }
variable "ot_vpn_cidrs" { type = list(string) }
variable "tags" { type = map(string) }

locals {
  # Vùng được có đường ra Internet (NAT) — chỉ Biên/DMZ (LB) và Quản trị (tải image/cập nhật qua proxy).
  # Ứng dụng, Dữ liệu, Định danh: KHÔNG NAT ra Internet; tới dịch vụ trong lãnh thổ qua endpoint riêng.
  egress_internet = { for z, _ in var.subnets : z => contains(["edge", "ops"], z) }
}

# Ý định (không cần provider). Thay bằng tài nguyên VPC/subnet/security group/VPN của nhà cung cấp —
# chờ chọn nhà cung cấp.
resource "terraform_data" "network" {
  input = {
    name            = var.name
    region          = var.region
    cidr            = var.network_cidr
    subnets         = var.subnets
    egress_internet = local.egress_internet
    admin_vpn       = var.admin_vpn_cidrs
    ot_vpn          = var.ot_vpn_cidrs
    tags            = var.tags
  }
}

# chờ chọn nhà cung cấp:
# resource "<provider>_vpc"    "this"  { name = var.name, cidr = var.network_cidr }
# resource "<provider>_subnet" "zone"  { for_each = var.subnets, vpc_id = ..., cidr = each.value }
# resource "<provider>_nat"    "egress" { for_each = { for z, on in local.egress_internet : z => on if on } ... }
# resource "<provider>_vpn_gateway" "admin" { ... cidrs = var.admin_vpn_cidrs, MFA ở IdP }
# resource "<provider>_vpn_gateway" "ot"    { ... cidrs = var.ot_vpn_cidrs } — site-to-site với hub cân
# resource "<provider>_lb_ip" "ingress"  { public = true }   # Biên → DMZ (WAF/chống DDoS của nhà cung cấp trong lãnh thổ)
# resource "<provider>_lb_ip" "ot_ingest" { public = false } # chỉ đi qua VPN OT

output "network_id" { value = terraform_data.network.id }
output "subnet_ids" { value = { for z, c in var.subnets : z => "${terraform_data.network.id}/${z}" } }
output "ingress_ip" { value = "0.0.0.0" }   # chờ chọn nhà cung cấp
output "ot_ingest_ip" { value = "0.0.0.0" } # chờ chọn nhà cung cấp
