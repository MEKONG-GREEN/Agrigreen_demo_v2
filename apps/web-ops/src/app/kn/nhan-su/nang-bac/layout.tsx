import type { ReactNode } from "react";
import { PromotionSelectionProvider } from "@/kn/hrm/promotion-selection";

export default function Layout({children}:{children:ReactNode}) {
  return <PromotionSelectionProvider>{children}</PromotionSelectionProvider>;
}
