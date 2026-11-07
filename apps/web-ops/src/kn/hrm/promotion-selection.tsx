"use client";

// FN09 selection lives only in this route's mounted layout, scoped to its authority.
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { contextStore } from "@/kn/contextStore";

interface Selection {
  keys: string[];
  select: (keys: string[]) => void;
  clear: () => void;
  isCurrent: () => boolean;
}
const SelectionContext = createContext<Selection | null>(null);

function SelectionState({ contextId, children }: { contextId: string; children: ReactNode }) {
  const [keys, setKeys] = useState<string[]>([]);
  const clear = useCallback(() => setKeys([]), []);
  const isCurrent = useCallback(() => !!contextId && contextStore.get() === contextId, [contextId]);
  const select = useCallback((next: string[]) => setKeys(isCurrent() ? [...next] : []), [isCurrent]);
  const value = useMemo(() => ({ keys, select, clear, isCurrent }), [keys, select, clear, isCurrent]);
  return <SelectionContext.Provider value={value}>{children}</SelectionContext.Provider>;
}

export function PromotionSelectionProvider({ children }: { children: ReactNode }) {
  const { principal, kn } = useAuth();
  const contextId = kn.context?.context_id ?? "";
  const authority = JSON.stringify([principal?.tenantId, principal?.subject, contextId, kn.context?.read_only, kn.context?.support_session_id, kn.context?.on_behalf_of]);
  return <SelectionState key={authority} contextId={contextId}>{children}</SelectionState>;
}

export function usePromotionSelection(): Selection {
  const value = useContext(SelectionContext);
  if (!value) throw new Error("FN09 selection requires its route layout");
  return value;
}
