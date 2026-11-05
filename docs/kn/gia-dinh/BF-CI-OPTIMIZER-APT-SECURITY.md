# BF-CI-OPTIMIZER-APT-SECURITY — vá APT trong image optimizer

Issue: [#235](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/235), mở chặn CI #224. **NGOẠI LỆ LÀN:** chỉ Dockerfile optimizer và hai tài liệu gói; Codex viết, Claude review chéo.

## Hành vi và căn cứ

Image optimizer cũ dùng Python3.12 slim/Debian trixie, chứa apt và libapt-pkg7.0 phiên bản3.0.3. Trivy của CI41967131864 báo hai HIGH cùng CVE-2026-109518. [Debian Security Tracker](https://security-tracker.debian.org/tracker/CVE-2026-109518) xác nhận bản vá trixie3.0.4.

Dockerfile dùng base Python3.12 slim-trixie chung cho stage build và runtime. Base nâng riêng hai gói APT, kiểm từng phiên bản bằng dpkg ít nhất3.0.4 rồi xóa package index. Nếu kho chưa có bản vá hoặc nâng cấp/kiểm phiên bản thất bại, build phải dừng. Acquire::Retries=0 chỉ áp dụng cho hai lệnh APT bootstrap vì [thông báo Debian](https://lists.debian.org/deity/2026/11/msg00005.html) xác định retry là điều kiện của CVE; không thêm cấu hình APT lâu dài.

Giữ nguyên optcore named context, wheels và MILP, dependency Python, user10001, entrypoint/consumer. Không đổi hợp đồng optimizer/RB-7, quyền hay dữ liệu nghiệp vụ. Scanner, severity, ignore/baseline và workflow giữ nguyên.

## Kiểm chứng và giới hạn

- Chạy bộ Python optcore/optimizer hiện hữu, các cổng repo và PG tạm theo quy trình.
- Build thực tế phải in phiên bản cả hai gói APT. SBOM/Trivy trên runtime image hoàn chỉnh phải đạt; scan cũ là bằng chứng hồi quy, không gọi là một controlled mutation mới.
- CI toàn nhánh đúng SHA phải xanh, gồm Linux/race, optimizer và đủ matrix image. Các job bị cancelled trước đó chưa có kết luận an toàn.
- Nếu máy không có Docker engine hoạt động, ghi rõ giới hạn local; lấy build/SBOM/Trivy thực tế từ CI Linux. Không thay đổi Docker Compose dev để chạy gói này.
- Kết quả và SHA kiểm chứng ghi ở issue #235 trước READY; Claude review/gộp qua ticket. Sau khi bản vá thực sự vào dev, #224 mới đồng bộ dev và kiểm CI trên SHA mới.

Không có migration, API/web hoặc câu hỏi PO nghiệp vụ mới. Không tuyên bố #224 đã READY hay đã triển khai production.
