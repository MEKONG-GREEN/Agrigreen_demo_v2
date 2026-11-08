# KHTC v2.3 — supplier phiếu chờ rà soát #293

Ma trận này chỉ chứng minh public supplier Forms trong FN-TI-02.01 P760/P768, FN-TI-02.04 P894–904 và BR-DL-02 của BRD final v2.3; không sử dụng ma trận v2.2 cũ làm đặc tả. UI/dashboard/menu/consumer và toàn FN chưa nghiệm thu bởi gói này.

| Function/điều kiện | Native test |
|---|---|
| Phiếu đã gửi, count đúng cùng quyền qua nhiều trang, không lấy count từ page | `TestPending293NativeCountBeyondPage`, `TestPending293ScopeIntersectionAndConnectionReuse` |
| Latest attempt/identity/time; rà soát/trả lại/gửi lại; lần cuối tự khóa, kỳ đóng/hủy/held không pending | `TestPending293NativeLifecycleAndLockedLimit`, `TestPending293NativeMutationInvalidatesContinuation` |
| Current caller/context/MFA/role/node/read-only/revoke trước data/replay | `TestPending293CurrentAuthorityBeforeCursorAndData` |
| Scope đầy đủ/empty khác unavailable; malformed/oversize chặn sớm; unknown/duplicate/incomplete fail closed | `TestPending293BoundsBeforeOwnerOrDB`, `TestPending293NativeInvalidCoverageAcrossCandidates`, `TestPending293NativeBoundaryRejectsInvalidEvidence` |
| Consistency/query/current authority/visibility thay đổi bắt restart; commit active XID không cần XID mới | `TestPending293NativeVisibilityCommitWithoutNewXID`, `TestPending293QueryAndCursorTampering` |
| Pool1, gọi đồng thời, RLS/connection reuse; owner I/O ngoài TX | `TestPending293PoolOneOwnerIOAndRLSCleanup`, `TestPending293ScopeIntersectionAndConnectionReuse` |

Cách chạy, giới hạn visibility epoch và owner wiring ở [giả định gói](../kn/gia-dinh/KHTC-PENDING-NATIVE-BATCH-COUNT.md). Native row-boundary fault injection đọc SQL thật trước khi mô phỏng evidence lỗi; không tắt constraint/RLS/ledger. Whole Linux CI, consumer production và UAT tổng thể phải chạy sau tích hợp thật.
