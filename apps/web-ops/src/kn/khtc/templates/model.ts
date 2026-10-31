// Pure labels/constants: safe for both client screens and mock registration.
import type { TemplateKey } from "./api";
export const TEMPLATE_KEYS: TemplateKey[] = ["mau_1", "mau_2", "mau_3", "mau_4", "mau_5"];
export const TEMPLATE_TITLES: Record<TemplateKey, string> = { mau_1: "Mẫu 1 — Báo cáo tuần", mau_2: "Mẫu 2 — Báo cáo tháng", mau_3: "Mẫu 3 — Báo cáo quý", mau_4: "Mẫu 4 — Báo cáo năm", mau_5: "Mẫu 5 — Tiến độ chương trình" };
