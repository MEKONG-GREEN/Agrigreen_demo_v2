# KHTC v2.3 — kiểm chứng slice #217, chưa nghiệm thu toàn FN

Nguồn: Function FN-TI-02.03 P849/P862/P873/P876 và FN-TI-02.04 P900/P901/P904. Tài liệu v2.2 lịch sử không quyết định nghiệp vụ của slice này.

| BRD / hành vi | Kiểm chứng hiện có | Giới hạn |
|---|---|---|
| FN-TI-02.03 P849/P876: cap3 cố định, chỉ đọc | `TestFormsFixedLimitRejectsPublicationOverride217`, `TestFormsFixedLimitLegacyAndDirectSQL217`, `TestFormsFixedPolicyRegistrationAndGenericCompatibility217`; web routes test217 | Không mở màn tham số mới, không thay profile khác |
| FN-TI-02.04 P900/P901/P904: lần3 khóa, không review | `TestThirdSubmissionAutoLocks217`, `TestFormsFixedLimitLegacyAndDirectSQL217`, UI review241 test217 với stale legacy flags | Bài/phiếu cũ giữ nguyên dữ liệu; hiển thị hiệu lực theo rule |
| Một winner ở hai request cuối, lần4 không ghi | `TestFormsFixedLimitConcurrentFinalReceipt217`; direct SQL/native reject | Cần Linux race trên SHA tích hợp |
| Mất ACK/replay đúng lần gốc, quyền hiện tại, rollback | `TestFormsFixedLimitConcurrentFinalReceipt217`, `TestFormsSubmitReceiptAtomicRollback217`, `TestKHTCFixedThreeAndFinalLostACK217` | Mock browser không chứng minh real OIDC/PG |
| FN-TI-02.03 P862/P873: Copy popup và assignment gốc | Formal remainder [#302](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/302), sau actual #217/#300/#239/#211 | Chưa triển khai Copy trong slice; #300 không hứa recipient, không đoán old provenance |

Chạy native bằng pg-ephemeral với `-count=1 -p 1 -timeout 55m -v`, bắt buộc zero SKIP. Web: `npm ci`, `npm run typecheck`, `npm run lint`, `npm test`, `npm run check:api`, `npm run build:mock` và browser production mock desktop/mobile. G1/UAT trên KN standalone, real OIDC và final whole CI thuộc giai đoạn tích hợp; bảng này không đánh dấu hoàn thành các mục đó.
