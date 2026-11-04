"use client";

import { useEffect, useState } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { useAuth } from "@/auth/AuthProvider";
import { createKhtcFormApi, type KhtcFormSheet, KHTC_DEPT } from "../forms/api";
import { INBOX_BASE, parseInboxRoute, type InboxRoute } from "./inbox-routes";
import { InboxDetail } from "./InboxDetail";
import { InboxHistory } from "./InboxHistory";
import { DetailPage } from "@/components/patterns/detail";

const clients = new WeakMap<object, number>();
let nextClient = 0;
export function useInboxAuthorityKey(): string {
  const auth = useAuth();
  let client = clients.get(auth.api);
  if (client === undefined) { client = ++nextClient; clients.set(auth.api, client); }
  return JSON.stringify([auth.status, auth.principal, auth.kn.status, auth.kn.context, client]);
}

export function InboxWorkspace() {
  const path = usePathname();
  const query = useSearchParams();
  const identity = useInboxAuthorityKey();
  const route = parseInboxRoute(path, new URLSearchParams(query.toString()));
  if (!route) return <DetailPage title="Đường dẫn phiếu không hợp lệ" back={{ href: INBOX_BASE, label: "Báo cáo cho KHTC" }}><p role="alert">Kiểm tra mã kỳ, phòng và góc nhìn.</p></DetailPage>;
  // A remounted route has a fresh lifetime even after committed A→B→A navigation.
  return <InboxResource key={`${identity}:${path}:${query}`} route={route} />;
}

function InboxResource({ route }: { route: InboxRoute }) {
  const { api, kn, status, principal } = useAuth();
  const department = route.department || (kn.context?.department_code !== KHTC_DEPT ? kn.context?.department_code : "") || "";
  const [sheet, setSheet] = useState<KhtcFormSheet | null>(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let active = true;
    if (!api || status !== "authenticated" || !principal || !kn.context || kn.status!=="active" || !department) return;
    void createKhtcFormApi(api).get(route.id, department).then(value => {
      if (!active) return;
      if (value.period.id !== route.id || value.department !== department) { setFailed(true); return; }
      setSheet(value);
    }, () => { if (active) setFailed(true); });
    return () => { active = false; };
  }, [api, status, principal, kn.status, kn.context, department, route.id]);
  if (status !== "authenticated" || !principal || !kn.context || kn.status!=="active") return <p role="alert">Chọn ngữ cảnh làm việc để đọc phiếu.</p>;
  if (!department) return <p role="alert">Chọn phòng để đọc phiếu.</p>;
  if (failed) return <p role="alert">Không đọc được phiếu trong ngữ cảnh hiện tại. Kiểm tra quyền, phòng và mã kỳ.</p>;
  if (!sheet) return <p role="status">Đang đọc phiếu…</p>;
  const full = { ...route, department };
  return route.history ? <InboxHistory route={full} sheet={sheet} /> : <InboxDetail route={full} sheet={sheet} />;
}
