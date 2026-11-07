# Nghiệm thu kỹ thuật image optimizer — #287

Phạm vi HẠ_TẦNG của BRD 2.0; không chứng nhận FN/UAT nghiệp vụ hoặc triển khai KN độc lập.

1. Build và runtime kế thừa cùng stage base Python 3.12 slim-trixie. APT vẫn được nâng và kiểm tối thiểu 3.0.4; zlib1g thực tế đạt tối thiểu bản Trixie-security của DSA-6613-1.
2. Canary gốc và bản làm sai dùng base digest bất biến, build thành công, có phiên bản/hash thư viện và Trivy JSON chứa lỗi zlib; exit 1 do scanner. Lỗi build, mạng, xác thực không được tính là lỗi bảo mật mong đợi.
3. Bản vá và bản khôi phục nguyên byte đều được build/scan thật, không còn HIGH/CRITICAL theo cổng hiện hành. Không đổi scanner, ngưỡng hoặc ignore/baseline.
4. Có bằng chứng image optimizer cuối cùng trong CI chính, cùng toàn bộ các job còn lại trên đúng SHA, rồi tự review và gộp qua helper. Không dùng scan base thay nghiệm thu image cuối.
5. Người dùng runtime, wheel optimizer/optcore, hợp đồng consumer và dữ liệu không định danh giữ nguyên. Không push/deploy image canary, không dùng Docker/Compose/PostgreSQL dùng chung trên máy phát triển.

Thao tác kiểm và giới hạn: [giả định gói](../kn/gia-dinh/BF-CI-OPTIMIZER-ZLIB.md).
