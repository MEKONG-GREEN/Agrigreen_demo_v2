- [x] KNSPLIT-2-ACCOUNTS-POLICY-ROLLOUT (#196): nối owner đơn vị/ủy quyền vào consumer shared, giữ parity và compatibility đến #57.

- [x] KNSPLIT-2-ACCOUNTS-POLICY-ROLLOUT: `UnitDelegationPolicy` #195 nối accounts service/domain/handler, API assembly và worker; owner định nghĩa đơn vị/seed/ủy quyền và quyết định theo phiên chủ. Shared giữ ledger/RLS/idempotency, current assignment, tenant, grantor, revoke/expiry, Admin/MFA và parity native HTX/MKG.

- [x] KNSPLIT-2-ACCOUNTS-POLICY-ROLLOUT: thứ tự dependency → data → service → API → UI và lệnh chạy lại trong [thiết kế](../../dev/accounts-unit-delegation-rollout.md); [giả định](../../kn/gia-dinh/KNSPLIT-2-ACCOUNTS-POLICY-ROLLOUT.md), [ma trận nghiệm thu](../../uat/accounts-policy-acceptance.md). Không migration/UI mới, không retire #57 hoặc chứng nhận cutover/G1; bằng chứng PG/HTTP, mutation và CI/race đúng SHA được ghi ở issue.
