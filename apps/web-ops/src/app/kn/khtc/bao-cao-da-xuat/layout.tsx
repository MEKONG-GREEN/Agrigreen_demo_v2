import type { ReactNode } from "react";
import { ArchiveSelectionBoundary } from "@/kn/khtc/reports/archive-selection";
export default function Layout({children}: {children: ReactNode}) { return <ArchiveSelectionBoundary>{children}</ArchiveSelectionBoundary>; }
