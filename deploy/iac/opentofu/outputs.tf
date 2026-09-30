output "cluster_id" {
  description = "Mã cụm Kubernetes được quản trị."
  value       = module.cluster.cluster_id
}

output "api_server_cidrs" {
  description = "Địa chỉ API server → external.kubeAPI của values Helm."
  value       = module.cluster.api_server_cidrs
}

output "subnets" {
  description = "Subnet theo vùng (mục 17)."
  value       = module.network.subnet_ids
}

output "object_storage_endpoint" {
  description = "Endpoint S3 trong lãnh thổ → common.s3.endpoint, postgres.backup.s3.endpoint."
  value       = module.object_storage.endpoint
}

output "object_storage_cidrs" {
  description = "Dải địa chỉ kho đối tượng → external.objectStorage."
  value       = module.object_storage.cidrs
}

output "kms_cidrs" {
  description = "Dải KMS → external.kms."
  value       = module.kms.cidrs
}

output "buckets" {
  description = "Tên bucket đã tạo."
  value       = module.object_storage.bucket_names
}
