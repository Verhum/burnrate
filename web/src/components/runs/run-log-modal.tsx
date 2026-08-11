"use client";

import { useState } from "react";
import { useRunStore } from "@/stores/run-store";
import { Button, ConfirmDialog, Modal } from "@/components/ui";
import { client } from "@/lib/api/client";
import { apiErrorMessage } from "@/lib/api/errors";
import { copyToClipboard } from "@/lib/clipboard";
import { formatStartTime, isActiveRun } from "@/lib/format";
import { toast } from "@/lib/toast";
import { RunLogViewer } from "./run-log-viewer";
import type { Run } from "@/lib/api/types";

interface RunLogModalProps {
  run: Run | null;
  onClose: () => void;
}

export function RunLogModal({ run, onClose }: RunLogModalProps) {
  const { cancelRun } = useRunStore();
  const [cancelOpen, setCancelOpen] = useState(false);
  const [copyingResume, setCopyingResume] = useState(false);

  const handleCopyResume = async (runId: number) => {
    setCopyingResume(true);
    try {
      const { command } = await client.getRunResume(runId);
      if (!command) {
        toast.info(`Run #${runId} has no session`, "It never reported a session id, so there is nothing to resume.");
        return;
      }
      if (await copyToClipboard(command)) {
        toast.success("Resume command copied", command);
      } else {
        toast.error("Couldn't reach the clipboard", command);
      }
    } catch (err) {
      toast.error(`Couldn't build a resume command for run #${runId}`, apiErrorMessage(err));
    } finally {
      setCopyingResume(false);
    }
  };

  return (
    <>
      <Modal
        open={run != null}
        onClose={onClose}
        title={
          run
            ? run.started_at
              ? `Run #${run.id} Log — started ${formatStartTime(run.started_at)}`
              : `Run #${run.id} Log`
            : "Run Log"
        }
      >
        {run && (
          <div className="flex flex-col gap-3">
            <div className="flex items-center gap-2">
              {isActiveRun(run.status) && (
                <Button
                  variant="primary"
                  size="sm"
                  onClick={() => setCancelOpen(true)}
                >
                  Cancel
                </Button>
              )}
              {run.session_id && (
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={copyingResume}
                  onClick={() => handleCopyResume(run.id)}
                  title={`Copy the shell command that reattaches to session ${run.session_id}`}
                >
                  {copyingResume ? "Copying…" : "Copy resume cmd"}
                </Button>
              )}
              {run.pr_url && (
                <a
                  href={run.pr_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="font-mono text-[9px] font-bold uppercase tracking-wider text-amber hover:text-gold"
                >
                  View PR
                </a>
              )}
            </div>
            <RunLogViewer
              runId={run.id}
              live={isActiveRun(run.status)}
            />
          </div>
        )}
      </Modal>

      <ConfirmDialog
        open={cancelOpen}
        title="Cancel run"
        message={`Kill run #${run?.id} now? The agent stops mid-work; the task stays resumable if it recorded a session.`}
        confirmLabel="Cancel run"
        cancelLabel="Keep running"
        destructive
        onConfirm={async () => {
          setCancelOpen(false);
          if (run) await cancelRun(run.id);
        }}
        onCancel={() => setCancelOpen(false)}
      />
    </>
  );
}
