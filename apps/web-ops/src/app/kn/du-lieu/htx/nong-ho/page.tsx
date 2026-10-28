"use client";

import { ProducerListScreen } from "@/kn/cycle/screens/ProducerScreens";

/** TT-09 (#102): Danh sách Nông hộ — hồ sơ KN riêng, bộ lọc Xã/phường, Trạng thái, Thuộc HTX/THT. */
export default function Page() {
  return <ProducerListScreen kind="household" />;
}