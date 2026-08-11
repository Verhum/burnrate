"use client";

import type { HumanRequest } from "@/lib/api/types";
import { requestSummary } from "@/lib/human-requests";
import { formatDuration, formatTimestamp } from "@/lib/format";
import { RequestDetail } from "./request-detail";

const KIND_LABEL: Record<string, string> = {
  question: "question",
  demo: "demo",
  capture_approval: "capture",
};

interface PendingRequestRowProps {
  request: HumanRequest;
  displayId: string;
  /** Ticking clock, so the age counts up without each row owning a timer. */
  now: number;
  expanded: boolean;
  onToggle: (requestId: number) => void;
  onOpenTask: (taskId: number) => void;
}

/**
 * One line in the pending-requests banner — BR42 · demo · summary · 3m —
 * that opens in place into the full question and the controls to answer it.
 *
 * Answering here is the point: the banner is the home page, and a request that
 * can only be answered by opening its task is a request the human walks past.
 * Opening the task stays available, as a link inside the expanded panel.
 */
export function PendingRequestRow({
  request,
  displayId,
  now,
  expanded,
  onToggle,
  onOpenTask,
}: PendingRequestRowProps) {
  const created = request.created_at ? new Date(request.created_at).getTime() : 0;
  const age = created > 0 ? formatDuration(Math.max(0, now - created)) : "";

  return (
    <div className="flex flex-col">
      <button
        type="button"
        onClick={() => onToggle(request.id)}
        aria-expanded={expanded}
        className="w-full flex items-center gap-2 px-2 py-1 bg-transparent border-none cursor-pointer
          text-left hover:bg-black/10 transition-colors"
      >
        <span className="text-[9px] text-black/50 font-mono shrink-0 w-2">
          {expanded ? "▾" : "▸"}
        </span>
        <span className="text-[11px] text-black/60 font-mono shrink-0">{displayId}</span>
        <span className="text-[9px] font-bold uppercase tracking-wider text-black/60 shrink-0">
          {KIND_LABEL[request.kind] ?? request.kind}
        </span>
        <span className="text-[11px] text-black truncate min-w-0 flex-1">
          {requestSummary(request)}
        </span>
        {request.live && (
          <span className="text-[9px] font-bold uppercase tracking-wider text-black shrink-0">
            LIVE
          </span>
        )}
        {age && (
          <span
            className="text-[9px] text-black/60 font-mono tabular-nums shrink-0"
            title={`raised ${formatTimestamp(request.created_at)}`}
          >
            {age}
          </span>
        )}
      </button>

      {expanded && (
        // The reply controls are built for the app's dark surfaces, so the
        // panel brings one with it rather than restating every colour. It is
        // `elevated`, not `surface`: the textarea and the demo brief's revival
        // block are `surface`, and would vanish into a panel of the same shade.
        <div className="bg-elevated px-2.5 py-2 mx-1 mb-1">
          <RequestDetail request={request} />
          <div className="flex justify-end mt-1.5">
            <button
              type="button"
              onClick={() => onOpenTask(request.task_id)}
              className="text-[9px] font-bold uppercase tracking-wider text-muted hover:text-primary
                bg-transparent border-none cursor-pointer font-mono px-0"
            >
              Open task {displayId} →
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
