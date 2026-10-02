"use client";

import Link from "next/link";
import { PageHeader } from "@/components/patterns/list";
import { GapNotice } from "@/kn/cycle/screens/DataScreens";

/** Admin › Phòng ban › Thu thập dữ liệu: HTX, máy móc, thửa, vật tư (nạp ban đầu ở màn quản trị hiện có). */
export default function Page() {
  return (
    <>
      <PageHeader title="Thu thập dữ liệu" />
      <GapNotice what="Xem dữ liệu thu thập của mọi xã theo phòng ban" />
      <ul className="list-plain">
        <li>
          <Link href="/quan-tri/nhap-nong-ho">Nạp nông hộ (Excel)</Link>
        </li>
        <li>
          <Link href="/quan-tri/nhap-thua">Nạp thửa ruộng (KML/Shapefile/GeoJSON)</Link>
        </li>
        <li>
          <Link href="/htx">Danh sách HTX (quản trị)</Link>
        </li>
      </ul>
    </>
  );
}
