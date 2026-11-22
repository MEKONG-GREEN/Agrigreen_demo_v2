"use client";

import { ProducerListScreen } from "@/kn/cycle/screens/ProducerScreens";

/** TT-09 (#102): Danh sách Tổ hợp tác — hồ sơ KN riêng, bộ lọc Xã/phường, Trạng thái, Tên. */
export default function Page() {
  return <ProducerListScreen kind="coop_group" />;
}