// Ngữ cảnh phiên đang dùng (P1 context_id) — giữ trong bộ nhớ để middleware gắn header X-AG-Context.
// sessionStorage chỉ nhớ LỰA CHỌN (assignment_id + phòng ban, không phải bí mật) để sau khi tải lại trang
// client chọn lại cùng vai trò/phòng ban mà không bắt người dùng chọn lại.
import type { SelectContextRequest } from "./contract";

let contextId: string | null = null;

export const contextStore = {
  get: (): string | null => contextId,
  set: (id: string | null): void => {
    contextId = id;
  },
};

const SELECTION_KEY = "ag.kn.selection";

export function readSelection(): SelectContextRequest | null {
  try {
    const raw = window.sessionStorage.getItem(SELECTION_KEY);
    if (!raw) return null;
    const v = JSON.parse(raw) as Partial<SelectContextRequest>;
    return typeof v.assignment_id === "string" ? { assignment_id: v.assignment_id, department_code: v.department_code ?? null } : null;
  } catch {
    return null;
  }
}

export function writeSelection(sel: SelectContextRequest | null): void {
  try {
    if (sel) window.sessionStorage.setItem(SELECTION_KEY, JSON.stringify(sel));
    else window.sessionStorage.removeItem(SELECTION_KEY);
  } catch {
    // bỏ qua: chỉ mất khả năng nhớ lựa chọn khi tải lại
  }
}
