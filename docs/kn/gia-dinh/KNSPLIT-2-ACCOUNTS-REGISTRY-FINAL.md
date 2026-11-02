## KNSPLIT-2-ACCOUNTS-REGISTRY-FINAL — Role rollout và kiểm kê (lát đầu)

#193 nối actual owner RoleCatalog của #95 trên NativeRoleCatalog #96 cho API và cả năm constructors worker. Generic WithRoleCatalogOrder chỉ chấp nhận hoán vị đầy đủ để giữ output ordering legacy; compatibility snapshot còn làm reference thứ tự, không làm nguồn policy. Không đổi OrgKinds, grant/MFA/TOTP, realm hoặc ngữ cảnh, không retire bridge/default/domain helpers của #57.

[Kiểm kê và giới hạn](../../dev/accounts-role-rollout.md). Phần units/delegation cần owner exact contract #195 (Claude/lane KN) và shared rollout #196 sau #193/#195; chưa coi toàn acceptance #52/#193/#57, tenant cutover/4b/G1 đã hoàn tất. Các SessionPolicies #60, AssignmentRules #72 và AttributeCatalog #79 có sẵn không bị tạo lại. Không có quyết định PO mới hoặc migration/API/web mới. Mọi bằng chứng kiểm/CI đúng SHA được đăng issue khi hoàn tất.
