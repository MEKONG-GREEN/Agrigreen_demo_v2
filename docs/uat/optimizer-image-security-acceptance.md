# Nghiệm thu kỹ thuật image optimizer — #287

Phạm vi HẠ_TẦNG của BRD 2.0; không chứng nhận FN/UAT nghiệp vụ hoặc triển khai KN độc lập.

1. Build và runtime kế thừa cùng stage base Python 3.12 slim-trixie. APT vẫn được nâng và kiểm tối thiểu 3.0.4; zlib1g thực tế đạt tối thiểu bản Trixie-security của DSA-6613-1.
2. Canary gốc và bản làm sai dùng base digest bất biến, build thành công, có phiên bản/hash thư viện và Trivy JSON chứa lỗi zlib; exit 1 do scanner. Lỗi build, mạng, xác thực không được tính là lỗi bảo mật mong đợi.
3. Bản vá và bản khôi phục nguyên byte đều được build/scan thật, không còn HIGH/CRITICAL theo cổng hiện hành. Không đổi scanner, ngưỡng hoặc ignore/baseline.
4. Có bằng chứng image optimizer cuối cùng trong CI chính, cùng toàn bộ các job còn lại trên đúng SHA, rồi tự review và gộp qua helper. Không dùng scan base thay nghiệm thu image cuối.
5. Người dùng runtime, wheel optimizer/optcore, hợp đồng consumer và dữ liệu không định danh giữ nguyên. Không push/deploy image canary, không dùng Docker/Compose/PostgreSQL dùng chung trên máy phát triển.

Thao tác kiểm và giới hạn: [giả định gói](../kn/gia-dinh/BF-CI-OPTIMIZER-ZLIB.md).

6. Cổng contact của cùng #287: metadata UUID/hash/derived key hợp lệ chứa `1970` không báo nhầm; PII fixture ở text, unknown/nested/array/number trong DB/log vẫn bị phát hiện (exact và embedded). Kiểm Scan/rows.Err, giữ RLS/tenant/sổ bất biến và negative control thật FAIL → khôi phục byte/hash → PASS; không sửa runtime/auth/migration/vault.
7. Chứng cứ canary 8bcd99c7 là lịch sử độc lập; phải đối chiếu tất cả input/tool trước khi tái dùng. CI chính mới trên SHA cuối phải đạt cả contact race và optimizer image/matrix; không suy diễn actual CI gốc không có leak từ tái hiện có kiểm soát.
