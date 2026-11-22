## BF-KHTC-HRM-PERIOD-CAS-CONSUMER — lát đổi hạn KHTC

Issue #209, Codex viết và Claude review. Primitive CAS #159 và template policy #191 đã được review chéo và gộp dev; bản này chỉ nối consumer ChangeDeadline cùng undo của chính mutation đó. Không nghiệm thu toàn CAS KHTC/HCTH hoặc G1.

### Ý định và replay

Cổng ConditionalPeriodControl được inject tường minh; thiếu cổng trả503 trước DB/intent/native write, không type assertion hoặc fallback legacy. ControlSnapshot ngoài TX; expected revision cùng tenant/period/nativeRef/request/actor/fingerprint quyền server được ghi bền vào sổ append-only trước nativeIO. Revision không phải quyền; native vẫn kiểm chính sách hiện hành.

Sổ intent có prepared, applied, completed, undo_prepared, undone, unresolved. Mọi native error, kể cả409 hay mất ACK sau commit, không được hiểu là rollback/thành công. Không lấy due/status/head mới để đoán receipt hoặc refresh expected. Intent chưa xác định giữ chặn thao tác mới trên tài nguyên, không có lease hết hạn rồi tự chiếm lại. Đây là đánh đổi tính sẵn sàng có chủ ý trong lát này, không phải tự đối soát hoàn thành.

Completed replay sau kiểm quyền của request mới trả stored Detail của chính operation, không Get hiện tại của successor. Request hash phân biệt hạn xuất bỏ trống với hạn truyền tường minh, ổn định theo UTC; cùngkey/body khác tài nguyên/actor/fingerprint bị từ chối. Request mới phải qua production middleware resolve quyền như hiện hành; gọi periodWriter lần nữa trong held request chỉ đọc facts đã bind, không chứng minh thu hồi midcall từ live registry.

### Applied revision và undo

Applied revision chỉ lấy từ native CAS thành công và phải lưu bền trước undo. Event KHTC và completed outcome cùng ownTX; projection đọc Forms/nguồn/danh mục ngoài TX, event seq thực được điền trước khi đóng băng response. Local-write failure đã rollback mới chuẩn bị undo; commit local không rõ kết quả giữ unresolved, không blind undo. Undo dùng đúng applied revision đã lưu, không tự cộng seq/refresh; successor chen vào làm undoCAS conflict thì giữ unresolved. Không I/O Forms trong TX KN, không SQL schema Forms hoặc KN ghi HTX.

### Phạm vi còn lại và thứ tự

[#211](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/211) giữ KHTC close/cancel/open compensation và HRM open compensation/ClosePlan, cùng consumer end-to-end/ABA/reconciliation còn lại. [#212](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/212) giữ public operation receipt để đối soát unknown ACK; snapshot progress/head hiện hữu không chứng minh operation nào đã commit. Không dùng việc native159 PASS làm bằng chứng các consumer còn lại đã fenced.

Migration209 chỉ own pf_extension, append-only/FORCE RLS; phải gộp sau W11.69-ARCHIVE-FINAL (#199), transitively W10.18-GUEST-OTP (#184), để giữ thứ tự mốc migration pending. Không stack branch parent, không tăng baseline.

### Kiểm chứng và câu hỏi PO

RED oldbase191: missing CAS vẫnHTTP200 và đãghi deadline; pure guard cũng vàoDB trái kỳ vọng. Kiểm PG thật, mutation, cổng, full affected/CI đúngSHA và sốPASS/0SKIP ghi trong bình luận sẵn sàng sau khi chạy. Test mới giữ fixture riêng và synthetic identities; không tuyên bố OIDC/staging hoặc live mid-request revocation proof. Không thêm ô số/API numeric mới. Không có câu hỏi nghiệp vụ PO mới; giới hạn unresolved/receipt nêu rõ ở trên.
