// DỮ LIỆU GIẢ LẬP — người dùng thử cho chế độ NEXT_PUBLIC_API_MOCK=1. Không phải người thật.
// `me` có ĐÚNG hình dạng GET /v1/me (components.schemas.Me) — mock trả nguyên văn.
// Tổ chức của persona trùng tổ chức trong cây giả lập (ORGS) để màn hình tài khoản/tổ chức khớp nhau.
import type { Me } from "../types";
import { COMMUNES, communeOrgRef, DCRD_REF, htxCoop, MKG_REF, NATIONAL_REF, PROVINCES, provinceOrgRef } from "./data";

export interface MockPersona {
  id: string;
  title: string;
  description: string;
  /** Tên hiển thị — trong thực tế lấy từ claim `name` của token (Me không chứa tên, R-04). */
  displayName: string;
  /** amr giả lập: có "otp" khi đã qua MFA. */
  amr: string[];
  me: Me;
}

const TENANT = "01927b3e-0000-7000-8000-00000000a001";
const MKG_ORG = { ref: MKG_REF, kind: "mkg", name: "Mekong Green", admin_unit_code: null, active: true } as const;

const dtCommunes = COMMUNES.filter((c) => c.province_code === "82");
const myCommune = dtCommunes[0];
const names = (codes: string[]) =>
  Object.fromEntries(codes.map((c) => [c, PROVINCES.find((p) => p.code === c)?.name ?? COMMUNES.find((x) => x.code === c)?.name ?? c]));

function persona(p: Omit<MockPersona, "me"> & { actor: string; roles: string[]; org: Me["org"]; scope: Omit<Me["scope"], "unit_names"> }): MockPersona {
  const unitCodes = [...p.scope.units, ...(p.org?.admin_unit_code ? [p.org.admin_unit_code] : [])];
  return {
    id: p.id,
    title: p.title,
    description: p.description,
    displayName: p.displayName,
    amr: p.amr,
    me: {
      actor_ref: p.actor,
      tenant_id: TENANT,
      roles: p.roles,
      mfa: p.amr.includes("otp"),
      org: p.org,
      scope: { ...p.scope, unit_names: names(Array.from(new Set(unitCodes))) },
    },
  };
}

const HTX = htxCoop();

const DT_ORG = { ref: provinceOrgRef(0), kind: "ttkn_province", name: "TTKN tỉnh Đồng Tháp", admin_unit_code: "82", active: true } as const;
const DT_SCOPE = { all: false, level: "province", units: ["82", ...dtCommunes.map((c) => c.code)] } as const;

export const MOCK_PERSONAS: readonly MockPersona[] = [
  persona({
    id: "tw",
    title: "Cán bộ TTKN Quốc gia",
    description: "Phạm vi toàn quốc — tổng hợp, chỉ đạo khẩn, báo cáo, thư viện",
    displayName: "Cán bộ TW (giả lập)",
    amr: ["pwd"],
    actor: "mock-tw-0001",
    roles: ["kn_national_admin"],
    org: { ref: NATIONAL_REF, kind: "ttkn_national", name: "Trung tâm Khuyến nông Quốc gia", admin_unit_code: null, active: true },
    scope: { all: true, level: "national", units: [] },
  }),
  persona({
    id: "tinh",
    title: "Cán bộ TTKN tỉnh Đồng Tháp",
    description: "Phạm vi tỉnh Đồng Tháp và các xã trực thuộc — ban hành chỉ đạo, đồng bộ ngay",
    displayName: "Cán bộ tỉnh (giả lập)",
    amr: ["pwd"],
    actor: "mock-tinh-0082",
    roles: ["kn_province_admin", "mrv_reviewer", "mrv_viewer"],
    org: DT_ORG,
    scope: { ...DT_SCOPE, units: [...DT_SCOPE.units] },
  }),
  persona({
    id: "xa",
    title: "Khuyến nông viên xã",
    description: `Phạm vi ${myCommune?.name ?? "một xã"}, tỉnh Đồng Tháp`,
    displayName: "KNV xã (giả lập)",
    amr: ["pwd"],
    actor: "mock-xa-0001",
    roles: ["kn_officer", "mrv_collector"],
    org: {
      ref: communeOrgRef(0, 0),
      kind: "extension_team",
      name: `Tổ khuyến nông cộng đồng ${myCommune?.name ?? ""}`,
      admin_unit_code: myCommune?.code ?? null,
      active: true,
    },
    scope: { all: false, level: "commune", units: myCommune ? [myCommune.code] : [] },
  }),
  persona({
    id: "htx",
    title: "Quản lý HTX",
    description: `coop_manager — ${HTX.api.name}: mở vụ, ghi nhật ký hộ, duyệt nhật ký, sản lượng, tài khoản nông dân`,
    displayName: "Quản lý HTX (giả lập)",
    amr: ["pwd"],
    actor: "mock-htx-0001",
    roles: ["coop_manager"],
    org: { ref: HTX.api.ref, kind: "cooperative", name: HTX.api.name, admin_unit_code: HTX.api.admin_unit_code, active: true },
    scope: { all: false, level: "cooperative", units: HTX.api.admin_unit_code ? [HTX.api.admin_unit_code] : [] },
  }),
  persona({
    id: "cuc",
    title: "Cục Kinh tế hợp tác & PTNT",
    description: "Cổng Bản đồ CGH — chỉ xem, toàn vùng; xuất báo cáo CGH",
    displayName: "Chuyên viên Cục (giả lập)",
    amr: ["pwd"],
    actor: "mock-cuc-0001",
    roles: ["cgh_viewer"],
    org: { ref: DCRD_REF, kind: "dcrd", name: "Cục Kinh tế hợp tác và PTNT", admin_unit_code: null, active: true },
    scope: { all: true, level: "national", units: [] },
  }),
  persona({
    id: "setter",
    title: "Quản trị nền tảng (đã qua MFA)",
    description:
      "platform_admin + đặt/duyệt tham số, soạn SOP, tài khoản, mẫu báo cáo, xung đột đồng bộ, dự báo CGH — thử quy tắc không tự duyệt",
    displayName: "Quản trị viên A (giả lập)",
    amr: ["pwd", "otp"],
    actor: "mock-admin-setter",
    roles: [
      "platform_admin",
      "masterdata_admin",
      "registry_admin",
      "sop_admin",
      "param_setter",
      "param_approver",
      "account_admin",
      "reports_admin",
      "sync_conflict_resolver",
      "device_admin",
      "cgh_admin",
      "geo_admin",
      "waterway_editor",
      "waterway_publisher",
      "simulation_analyst",
      "content_editor_national",
      "metrics_viewer",
    ],
    org: MKG_ORG,
    scope: { all: true, level: "national", units: [] },
  }),
  persona({
    id: "approver",
    title: "Quản trị — người phê duyệt",
    description: "param_approver + phát hành mạng đường thủy (waterway_publisher), đã qua MFA",
    displayName: "Quản trị viên B (giả lập)",
    amr: ["pwd", "otp"],
    actor: "mock-admin-approver",
    roles: ["param_approver", "waterway_publisher"],
    org: MKG_ORG,
    scope: { all: true, level: "national", units: [] },
  }),
  persona({
    id: "account-tinh",
    title: "Quản trị tài khoản tỉnh Đồng Tháp",
    description: "account_admin_province (đã qua MFA) — chỉ cấp vai kn_officer, coop_manager trong tỉnh",
    displayName: "Quản trị tài khoản tỉnh (giả lập)",
    amr: ["pwd", "otp"],
    actor: "mock-acc-0082",
    roles: ["account_admin_province"],
    org: DT_ORG,
    scope: { ...DT_SCOPE, units: [...DT_SCOPE.units] },
  }),
  // Quản trị CHƯA qua MFA: mọi lời gọi Admin API trả 403 mfa_required (thử thông báo).
  persona({
    id: "admin-no-mfa",
    title: "Quản trị — chưa qua MFA",
    description: "platform_admin nhưng đăng nhập chỉ bằng mật khẩu — Admin API từ chối (mfa_required)",
    displayName: "Quản trị viên D (giả lập)",
    amr: ["pwd"],
    actor: "mock-admin-nomfa",
    roles: ["platform_admin", "param_setter", "account_admin"],
    org: MKG_ORG,
    scope: { all: true, level: "national", units: [] },
  }),
];

export function findPersona(id: string | null | undefined): MockPersona | undefined {
  return MOCK_PERSONAS.find((p) => p.id === id);
}

/** Token giả lập: "mock.<persona>" — chỉ chế độ mock hiểu. */
export function mockToken(personaId: string): string {
  return `mock.${personaId}`;
}

export function personaFromAuthHeader(header: string | null): MockPersona | undefined {
  const m = header ? /^Bearer mock\.([\w-]+)$/.exec(header) : null;
  return findPersona(m?.[1]);
}
