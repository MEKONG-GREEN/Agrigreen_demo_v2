// DỮ LIỆU GIẢ LẬP — người dùng thử cho chế độ NEXT_PUBLIC_API_MOCK=1. Không phải người thật.
import type { Me } from "../pending/party";
import { COMMUNES, PROVINCES } from "./data";

export interface MockPersona {
  id: string;
  title: string;
  description: string;
  me: Me;
  /** amr giả lập: có "otp" khi đã qua MFA. */
  amr: string[];
}

const TENANT = "01927b3e-0000-7000-8000-00000000a001";

const dt = PROVINCES.find((p) => p.code === "82");
const dtCommunes = COMMUNES.filter((c) => c.province_code === "82");
const myCommune = dtCommunes[0];

export const MOCK_PERSONAS: readonly MockPersona[] = [
  {
    id: "tw",
    title: "Cán bộ TTKN Quốc gia",
    description: "Phạm vi toàn quốc — xem tổng hợp, thư viện, nhiệm vụ",
    amr: ["pwd"],
    me: {
      subject: "mock-tw-0001",
      display_name: "Cán bộ TW (giả lập)",
      tenant_id: TENANT,
      roles: [],
      org: { ref: "01927b3e-0000-7000-8000-0000000000a1", kind: "ttkn_national", name: "Trung tâm Khuyến nông Quốc gia", admin_unit_code: null },
      scope: { level: "tw", all: true, units: [], unit_name: "Toàn quốc" },
    },
  },
  {
    id: "tinh",
    title: "Cán bộ TTKN tỉnh Đồng Tháp",
    description: "Phạm vi tỉnh Đồng Tháp và các xã trực thuộc",
    amr: ["pwd"],
    me: {
      subject: "mock-tinh-0082",
      display_name: "Cán bộ tỉnh (giả lập)",
      tenant_id: TENANT,
      roles: [],
      org: { ref: "01927b3e-0000-7000-8000-0000000082a1", kind: "ttkn_province", name: "TTKN tỉnh Đồng Tháp", admin_unit_code: "82" },
      scope: { level: "tinh", all: false, units: ["82", ...dtCommunes.map((c) => c.code)], unit_name: dt?.name ?? "Đồng Tháp" },
    },
  },
  {
    id: "xa",
    title: "Khuyến nông viên xã",
    description: `Phạm vi ${myCommune?.name ?? "một xã"}, tỉnh Đồng Tháp`,
    amr: ["pwd"],
    me: {
      subject: "mock-xa-0001",
      display_name: "KNV xã (giả lập)",
      tenant_id: TENANT,
      roles: [],
      org: {
        ref: "01927b3e-0000-7000-8000-0000000082b1",
        kind: "extension_team",
        name: `Tổ khuyến nông ${myCommune?.name ?? ""}`,
        admin_unit_code: myCommune?.code ?? null,
      },
      scope: { level: "xa", all: false, units: myCommune ? [myCommune.code] : [], unit_name: myCommune?.name ?? null },
    },
  },
  {
    id: "cuc",
    title: "Cục Kinh tế hợp tác & PTNT",
    description: "Cổng Bản đồ CGH — chỉ xem, toàn vùng",
    amr: ["pwd"],
    me: {
      subject: "mock-cuc-0001",
      display_name: "Chuyên viên Cục (giả lập)",
      tenant_id: TENANT,
      roles: [],
      org: { ref: "01927b3e-0000-7000-8000-0000000000c1", kind: "dcrd", name: "Cục Kinh tế hợp tác và PTNT", admin_unit_code: null },
      scope: { level: "cuc", all: true, units: [], unit_name: "Toàn vùng" },
    },
  },
  {
    id: "setter",
    title: "Quản trị — đặt & duyệt tham số",
    description: "platform_admin + param_setter + param_approver, đã qua MFA (thử quy tắc không tự duyệt)",
    amr: ["pwd", "otp"],
    me: {
      subject: "mock-admin-setter",
      display_name: "Quản trị viên A (giả lập)",
      tenant_id: TENANT,
      roles: ["platform_admin", "param_setter", "param_approver"],
      org: { ref: "01927b3e-0000-7000-8000-0000000000d1", kind: "mkg", name: "Mekong Green", admin_unit_code: null },
      scope: { level: "tw", all: true, units: [], unit_name: "Toàn quốc" },
    },
  },
  {
    id: "approver",
    title: "Quản trị — người phê duyệt",
    description: "param_approver, đã qua MFA",
    amr: ["pwd", "otp"],
    me: {
      subject: "mock-admin-approver",
      display_name: "Quản trị viên B (giả lập)",
      tenant_id: TENANT,
      roles: ["param_approver"],
      org: { ref: "01927b3e-0000-7000-8000-0000000000d1", kind: "mkg", name: "Mekong Green", admin_unit_code: null },
      scope: { level: "tw", all: true, units: [], unit_name: "Toàn quốc" },
    },
  },
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
