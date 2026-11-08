# BF-CN-FORM-DESIGN-EDIT-DIARY (#275)

Phần đầu FN-07: chọn explicit immutable ID/version cho kỳ CNTS native, server/runtime giữ schema cũ, durable create/save và publication recovery, guard current authority trước cache, UI chọn version. Dùng public actual-dev #191/#192; không thêm dependency mới.

Kiểm: `TestCNTS275*`, parent FN-07 #192 và toàn bộ package/consumer bị ảnh hưởng qua PostgreSQL tạm 0 SKIP; whole web, production built desktop/mobile browser, source/input/tool closure và fault assertion với byte restoration. Chi tiết và cách chạy: [giả định](../../kn/gia-dinh/BF-CN-FORM-DESIGN-EDIT-DIARY.md).

Còn lại: generic versioned runtime/settings + public usage recorder/witness chống first-use race; exact identity assignment FN-14; consumer nhật ký động, GPS/ảnh/tần suất, owner, lịch và mã báo cáo mới; native atomic copy provenance. ROOT đang xử lý công bố supplier chính thức; chưa có số issue. Không nhận full FN-07/G1/UAT từ phần đầu này. Linux CI/race, public review và integration còn chờ ROOT.
