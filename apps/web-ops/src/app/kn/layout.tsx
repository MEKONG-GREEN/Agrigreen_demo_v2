"use client";

import type { ReactNode } from "react";
import { AppShell } from "@/components/AppShell";
import { KhtcOpenBadgeSidebar } from "@/kn/khtc/dashboard/KhtcOpenBadgeSidebar";

// Module-owned composition: the shared shell only accepts a generic component.
export default function Layout({ children }: { children: ReactNode }) {
  return <AppShell SidebarComponent={KhtcOpenBadgeSidebar}>{children}</AppShell>;
}
