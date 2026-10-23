"use client";

import Link from "next/link";
import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { Icon } from "@/components/Icon";
import type { MenuBadges, MenuNode, MenuTree } from "@/kn/contract";
import { BrandMark } from "./BrandMark";
import { BRAND, footerText } from "./brand";
import { badgeOf, findActive } from "./menu";

export interface SidebarProps {
  menu: MenuTree;
  pathname: string;
  badges?: MenuBadges | null;
  /** Thu gọn: chỉ icon (72 px); rê chuột/bấm mục cha mở flyout trắng chứa menu con. */
  collapsed?: boolean;
  onToggleCollapse?: () => void;
  /** Gọi khi bấm một liên kết (đóng ngăn kéo trên màn hẹp). */
  onNavigate?: () => void;
}

function Badge({ n }: { n: number }) {
  if (n <= 0) return null;
  return (
    <span className="nav-badge" aria-label={`${n} việc cần xử lý`}>
      {n}
    </span>
  );
}

function Leaf({
  node,
  active,
  badges,
  collapsed,
  onNavigate,
  sub,
}: {
  node: MenuNode;
  active: boolean;
  badges?: MenuBadges | null;
  collapsed: boolean;
  onNavigate?: () => void;
  sub?: boolean;
}) {
  const n = badgeOf(node, badges ?? null);
  if (node.status === "not_open" || node.status === "coming_soon" || !node.href) {
    // kn.menu/v3: "Sắp có" = mục của module chưa có; "Chưa mở" = phòng ban chưa mở.
    const tag = node.status === "coming_soon" ? "Sắp có" : node.status === "not_open" ? "Chưa mở" : null;
    return (
      <span className={`nav-item is-disabled${sub ? " nav-sub-item" : ""}`} aria-disabled="true" title={collapsed && tag ? `${node.label} — ${tag}` : undefined}>
        <Icon name={node.icon} />
        <span className="nav-text">{node.label}</span>
        {tag ? <span className="nav-tag">{tag}</span> : null}
      </span>
    );
  }
  return (
    <Link
      href={node.href}
      className={`nav-item${sub ? " nav-sub-item" : ""}`}
      aria-current={active ? "page" : undefined}
      onClick={onNavigate}
      title={collapsed ? node.label : undefined}
    >
      <Icon name={node.icon} />
      <span className="nav-text">{node.label}</span>
      <Badge n={n} />
    </Link>
  );
}

/** Sidebar theo Design Sample: nhóm menu, mục cha mở/đóng, một mục active, số đếm chỉ cho việc cần xử lý, footer. */
export function Sidebar({ menu, pathname, badges, collapsed = false, onToggleCollapse, onNavigate }: SidebarProps) {
  const base = useId();
  const active = findActive(menu, pathname);
  const activeHref = active?.node.href;
  // Mọi mục cha của mục active (menu có thể 3 cấp) tự mở và đậm chữ.
  const activeAncestors = new Set((active?.ancestors ?? []).map((n) => n.key));
  // Người dùng mở/đóng tay, ghi kèm trang lúc thao tác: đổi sang trang khác thì mục cha của mục active
  // tự mở lại ("mục cha tự mở khi con active"), các mục khác giữ trạng thái tay.
  const [manual, setManual] = useState<Record<string, { open: boolean; at: string | undefined }>>({});
  const [flyout, setFlyout] = useState<string | null>(null);
  const asideRef = useRef<HTMLElement>(null);
  const isOpen = (key: string): boolean => {
    const m = manual[key];
    if (m && m.at === activeHref) return m.open;
    if (activeAncestors.has(key)) return true;
    return m?.open ?? false;
  };

  // Lá trong flyout (menu thu gọn): mục cấp sâu hơn hiện kèm tiêu đề mục cha của nó.
  const flyoutItems = (items: readonly MenuNode[], close: () => void): ReactNode =>
    items.map((c) =>
      c.children?.length ? (
        <li key={c.key}>
          <div className="flyout-sub-title">{c.label}</div>
          <ul>{flyoutItems(c.children, close)}</ul>
        </li>
      ) : (
        <li key={c.key}>
          <Leaf
            node={c}
            active={c.href === activeHref}
            badges={badges}
            collapsed={false}
            onNavigate={() => {
              close();
              onNavigate?.();
            }}
          />
        </li>
      ),
    );

  // Mục có con, mọi cấp (khi mở rộng). Mục cấp 1 khi thu gọn dùng flyout.
  const branch = (item: MenuNode, depth: number): ReactNode => {
    const expanded = isOpen(item.key);
    const hasActive = activeAncestors.has(item.key);
    const subId = `${base}-sub-${item.key}`;
    const fly = collapsed && depth === 0;
    const showFly = fly && flyout === item.key;
    const disabled = item.status === "not_open" || item.status === "coming_soon";
    return (
      <li
        key={item.key}
        className={`nav-parent${hasActive ? " has-active" : ""}`}
        onMouseEnter={fly && !disabled ? () => setFlyout(item.key) : undefined}
        onMouseLeave={fly ? () => setFlyout((f) => (f === item.key ? null : f)) : undefined}
      >
        <button
          type="button"
          className={`nav-item${disabled ? " is-disabled" : ""}${depth > 0 ? " nav-sub-item" : ""}`}
          aria-expanded={fly ? showFly : expanded}
          aria-controls={subId}
          onClick={() => toggle(item.key, fly)}
          title={fly ? item.label : undefined}
          disabled={disabled}
        >
          <Icon name={item.icon} />
          <span className="nav-text">{item.label}</span>
          <span className="nav-chev" aria-hidden="true">
            <Icon name={fly ? "chevronRight" : expanded ? "chevronDown" : "chevronRight"} size={16} />
          </span>
        </button>
        {fly ? (
          <div id={subId} className="flyout" role="group" aria-label={item.label} hidden={!showFly}>
            <div className="flyout-title">{item.label}</div>
            <ul>{flyoutItems(item.children ?? [], () => setFlyout(null))}</ul>
          </div>
        ) : (
          <ul id={subId} className="nav-sub" hidden={!expanded}>
            {(item.children ?? []).map((c) =>
              c.children?.length ? (
                branch(c, depth + 1)
              ) : (
                <li key={c.key}>
                  <Leaf node={c} active={c.href === activeHref} badges={badges} collapsed={false} onNavigate={onNavigate} sub />
                </li>
              ),
            )}
          </ul>
        )}
      </li>
    );
  };

  useEffect(() => {
    if (!flyout) return;
    const onDoc = (e: MouseEvent) => {
      if (asideRef.current && !asideRef.current.contains(e.target as Node)) setFlyout(null);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [flyout]);

  const toggle = (key: string, fly: boolean) => {
    if (fly) {
      setFlyout((f) => (f === key ? null : key));
      return;
    }
    setManual((m) => ({ ...m, [key]: { open: !isOpen(key), at: activeHref } }));
  };

  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape" && flyout) {
      setFlyout(null);
    }
  };

  return (
    <aside ref={asideRef} className="side" id="side-nav" aria-label="Điều hướng chính" data-collapsed={collapsed ? "true" : undefined} onKeyDown={onKey}>
      <div className="side-brand">
        <BrandMark />
        <b className="side-brand-name">{BRAND.productName}</b>
      </div>
      <nav className="nav" aria-label="Menu chức năng">
        {menu.groups.map((g, gi) => (
          <div className="nav-grp" key={g.key} role="group" aria-labelledby={`${base}-g${gi}`}>
            <div className="nav-lbl" id={`${base}-g${gi}`}>
              {g.label}
            </div>
            <ul className="nav-list">
              {g.items.map((item) =>
                item.children?.length ? (
                  branch(item, 0)
                ) : (
                  <li key={item.key}>
                    <Leaf node={item} active={item.href === activeHref} badges={badges} collapsed={collapsed} onNavigate={onNavigate} />
                  </li>
                ),
              )}
            </ul>
          </div>
        ))}
      </nav>
      <div className="side-foot">
        {onToggleCollapse ? (
          <button type="button" className="side-collapse" onClick={onToggleCollapse} aria-label={collapsed ? "Mở rộng menu" : "Thu gọn menu"} aria-pressed={collapsed}>
            <Icon name="panelLeft" />
            <span className="nav-text">{collapsed ? "Mở rộng menu" : "Thu gọn menu"}</span>
          </button>
        ) : null}
        <div className="side-footer" title={collapsed ? `${BRAND.productName} · ${footerText()}` : undefined}>
          <BrandMark size={28} />
          <div className="nav-text">
            <b>{BRAND.productName}</b>
            <span>{footerText()}</span>
          </div>
        </div>
      </div>
    </aside>
  );
}
