## KNSPLIT-2-FORMS-B — Lõi biểu mẫu chỉ dùng đăng ký ứng dụng

Gỡ nhánh tự đọc Principal.KN, actor.kn và phép kiểm KN riêng trong cap/forms. Profile có resolver do ứng dụng sở hữu; khi không có ngữ cảnh ứng dụng áp dụng thì chỉ xét vai realm đã cấu hình. Cấu hình/quy tắc ba profile KN và giới hạn mục tiêu vẫn nằm extension.FormProfiles từ bước A, không đổi API/SQL/tenant hay các quyết định PO.

Fixture app/HTTP của forms dùng resolver giả độc lập và dữ liệu ResolvedActor trong context; không mượn kiểu KNContext hoặc import module KN. Giữ các ca lịch sử CNTS/KHTC để kiểm khả năng generic theo cơ sở/phòng ban, không dời luật KN vào tên generic.

Ca âm ở assembly chứng minh profile chưa đăng ký resolver không tự suy quyền từ ngữ cảnh KN: đường cũ đi đến DB dù không có realm roles; sau gỡ phải Forbidden trước DB. Các ca resolver nhận diện phiên nhưng vai rỗng/lạ không được kế thừa quyền realm cao hơn. Đang hồi quy trước tích hợp; xem docs/REGRESSION.md.
