"use client";

import { useEffect } from "react";
import { useUsageStore } from "@/stores/usage-store";
import { useNow } from "@/hooks/use-now";
import {
  formatDuration,
  formatLongCountdown,
  formatRelativeTime,
  formatResetDay,
  formatStartTime,
  formatTimestamp,
} from "@/lib/format";
import { Card, CardBody, Badge, Spinner } from "@/components/ui";

function windowStateToBadgeVariant(
  state: string
): "running" | "rate_limited" | "paused" | "default" {
  switch (state) {
    case "open":
    case "active":
      return "running";
    case "rate_limited":
    case "blocked":
      return "rate_limited";
    case "paused":
    case "draining":
      return "paused";
    default:
      return "default";
  }
}

export function StatusPanel() {
  const { status, fetchStatus } = useUsageStore();
  const now = useNow(5_000);

  useEffect(() => {
    fetchStatus();
  }, [fetchStatus]);

  if (!status) {
    return (
      <Card>
        <CardBody className="flex items-center justify-center py-8">
          <Spinner size="md" />
        </CardBody>
      </Card>
    );
  }

  const { batch } = status;
  const sevenDayResetsAt = status.seven_day_resets_at ?? null;
  const sevenDayResetLabel = formatResetDay(sevenDayResetsAt);
  const sevenDayResetIn = sevenDayResetsAt
    ? formatLongCountdown(new Date(sevenDayResetsAt), now)
    : "";

  return (
    <div className="flex flex-col gap-0.5">
      {/* SCHEDULER */}
      <Card>
        <CardBody className="flex flex-col gap-0.5 py-3 px-4">
          <p className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono mb-1">
            Scheduler
          </p>

          {/* Info grid */}
          <div className="flex flex-wrap gap-0.5">
            <div className="flex items-center gap-0.5 mr-3">
              <span className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono">
                State
              </span>
              <Badge variant={windowStateToBadgeVariant(status.window_state)}>
                {status.window_state}
              </Badge>
            </div>

            {/* Absent until the first usage fetch succeeds (fresh install). */}
            {batch && (
              <>
                <div className="flex items-center gap-0.5 mr-3">
                  <span className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono">
                    Slots
                  </span>
                  <span className="text-[10px] font-mono text-dim tabular-nums">
                    {batch.running_count}/{batch.parallel_n}
                  </span>
                </div>

                <div className="flex items-center gap-0.5 mr-3">
                  <span className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono">
                    Launched
                  </span>
                  <span className="text-[10px] font-mono text-dim tabular-nums">
                    {batch.launched_this_window}
                  </span>
                </div>

                <div className="flex items-center gap-0.5 mr-3">
                  <span className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono">
                    Window cost
                  </span>
                  <span className="text-[10px] font-mono text-dim tabular-nums">
                    ${batch.window_cost.toFixed(2)}
                  </span>
                </div>
              </>
            )}

            {status.next_candidate && (
              <div className="flex items-center gap-0.5 mr-3">
                <span className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono">
                  Next
                </span>
                <span className="text-[10px] font-mono text-dim">
                  {status.next_candidate}
                </span>
              </div>
            )}

            {status.resets_at && (
              <div className="flex items-center gap-0.5 mr-3">
                <span className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono">
                  5h resets
                </span>
                <span className="text-[10px] font-mono text-dim">
                  {formatRelativeTime(status.resets_at)}
                </span>
              </div>
            )}

            {sevenDayResetLabel && (
              <div className="flex items-center gap-0.5 mr-3">
                <span className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono">
                  7d resets
                </span>
                <span className="text-[10px] font-mono text-dim">
                  {sevenDayResetLabel} ({sevenDayResetIn})
                </span>
              </div>
            )}
          </div>

          {status.blocked_reason && (
            <div className="text-[10px] font-mono text-amber mt-1">
              blocked: {status.blocked_reason}
            </div>
          )}

          {status.rate_limit_until && (
            <div className="text-[10px] font-mono text-amber mt-1">
              usage API rate limited (attempt {status.rate_limit_attempt}), retry {formatRelativeTime(status.rate_limit_until)}
            </div>
          )}
        </CardBody>
      </Card>

      {/* RUNNING RUNS */}
      {status.running_runs?.length > 0 && (
        <Card>
          <CardBody className="flex flex-col gap-0.5 py-3 px-4">
            <p className="text-[9px] font-bold uppercase tracking-widest text-muted font-mono mb-1">
              Running
            </p>
            <div className="flex flex-wrap gap-0.5">
              {status.running_runs.map((run) => {
                const elapsed = run.started_at
                  ? now - new Date(run.started_at).getTime()
                  : 0;
                return (
                  <div
                    key={run.run_id}
                    className="inline-flex items-center gap-1.5 bg-elevated px-2 py-1 text-[10px] font-mono"
                  >
                    <span className="text-amber font-bold">
                      BR{run.task_id}
                    </span>
                    <span className="text-dim truncate max-w-[120px]">
                      {run.title}
                    </span>
                    <span className="text-muted">
                      att {run.attempt}
                    </span>
                    {elapsed > 0 && (
                      <span className="text-muted tabular-nums">
                        {formatDuration(elapsed)}
                      </span>
                    )}
                    {run.started_at && (
                      <span
                        className="text-dim tabular-nums"
                        title={`started ${formatTimestamp(run.started_at)}`}
                      >
                        @ {formatStartTime(run.started_at)}
                      </span>
                    )}
                  </div>
                );
              })}
            </div>
          </CardBody>
        </Card>
      )}

          </div>
  );
}
