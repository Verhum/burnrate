"use client";

import { useEffect, useRef, useState } from "react";
import { useRunStore } from "@/stores/run-store";
import { Toggle, Input } from "@/components/ui";
import { RunEvents } from "./run-events";

interface RunLogViewerProps {
  runId: number;
  live?: boolean;
}

export function RunLogViewer({ runId, live }: RunLogViewerProps) {
  const { activeRunLog, activeRunEvents, fetchEvents, fetchLog, clearActiveLog } =
    useRunStore();
  const [showRaw, setShowRaw] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const scrollRef = useRef<HTMLDivElement>(null);
  const prevLengthRef = useRef(0);

  // Fetch data on mount and when mode changes
  useEffect(() => {
    if (showRaw) {
      fetchLog(runId);
    } else {
      fetchEvents(runId);
    }
  }, [runId, showRaw, fetchLog, fetchEvents]);

  // Live polling
  useEffect(() => {
    if (!live) return;
    const interval = setInterval(() => {
      if (showRaw) {
        fetchLog(runId);
      } else {
        fetchEvents(runId);
      }
    }, 3000);
    return () => clearInterval(interval);
  }, [live, runId, showRaw, fetchLog, fetchEvents]);

  // Auto-scroll when new content arrives
  useEffect(() => {
    const currentLength = showRaw
      ? activeRunLog.length
      : activeRunEvents.length;
    if (currentLength > prevLengthRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
    prevLengthRef.current = currentLength;
  }, [activeRunLog, activeRunEvents, showRaw]);

  // Cleanup on unmount
  useEffect(() => {
    return () => clearActiveLog();
  }, [clearActiveLog]);

  const filteredLog = activeRunLog
    .split("\n")
    .filter((line) => {
      try {
        const parsed = JSON.parse(line);
        if (parsed?.subtype === "init" || (parsed?.type === "system" && parsed?.session_id)) return false;
      } catch {}
      if (/^session=[0-9a-f-]+\s+model=/.test(line)) return false;
      if (searchQuery && !line.toLowerCase().includes(searchQuery.toLowerCase())) return false;
      return true;
    })
    .join("\n");

  // Highlight matching text in raw log
  function renderRawLog(text: string) {
    if (!searchQuery) return text;
    const escaped = searchQuery.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const parts = text.split(new RegExp(`(${escaped})`, "gi"));
    return parts.map((part, i) =>
      part.toLowerCase() === searchQuery.toLowerCase() ? (
        <span
          key={i}
          className="bg-amber/40 text-primary px-0.5"
        >
          {part}
        </span>
      ) : (
        part
      )
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <Toggle
          checked={showRaw}
          onChange={setShowRaw}
          label="Raw log"
        />
        <div className="w-full max-w-[260px]">
          <Input
            placeholder="Search log..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />
        </div>
      </div>

      <div
        ref={scrollRef}
        className="bg-elevated p-3 font-mono text-xs overflow-auto max-h-[480px] min-h-[200px]"
      >
        {showRaw ? (
          <pre className="whitespace-pre-wrap break-all text-primary">
            {filteredLog
              ? renderRawLog(filteredLog)
              : <span className="text-muted">No log output</span>}
          </pre>
        ) : (
          <RunEvents events={activeRunEvents} searchQuery={searchQuery} />
        )}
      </div>
    </div>
  );
}
