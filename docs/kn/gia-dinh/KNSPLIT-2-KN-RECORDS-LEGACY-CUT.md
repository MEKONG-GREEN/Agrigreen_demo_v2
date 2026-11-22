## KNSPLIT-2-KN-RECORDS-LEGACY-CUT — Nghỉ route ghi HTX legacy của KN (#160)

Nguồn: ADR-0055/Q-KNS-04 và issue #160, #67, #161, #162, #53. #67/c67fdabf, mobile #161 (merge4f930d11) và #162/f34b5118 đã CLOSED/vào dev trước nhận gói. Nhánh từ dev06851773; không xếp chồng với gói chưa gộp.

### Mapping trước và sau

| Trước (Application API) | Sau #160 | Consumer thay thế |
|---|---|---|
| POST /v1/kn/cooperatives | không đăng ký, HTTP405 method_not_allowed (còn GET) | POST /v1/kn/producer-records, kind=cooperative/coop_group |
| PUT /v1/kn/cooperatives/{ref} | không đăng ký, HTTP405 method_not_allowed (còn GET) | POST /v1/kn/producer-records/{ref}, base_version |
| POST /v1/kn/cooperatives/{ref}/deactivate hoặc /reactivate | không đăng ký, HTTP404 | POST /v1/kn/producer-records/{ref}/deactivate hoặc /reactivate, base_version/lý do |
| POST /v1/kn/cooperatives/{ref}/households | không đăng ký, HTTP405 method_not_allowed (còn GET) | POST /v1/kn/producer-records, kind=household, parent_ref là hồ sơ KN |
| PUT /v1/kn/households/{ref} | không đăng ký, HTTP404 | POST /v1/kn/producer-records/{ref}, kind=household, base_version |

ID của hồ sơ KN và ID HTX là hai miền riêng; không chuyển hướng HTTP hoặc tự biến ref HTX thành producer-record. Web createCollectApi đã chuyển theo #67/#162; mobile kn_collection_service và action_queue đã chuyển theo #161, lệnh legacy đang chờ được retire. Không phục hồi phát lại lệnh cũ hoặc ghi HTX thay KN. Mocks bỏ các route ghi legacy còn tồn tại, giữ nhánh kn-collect-records; nguồn OpenAPI module party bỏ đúng sáu operation và sinh lại aggregate/types bằng gen:api.

### Phần đọc và native được bảo toàn

GET /v1/kn/cooperatives, /{ref}, /{ref}/log, /{ref}/households và GET /v1/kn/households/{ref}/log giữ registration/DTO/phạm vi/tenant/PII. Mobile cooperatives() vẫn là GET bộ chọn thửa; không gỡ chỉ vì #162 đã đóng phần web. Native HTX/member routes và producer-records của KN không đổi. Không sửa luồng Neo D2, coownership/activation, MRV HTX/MKG, nghiệp vụ máy/thửa/vật tư hoặc migration đã áp dụng. Đây là cắt bề mặt HTTP ghi legacy, không tuyên bố hoàn tất #53, toàn G1, tất cả điểm dính trong app/service hay toàn tenant cutover.

NGOẠI LỆ LÀN: issue #160 giao cooperative/member/handler và web mock kn-collect.ts cho Codex. tests/kn77 cập nhật regression: Admin nền tảng tạo HTX native qua HTTP Neo D2 (created_by=mkg), KN chỉ đọc, rồi kiểm GET/máy/thửa như cũ và sáu write404. Các test native/D2 vẫn chạy. Không sửa runtime làn KN của Claude. Baseline knboundary chỉ prune cạnh đã biến mất:314→305 (không tăng).

### Bằng chứng kỹ thuật

Hai route map theo module chủ kiểm sáu thao tác vắng và registration đọc/native còn nguyên; HTTP thật kiểm đúng405 method_not_allowed ở path còn GET, đúng404 not_found ở path đã vắng với ngữ cảnh hiện hành, giữ đọc sau lệnh nghỉ và đối soát sổ không đổi. Phép làm sai tạm đăng ký lại POST /kn/cooperatives làm regression FAIL; khôi phục bytes và control PASS. Mock dispatch kiểm sáu write404 cho Xã/Admin, dữ liệu HTX không đổi, GET và producer-records vẫn dùng được. Full static/lint, PG thật 0SKIP, web, consumer mobile và CI exactSHA được ghi trên issue khi đầy đủ, không lấy browser mock làm PG hay UAT. Không thêm ô số, luật vai hoặc schema. Claude review chéo trước merge, Codex không tự gộp.
Protocol: httpx.NewEngine bật HandleMethodNotAllowed (httpx.go:31–36); vì giữ GET nên ba write method cùng path trả405 theo NoMethod chung, không phải handler ghi. AC “không đăng ký” được kiểm độc lập bằng route map vắng tuyệt đối, các mã/lý do trả về được assert chính xác và số sổ HTX không đổi. Backend giả lập không có NoMethod nên cả sáu phương thức trả404 not_found. Không thêm tombstone route hay sửa NoMethod để tạo404 giả. Lượt PG đầu làm rõ khác biệt404/405 này; sửa expectation theo contract thực, không nới lỗi403/422 hoặc bỏ kiểm ghi.

Kịch bản kn77 dùng HTTP Neo D2 của AdminMKG làm dữ liệu native trước khi KN chỉ đọc. Sổ pf_coop.kn_profile_event của tenant kịch bản phải không có stream KN sinh; không tạo ghi HTX giả để lấp kỳ vọng dương cũ. Các sổ KN đang hoạt động vẫn verify_chain và kiểm UPDATE/DELETE bị chặn; native cooperative regression kiểm độc lập. Snapshot org_event phải có dữ liệu thật và cả org_event/household/profile giữ nguyên sau sáu phương thức nghỉ.
