"use client";

import { useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useAuth } from "@/auth/AuthProvider";
import { DetailPage } from "@/components/patterns/detail";
import { KhtcPlanScreen } from "../PlanScreen";
import { PLAN_BASE, parsePlanRoute } from "./routes";

// Only in-memory identity keys; never persist the draft or authority in storage.
const clients = new WeakMap<object, number>();
let nextClient = 0;
function clientKey(client: object): number {
  let key = clients.get(client);
  if (key === undefined) { key = ++nextClient; clients.set(client, key); }
  return key;
}
export function PlanWorkspace() {
  const auth = useAuth();
  const path = usePathname();
  const search = useSearchParams();
  const router = useRouter();
  const [currentYear] = useState(() => new Date(Date.now() + 7 * 3600000).getUTCFullYear());
  const route = parsePlanRoute(path, new URLSearchParams(search.toString()), currentYear);
  if (!route) return <DetailPage title="Đường dẫn kế hoạch không hợp lệ" back={{ href: PLAN_BASE, label: "Kế hoạch năm" }}><p role="alert">Kiểm tra năm và phiên bản trong đường dẫn.</p></DetailPage>;
  // Context, principal/MFA or API replacement invalidates every pending form.
  const identity = JSON.stringify([auth.status, auth.principal, auth.kn.context, clientKey(auth.api)]);
  return <KhtcPlanScreen key={identity} route={{ ...route, go: router.push }} />;
}
