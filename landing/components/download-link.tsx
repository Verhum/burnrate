"use client";

import { track } from "@vercel/analytics";
import type { ReactNode } from "react";

interface DownloadLinkProps {
  href: string;
  event: string;
  className?: string;
  children: ReactNode;
}

export function DownloadLink({ href, event, className, children }: DownloadLinkProps) {
  return (
    <a href={href} onClick={() => track(event)} className={className}>
      {children}
    </a>
  );
}
