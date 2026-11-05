---
name: brd-to-backlog
description: Đọc BRD (Word/Google Docs/PDF) trên Google Drive của một hệ, tách thành Epic → Story → Sub-task theo format chuẩn, gán priority, story point, xếp sprint 2 tuần, xuất bản nháp Google Sheet cho người duyệt; chỉ sau khi người dùng đồng ý mới tạo ticket trên Jira. Dùng khi người dùng đưa link folder/tệp BRD và muốn sinh backlog, chia sprint, tạo ticket Jira, hoặc cập nhật backlog khi BRD đổi phiên bản.
---

# BRD → backlog Jira

Quy trình 8 bước (0–7). Làm **tuần tự**; không bỏ bước; không ghi gì lên Jira trước bước 7.

Tài liệu đi kèm (đọc trước khi làm bước tương ứng):

| Tệp | Dùng ở bước |
|---|---|
| [rules/decomposition.md](rules/decomposition.md) | 2, 4 — rút yêu cầu, tách Epic/Story/Sub-task |
| [rules/priority-sprint.md](rules/priority-sprint.md) | 4 — priority, story point, xếp sprint |
| [templates/epic.md](templates/epic.md), [templates/story.md](templates/story.md), [templates/subtask.md](templates/subtask.md) | 4, 7 — nội dung từng ticket |
| [templates/google-sheet.md](templates/google-sheet.md) | 6 — bố cục bản nháp |
| [checklists/quality.md](checklists/quality.md) | 5 — tự kiểm trước khi đưa người duyệt |
| [docs/brd/jira/README.md](../../../docs/brd/jira/README.md) | 0, 7 — tệp nối BRD ↔ Jira trong repo |

## Nguyên tắc bắt buộc

1. **Bảo mật BRD.** Không chép nội dung BRD vào repo, commit, issue GitHub hay log. Repo chỉ lưu **link Drive, file id, phiên bản, mã mục/mã yêu cầu, mã ticket Jira** (tệp `docs/brd/jira/<mã-hệ>.yaml`). Bản BRD đã chuyển sang markdown và tệp trung gian để trong **thư mục scratchpad** của session, không để trong repo.
2. **Không đoán.** Chỗ BRD mơ hồ/mâu thuẫn/thiếu → ghi vào tab *Câu hỏi mở*. Nếu buộc phải giả định để tách tiếp, ghi rõ "Giả định:" trong Story và tab *Câu hỏi mở*; không giấu giả định trong tiêu chí nghiệm thu.
3. **Không bịa trường Jira.** Story point, Sprint, Epic/parent là trường cấu hình riêng của từng site. Đọc metadata của project trước khi ghi (bước 7); không tự đặt id trường.
4. **Không xin mật khẩu/token.** Drive và Jira dùng connector OAuth (Google Drive, Atlassian MCP). Thiếu quyền → nói người dùng chia sẻ folder hoặc kết nối connector; không nhận mật khẩu trong chat.
5. **Cổng duyệt.** Bước 7 chỉ chạy khi người dùng đã trả lời rõ đồng ý trên bản nháp (ví dụ "Đồng ý tạo ticket"). Đồng ý cho một lần chạy không áp dụng cho lần chạy sau hay hệ khác.
6. **Chạy lại không tạo trùng.** Luôn đọc `docs/brd/jira/<mã-hệ>.yaml` trước; mục đã có `jira:` thì cập nhật, không tạo mới.

## Đầu vào cần có

Hỏi một lần, gộp các mục còn thiếu:

| Đầu vào | Bắt buộc | Mặc định khi thiếu |
|---|---|---|
| Link folder hoặc tệp BRD trên Google Drive | có | — |
| Mã hệ (chữ thường, gạch ngang, vd `app-htx`) | có | đề xuất từ tên tệp, hỏi xác nhận |
| Jira: site (`xxx.atlassian.net`) và project key | có (bước 7) | — chỉ cần trước bước 7 |
| Velocity (điểm/sprint) hoặc thành phần team | không | **20 điểm/sprint, ghi rõ là giả định** |
| Ngày bắt đầu sprint 1 | không | thứ Hai kế tiếp, ghi là giả định |
| Độ dài sprint | không | **2 tuần** |

Cấu trúc ticket cố định: **Epic → Story → Sub-task** (Sub-task là con của Story).

## Bước 0 — Kết nối và chụp phiên bản

1. Kiểm tra công cụ Google Drive có sẵn. Không có → dừng, hướng dẫn người dùng kết nối connector Google Drive trên claude.ai.
2. Từ link: lấy folder id hoặc file id. Folder → liệt kê tệp (`search_files` với `parentId = '<id>'`). Chọn tệp BRD của hệ; nhiều phiên bản → lấy bản mới nhất theo số phiên bản trong tên (`_vX.Y`) hoặc `modifiedTime`, hỏi lại nếu không chắc.
3. Không thấy tệp/không đọc được → tài khoản đang kết nối chưa có quyền. Dừng, nhờ người dùng chia sẻ folder (quyền xem) cho tài khoản đó.
4. Ghi lại: file id, tên tệp, `modifiedTime`, link. Đọc `docs/brd/jira/<mã-hệ>.yaml` nếu có: cùng phiên bản → hỏi người dùng muốn chạy lại hay dừng; khác phiên bản → chế độ **cập nhật** (xem cuối tệp).

## Bước 1 — Đọc BRD

1. Google Docs → `read_file_content`. `.docx`/`.pdf` → `download_file_content` vào thư mục mới trong scratchpad rồi chuyển sang markdown (`.docx`: pandoc hoặc python-docx; `.pdf`: skill pdf). Hình (quy trình, wireframe) → xem như ảnh khi cần hiểu luồng.
2. Giữ nguyên **số mục** (§1.3, §4.2…) và **mã có sẵn** của BRD (`UR-…`, `FN-…`, `BR-…`, `NFR-…`). Bảng giữ dạng bảng.
3. BRD dài: giao subagent `brd-analyst` đọc và trả danh mục yêu cầu (bước 2) để giữ ngữ cảnh chính gọn; gửi cho nó đường dẫn bản markdown trong scratchpad và yêu cầu đầu ra theo bước 2.

## Bước 2 — Danh mục yêu cầu

Theo [rules/decomposition.md §1](rules/decomposition.md#1-danh-mục-yêu-cầu). Kết quả: danh sách yêu cầu, mỗi dòng có mã (ưu tiên mã BRD; không có mới đặt `REQ-nnn`), mục BRD, loại, mức ưu tiên BRD, tóm tắt một câu, vai trò, tiên quyết.

## Bước 3 — Phát hiện vấn đề

Duyệt danh mục và BRD tìm: thiếu tiêu chí/quy tắc kiểm, vai trò không rõ, mâu thuẫn giữa mục, yêu cầu phi chức năng không đo được, phụ thuộc hệ ngoài chưa có đặc tả, thuật ngữ không định nghĩa. Mỗi vấn đề → một dòng *Câu hỏi mở* (mục BRD, câu hỏi, Story bị ảnh hưởng, giả định tạm nếu có).

## Bước 4 — Tách việc, ưu tiên, xếp sprint

1. Tách Epic → Story → Sub-task theo [rules/decomposition.md §2–§4](rules/decomposition.md#2-epic); điền nội dung theo template.
2. Gán mã tạm: Epic `E01`, Story `E01-S01`, Sub-task `E01-S01-T01`. Mã tạm không đổi giữa các lần chạy (dùng làm khóa trong tệp nối).
3. Ước lượng story point, priority, phụ thuộc, xếp sprint theo [rules/priority-sprint.md](rules/priority-sprint.md).

## Bước 5 — Tự kiểm

Chạy toàn bộ [checklists/quality.md](checklists/quality.md). Mục trượt → sửa rồi kiểm lại; không sửa được (do BRD) → ghi *Câu hỏi mở*. Ghi kết quả vào tab *Kiểm tra*.

## Bước 6 — Bản nháp cho người duyệt

1. Đọc skill `anthropic-skills:google-workspace` trước khi tạo Google Sheet.
2. Tạo Google Sheet trong **folder review riêng**, không đặt chung folder BRD: folder người dùng chỉ định, hoặc nếu chưa có thì tạo folder `Backlog nháp` cùng cấp với folder BRD (cùng folder cha) và báo người dùng. Tên tệp `Backlog nháp — <mã-hệ> — BRD <phiên bản> — <yyyy-mm-dd>`, bố cục theo [templates/google-sheet.md](templates/google-sheet.md). Số tổng hợp (phủ yêu cầu, tổng điểm, ngày sprint, Story mỗi sprint) là công thức, không gõ tay.
3. Gửi người dùng: link Sheet; số Epic/Story/Sub-task, tổng điểm, số sprint; các giả định (velocity, ngày bắt đầu); số câu hỏi mở và 3–5 câu quan trọng nhất. **Dừng, chờ duyệt.**
4. Người dùng sửa trực tiếp trên Sheet (cột *Duyệt*: `OK` / `Sửa` / `Bỏ`, cột *Ghi chú duyệt*) hoặc góp ý trong chat. Khi họ báo xong: đọc lại Sheet, áp dụng, chạy lại bước 5, cập nhật Sheet, báo thay đổi. Lặp tới khi người dùng đồng ý tạo ticket.

## Bước 7 — Tạo ticket Jira (chỉ sau khi đồng ý)

1. Kiểm tra công cụ Atlassian/Jira có sẵn. Không có → dừng, hướng dẫn kết nối connector Atlassian trên claude.ai.
2. Lấy site/cloud id; đọc metadata project: loại issue có sẵn (Epic, Story, Sub-task), priority, trường Story point, trường Sprint, cách gắn cha (parent). Thiếu loại issue hoặc trường → dừng, báo người dùng, không tự thay thế.
3. Sprint: tìm board Scrum của project, liệt kê sprint sẵn có. Sprint trong kế hoạch chưa có → **hỏi** người dùng tự tạo hay cho phép tạo (tên `<KEY> Sprint n`, ngày theo kế hoạch). Không tạo sprint khi chưa được phép; khi đó để trống trường Sprint và ghi sprint dự kiến trong mô tả.
4. Tạo theo thứ tự Epic → Story → Sub-task; chỉ dòng có *Duyệt* ≠ `Bỏ`. Nội dung theo template; summary không có mã tạm. Label: `brd-<mã-hệ>`. Phụ thuộc giữa Story → issue link "blocks / is blocked by" khi project hỗ trợ.
5. Sau **mỗi** ticket tạo thành công: ghi mã Jira vào cột *Jira key* của Sheet và vào `docs/brd/jira/<mã-hệ>.yaml`. Lỗi giữa chừng → dừng, báo đã tạo những gì; lần chạy lại tiếp tục từ mục chưa có `jira:`.
6. Xong: commit tệp nối (commit message tiếng Việt, tiền tố mã gói theo AGENTS.md §4), báo người dùng link filter Jira (`labels = brd-<mã-hệ>`), số ticket theo loại, sprint đã gán.

## Chế độ phần còn lại (hệ đã triển khai một phần)

Khi repo đã có mã cho BRD (có `docs/uat/*-acceptance.md`, gói trong `docs/roadmap.d/`, issue GitHub theo phòng): hỏi người dùng muốn backlog đủ BRD hay chỉ phần còn lại. Mỗi hệ BRD dùng một project Jira riêng nếu người dùng nói vậy; không trộn hệ.

Chỉ phần còn lại thì:
1. Lấy yêu cầu "một phần" và "chưa làm" từ ma trận nghiệm thu, rồi **đối chiếu lại** với các gói gộp sau ngày của ma trận (roadmap, giả định gói, mã) — ma trận thường cũ.
2. Đọc issue GitHub đang mở của phòng để không tạo trùng; ghi số issue vào cột `Issue GitHub liên quan`.
3. Tab Story thêm cột `Hiện trạng trong repo` (đã có gì, còn thiếu gì); tab Yêu cầu chỉ liệt kê yêu cầu còn mở, thêm cột hiện trạng.
4. Giữ mã câu hỏi khách đã có trong repo (Q-xx) để truy vết; việc chờ khách xếp từ sprint 3, sprint 1–2 chỉ gồm việc làm được ngay.

## Chế độ cập nhật (BRD đổi phiên bản)

1. Đọc BRD mới (bước 1–2), so danh mục yêu cầu với lần trước theo mã yêu cầu/mục BRD trong tệp nối.
2. Phân loại: yêu cầu **mới** → Story mới; **đổi** → Story cần sửa; **bỏ** → Story đề xuất đóng (không xóa ticket).
3. Bản nháp chỉ chứa thay đổi (thêm cột *Thay đổi*: `Mới` / `Sửa` / `Đóng`). Duyệt như bước 6; bước 7 cập nhật ticket có sẵn thay vì tạo mới. Ticket Jira đã ở trạng thái đang làm/đã xong mà bị đổi → không sửa mô tả, thêm comment nêu thay đổi và hỏi người dùng.
