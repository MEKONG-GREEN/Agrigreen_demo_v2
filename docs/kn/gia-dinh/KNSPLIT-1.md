## KNSPLIT-1 — Chuyển namespace KN, giữ định danh dữ liệu

Chuyển extension, price, hrm, contact sang internal/kn; giữ tên module pf/..., schema pf_* và role r_* hiện hành. Không đổi SQL migration, luồng D2, tenant hoặc phân quyền trong gói cơ học này. Các quyết định PO đã chốt tiếp tục được giữ; đảo phụ thuộc và tenant thuộc các gói tiếp theo.

modreg và modgen nhận diện namespace mới; bộ kiểm tra kiến trúc vẫn cưỡng chế ranh giới module, shared, MKG, vault, port, tiền chính xác và sổ không xóa cứng. knboundary giữ nguyên cấu hình, baseline và canonical_paths. Kiểm thử mới bắt được thiếu phạm vi các gói KHTC cạnh khtc; đã bổ sung khtcform/khtcperiod/khtcreport/khtcsource cho cả đường dẫn cũ và mới, không miễn trừ mã.

Registry menu/mock/OpenAPI được sinh lại. Năm SHA256 (golden menu, hai OpenAPI, hai TypeScript API) giữ nguyên so với dev b13ae196; toàn bộ migrations không đổi. Do không đổi mã giao diện/API/Flutter, không chạy lại bộ web/mobile trên nội dung giữ nguyên; vẫn kiểm bộ sinh registry và menu qua PostgreSQL thật. Bằng chứng và trạng thái tích hợp ghi tại docs/REGRESSION.md. Đã có PASS cho 56/56 gói hồi quy, kiểm tra tĩnh và bộ sinh; chỉ tích hợp qua helper sau cổng migration PostgreSQL thật. Ba fixture JSON chỉ đổi ghi chú đường dẫn, payload không đổi.
