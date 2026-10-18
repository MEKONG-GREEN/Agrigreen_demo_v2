## KNSPLIT-2-NOTIFY-KINDS-A — Đăng ký loại thông báo

Nền43b14525. TestLegacyNotificationKindContract đọc pg_get_constraintdef và thực thi chính biểu thức CHECK trên từng giá trị: capture5154actual0/noskip,43keys được chấp nhận và7 giá trị ngoài danh sách bị từ chối. Chỉ đọc DB riêng65308/35ed86da5496, đã dọn trước khi viết registry. Golden kn-notify-kinds.json lấy từ OLD DB, không sinh từ code mới.

Registry immutable theo instance, startup một lần, pattern a-z/underscore1..40 tương thích email_delivery, chặn empty/duplicate/native collision. Core sở hữu18 persisted kinds, không gồm sync_now chỉ gửi dữ liệu; KN extension16 vàprice9 tự đăng ký. Union43 đúng OLD. Bốn điểm API/worker, fixture và customPG phát KNkind nạp owner; no fallback về các hằng KN trong core. Deliver từ chối kind chưa đăng ký trước SQL; giữ Normalize/Redact/IDs/RLS/inbox/provenance.

Unit kiểm clone/isolation/init/native/format và từ chối trướcDB; external so25owner keys vớiOLD43. PG thực phát đủ43keys, unknown không ghi và retry/dedup, thiếu khai báo ownedkind không chạmDB. A giữ nguyên finite CHECK/SQL/schema/migrations; B chỉ được cutover sau actualupgrade/count/checksum/row preservation và không sửa migration cũ. Đây là thêm validation, không nới cổng CI hay alias.

Impact fullnotify/... +KN extension/price +APIassembly/worker +testsnotifyconsumers/testsnotify,9package có test. Cần pure5/fullstatic/boundary318 không tăng/full9PG thậtnoFAIL-SKIP/helper9/remote trướcdev.15oldgoldens nguyên43b14525, thêmfrozenkindcontract. KhôngAPI/UI/mobile thay đổi. G1 còn nativeKNpolicies/data/rights/routes/tenant/client/KHTC reminder; quyết địnhPO/main/worktrees/lịch giữ.


Pure48851actual0 đủ5testpackage(+KNpricecompile), Kindregistry clone/isolation/pattern/duplicate/native/initonce/unknown-beforeDB và25ownerdefinitions khớpOLD43 PASS;52/11/job và các golden nguồn cũ giữ.15oldgoldenGitblobs nguyên43b14525, baseline/rules nguyên, không migrationchange. TrướcPGcompile sửa riêng test mới EventHandler chỉ trả một error (không hai giá trị); không đổi production/pure assertions. Production7870e3d8;chưa static/full9PG/helper/dev.


Static57288 actualexit0 đủ toàn cổng, boundary318/lint0/gofmt chỉ2 fixture cũ. Full9PG42658 actualexit0/không FAIL-SKIP: assembly148.655s, testsnotify160.004s, phát đủ43keys qua runtime, unknown bị chặn và retry/dedup, thiếu ownedkind không ghi. Private56976/8f91017f953f đã dọn;Go0; không sửa Go sauPG. Pure48851actual0/capture5154actual0. Còn helper9migration +remote trước tích hợp.
