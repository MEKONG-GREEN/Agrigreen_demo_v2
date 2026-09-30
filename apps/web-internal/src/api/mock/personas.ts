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
