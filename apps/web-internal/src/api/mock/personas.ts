// DỮ LIỆU GIẢ LẬP — người dùng thử cho chế độ NEXT_PUBLIC_API_MOCK=1. Không phải người thật.
// Claim giống access token của Keycloak (sub = actor_ref UUID, realm_access.roles, amr).

export interface MockPersona {
  id: string;
  title: string;
  description: string;
  displayName: string;
  actor: string;
  roles: string[];
  /** amr giả lập: có "otp" khi đã qua MFA. */
  amr: string[];
}

export const MKG_TENANT = "0192f000-0000-7000-8000-00000000f001";

export const ACTORS = {
  dieuPhoi: "0192f000-0000-7000-8000-00000000a001",
  pheDuyet: "0192f000-0000-7000-8000-00000000a002",
  hub: "0192f000-0000-7000-8000-00000000a003",
  can: "0192f000-0000-7000-8000-00000000a004",
  hienTruong: "0192f000-0000-7000-8000-00000000a005",
  vanTai: "0192f000-0000-7000-8000-00000000a006",
  taiXe: "0192f000-0000-7000-8000-00000000a007",
  kinhDoanh: "0192f000-0000-7000-8000-00000000a008",
  truongKinhDoanh: "0192f000-0000-7000-8000-00000000a009",
  banHang: "0192f000-0000-7000-8000-00000000a00a",
  keToan: "0192f000-0000-7000-8000-00000000a00b",
  keToanTruong: "0192f000-0000-7000-8000-00000000a00c",
  system: "0192f000-0000-7000-8000-00000000a0ff",
} as const;

export const MOCK_PERSONAS: readonly MockPersona[] = [
  {
    id: "dieu-phoi",
    title: "Điều phối vận hành MKG (đã qua MFA)",
    description: "Đề xuất cơ sở, tham số, thiết bị cân, dung sai; nhận hàng, kiểm kê; lập lệnh gom — có cả vai duyệt để thử quy tắc không tự duyệt",
    displayName: "Điều phối viên A (giả lập)",
    actor: ACTORS.dieuPhoi,
    amr: ["pwd", "otp"],
    roles: [
      "hub_operator",
      "hub_approver",
      "scale_supervisor",
      "device_approver",
      "reconciliation_officer",
      "tolerance_approver",
      "dispute_officer",
      "field_supervisor",
      "field_collector",
    ],
  },
  {
    id: "phe-duyet",
    title: "Trưởng bộ phận — người phê duyệt (đã qua MFA)",
    description: "hub_approver, device_approver, tolerance_approver, field_supervisor — duyệt đề xuất của người khác",
    displayName: "Trưởng bộ phận B (giả lập)",
    actor: ACTORS.pheDuyet,
    amr: ["pwd", "otp"],
    roles: ["hub_approver", "device_approver", "tolerance_approver", "field_supervisor", "hub_operator", "reconciliation_officer"],
  },
  {
    id: "hub",
    title: "Nhân viên hub — chưa qua MFA",
    description: "hub_operator + scale_supervisor, đăng nhập chỉ bằng mật khẩu — bề mặt quản trị từ chối (mfa_required)",
    displayName: "Nhân viên hub C (giả lập)",
    actor: ACTORS.hub,
    amr: ["pwd"],
    roles: ["hub_operator", "scale_supervisor"],
  },
  {
    id: "can",
    title: "Giám sát cân (đã qua MFA)",
    description: "scale_supervisor + reconciliation_officer + dispute_officer — phiên cân, phiếu tay, nhập số cân khách, đối soát",
    displayName: "Giám sát cân D (giả lập)",
    actor: ACTORS.can,
    amr: ["pwd", "otp"],
    roles: ["scale_supervisor", "reconciliation_officer", "dispute_officer"],
  },
  {
    id: "hien-truong",
    title: "Tổ gom hiện trường",
    description: "field_collector — ghi số cuộn theo công đoạn, xác nhận HTX; không lập/duyệt lệnh gom",
    displayName: "Tổ trưởng tổ gom E (giả lập)",
    actor: ACTORS.hienTruong,
    amr: ["pwd"],
    roles: ["field_collector"],
  },
  // G4 — vận tải, đơn hàng, giá thành, cầu nối kế toán (docs/dev/g4-conventions.md §3.2)
  {
    id: "van-tai",
    title: "Điều phối vận tải (đã qua MFA)",
    description: "dispatcher + driver — lập/huỷ chuyến, chặng, gán ghe, danh mục ghe, đề xuất tham số cước (kế toán trưởng duyệt)",
    displayName: "Điều phối vận tải F (giả lập)",
    actor: ACTORS.vanTai,
    amr: ["pwd", "otp"],
    roles: ["dispatcher", "driver"],
  },
  {
    id: "tai-xe",
    title: "Tài xế / chủ ghe",
    description: "driver — chỉ xem chuyến và ghi mốc xuất phát / đến nơi",
    displayName: "Tài xế G (giả lập)",
    actor: ACTORS.taiXe,
    amr: ["pwd"],
    roles: ["driver"],
  },
  {
    id: "kinh-doanh",
    title: "Nhân viên kinh doanh — có vai duyệt (đã qua MFA)",
    description: "sales_officer + sales_approver — lập hợp đồng, đơn mua/bán, phiếu nhận, giao nhận; thử quy tắc không tự duyệt",
    displayName: "Nhân viên kinh doanh H (giả lập)",
    actor: ACTORS.kinhDoanh,
    amr: ["pwd", "otp"],
    roles: ["sales_officer", "sales_approver"],
  },
  {
    id: "truong-kinh-doanh",
    title: "Trưởng kinh doanh — người duyệt (đã qua MFA)",
    description: "sales_approver + sales_officer — duyệt hợp đồng, đơn mua, phiếu nhận, giá/chiết khấu của người khác",
    displayName: "Trưởng kinh doanh I (giả lập)",
    actor: ACTORS.truongKinhDoanh,
    amr: ["pwd", "otp"],
    roles: ["sales_approver", "sales_officer"],
  },
  {
    id: "ban-hang",
    title: "Nhân viên bán hàng (không có vai duyệt)",
    description: "sales_officer — lập đơn; đơn vượt ngưỡng giá/chiết khấu cần người có vai duyệt",
    displayName: "Nhân viên bán hàng K (giả lập)",
    actor: ACTORS.banHang,
    amr: ["pwd"],
    roles: ["sales_officer"],
  },
  {
    id: "ke-toan",
    title: "Kế toán giá thành (đã qua MFA)",
    description: "cost_accountant + period_closer + accounting_bridge_operator + chief_accountant — chi phí, quyết toán, đóng kỳ, ánh xạ; thử quy tắc không tự duyệt",
    displayName: "Kế toán giá thành L (giả lập)",
    actor: ACTORS.keToan,
    amr: ["pwd", "otp"],
    roles: ["cost_accountant", "period_closer", "accounting_bridge_operator", "chief_accountant"],
  },
  {
    id: "ke-toan-truong",
    title: "Kế toán trưởng (đã qua MFA)",
    description: "chief_accountant + period_closer + accounting_bridge_operator — duyệt quyết toán, tham số, ánh xạ, bút toán của người khác",
    displayName: "Kế toán trưởng M (giả lập)",
    actor: ACTORS.keToanTruong,
    amr: ["pwd", "otp"],
    roles: ["chief_accountant", "period_closer", "accounting_bridge_operator"],
  },
];

export function findPersona(id: string | null | undefined): MockPersona | undefined {
  return MOCK_PERSONAS.find((p) => p.id === id);
}

export function personaByActor(actor: string): MockPersona | undefined {
  return MOCK_PERSONAS.find((p) => p.actor === actor);
}

/** Token giả lập: "mock.<persona>" — chỉ chế độ mock hiểu. */
export function mockToken(personaId: string): string {
  return `mock.${personaId}`;
}

export function personaFromAuthHeader(header: string | null): MockPersona | undefined {
  const m = header ? /^Bearer mock\.([\w-]+)$/.exec(header) : null;
  return findPersona(m?.[1]);
}
