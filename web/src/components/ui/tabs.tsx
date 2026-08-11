"use client";

import { createContext, useContext, useState, type ReactNode } from "react";

const TabCtx = createContext<{ active: string; set: (id: string) => void }>({
  active: "",
  set: () => {},
});

export function Tabs({ defaultTab, children }: { defaultTab: string; children: ReactNode }) {
  const [active, set] = useState(defaultTab);
  return <TabCtx.Provider value={{ active, set }}>{children}</TabCtx.Provider>;
}

export function TabList({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <nav className={`grid gap-0.5 ${className}`}>{children}</nav>;
}

export function Tab({ id, label }: { id: string; label: string }) {
  const { active, set } = useContext(TabCtx);
  return (
    <button
      onClick={() => set(id)}
      className={`py-2.5 text-[10px] font-bold uppercase tracking-widest text-center cursor-pointer
        border-none font-mono transition-colors
        ${active === id ? "bg-elevated text-primary" : "bg-surface text-dim hover:bg-elevated hover:text-primary"}`}
    >
      {label}
    </button>
  );
}

export function TabPanel({ id, children }: { id: string; children: ReactNode }) {
  const { active } = useContext(TabCtx);
  if (active !== id) return null;
  return <div className="mt-0.5">{children}</div>;
}
