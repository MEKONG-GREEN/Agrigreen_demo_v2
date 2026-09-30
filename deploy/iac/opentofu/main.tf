# Gốc hạ tầng G4: mạng theo vùng → cụm Kubernetes được quản trị → nhóm node theo vùng → kho đối
# tượng → KMS → DNS. Sau đó: cài operator + chart Helm theo docs/runbooks/k8s-deploy.md.
locals {
  name = "agrigreen-${var.environment}"
  # Subnet theo vùng mục 17 (R-81/R-82). /20 mỗi vùng trong network_cidr.
  zones   = ["edge", "app-pf", "app-mkg", "identity", "data", "ops"]
  subnets = { for i, z in local.zones : z => cidrsubnet(var.network_cidr, 4, i) }
  tags    = merge(var.tags, { environment = var.environment, provider = var.provider_name })
}

module "network" {
  source          = "./modules/network"
  name            = local.name
  region          = var.region
  network_cidr    = var.network_cidr
  subnets         = local.subnets
  admin_vpn_cidrs = var.admin_vpn_cidrs
  ot_vpn_cidrs    = var.ot_vpn_cidrs
  tags            = local.tags
}

module "cluster" {
  source             = "./modules/cluster"
  name               = local.name
  region             = var.region
  kubernetes_version = var.kubernetes_version
  network_id         = module.network.network_id
  control_subnet_id  = module.network.subnet_ids["ops"]
  api_allowed_cidrs  = var.admin_vpn_cidrs
  kms_key_id         = module.kms.etcd_key_id
  tags               = local.tags
}

module "node_pool" {
  source     = "./modules/node-pool"
  for_each   = var.node_pools
  name       = "${local.name}-${each.key}"
  cluster_id = module.cluster.cluster_id
  subnet_id  = module.network.subnet_ids[each.value.zone]
  pool_label = each.key
  flavor     = each.value.flavor
  min_nodes  = each.value.min_nodes
  max_nodes  = each.value.max_nodes
  disk_gb    = each.value.disk_gb
  dedicated  = each.value.dedicated
  tags       = local.tags
}

module "kms" {
  source = "./modules/kms"
  name   = local.name
  region = var.region
  # Khóa: mã hóa etcd của cụm, KEK kho định danh (AG_VAULT_KEK), sao lưu vault (khóa RIÊNG, mục 22).
  keys = ["etcd", "vault-kek", "backup-data", "backup-vault", "object-storage"]
  tags = local.tags
}

module "object_storage" {
  source           = "./modules/object-storage"
  name             = local.name
  region           = var.region
  secondary_region = var.secondary_region
  buckets          = var.buckets
  kms_key_ids      = module.kms.key_ids
  tags             = local.tags
}

module "dns" {
  source     = "./modules/dns"
  domain     = var.domain
  records    = var.dns_records
  ingress_ip = module.network.ingress_ip
  # ingest.ot.<domain> chỉ ở DNS nội bộ của VPN OT (không công bố Internet).
  private_records = { "ingest.ot" = module.network.ot_ingest_ip }
  tags            = local.tags
}
