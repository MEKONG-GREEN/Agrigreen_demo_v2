# Dấu quyền phiên chủ cho idempotency

`authz.Session.AuthorizationStamp()` trả digest SHA-256 có phiên bản, bao gồm profile, ID phiên, toàn bộ SessionScope và payload quyền do chủ ứng dụng cung cấp. Payload là byte opaque: helper không giải mã, không sửa, không log và không tự cấp quyền. Phiên zero/không hợp lệ bị từ chối.

Middleware accounts giữ ngữ cảnh legacy đã giải (hiệu lực, thuộc tính, ủy quyền, chế độ hỗ trợ/lý do) và ghép digest phiên chủ vào dấu phạm vi trước middleware idempotency. Quyền mới từ owner payload vì vậy ảnh hưởng cache ngay cả khi ngữ cảnh legacy không đổi. Actor, tenant, vai realm, MFA, route, body và khóa idempotency tiếp tục được kiểm như trước; request native không chọn phiên không chạy owner projector.

Cùng khóa, cùng request và cùng quyền hiện hành được phát lại. Khi payload/phạm vi chủ thay đổi, khóa cũ trả `idempotency_key_reused` thay vì phát lại phản hồi theo quyền cũ. Provider lỗi/không giải được phiên vẫn bị từ chối trước cache. Dấu mới có thể làm khóa đã lưu trước rollout bị xung đột; đây là từ chối an toàn, không migration hoặc ghi lại bản ghi cũ.

Chủ ứng dụng phải trả payload ổn định khi quyền không đổi và đưa mọi quyền ảnh hưởng thao tác vào projection. Digest không phải bằng chứng xác thực hay cổng thay cho việc kiểm quyền từng use case. #69 chỉ bổ sung dấu cache, không triển khai owner CNTS grants hoặc chuyển việc/nháp.

Kiểm thử: ca thuần thay profile/ID/node/phòng ban/xã/người được hỗ trợ/node guard/payload target/read-only; ca HTTP trên PG thật giữ legacy context và request giống nhau, đổi owner payload thì không replay, owner invalid thì deny, không sửa danh tính và không ảnh hưởng request native.
