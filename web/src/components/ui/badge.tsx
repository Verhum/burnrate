import type { ReactNode } from "react";

type BadgeVariant =
  | "queued" | "running" | "resumable" | "paused"
  | "done" | "succeeded" | "dismissed"
  | "failed" | "errored" | "starting" | "resuming"
  | "rate_limited" | "timed_out" | "abandoned"
  | "backlog" | "pr_created" | "awaiting_human" | "default";

interface BadgeProps {
  variant?: BadgeVariant;
  children: ReactNode;
  className?: string;
  onClick?: (e: React.MouseEvent) => void;
}

function variantClass(v: BadgeVariant): string {
  switch (v) {
    case "running":
    case "starting":
    case "resuming":
      return "bg-elevated text-amber";
    case "queued":
      return "bg-elevated text-dim";
    case "done":
    case "succeeded":
      return "bg-elevated text-sage";
    case "pr_created":
    case "resumable":
      return "bg-amber text-surface";
    case "awaiting_human":
      return "bg-amber text-surface";
    case "paused":
    case "rate_limited":
      return "bg-raised text-dim";
    case "failed":
    case "errored":
    case "timed_out":
    case "abandoned":
      return "bg-elevated text-muted";
    case "backlog":
    case "dismissed":
    default:
      return "bg-elevated text-muted";
  }
}

export function Badge({ variant = "default", children, className = "", onClick }: BadgeProps) {
  return (
    <span
      className={`inline-block px-2 py-0.5 text-[9px] font-bold uppercase tracking-widest ${variantClass(variant)} ${className} ${onClick ? "cursor-pointer hover:brightness-125" : ""}`}
      onClick={onClick}
    >
      {children}
    </span>
  );
}
