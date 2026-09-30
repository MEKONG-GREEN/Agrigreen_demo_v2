# Nhóm node theo vùng: nhãn agrigreen.vn/pool=<pool_label> (chart: scheduling.nodeSelectorByZone),
# taint khi dedicated để chỉ Pod của vùng chạy trên đó (tách vật lý Dữ liệu / Định danh khỏi Ứng dụng).
variable "name" { type = string }
variable "cluster_id" { type = string }
variable "subnet_id" { type = string }
variable "pool_label" { type = string }
variable "flavor" { type = string }
variable "min_nodes" { type = number }
variable "max_nodes" { type = number }
variable "disk_gb" { type = number }
variable "dedicated" { type = bool }
variable "tags" { type = map(string) }

locals {
  labels = { "agrigreen.vn/pool" = var.pool_label }
  taints = var.dedicated ? [{ key = "agrigreen.vn/pool", value = var.pool_label, effect = "NoSchedule" }] : []
}

resource "terraform_data" "pool" {
  input = {
    name           = var.name
    cluster        = var.cluster_id
    subnet         = var.subnet_id
    flavor         = var.flavor
    min_nodes      = var.min_nodes
    max_nodes      = var.max_nodes
    disk_gb        = var.disk_gb
    disk_encrypted = true
    labels         = local.labels
    taints         = local.taints
    tags           = var.tags
  }
}

# chờ chọn nhà cung cấp:
# resource "<provider>_kubernetes_node_pool" "this" {
#   cluster_id = var.cluster_id; subnet_id = var.subnet_id; flavor = var.flavor
#   autoscale { min = var.min_nodes, max = var.max_nodes }; labels = local.labels; taints = local.taints
# }

output "labels" { value = local.labels }
output "taints" { value = local.taints }
