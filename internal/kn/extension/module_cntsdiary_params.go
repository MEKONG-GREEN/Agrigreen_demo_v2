package extension

import (
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

// CNTSDiaryMasterdataParams là đăng ký RIÊNG của KN cho hai tham số nhật ký nuôi CNTS (W11.48, KN-PARAMS-CNTS-DIARY-OWNER
// #85): hạn sửa lần ghi đã gửi chưa xác thực và hạn lưu tọa độ GPS lúc ghi. Tách khỏi MasterdataParams để đăng ký KN cũ
// (16 spec/13 seed, tests/golden/kn-masterdata-params.json) giữ nguyên từng byte. Gốc lắp ráp nối thêm đăng ký này vào
// masterdata.Deps.Params (#84). Mỗi lần gọi trả bản sao độc lập.
//
// Giới hạn (Min/Max) là hợp đồng với consumer cntsdiary/app: giá trị đã duyệt phải là số nguyên trong khoảng; ngoài khoảng
// → consumer trả 503, không cắt hay rơi về mặc định. Seed = mặc định đang chạy (7/365), hiệu lực từ 01/01/2026 như các
// ngưỡng CNTS khác (W11.41) — trước đó nhật ký chưa có số liệu.
func CNTSDiaryMasterdataParams() mdport.ParamRegistration {
	edit, retention := "7", "365"
	return mdport.ParamRegistration{
		Specs: []mdport.ParamSpec{
			{Key: extport.ParamCNTSDiaryEditWindowDays, Name: "Nhật ký nuôi CNTS: số ngày được sửa lần ghi đã gửi chưa xác thực (tính từ ngày ghi)",
				Unit: "ngày", Kind: mdport.ParamKindDecimal, Min: "0", Max: "30"},
			{Key: extport.ParamCNTSDiaryGPSRetentionDays, Name: "Nhật ký nuôi CNTS: thời hạn lưu tọa độ GPS lúc ghi",
				Unit: "ngày", Kind: mdport.ParamKindDecimal, Min: "1", Max: "3650"},
		},
		Seeds: []mdport.ParamSeed{
			{Key: extport.ParamCNTSDiaryEditWindowDays, Value: &edit, Unit: "ngày", EffectiveFrom: "2026-01-01",
				SourceDoc: "BRD CNTS v3.7 Mục III.5 — lần ghi đã gửi chưa xác thực sửa trong 7 ngày kể từ ngày ghi (W11.48)"},
			{Key: extport.ParamCNTSDiaryGPSRetentionDays, Value: &retention, Unit: "ngày", EffectiveFrom: "2026-01-01",
				SourceDoc: "W11.48 giả định tạm Q-005 — lưu tọa độ GPS lúc ghi nhật ký 365 ngày (QĐ-8), chờ DPIA/pháp chế xác nhận"},
		},
	}
}
