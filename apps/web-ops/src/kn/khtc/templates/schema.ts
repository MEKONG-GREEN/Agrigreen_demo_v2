import type { TemplateBinding } from "./api";
export const SOURCE_LABELS = { plan: "Kế hoạch năm", auto: "Theo nguồn hạng mục", form: "Phiếu phòng", module: "Nguồn module", calculated: "Hệ thống tính", admin: "Ô hành chính", text: "Lời văn KHTC" };
export const FIELDS: Record<TemplateBinding["source"], string[]> = {
    plan: ["item_name", "program_name", "roman", "unit", "planned", "department"],
    auto: ["value", "content", "unit", "label", "department", "status", "people", "budget", "from_price", "to_price"],
    form: ["value", "content", "unit", "label", "department", "status", "people", "budget", "from_price", "to_price"],
    module: ["value", "content", "unit", "label", "department", "status", "people", "budget", "from_price", "to_price"],
    calculated: ["year_to_date", "percent"], admin: ["document_no", "place", "report_date", "recipient", "basis", "recipients", "authority", "signer_title", "signer"], text: ["narrative"],
};
export const FIELD_LABELS: Record<string, string> = { item_name: "Tên hạng mục", program_name: "Tên chương trình", roman: "Số La Mã", unit: "Đơn vị tính", planned: "Kế hoạch năm", department: "Phòng", value: "Số trong kỳ", content: "Nội dung", label: "Tên chỉ tiêu / mặt hàng", status: "Tình trạng", people: "Số người", budget: "Kinh phí", from_price: "Giá từ", to_price: "Giá đến", year_to_date: "Lũy kế năm", percent: "% kế hoạch", document_no: "Số văn bản", place: "Địa danh", report_date: "Ngày báo cáo", recipient: "Kính gửi", basis: "Căn cứ", recipients: "Nơi nhận", authority: "Thẩm quyền", signer_title: "Chức vụ người ký", signer: "Người ký", narrative: "Lời văn tự do" };
export const REPEAT_OPTIONS = [{ value: "none", label: "Không lặp" }, { value: "plan_item", label: "Hạng mục kế hoạch" }, { value: "program", label: "Chương trình" }, { value: "metric", label: "Nội dung theo dõi" }, { value: "commodity", label: "Mặt hàng giá cả" }, { value: "form_bullet", label: "Đầu mục phiếu phòng" }];
