-- Quyết định A2 (chốt 03/10/2026, docs/cau-hoi-ton-dong.md; BRD v7.7 FN-XA-03.05): thửa ruộng tối thiểu 3 điểm
-- ranh giới không thẳng hàng (tam giác hợp lệ) thay cho 4. Quy tắc nằm ở domain (geo/domain.MinPoints = 3); CHECK
-- ở DB là sàn an toàn nên nới từ >= 4 xuống >= 3. Ranh giới vùng HTX (cooperative_boundary) dùng chung một quy tắc
-- hình học (MultiPolygon.Normalize) nên nới cùng — nếu không, tam giác vùng HTX qua được domain nhưng vỡ CHECK (500).
-- Điểm trùng/thẳng hàng vẫn bị domain chặn (422 geo.too_few_points). Không bảng mới, không sửa dữ liệu.

DO $$
DECLARE
  t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['plot_boundary', 'cooperative_boundary'] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_constraint c
                    WHERE c.conrelid = format('pf_geo.%I', t)::regclass AND c.conname = t || '_point_count_check') THEN
      RAISE EXCEPTION 'thiếu ràng buộc pf_geo.%.%_point_count_check', t, t;
    END IF;
    EXECUTE format('ALTER TABLE pf_geo.%I DROP CONSTRAINT %I', t, t || '_point_count_check');
    EXECUTE format('ALTER TABLE pf_geo.%I ADD CONSTRAINT %I CHECK (point_count >= 3)', t, t || '_point_count_check');
  END LOOP;
END
$$;
