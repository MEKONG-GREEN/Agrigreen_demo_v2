"use client";

import { HouseholdAddRoute } from "@/kn/cycle/screens/ProducerScreens";

/** #102: Thêm nông hộ thuộc Tổ hợp tác trên trang riêng. */
export default function Page() {
  return <HouseholdAddRoute parentKind="coop_group" />;
}