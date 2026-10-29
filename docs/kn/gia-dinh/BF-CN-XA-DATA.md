## BF-CN-XA-DATA — Số liệu cấp xã của Tỉnh: Nhập liệu tuần BM-04 và Nhật ký sản xuất (#175)

Đặc tả: BRD CNTS v3.7 (07/10/2026) FN-09 (Cấp Tỉnh R, yêu cầu điều chỉnh/xác thực khi là người phụ trách biểu; BR-01, BR-02, AC-01…AC-06), FN-10 (Tỉnh đọc nhật ký), FN-13/UR-TINH-12. Prototype chỉ là đặc tả menu (`menus/base.json`).

| Câu hỏi / mục | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Nguồn số liệu BM-04 | Không thêm endpoint: `GET /v1/forms/tasks?profile=kn_cnts&code=CNTS-BM04&from&to&limit=200` (cap/forms lọc phạm vi người gọi trước khi trả, tối đa 200 kỳ một năm), lịch sử `GET /v1/forms/periods/{id}/submissions`, rà soát `POST /v1/forms/submissions/{id}/review`, người phụ trách `GET /v1/extension/cnts/period-tracking` | `apps/web-ops/src/kn/cnts/periods/{api.ts,bm04.ts,Bm04ProvinceScreen.tsx}` | Khi số xã × tuần lớn (đọc chậm): thêm endpoint tổng hợp ở `reportcycle` đọc qua `FiguresReader` |
| Cột lũy kế (BR-01) | Chỉ cột phát sinh trong tuần: tiêm phòng CN-02 (`tiem_*`), hóa chất CN-04 (`hoa_chat_*`), KSGM CN-06 (`ksgm_*`). Tổng đàn CN-01, vịt chạy đồng CN-08/09 là số hiện có — không cộng dồn | `bm04.ts` `FLOW_PREFIXES` | PO chỉ định cột khác → sửa danh sách tiền tố |
| "Số liệu hợp lệ" của lũy kế | Phiên bản cuối đã gửi, trừ bài bị "Yêu cầu điều chỉnh" (chưa cộng, hiện "chờ điều chỉnh tuần …") và "Chờ mở kỳ". Kỳ có công việc nhưng chưa gửi tính 0, hiện "Thiếu kỳ …". Kỳ xã không được giao: bỏ qua | `bm04.ts` `cumulative` | PO muốn chỉ cộng bản Đã xác thực → đổi `hasValid` |
| Gốc lũy kế | Từ đầu năm hoặc đầu quý (người xem chọn); năm theo khóa tuần `YYYY-Www`, quý theo ngày cuối tuần | `bm04.ts` | — |
| Cùng tuần nhiều lô phát hành | Gộp cột theo `period_key`; cùng xã cùng tuần hai kỳ → lấy kỳ có bài gửi mới hơn | `bm04Data` | — |
| AC-02 "hiện ở Tỉnh trong 1 phút" | Màn đọc trực tiếp bài nộp hiện hành (không bộ đệm); nút "Tải lại" | màn | Thêm tự tải lại định kỳ nếu PO cần |
| AC-06 cờ "Đã sửa sau xác thực" | Suy từ lịch sử phiên bản: có phiên bản cũ Đã xác thực và phiên bản hiện hành chưa xác thực lại (gửi lại sau khi Tỉnh mở lại kỳ). Hiện ở chi tiết xã (không ở bảng — tránh N lời gọi lịch sử) | `editedAfterAccept` | Muốn cờ trên bảng → cap/forms trả cờ trong `FormsTask` |
| Người phụ trách biểu xác thực | Nút Xác thực / Yêu cầu điều chỉnh chỉ hiện cho Cấp Tỉnh là người phụ trách (owner_ref của lần phát hành = người đăng nhập); lần phát hành không chọn người phụ trách → mọi Cấp Tỉnh CNTS (như cap/forms `review_by = TINH`). **Máy chủ chưa chặn người không phụ trách** (cap/forms chỉ kiểm cấp, bốn mắt) | `Bm04ProvinceScreen.tsx` | Ticket con: cổng kiểm người phụ trách ở cap/forms (hook `ReviewGuard` của profile) — xem câu hỏi PO |
| Lọc khu vực | Lấy xã + khu vực từ Xem trước phát hành "Tất cả xã" (không ghi); không lấy được → ẩn lọc khu vực | màn | — |
| Xuất Excel BM-04 tổng hợp | Ngoài phạm vi gói này (đường DLP/pf/export cho bảng này chưa có) | — | Gói riêng qua pf/export |
| Nhật ký — lọc và trang | Mở rộng `GET /v1/extension/cnts/diary-entries`: `commune`, `form`, `from`, `to` (ngày ghi của bản hiện hành), `limit` 1–200, `cursor` (khóa `entry_date, captured_at, entry_ref` giảm dần, base64url) → `next_cursor`. Không truyền `limit`: như cũ (tối đa 2000), vẫn trả `next_cursor` khi còn | `cntsdiary/{store,app,handler}` | — |
| Phạm vi nhật ký | Máy chủ lọc trước khi trả: hộ ngoài phạm vi bỏ (như cũ); `commune` ngoài phạm vi → danh sách rỗng (không 403, không lộ tồn tại); người gọi không phải Cấp Xã/Admin không thấy nháp — lọc luôn ở SQL. Trang lọc phạm vi sau khi đọc nên có thể ít hơn `limit` dòng; con trỏ vẫn đi tiếp | `ListEntries` | Nếu trang hụt nhiều: đẩy lọc phạm vi xã vào SQL |
| Tỉnh không sửa số liệu xã | Màn nhật ký chỉ đọc (không nút ghi); lần ghi chờ xác thực có liên kết sang "Rà soát nhật ký" | `ProvinceDiaryScreen.tsx` | — |
| Lọc hộ/xã/chương trình nhật ký | Danh sách xã, hộ lấy từ hộ mô hình trong phạm vi (`/model-households`), chương trình từ `/projects` | màn | — |
