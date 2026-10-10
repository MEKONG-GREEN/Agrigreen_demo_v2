"use client";

import { useState } from "react";
import { Icon } from "@/components/Icon";
import { KN_ROLE_LABEL, type RoleAssignment, type SelectContextRequest } from "@/kn/contract";
import { assignmentSubtitle } from "@/kn/session";

/**
 * "Chọn vai trò và phòng ban" (Hình 3b): hàng thẻ vai trò, rồi lưới thẻ phòng ban của vai trò đã chọn —
 * bấm thẻ phòng ban là vào luôn; phòng ban chưa mở hiện mờ "Chưa mở". Admin: bấm thẻ vai trò là vào (không chọn phòng ban).
 */
export function RoleContextPicker({
  assignments,
  onSelect,
  busy,
}: {
  assignments: RoleAssignment[];
  onSelect: (sel: SelectContextRequest) => void;
  busy?: boolean;
}) {
  const [roleId, setRoleId] = useState(assignments.find((a) => a.role !== "admin")?.assignment_id ?? assignments[0]?.assignment_id ?? "");
  const current = assignments.find((a) => a.assignment_id === roleId);
  const anyEnterable = assignments.some((a) => a.role === "admin" || a.departments.some((d) => d.status === "active"));

  const pickRole = (a: RoleAssignment) => {
    if (a.role === "admin") {
      onSelect({ assignment_id: a.assignment_id, department_code: null });
      return;
    }
    setRoleId(a.assignment_id);
  };

  return (
    <div className="stack" style={{ gap: 10 }}>
      <div className="step-lbl" id="step-role">
        1 · Vai trò
      </div>
      <div className="role-row" role="group" aria-labelledby="step-role">
        {assignments.map((a) => (
          <button
            key={a.assignment_id}
            type="button"
            className="role-card"
            aria-pressed={a.assignment_id === roleId && a.role !== "admin"}
            disabled={busy}
            onClick={() => pickRole(a)}
          >
            <b>{KN_ROLE_LABEL[a.role]}</b>
            <span>{assignmentSubtitle(a)}</span>
          </button>
        ))}
      </div>
      {current && current.role !== "admin" ? (
        <>
          <div className="step-lbl" id="step-dept">
            2 · Phòng ban (của vai trò {KN_ROLE_LABEL[current.role]})
          </div>
          <div className="dept-row" role="group" aria-labelledby="step-dept">
            {current.departments.map((d) => {
              const open = d.status === "active";
              return (
                <button
                  key={d.code}
                  type="button"
                  className="dept-pick"
                  disabled={!open || busy}
                  aria-label={open ? `${d.name} — ${d.read_only ? "Vào xem (chỉ xem)" : "Vào hệ thống"}` : `${d.name} — Chưa mở`}
                  onClick={() => onSelect({ assignment_id: current.assignment_id, department_code: d.code })}
                >
                  <span className="ic" aria-hidden="true">
                    <Icon name="building" size={20} />
                  </span>
                  <b>{d.name}</b>
                  {d.description ? <span className="d">{d.description}</span> : null}
                  {open ? <span className="go">{d.read_only ? "Chỉ xem →" : "Vào hệ thống →"}</span> : <span className="closed">Chưa mở</span>}
                </button>
              );
            })}
          </div>
        </>
      ) : null}
      {!anyEnterable ? (
        <div className="choose-msg" role="alert">
          <b>Tài khoản chưa có vai trò hoặc phòng ban đang áp dụng.</b>
          <p className="mute">Vui lòng liên hệ quản trị viên để được phân quyền.</p>
        </div>
      ) : null}
    </div>
  );
}
