"use client";

import { useEffect, useId, useRef, useState } from "react";
import { Icon } from "@/components/Icon";
import { initials } from "@/kn/session";

export interface AccountSwitchOption {
  key: string;
  label: string;
}

export interface AccountMenuProps {
  displayName: string;
  /** Dòng phụ dưới tên: "Cấp Tỉnh · Trồng trọt". */
  subtitle: string;
  /** "Đang dùng": "Cấp Tỉnh · Phòng Trồng trọt". */
  current: string;
  /** Các cặp vai trò/phòng ban khác (rỗng → "Tài khoản chỉ có 1 vai trò, 1 phòng ban"). */
  others: AccountSwitchOption[];
  onSwitch: (key: string) => void | Promise<void>;
  onChangePassword: () => void;
  onLogout: () => void;
  /** Thông báo ngắn sau thao tác (vd chế độ giả lập không đổi mật khẩu). */
  note?: string | null;
}

/** Menu tài khoản trên thanh trên (BRD §4.0 mục 2, §6). */
export function AccountMenu({ displayName, subtitle, current, others, onSwitch, onChangePassword, onLogout, note }: AccountMenuProps) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const wrap = useRef<HTMLDivElement>(null);
  const id = useId();

  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (wrap.current && !wrap.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const doSwitch = async (key: string) => {
    setBusy(key);
    try {
      await onSwitch(key);
      setOpen(false);
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="acct" ref={wrap}>
      <button
        type="button"
        className="acct-btn"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={`${id}-menu`}
        aria-label={`Tài khoản: ${displayName} — ${subtitle}`}
        onClick={() => setOpen((v) => !v)}
        data-testid="account-menu"
      >
        <span className="acct-av" aria-hidden="true">
          {initials(displayName)}
        </span>
        <span className="acct-txt">
          <b>{displayName}</b>
          <span>{subtitle}</span>
        </span>
        <Icon name="chevronDown" size={16} />
      </button>
      {open ? (
        <div className="acct-menu" role="menu" id={`${id}-menu`} aria-label="Menu tài khoản">
          <div className="acct-sec">Đang dùng</div>
          <div className="acct-current" role="presentation">
            {current}
          </div>
          <div className="acct-sec">Đổi vai trò / phòng ban</div>
          {others.length ? (
            others.map((o) => (
              <button key={o.key} type="button" role="menuitem" className="acct-item" disabled={!!busy} onClick={() => void doSwitch(o.key)}>
                {busy === o.key ? "Đang chuyển…" : o.label}
              </button>
            ))
          ) : (
            <div className="acct-note">Tài khoản chỉ có 1 vai trò, 1 phòng ban</div>
          )}
          <div className="acct-sep" role="separator" />
          <button type="button" role="menuitem" className="acct-item" onClick={onChangePassword}>
            Đổi mật khẩu
          </button>
          <button
            type="button"
            role="menuitem"
            className="acct-item acct-danger"
            onClick={() => {
              setOpen(false);
              onLogout();
            }}
          >
            Đăng xuất
          </button>
          {note ? (
            <div className="acct-note" role="status">
              {note}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
