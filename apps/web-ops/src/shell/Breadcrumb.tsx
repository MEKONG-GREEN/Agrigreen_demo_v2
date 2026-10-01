"use client";

import Link from "next/link";
import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

export interface Crumb {
  label: string;
  href?: string;
}

/** Breadcrumb thanh trên: "Điều hành / Dashboard"; mục cuối đậm (Design Sample). */
export function Breadcrumb({ items }: { items: Crumb[] }) {
  if (!items.length) return null;
  return (
    <nav className="crumb" aria-label="Breadcrumb">
      <ol>
        {items.map((c, i) => {
          const last = i === items.length - 1;
          return (
            <li key={`${i}-${c.label}`}>
              {last ? (
                <b aria-current="page">{c.label}</b>
              ) : c.href ? (
                <Link href={c.href}>{c.label}</Link>
              ) : (
                <span>{c.label}</span>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

// Trang chi tiết thêm mắt xích cuối ("Thu thập dữ liệu / Danh sách HTX / HTX Mỹ Hòa").
interface TailCtx {
  tail: Crumb[];
  setTail: (c: Crumb[]) => void;
}
const BreadcrumbTailContext = createContext<TailCtx>({ tail: [], setTail: () => undefined });

export function BreadcrumbTailProvider({ children }: { children: ReactNode }) {
  const [tail, setTail] = useState<Crumb[]>([]);
  return <BreadcrumbTailContext.Provider value={{ tail, setTail }}>{children}</BreadcrumbTailContext.Provider>;
}

export function useBreadcrumbTailValue(): Crumb[] {
  return useContext(BreadcrumbTailContext).tail;
}

/** Trang gọi để thêm mắt xích; tự gỡ khi rời trang. */
export function useBreadcrumbTail(items: Crumb[]): void {
  const { setTail } = useContext(BreadcrumbTailContext);
  const key = JSON.stringify(items);
  useEffect(() => {
    setTail(JSON.parse(key) as Crumb[]);
    return () => setTail([]);
  }, [key, setTail]);
}
