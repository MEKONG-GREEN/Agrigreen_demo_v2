## BF-CI-OPTIMIZER-ZLIB — zlib đã vá trong image optimizer

Gói #287 là HẠ_TẦNG CI/runtime, không thêm hoặc thay đổi FN, dữ liệu, API, màn hình hay quyền KN theo BRD 2.0.

### Phạm vi và căn cứ

- `optimizer/Dockerfile` vẫn dùng Python 3.12 slim-trixie và stage `base` chung cho build/runtime. Nâng riêng `zlib1g` cùng các gói APT đã được bảo vệ trước đó; dùng `dpkg --compare-versions` để build từ chối phiên bản thấp hơn `1:1.3.dfsg+really1.3.1-1+deb13u1`.
- [Debian DSA-6613-1](https://security-tracker.debian.org/tracker/DSA-6613-1) và [CVE-2026-76844](https://security-tracker.debian.org/tracker/CVE-2026-76844) ghi bản vá Trixie-security trên. Kiểm kho dùng InRelease có chữ ký Debian, checksum của Packages và checksum gói `.deb`; kiểm này chứng minh gói có sẵn, chưa thay thế build/scan image thực tế.
- Giữ kiểm APT `>= 3.0.4`, `Acquire::Retries=0` khi bootstrap, người dùng runtime `10001:10001`, hợp đồng wheel/consumer và các cổng scanner hiện hành. Không thêm ignore/baseline, không đổi ngưỡng HIGH/CRITICAL, không coi việc có gói là bằng chứng đường gọi bị khai thác.
- Canary dùng image gốc theo digest bất biến, build/scan trên GitHub Actions. Bản gốc và bản làm sai chỉ bỏ nâng/kiểm zlib phải được build thành công rồi bị scanner từ chối; lỗi mạng/build không được tính RED. Bản vá và bản khôi phục phải có phiên bản thực, hash thư viện và scan đạt. Image kiểm không được push/deploy.

### Thứ tự dựng

Bước 1 sửa dependency hệ thống và kiểm image. Bước 2–5 không có thay đổi. Tệp kiểm canary và tiêu chí kỹ thuật nằm ở script/workflow tương ứng và [nghiệm thu image](../../uat/optimizer-image-security-acceptance.md).

### Cách chạy và giới hạn

- Trên nhánh gói, chạy workflow `optimizer-zlib-canary.yml`; xem artifact kiểm phiên bản, thư viện, scanner JSON và exit code của bốn trường hợp. Workflow/script từ chối chạy ngoài GitHub Actions Linux.
- Chạy các cổng repo, Python optimizer/optcore unit/golden, và `.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./tools/docscheck ./tests/kn77` với toolchain trong `go.mod`.
- Sau canary, CI chính phải xanh trên đúng SHA, bao gồm image optimizer cuối cùng và toàn matrix. Không dùng kết quả chỉ quét base để tuyên bố image chứa wheel hoặc toàn CI đã đạt.
- Cách khởi động optimizer hiện hữu giữ theo `optimizer/README.md`; gói không thêm cách triển khai hoặc kết nối mới cho KN. Môi trường kiểm của gói không chạy Docker/Compose hay PostgreSQL dùng chung trên máy phát triển.
