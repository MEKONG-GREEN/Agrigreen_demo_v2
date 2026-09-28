# Dự báo nhu cầu cơ giới hóa (CGH FN-09) — phương pháp tạm `cgh.forecast/v1`

> **Cần DCRD xác nhận phương pháp.** BRD Bản đồ CGH v1.3 yêu cầu "cân đối và dự báo" nhưng không nêu cách dự báo. Phương pháp dưới đây là giả định của nhóm phát triển để có số liệu cho G1. Mọi kết quả mang `rule_version = cgh.forecast/v1` và `method_provisional = true`. Khi DCRD chốt phương pháp khác, thêm phiên bản quy tắc mới (`cgh.forecast/v2`); kết quả cũ không bị tính lại.

## Công thức

Cho vụ dự báo S (mã vụ trong danh mục `season`) năm Y, với từng HTX:

1. **Diện tích gốc** `A(Y−1)`: diện tích canh tác cùng vụ S năm Y−1 (bảng `season_area`, theo QT-03: App HTX ưu tiên, nhập tay khi thiếu hoặc đã khóa). Không có → dòng `status = no_data`.
2. **Tăng trưởng từng năm** `g(y) = (A(y) − A(y−1)) ÷ A(y−1)` với y = Y−1, Y−2, Y−3 — chỉ tính khi có đủ hai năm liên tiếp và `A(y−1) > 0`.
3. **Tăng trưởng áp dụng** `g = trung bình các g(y)` (tối đa 3 vụ cùng loại gần nhất), **chặn trong [−20 %, +20 %]**. Không có tỷ lệ nào → `g = 0`.
4. **Diện tích dự báo** `F = A(Y−1) × (1 + g)`, làm tròn 4 chữ số thập phân.
5. **Số máy cần** theo chủng loại máy của khâu thí điểm: `F ÷ định mức`, với định mức `cgh.norm_ha_per_machine:<loại máy>:<khâu>` **có hiệu lực tại ngày bắt đầu vụ dự báo** (tháng `start_month` của vụ S năm Y — mục 16). Không có định mức → `status = no_norm`. Làm tròn lên để ra `required_whole`.
6. **Chênh lệch** `gap = capacity_now − required_whole`, với `capacity_now` là số máy "Hoạt động" của HTX tại lúc chạy dự báo.

Ví dụ: 2023 = 400 ha, 2024 = 440 ha, 2025 = 484 ha → g = (10 % + 10 %) / 2 = 10 % → F(2026) = 532,4 ha; định mức 100 ha/máy/vụ → 5,324 → cần 6 máy.

## Lưu trữ và API

- Migration `000015`: sổ bất biến `pf_mechanization.forecast_run` (stream `forecast:<vụ>-<năm>`) và `forecast_result` (stream `frun:<run_id>`). Mỗi dòng lưu đầu vào đã dùng: diện tích lịch sử theo năm, tỷ lệ tăng từng năm, trung bình trước chặn, cờ chặn, ngày tra định mức, giá trị + phiên bản định mức.
- Chạy: `POST /v1/mechanization/forecasts {season_code, season_year}` (Admin API, vai `cgh_admin`, Idempotency-Key). Chạy lại tạo lần mới; lần cũ giữ nguyên.
- Đọc: `GET /v1/mechanization/forecasts?season_code=&season_year=`, `GET /v1/mechanization/forecasts/{id}?province_code=&commune_code=&cooperative_ref=&farm_stage=&machine_type=` (Application API, vai `cgh_viewer`/`cgh_admin`, lọc theo phạm vi).

## Câu hỏi cho DCRD

1. Có dùng tăng trưởng lịch sử không, hay dùng kế hoạch diện tích của địa phương?
2. Số vụ lịch sử (hiện 3) và mức chặn (hiện ±20 %)?
3. Năng lực so sánh là máy hiện có hay máy dự kiến đầu tư?
