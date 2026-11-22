## KNSPLIT-2-ACCOUNTS-POLICY-ROLLOUT — giả định kỹ thuật

#196 nối hợp đồng owner #195 đã vào dev, theo [thiết kế rollout](../../dev/accounts-unit-delegation-rollout.md). Nguồn nghiệp vụ duy nhất là phần Function chi tiết BRD 2.0 HCTH v4.0 FN-04, FN-05 BR-04/AC-01, FN-07 BR-04/BR-07/AC-02 và HS-37. Không thêm hoặc thẩm định lại nghiệp vụ.

Giữ policy/seed/giới hạn 30 ngày do owner khai báo. Provider thuần không I/O; mọi dữ liệu được lấy trước transaction. Nil giữ compatibility, đã cấu hình thì quyết định/lỗi là cuối; native tenant/node/assignment/grantor/idempotency/expiry/revoke, Admin/MFA và quyền HTX/MKG giữ nguyên. Không migration, dependency, OpenAPI hoặc UI mới.

Giữ catalog ordering #193 và các fallback/global/domain helper đến #57 đủ consumer coverage. Không đóng #57, các gói lifecycle #87–94, tenant cutover/4b hoặc G1 từ bằng chứng rollout này. Không có câu hỏi nghiệp vụ hay quyết định PO mới.
