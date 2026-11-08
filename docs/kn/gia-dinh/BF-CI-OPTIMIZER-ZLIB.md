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

### Bổ sung cổng contact — cùng gói #287

- [CI trên 8bcd99c7](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/actions/runs/42388061116) thất bại ở `TestContact_RLSNoPII`, trước matrix image. Tái hiện có kiểm soát dùng actor UUIDv7 hợp lệ chứa `1970`: oracle substring cũ báo PII dù tiền điều kiện fixture xác nhận dữ liệu cá nhân ở fake vault và sổ chỉ giữ metadata dự kiến. Điều này chứng minh một chế độ báo nhầm; CI gốc không dump dòng nên chưa xác định dòng gây lỗi thực tế.
- Chỉ sửa test/helper trong `internal/kn/contact/handler`. JSON từ row_to_json/slog được đọc bằng decoder chuẩn; UUID/hash/timestamp/sequence tại đúng root phải có kiểu/cấu trúc hợp lệ, stream key phải bằng prefix cộng reference đã kiểm. Idempotency fallback chỉ được nhận dạng khi đúng prefix và UUIDv7 như `app/write.go`; text còn lại vẫn quét. Nội dung free text, key/path chưa biết, nested/array/number vẫn kiểm PII, gồm sentinel `1970`. Scan/rows.Err lỗi thì kiểm thất bại. RLS, tenant và sổ bất biến được giữ nguyên; runtime/auth/migration/vault không đổi.
- Bước 1 bổ sung test/helper và fixture metadata; bước 2–5 tiếp tục không đổi mã nghiệp vụ. Case kiểm có PII thật theo fixture ở cả DB/log (tên đầy đủ/đoạn tên, SĐT, email, năm sinh; exact/embedded/nested/array), metadata lành tính chứa 1970 và lỗi đọc/parse. Controlled fault phải làm checker FAIL rồi khôi phục đúng byte/hash và control PASS.
- TestsCSV mở rộng: `./internal/kn/contact/...,./tools/docscheck,./tests/kn77`. Native dùng pg-ephemeral với `-count=1 -p 1 -timeout 55m -v`; race native theo khả năng compiler, race Linux trong CI chính bắt buộc. Không rerun CI gốc; CI mới trên SHA cuối phải xanh trước tự review/gộp.
- Canary image trước đó thuộc SHA `8bcd99c7b34bb3d0f9b7a599425a721e9db7b5a4`, không phải SHA sửa oracle. Chỉ tái dùng khi toàn bộ tree input `optimizer/`, `lib/optcore/`, Dockerfile, workflow/script canary và các version/digest tool trùng; code test contact nằm ngoài các build context này. CI chính vẫn phải build/SBOM/scan optimizer thật trên SHA mới và toàn bộ matrix đạt.
