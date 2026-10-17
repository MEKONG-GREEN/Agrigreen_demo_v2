## KNSPLIT-2-NOTIFY-DIRECTIVE-B — Xóa chỉ đạo và email legacy

Nền292d9a15. RED19162actual1 đúng5 fallback: thiếu planner nil/empty×2, thiếu digest policy nil/empty vẫn có job×2, RunEmailDigest thiếu policy vẫn liệt kê tenant×1. Xóa case/HandledEvent/legacyDirectivePlan/onDirective/capture generators, DirectiveTemplate và officerRoles riêng. Core không còn import KN extension. Xóa DigestMessage và job/formatter mặc định; thiếu policy không tạo job hoặc liệt kê tenant. Source/job format do KN đăng ký.

Chuyển KindDirective/JobEmailDigest identifier/literal/comment về extensionport, đổi owner/worker selection/help/test qualifiers, không alias. DigestItem nội bộ dùng Reference cho tham chiếu nguồn tổng quát: registry/formatter đã hỗ trợ dữ liệu nguồn tùy ý và GenericAtomicPlan PG chứng minh; field rename đi cùng việc xóa policy cũ. Giữ nguyên physical directive_id và mọi câu SQL/args, không DDL/DML/hash rewrite. Normalize/Tx/RLS/inbox/IDs/provenance/MarkSent giữ.

Mười lăm golden Gitblob phải nguyênA: metadata16/41,CNTS46,workflow77,price118/directory16/32,task83,directive52/digest11/job. Owner chỉ đọc expected; các assertion GenericAtomicPlan/NoAudience/lookupretry/SMTPretry và source cũ giữ. Scope pure5/fullstatic/actualpruneonly từ320/full9PG thật noFAIL-SKIP/helper9migration/remote trước dev. Không OpenAPI/UI/mobile change.

Notify/G1 còn kindCHECK/nativeKNrecipient policy/authz/accounts/party/data/routes/tenant/client/KHTC reminder; MRV thuộc HTX/MKG, tenant KN theo Trung tâm và các PO khác giữ. Main/worktrees/lịch giữ.
