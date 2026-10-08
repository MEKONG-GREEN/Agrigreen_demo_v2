-- pf/simulation: chỉ mục cho khóa ngoại chưa có chỉ mục dẫn đầu tương ứng (rà soát FK).
-- Chạy bằng vai ag_migrator (R-25). Chỉ tiến (R-24).
--
-- (pf 000024)
--   - scenario_hub: FK (tenant_id, hub_id) → candidate_hub; PK (tenant_id, scenario_id, hub_id) không
--     dẫn đầu bằng hub_id — tra "kịch bản nào dùng Hub này" và kiểm FK phải quét bảng.
--   - result: FK (tenant_id, snapshot_id) → input_snapshot; PK (tenant_id, simulation_id).
-- Không CONCURRENTLY: mỗi tệp migration chạy trong một giao dịch; bảng nhỏ, khóa SHARE ngắn.
SET LOCAL lock_timeout = '5s';

CREATE INDEX IF NOT EXISTS scenario_hub_hub ON pf_simulation.scenario_hub (tenant_id, hub_id);
CREATE INDEX IF NOT EXISTS result_snapshot ON pf_simulation.result (tenant_id, snapshot_id);
