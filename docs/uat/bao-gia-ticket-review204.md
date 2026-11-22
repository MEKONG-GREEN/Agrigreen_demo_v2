# UAT riêng lát phiếu Cấp Tỉnh #204

Nguồn BRD Báo giá v1.3: FN-TI-02.02 AC-01/02/03, FN-TI-02.04 AC-01/03/04/05. Không sửa ma trận tổng thể hoặc tự công nhận toàn FN/BRD. AC-02 editor-presence/lease còn gap#210 vì API chỉ có optimistic version, không có người đang giữ form.

| Ca | Kết quả cần chứng minh | Bằng chứng |
|---|---|---|
| Danh sách→chi tiết theo ID, giữ day/mine/tab; Hủy/back | route riêng, không Dialog nghiệp vụ, không POST | pure/flow/E2E204 |
| 404/UUID/path sai, đổi vai/ngữ cảnh/readonly/MFA/API/session | không rơi sang phiếu khác, không lộ state cũ/ghi/lateACK điều hướng | flow204 + native role/tenant + assembly |
| Hiệu chỉnh cặp giá, lý do, positive/order/cap/precision | lỗi dưới từng trường; gửi chuỗi chuẩn hóa; giữ giá Xã gốc | form204/flow204/native lifecycle |
| Numeric min/max hostile |1e100000,1e-100000,100000digits bị từ chối trước constructor/normalize, không ghi sổ | pureguard/hostile native/E2E; RED1e3 trước fix |
| Null/empty/leading-trailingzeros/negative/precision/keep | không đổi nghiệp vụ input chung; generic guard không áp cap giá cho keep | compatibility pure + fullPrice native |
| Log nhiều lần sửa | actor/at/version/lý do thật; old→new là effective previous line, giá Xã vẫn nguyên | view204 unit + native Log + E2E |
| Mất ACK/thử lại, đổi body cùng key, stale version | retry cùngbody/key trả đúng replay, không tăng ticket/line/alert; conflict không ghi | flow204 + native lifecycle |
| Sửa ngày/xóa mềm | lý do bắt buộc, version/can native; chặn ngày sai/trùng/tương lai; sau xóa còn Log | native lifecycle/fullPrice + E2E204 |
| Cổng |9Go/lintnew0/fullPrice+assembly+worker0SKIP/8migration/fullweb/checkAPI/build/E2E/screenshotview/wholeCI exactSHA | bình luận READY cuối cùng |

Test UI/mock chỉ kiểm client/luồng render. Chúng không thay nativeHTTP/PG/real ledger hoặc CI. Các ca chưa có log cuối không được xem là PASS. Không có schema/migration/OpenAPI mới trong lát này.
