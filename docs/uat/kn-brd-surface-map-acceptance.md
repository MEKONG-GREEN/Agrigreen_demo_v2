# KN-STANDALONE-10A-BRD-SURFACE-MAP — nghiệm thu kỹ thuật

| Phần | Mã tham chiếu | Bằng chứng |
|---|---|---|
| Catalogue phạm vi | TT 25 FN, CNTS 21 FN, HCTH 20 FN, KHTC 18 FN, Báo giá 29 FN | `docs/kn/brd-surface.yaml`, mã có namespace và phiên bản từng module |
| Menu | TT FN-AD-04.07; đăng nhập/chọn phòng của cả năm module; FN nghiệp vụ ghi tại từng dòng | Go dùng `extension.MenuCatalog` + `Registry.Build`, đủ ba cấp × năm phòng, Admin; bao gồm nhóm và mục tắt/điều kiện |
| Web/mobile | FN hoặc gói gỡ tại từng dòng | Go/TS đọc cây trang/route handler KN, trang ngoài KN và tên lớp màn mobile; TS đọc fixture menu đã sinh |
| API | FN/needs hoặc gói gỡ/hoãn tại từng gốc tài nguyên | Go gọi `RegisterApp`/`RegisterAdmin` thật của bốn module KN; dependency ngoài registry được liệt kê để đối chiếu thủ công |
| Chế độ báo cáo | Gói #262, bật chặn ở #272 | Mục thiếu, không quan sát thấy và chồng nhau được ghi log; test tổng hợp vẫn PASS |
| Schema/đối chiếu | Hợp đồng tệp YAML | Sai cú pháp, trường lạ, trùng khóa, FN cũ/không tồn tại và thiếu gói/FN bắt buộc phải FAIL; tiền tố `/plots` không nhận `/plots-extra` |

Gói này kiểm chứng cơ chế kiểm kê/phân loại và chế độ báo cáo. Việc đạt các AC nghiệp vụ của từng FN được nghiệm thu trong gói chức năng tương ứng. Xem [quy ước và lệnh chạy](../kn/gia-dinh/KN-STANDALONE-10A-BRD-SURFACE-MAP.md).
