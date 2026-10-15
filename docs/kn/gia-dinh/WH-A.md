## WH-A — Đăng ký menu/mock/OpenAPI theo module (không migration)

Phạm vi: nguồn khai báo do module sở hữu, bộ ghép tự phát hiện, giữ hành vi và thứ tự hiện hành. Không đổi quyết định nghiệp vụ/tenant/D2. Registry menu biên dịch vào vị trí tương thích đã kiểm kê trong party, mock KN ghép qua điểm vào kn-admin có sẵn; nợ đảo phụ thuộc còn thuộc KNSPLIT-2. Không thêm coupling hay miễn trừ.

Đã giữ nguyên SHA256 golden menu, hai OpenAPI và hai tệp TypeScript API so với dev b5906660 ở lượt đầu. Các ca thực thi cuối và bằng chứng tích hợp ghi trong docs/REGRESSION.md. Không coi WH-A hoặc5XFAIL còn lại là hoàn thành G1.
