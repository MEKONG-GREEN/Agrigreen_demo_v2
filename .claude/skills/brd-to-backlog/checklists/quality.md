# Checklist tự kiểm (bước 5)

Mỗi mục ghi một dòng vào tab *Kiểm tra*. Mục trượt phải sửa, hoặc giải thích và ghi *Câu hỏi mở*.

## Phủ yêu cầu
- [ ] **C1** Mọi yêu cầu `FR`/`BR` trong danh mục có ≥ 1 Story phủ (tab Yêu cầu không còn `Thiếu`).
- [ ] **C2** Mọi `NFR` được phủ bởi tiêu chí nghiệm thu hoặc Story kỹ thuật.
- [ ] **C3** Không Story nào thiếu truy vết (mã yêu cầu hoặc mục BRD).
- [ ] **C4** Không có Story ngoài phạm vi BRD (nếu có, đánh dấu "đề xuất ngoài BRD" và để `Lowest`).

## Chất lượng Story (INVEST)
- [ ] **I1 Independent** — phụ thuộc chỉ ở những chỗ thật cần; không vòng phụ thuộc.
- [ ] **I2 Negotiable** — mô tả nêu *cái gì/vì sao*, không áp đặt thiết kế chi tiết khi BRD không yêu cầu.
- [ ] **I3 Valuable** — có vai trò và giá trị rõ; Story kỹ thuật có lý do ở *Giá trị*.
- [ ] **I4 Estimable** — có story point; điểm "ước tạm" có câu hỏi mở đi kèm.
- [ ] **I5 Small** — không Story nào 13 điểm trở lên; mỗi Story vừa một sprint.
- [ ] **I6 Testable** — 3–8 tiêu chí Given/When/Then, có luồng thành công, có dữ liệu sai bị từ chối ở server, có không đủ quyền khi chức năng có phân quyền.

## Định nghĩa sẵn sàng (Definition of Ready)
- [ ] **R1** Summary ≤ 80 ký tự, bắt đầu bằng động từ (Story), không chứa mã tạm.
- [ ] **R2** Story có ≥ 1 Sub-task; Sub-task có loại hợp lệ và ≤ 2 ngày công.
- [ ] **R3** Story có trường dữ liệu nhập → có bảng *Dữ liệu và kiểm nhập*.
- [ ] **R4** Story có lệnh ghi/số liệu/gửi thông báo → mục *Lưu ý kỹ thuật bắt buộc* ghi đúng điều AGENTS.md §9 áp dụng.

## Priority và sprint
- [ ] **P1** Mỗi Epic/Story có priority và lý do một dòng.
- [ ] **P2** Không Story nào có priority cao hơn Story nó phụ thuộc.
- [ ] **P3** Không sprint nào vượt năng lực kế hoạch.
- [ ] **P4** Story phụ thuộc nằm sau tiền đề (hoặc cùng sprint theo ngoại lệ ≤ 3 điểm, có ghi thứ tự).
- [ ] **P5** Mỗi sprint có sprint goal demo được; sprint 1 có ít nhất một lát dọc.
- [ ] **P6** Story bị câu hỏi mở *Chặn* không nằm ở sprint 1–2.
- [ ] **P7** Giả định velocity / ngày bắt đầu được ghi rõ ở tab Tổng quan.

## Trùng lặp và bảo mật
- [ ] **D1** Không hai Story trùng mục đích (cùng vai trò + hành động + dữ liệu).
- [ ] **D2** Chạy lại/cập nhật: không mục nào đã có `jira:` trong tệp nối bị đề xuất tạo mới.
- [ ] **S1** Không có đoạn BRD chép nguyên văn dài (> 2 câu) trong ticket; tóm tắt bằng lời mình và dẫn mục BRD.
- [ ] **S2** Không có nội dung BRD trong các tệp sắp commit vào repo (chỉ link, id, phiên bản, mã mục, mã Jira).
