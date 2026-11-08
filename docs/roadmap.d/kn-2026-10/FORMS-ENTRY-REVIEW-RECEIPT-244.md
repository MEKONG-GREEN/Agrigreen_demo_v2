# FORMS-ENTRY-REVIEW-RECEIPT-244 (#288)

- Function: KHTC v2.3 FN-TI-02.04 B4 và truy vết; chỉ supplier Forms, chưa consumer/UI/UAT.
- Data: migration mới `pf_forms_entry_review_receipt`, permanent operation key + ledger receipt/audit/RLS; giữ dữ liệu cũ.
- Service: exact target/version/decision binding, native review/workflow atomic, immutable committed identity/time, read/replay/unknown recovery; không đổi Entry hiện hữu.
- API/port: additive `EntryOperations`, explicit owner current-authority policy, không REST route. Giới hạn unique OIDC metadata #259 được ghi rõ.
- Kiểm: native PG RED, adversarial receipt/rollback/real COMMIT ACK/pool1/RLS/HTTP owner chain và direct consumers; các cổng CONTEXT 3.3, controlled fault/restore/control. Linux CI race và integration/public actions chờ ROOT release.
- Tiếp theo: sau supplier thực sự vào dev, #289 dùng receipt cho durable KN review intent; #290/#291/#292 là phần độc lập. [Phạm vi và cách kiểm](../../kn/gia-dinh/FORMS-ENTRY-REVIEW-RECEIPT-244.md).
