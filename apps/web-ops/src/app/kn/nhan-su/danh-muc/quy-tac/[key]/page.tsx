import { Suspense } from "react";
import { HrmRuleScreen } from "@/kn/hrm/RuleScreens";
export default function Page() { return <Suspense fallback={<p>Đang tải...</p>}><HrmRuleScreen /></Suspense>; }
