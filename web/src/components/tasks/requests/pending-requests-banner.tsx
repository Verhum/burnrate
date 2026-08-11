"use client";

import { useMemo, useState } from "react";
import { useRequestStore } from "@/stores/request-store";
import { useTaskStore } from "@/stores/task-store";
import { sortRequests } from "@/lib/human-requests";
import { useNow } from "@/hooks/use-now";
import { PendingRequestRow } from "./pending-request-row";

/**
 * Which row is open, expressed as the human's last choice rather than as an
 * index: the queue is server state and rows leave it the moment they are
 * answered.
 *
 * `auto` — nobody has clicked; the head of the queue opens on arrival, so the
 * common case (one request) is answerable with no click at all.
 * `open`/`collapsed` — an explicit choice, and only while that request is still
 * pending. Once it leaves, the head of the queue takes over again, which is
 * also how a collapse stops applying to the next interruption.
 */
type Disclosure =
  | { mode: "auto" }
  | { mode: "open"; id: number }
  | { mode: "collapsed"; id: number };

/**
 * Global surface for pending human requests on the tasks tab. Without it a
 * parked task is only discoverable by opening it, which is exactly the case
 * where an agent is already blocked.
 *
 * Rows answer in place (see PendingRequestRow); `onSelect` is the secondary
 * path into the task, not the way to respond.
 */
export function PendingRequestsBanner({ onSelect }: { onSelect: (taskId: number) => void }) {
  const pending = useRequestStore((s) => s.pending);
  const tasks = useTaskStore((s) => s.tasks);
  const now = useNow(30_000);
  const [disclosure, setDisclosure] = useState<Disclosure>({ mode: "auto" });

  const requests = useMemo(() => sortRequests(pending), [pending]);
  const displayIds = useMemo(() => {
    const map = new Map<number, string>();
    for (const t of tasks) map.set(t.id, t.display_id || `BR${t.id}`);
    return map;
  }, [tasks]);

  const topId = requests[0]?.id ?? null;
  const stillPending = disclosure.mode !== "auto" && requests.some((r) => r.id === disclosure.id);
  let expandedId: number | null = topId;
  if (disclosure.mode === "open" && stillPending) expandedId = disclosure.id;
  if (disclosure.mode === "collapsed" && stillPending) expandedId = null;

  if (requests.length === 0) return null;

  return (
    <div className="bg-amber">
      <div className="px-3 py-1.5">
        <span className="text-[9px] font-bold uppercase tracking-widest text-black/60">
          Needs You ({requests.length})
        </span>
      </div>
      <div className="px-1 pb-1 flex flex-col">
        {requests.map((req) => (
          <PendingRequestRow
            key={req.id}
            request={req}
            displayId={displayIds.get(req.task_id) ?? `BR${req.task_id}`}
            now={now}
            expanded={expandedId === req.id}
            onToggle={(id) =>
              setDisclosure({ mode: expandedId === id ? "collapsed" : "open", id })
            }
            onOpenTask={onSelect}
          />
        ))}
      </div>
    </div>
  );
}
