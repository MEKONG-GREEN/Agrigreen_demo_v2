package port

// ParamProgramTargetPrefix: mục tiêu diện tích Đề án 1 triệu ha của một tỉnh (ha),
// khóa đầy đủ "extension.program_target_ha:<mã tỉnh>" (FR-03). Chưa có → không
// tính tỷ lệ hoàn thành mục tiêu.
const ParamProgramTargetPrefix = "extension.program_target_ha"

// ProgramTargetKey dựng khóa tham số mục tiêu của tỉnh.
func ProgramTargetKey(province string) string { return ParamProgramTargetPrefix + ":" + province }
