## KNSPLIT-2-NOTIFY-PRICE-B — Xóa đường báo giá legacy

Nền ca6ec58e. RED82657 actualexit1 đúng 18 fallback (nil/empty builder × chín sự kiện). Xóa chín case/metadata HandledEvents, pipeline app/domain cùng unit/capture generator, PriceDesk và Deps/UsePriceDesk cũ. Chín KindPrice chuyển nguyên khai báo về KNpriceport, không alias. Selector assembly cũ đã đối chiếu A được xóa; worker dùng NotificationDirectory KN. Các hợp đồng email dùng chung giữ nguyên nội dung, tệp đổi tên sau khi đã gỡ nghiệp vụ báo giá. EmailMessage vốn chỉ dùng Title/Body/DeepLink chung, không chọn theo KindPrice.

Giữ nguyên tám golden Gitblob từ A: consumer16/41, CNTS46, workflow77, price118 inputs/expected và directory16/32. Directory test chỉ đọc expected cũ đã capture, giữ kiểm bản sao ba trường profile, kết quả/lỗi/tenant/role/duty và thứ tự gọi; không tạo expected từ owner, không giữ selector cũ hay import assembly để tạo oracle. Unit legacy được thay bằng các ca full draft/lỗi/query nói trên và toàn PG giá/email/topic/delivery.

Impact: notify dispatch/Deps/module metadata, worker, assembly, KNprice và fixture/test. Giữ core transaction/Normalize/Redact/RLS/inbox/provenance, native MRV thuộc HTX/MKG. Không SQL/schema/migration/API/UI/mobile thay đổi. Cần pure5, fullstatic, actualpruneonly (nền324), full9PG và helper9migration/remote trước dev; chưa coi các bước chưa chạy là đạt. G1 còn directive/task/digest và các gói đảo phụ thuộc, dữ liệu, route, tenant, client, KHTC reminder. Main/worktrees/lịch tự tiếp tục giữ.


Pure68503 actualexit0 đủ năm package có test (+KNprice compile). RED82657 actual1 → GREEN nil/empty×9 không fallback; owner có đăng ký vẫn kiểm payload lỗi. 118 output/lỗi/query,16 directory/32 thao tác,77 workflow và46 CNTS đều khớp expected cũ. Tám golden Gitblob nguyên ca6ec58e; chín Kind khai báo nguyên văn, các hợp đồng email giữ nguyên văn. Baseline324→320 chỉ bốn mục thật đã gỡ; rule/alias không đổi. Production824ea9f7, chưa static/PG/helper/dev.


Kết quả thật production824ea9f7: pure68503actual0/5package, static38890actual0/fullgates/boundary320/lint0/gofmt2old. Full9PG38198actualexit0, không FAIL/SKIP; giá/email/topic, lookup retry/tenant/redaction, CNTS/workflow và generic delivery guards đều PASS. Cụm riêng54797/61007113c1ac đã dọn,Go0; không Gochange sauPG. Tám golden nguyên ca6ec58e, chín Kind và hợp đồng email đúng nguyên văn; baseline324→320 chỉ bốn mục xóa thực. Cần helper9migration/remote trước dev.
