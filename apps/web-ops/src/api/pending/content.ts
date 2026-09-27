// ĐANG CHỜ HỢP ĐỒNG — module pf/content (thư viện số, quy trình kỹ thuật).
import type { DateTime, PageQuery, Uuid } from "./common";

export type DocumentKind = "quy_trinh" | "so_tay" | "video" | "van_ban" | "infographic";

/** GET /v1/documents */
export interface LibraryDocument {
  id: Uuid;
  title: string;
  kind: DocumentKind;
  topic: string;
  summary: string;
  version: number;
  /** Cấp ban hành: TW soạn và xuất bản, tỉnh bổ sung đặc thù địa phương. */
  issued_by_level: "tw" | "tinh";
  issuer_name: string;
  published_at: DateTime;
  read_count: number;
  /** Có bản bổ sung của tỉnh người gọi. */
  has_local_supplement: boolean;
}

export interface DocumentQuery extends PageQuery {
  q?: string;
  kind?: DocumentKind;
  topic?: string;
}
