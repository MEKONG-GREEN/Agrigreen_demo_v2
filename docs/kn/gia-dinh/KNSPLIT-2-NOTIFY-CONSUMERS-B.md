## KNSPLIT-2-NOTIFY-CONSUMERS-B — Bỏ đăng ký KN mặc định

Nền703bc354. RED45033exit1 xác nhận cả nil/empty/empty-definition trước sửa vẫn tự nạp16. B bỏ6metadata legacy khỏi notify, giữ10native với vị trí2/4/5/6/8/9/11/14/15/16 để owner điền sáu vị trí còn lại; private registry nhận orderedConsumer explicit, mọi nativeName/Order/Event được bảo vệ. Không suy KN khi thiếu provider. Nativepush/email giữ handler riêng. KN cung cấp6groups/22events và fullcatalog vẫn16groups/41bindings như golden A.

Chuyển nguyên6consumerIDliteral vềKNports với prefixNotifyConsumer*, xóa hằng core, không alias ngược. Root notify không còn importKNport/PriceEvents/CNTSEvents cho metadata. Builder/template/HandledEvents/kinds/rolepolicy vẫn còn ở core tới phase sau, chưa hoàn tất notify. NativeMRV vẫnHTX/MKG, không đổi quyền hay nội dung thông báo.

Move13black-box HTTPtestfiles/fakes từcap handler sang tests/notify (29Test +TestMain), fixture lắp6provider KN thật. Token proof giữ toàn bộ test/helper body và literal, chỉ normalize qualifier6ID và fieldfixtureConsumers mới; không bỏ assertion. GiữsourcePG/inbox/pushfake/emailfake/RLS/native/KN test. tests/notifyconsumers nay chỉ đọc frozenOLD16/41, không chạylegacy/generator. Coreunit vẫn invalid/deepcopy/isolation/handler/native reservation, nil/empty native10 nayGREEN.

Pure95918 actualexit0 bốnpackage cótest; priceRootcompile-only. Baseline350→343 chỉ prune6oldimports (5test/1root) +ConsumerCNTS symbol, không boundaryconfig/aliases. Golden16/41 Gitbytes nguyên; payload/HTTP/SQL/schema/UI/mobile không đổi.

Impact: rg ConsumerIDs/notify.New/registry+testmove tokenproof, gitdiff. Hồi quy8testpackages nhưA nhưng caphandlerkhôngcòntest, thêmtests/notify: fullnotify/...+KNroots+APIassembly/worker+tests/notifyconsumers+tests/notify. Fullstatic/helper9migration/noSKIP trướcdev. KN77/KNhandler/pricehandlerbusiness không đổi nên không lặp masterdata22; sourcehandlers/delivery giữ, actualPGfixtureprovider mới chứng minh path assembled. Cần exit thật static/PG/helper/remote trước hoàn tất; giữ PO/main/worktrees. G1 còn nhiều registry/data/routes/tenant/client/KHTCreminder.
