# OpenTofu (MPL-2.0) — hạ tầng G4 trong lãnh thổ (Phụ lục A, mục 4 "OpenTofu, Helm, Argo CD", RB-7).
#
# NHÀ CUNG CẤP CHƯA CHỌN (VNPT / Viettel / FPT Cloud hoặc khác có hạ tầng + pháp nhân trong lãnh thổ,
# R-82 là điều kiện mua sắm). Bộ khung này KHÔNG phụ thuộc provider: mỗi module khai báo đầu vào/đầu ra
# trung lập và một tài nguyên terraform_data ghi ý định; phần gọi API của nhà cung cấp đánh dấu
# "chờ chọn nhà cung cấp" và thay khi đã chọn (docs/runbooks/k8s-deploy.md mục 9).
terraform {
  required_version = ">= 1.8.0"

  # Provider được thêm khi chọn nhà cung cấp — chờ chọn nhà cung cấp. Ví dụ dạng khai báo:
  # required_providers {
  #   vncloud = { source = "<registry>/<nhà-cung-cấp>/<provider>", version = "~> x.y" }
  # }

  # Trạng thái lưu ở kho S3 TRONG LÃNH THỔ, có khóa đối tượng + mã hóa (RB-7) — chờ chọn nhà cung cấp.
  # backend "s3" {
  #   bucket                      = "agrigreen-tofu-state"
  #   key                         = "g4/<env>/terraform.tfstate"
  #   region                      = "<vùng trong lãnh thổ>"
  #   endpoints                   = { s3 = "https://<s3 trong lãnh thổ>" }
  #   use_path_style              = true
  #   skip_credentials_validation = true
  #   skip_region_validation      = true
  #   skip_requesting_account_id  = true
  #   use_lockfile                = true
  # }
}
