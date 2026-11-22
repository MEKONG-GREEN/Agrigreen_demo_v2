import { Suspense, type ReactNode } from "react";
import { PlanWorkspace } from "@/kn/khtc/plans/PlanWorkspace";
export default function Layout({ children }: { children: ReactNode }) { return <><Suspense fallback={<p role="status">Đang tải kế hoạch…</p>}><PlanWorkspace /></Suspense>{children}</>; }
