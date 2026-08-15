"use client";

import { useEffect } from "react";
import { useWizardStore, ORB_COLORS_CSS, type WizardNote } from "@/stores/wizard-store";
import { useRequestStore } from "@/stores/request-store";
import { useTaskStore } from "@/stores/task-store";
import { Card, CardBody, CardHeader, Button, Badge, Spinner } from "@/components/ui";

function formatAgo(ts: number | null): string {
  if (!ts) return "never";
  const sec = Math.floor((Date.now() - ts) / 1000);
  if (sec < 5) return "just now";
  if (sec < 60) return `${sec}s ago`;
  return `${Math.floor(sec / 60)}m ago`;
}

function WizardPreview() {
  return (
    <div className="flex items-center justify-center py-6">
      <div
        className="grid"
        style={{
          gridTemplateColumns: "repeat(24, 4px)",
          gridTemplateRows: "repeat(32, 4px)",
          gap: 0,
        }}
      >
        {WIZARD_MINI.flat().map((c, i) => (
          <div
            key={i}
            style={{
              backgroundColor: c === 0 ? "transparent" : PALETTE_CSS[c],
              width: 4,
              height: 4,
            }}
          />
        ))}
      </div>
    </div>
  );
}

const PALETTE_CSS: Record<number, string> = {
  1: "#003048",
  2: "#005454",
  3: "#20bfb8",
  4: "#ffe500",
  5: "#ffb468",
  6: "#cc6430",
  7: "#ffffff",
  8: "#103010",
  9: "#b8b8b0",
  10: "#888870",
  11: "#606048",
  12: "#103838",
  13: "#284848",
  14: "#386058",
  15: "#584838",
};

const WIZARD_MINI: number[][] = [
  [0,0,0,0,0,0,0,0,0,0,0,0,2,0,0,0,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,0,0,2,1,2,0,0,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,0,2,1,1,1,0,0,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,0,2,1,1,1,2,0,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,2,1,1,4,1,1,2,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,2,1,1,4,4,1,1,1,2,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,2,1,1,1,1,1,1,1,1,1,2,0,0,0,0,0,0],
  [0,0,0,0,0,0,3,2,1,1,1,1,1,1,1,1,1,2,3,0,0,0,0,0],
  [0,0,0,0,0,0,2,3,3,3,3,3,3,3,3,3,3,3,2,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,5,5,5,5,5,5,5,5,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,5,7,8,5,5,7,8,5,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,6,5,5,6,6,5,5,6,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,9,9,9,9,9,9,9,9,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,9,9,10,10,9,9,10,10,9,9,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,10,10,9,10,10,9,10,10,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,11,10,10,10,10,11,0,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,0,11,11,11,11,0,0,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,14,12,12,12,12,12,12,14,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,14,12,13,13,12,12,13,13,12,14,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,14,12,13,13,12,12,13,13,12,14,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,14,12,12,13,13,13,13,13,13,12,12,14,0,0,0,0,0,0],
  [0,0,0,0,0,0,14,12,12,13,13,13,13,13,13,12,12,14,0,0,0,0,0,0],
  [0,0,0,0,0,0,14,12,12,12,13,13,13,13,12,12,12,14,0,0,0,0,0,0],
  [0,0,0,0,0,0,14,14,14,14,14,14,14,14,14,14,14,14,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,14,14,14,14,14,14,14,14,14,14,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,0,14,0,0,14,0,0,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,0,14,0,0,14,0,0,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,15,15,0,0,15,15,0,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,15,15,0,0,15,15,0,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],
  [0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],
];

function ConnectionCard() {
  const { status, error, deviceName, connect, disconnect } = useWizardStore();

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <span className="text-[10px] font-bold uppercase tracking-widest text-dim">
            Device
          </span>
          <Badge
            variant={
              status === "connected"
                ? "running"
                : status === "connecting"
                  ? "starting"
                  : status === "error"
                    ? "failed"
                    : "default"
            }
          >
            {status}
          </Badge>
        </div>
      </CardHeader>
      <CardBody className="pt-0">
        {status === "connected" && (
          <div className="space-y-3">
            <div className="text-[11px] text-dim font-mono">
              {deviceName ?? "wizardboi"}
            </div>
            <Button variant="danger" size="sm" onClick={disconnect}>
              Disconnect
            </Button>
          </div>
        )}
        {status === "disconnected" && (
          <div className="space-y-3">
            <div className="text-[11px] text-muted font-mono">
              No wizard device paired
            </div>
            <Button variant="accent" size="sm" onClick={connect}>
              Connect
            </Button>
          </div>
        )}
        {status === "connecting" && (
          <div className="flex items-center gap-2">
            <Spinner size="sm" />
            <span className="text-[11px] text-dim font-mono">Pairing...</span>
          </div>
        )}
        {status === "error" && (
          <div className="space-y-3">
            <div className="text-[11px] text-danger font-mono">{error}</div>
            <Button variant="accent" size="sm" onClick={connect}>
              Retry
            </Button>
          </div>
        )}
      </CardBody>
    </Card>
  );
}

function SyncCard() {
  const { status, lastSyncAt, syncCount, syncRequests } = useWizardStore();
  const pending = useRequestStore((s) => s.pending);

  useEffect(() => {
    if (status !== "connected") return;
    syncRequests();
  }, [status, pending.length, syncRequests]);

  if (status !== "connected") return null;

  return (
    <Card>
      <CardHeader>
        <span className="text-[10px] font-bold uppercase tracking-widest text-dim">
          Live Sync
        </span>
      </CardHeader>
      <CardBody className="pt-0 space-y-4">
        <div className="grid grid-cols-3 gap-4">
          <div>
            <div className="text-[9px] font-bold uppercase tracking-widest text-muted">
              Requests
            </div>
            <div className="text-lg font-mono text-primary">{pending.length}</div>
          </div>
          <div>
            <div className="text-[9px] font-bold uppercase tracking-widest text-muted">
              Syncs
            </div>
            <div className="text-lg font-mono text-dim">{syncCount}</div>
          </div>
          <div>
            <div className="text-[9px] font-bold uppercase tracking-widest text-muted">
              Last
            </div>
            <div className="text-lg font-mono text-dim">
              {formatAgo(lastSyncAt)}
            </div>
          </div>
        </div>

        {pending.length > 0 && (
          <div className="space-y-1">
            <div className="text-[9px] font-bold uppercase tracking-widest text-muted">
              Showing on wizard
            </div>
            {pending.slice(0, 8).map((r) => (
              <div
                key={r.id}
                className="flex items-center gap-2 py-1 px-2 bg-elevated text-[10px] font-mono text-dim"
              >
                <Badge variant="awaiting_human" className="shrink-0">
                  #{r.task_id}
                </Badge>
                <span className="truncate">{r.title}</span>
              </div>
            ))}
          </div>
        )}
      </CardBody>
    </Card>
  );
}

function TestControls() {
  const { status, wandUp, raiseWand, lowerWand, sendCommand } = useWizardStore();
  const tasks = useTaskStore((s) => s.tasks);

  if (status !== "connected") return null;

  const active = tasks.filter((t) =>
    ["queued", "running", "resumable", "paused"].includes(t.status)
  );

  return (
    <Card>
      <CardHeader>
        <span className="text-[10px] font-bold uppercase tracking-widest text-dim">
          Test Controls
        </span>
      </CardHeader>
      <CardBody className="pt-0 space-y-4">
        <div className="flex gap-2">
          <Button
            variant={wandUp ? "done" : "accent"}
            size="sm"
            onClick={wandUp ? lowerWand : raiseWand}
          >
            {wandUp ? "Lower Wand" : "Raise Wand"}
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => sendCommand("idle")}
          >
            Reset
          </Button>
        </div>

        {active.length > 0 && (
          <div className="space-y-2">
            <div className="text-[9px] font-bold uppercase tracking-widest text-muted">
              Task Orbs ({active.length})
            </div>
            <div className="flex flex-wrap gap-2">
              {active.slice(0, 8).map((t, i) => (
                <div
                  key={t.id}
                  className="flex items-center gap-1.5 py-1 px-2 bg-elevated"
                >
                  <span
                    className="inline-block w-3 h-3 rounded-full shrink-0"
                    style={{
                      backgroundColor:
                        ORB_COLORS_CSS[i % ORB_COLORS_CSS.length],
                      boxShadow: `0 0 6px ${ORB_COLORS_CSS[i % ORB_COLORS_CSS.length]}80`,
                    }}
                  />
                  <span className="text-[10px] font-mono text-dim truncate max-w-[120px]">
                    {t.display_id || `BR${t.id}`}
                  </span>
                </div>
              ))}
            </div>
            {wandUp && (
              <div className="text-[10px] text-sage font-mono">
                {active.length} orb{active.length === 1 ? "" : "s"} orbiting wand
              </div>
            )}
          </div>
        )}

        {active.length === 0 && (
          <div className="text-[10px] text-muted font-mono">
            No active tasks — add tasks to see orbs on the wand
          </div>
        )}
      </CardBody>
    </Card>
  );
}

export function WizardTabLabel({ active }: { active: boolean }) {
  const status = useWizardStore((s) => s.status);
  return (
    <span className="inline-flex items-center gap-1.5 justify-center">
      WIZARD
      {status === "connected" && (
        <span
          className={`inline-block w-1.5 h-1.5 rounded-full ${active ? "bg-sage" : "bg-sage/60"}`}
        />
      )}
    </span>
  );
}

function formatTime(ts: number): string {
  const d = new Date(ts);
  const h = d.getHours();
  const m = d.getMinutes().toString().padStart(2, "0");
  const ampm = h >= 12 ? "pm" : "am";
  return `${h % 12 || 12}:${m}${ampm}`;
}

function formatDate(ts: number): string {
  const d = new Date(ts);
  const now = new Date();
  if (d.toDateString() === now.toDateString()) return "today";
  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  if (d.toDateString() === yesterday.toDateString()) return "yesterday";
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

function NotesCard() {
  const { notes, isSyncing, deleteNote } = useWizardStore();

  if (notes.length === 0 && !isSyncing) return null;

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <span className="text-[10px] font-bold uppercase tracking-widest text-dim">
            Notes
          </span>
          {isSyncing && (
            <Badge variant="running">
              <span className="inline-flex items-center gap-1">
                <Spinner size="sm" />
                syncing
              </span>
            </Badge>
          )}
        </div>
      </CardHeader>
      <CardBody className="pt-0 space-y-2">
        {notes.map((n: WizardNote) => (
          <div
            key={n.id}
            className="group flex items-start gap-2 py-1.5 px-2 bg-elevated text-[11px] font-mono text-dim"
          >
            <span className="shrink-0 text-muted">
              {formatDate(n.createdAt)} {formatTime(n.createdAt)}
            </span>
            <span className="flex-1 whitespace-pre-wrap">{n.text}</span>
            <button
              onClick={() => deleteNote(n.id)}
              className="shrink-0 opacity-0 group-hover:opacity-100 text-muted hover:text-danger transition-opacity text-[10px]"
            >
              ×
            </button>
          </div>
        ))}
      </CardBody>
    </Card>
  );
}

export function WizardPanel() {
  const status = useWizardStore((s) => s.status);

  return (
    <div className="flex flex-col" style={{ gap: "2px" }}>
      <ConnectionCard />
      {status === "disconnected" && <WizardPreview />}
      <TestControls />
      <SyncCard />
      <NotesCard />
    </div>
  );
}
