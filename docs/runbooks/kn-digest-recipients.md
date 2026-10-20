# Email tóm tắt KN: hộp thư công vụ theo tenant

KNSPLIT-4B-NOTIFY thay người nhận toàn process bằng mapping tin cậy theo tenant cho queue digest KN. Trước khi triển khai, người vận hành phải khai cả **tenant KN hiện tại** (có thể vẫn là tenant lưu ký trước chuyển) và từng tenant Trung tâm tỉnh được bật. Thiếu mapping giữ hàng chờ, job báo lỗi và không dùng AG_SMTP_DIGEST_TO. Không có email nào tự chuyển người nhận theo nội dung sự kiện, phòng ban từ client hoặc tenant trong body.

## Cấu hình được duyệt

Worker đọc một lần khi dựng module từ `AG_KN_DIGEST_RECIPIENTS_JSON`, dạng array dưới đây (UUID/địa chỉ chỉ là ví dụ giả):

```json
[
  {"tenant_id":"01990000-0000-7000-8000-000000000001","to":["vanthu-center-a@example.vn"]},
  {"tenant_id":"01990000-0000-7000-8000-000000000002","to":["vanthu-center-b@example.vn"]}
]
```

Chỉ hộp thư công vụ đúng Trung tâm được duyệt, tránh email cá nhân. Không sao chép ví dụ vào production. Mỗi tenant một entry; ít nhất một, tối đa50 địa chỉ. Không dùng display name, nhiều mailbox trong mộtchuỗi, CRLF hoặc khoảngtrắng ngoàiđịa chỉ. Địa chỉ so khôngphânbiệthoathường khôngđược lặplại trongcùngtenant hoặc giữaTrungtâm; nếu mộtmailboxchung cần nhận nhiềuTrungtâm phải chốt mộtthiếtkếquyền mới, khônglách bằng đổichoahoa. Tốiđa1024entry/1MiB. JSONkhôngcófieldthừa/trùng vàkhôngtrailing. Rỗng hoặc[]tạoresolverchặn, khôngphảichếđộtoàncục.

SMTP AG_SMTP_HOST/PORT/USERNAME/PASSWORD/FROM tiếp tục theo cấu hìnhhiện có; không in mật khẩu, rawJSONhoặc danh sáchngườinhận vào log. Parsererrors chỉ mãlỗichung; kiểm cấu hình ởkhovậnhành cóquyền. AG_SMTP_DIGEST_TO chỉcòn ýnghĩavớibản genericnotifykhôngnốiresolver; worker hiệnnốiresolverKN **luônluôn**, nên biếnnày khôngcứu queueKNkhi thiếumapping. Cácemailcánhân/price/accountkhác dùngkênhhiệnhành khôngđổi.

## Áp dụng và kiểm chứng

1. Đối chiếu tenant UUID với hồ sơ Trung tâm/bản chụp mapping đã duyệt; không suy tenant chỉ từ tên tỉnh hoặc địa chỉemail.
2. Sao lưu cấu hình vận hành trước thay đổi. Đặt JSON đã duyệt trong cấu hình worker, giữ credentials SMTP ởnguồnbímật hiệncó. Không thayrealmuser hoặcDBtenant tronggói này.
3. Khởi động lại worker theo quy trình triển khai. JSON sai làm buildNotify lỗi trước mởpool; JSONrỗng chặn delivery cho tới cómapping. Lazy consumer/job đều dùng cùng builder, không reload từevent/request.
4. Trên staging, với hai tenant thử và nguồn KN riêng, kiểm chỉ hộp thưđúngtenant nhận đúngthamchiếu; queue thiếu mapping vẫn pending. Không đưa token/nội dung/địa chỉ thật vào biênbản côngkhai. Test tự động dùng fakeMailer vàPG, khônggửi SMTP thật.
5. Log job hiện giữ sent/outcome, không có mailbox. Khi mapping thiếu: sửa đúng cấu hình, restart worker rồi job xử lý pending. Không mark sent thủcông hoặc xóa queue để làm báo lỗi biếnmất.
6. Mỗi lượt xửlý tốiđa200mục/tenant; mục dư xửlý lượt kếtiếp. SMTPlỗi hủyTX, giữpending. SMTP cóthểđãnhận nhưngDBcommitthấtbại: retrycóthểgửilại nhưcơchếcũ; gói này chưa thêm outboxSMTP/receiptđểbảođảmexactlyonce.

## Lùi cấu hình

Giữ nguyên cơ chế resolver; đưa mapping về bản trước đãduyệt hoặc dừng riêng job digest bằng quy trình vận hành nếu chưa rõ ngườinhận. Không chuyểnvềglobalfallback trong môi trường nhiềutenant. Pendingqueue giữnguyên; sửa mapping/restart sẽtiếptục. Không sửa nguồnHTX/MKG/MRV, không sửaMFA hay các quyếtđịnhPO.
