"use client";

import { useState, useCallback, useEffect, useRef } from "react";
import { Modal, Button, Spinner, Select } from "@/components/ui";
import type { useRecorder } from "@/lib/use-recorder";
import { client } from "@/lib/api/client";
import { apiErrorMessage } from "@/lib/api/errors";
import { toast } from "@/lib/toast";
import { useToastStore } from "@/stores/toast-store";
import { useTaskStore } from "@/stores/task-store";

type Phase = "loading" | "needs_install" | "installing" | "ready" | "recording" | "error";

interface VoiceRecorderProps {
  open: boolean;
  onClose: () => void;
  recorder: ReturnType<typeof useRecorder>;
}

function formatDuration(secs: number): string {
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return `${m}:${s.toString().padStart(2, "0")}`;
}

export function processVoiceInBackground(blob: Blob) {
  let toastId = toast.info("Transcribing voice recording...");

  client
    .transcribeAudio(blob)
    .then(({ text }) => {
      useToastStore.getState().dismiss(toastId);
      toastId = toast.info("Creating task from transcript...");
      return client.createVoiceTask(text);
    })
    .then((task) => {
      useToastStore.getState().dismiss(toastId);
      toast.success(`Created ${task.display_id || "BR" + task.id}: ${task.title}`);
      useTaskStore.getState().fetchTasks();
    })
    .catch((err) => {
      useToastStore.getState().dismiss(toastId);
      toast.error("Voice task failed", apiErrorMessage(err));
    });
}

export function VoiceRecorder({ open, onClose, recorder }: VoiceRecorderProps) {
  const {
    state: recState, duration, devices, selectedDeviceId,
    setSelectedDeviceId, start, stop, refreshDevices,
  } = recorder;
  const [phase, setPhase] = useState<Phase>("loading");
  const [error, setError] = useState("");
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const stopPolling = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  const checkStatus = useCallback(async () => {
    try {
      const status = await client.voiceStatus();
      switch (status.state) {
        case "ready":
          stopPolling();
          setPhase("ready");
          break;
        case "installing":
        case "checking":
          setPhase("installing");
          break;
        case "unavailable":
        case "unknown":
          setPhase("needs_install");
          break;
        case "error":
          stopPolling();
          setError(status.message || "Installation failed");
          setPhase("error");
          break;
      }
    } catch (err) {
      setError(apiErrorMessage(err));
      setPhase("error");
    }
  }, [stopPolling]);

  useEffect(() => {
    if (open) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- reset form state on open
      setError("");
      if (recState === "recording") {
        setPhase("recording");
      } else {
        checkStatus();
      }
      refreshDevices();
    } else {
      stopPolling();
    }
  }, [open, recState, checkStatus, stopPolling, refreshDevices]);

  useEffect(() => {
    return stopPolling;
  }, [stopPolling]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- sync derived state
    if (recState === "recording") setPhase("recording");
  }, [recState]);

  const handleInstall = useCallback(async () => {
    setPhase("installing");
    setError("");
    try {
      await client.voiceInstall();
      pollRef.current = setInterval(checkStatus, 2000);
    } catch (err) {
      setError(apiErrorMessage(err));
      setPhase("error");
    }
  }, [checkStatus]);

  const handleRecord = useCallback(() => {
    start(selectedDeviceId || undefined);
  }, [start, selectedDeviceId]);

  const handleStop = useCallback(() => {
    stop();
  }, [stop]);

  const handleClose = () => {
    stopPolling();
    // If recording, just close the modal — recording continues in the background
    onClose();
  };

  return (
    <Modal open={open} onClose={handleClose} title="Voice Task" size="sm">
      <div className="flex flex-col items-center gap-4 py-4">

        {phase === "loading" && <Spinner />}

        {phase === "needs_install" && (
          <>
            <div className="w-16 h-16 rounded-full bg-elevated flex items-center justify-center">
              <DownloadIcon size={32} />
            </div>
            <p className="text-[11px] text-muted text-center">
              Voice input requires the Whisper model (~150 MB download). This is a one-time setup.
            </p>
            <Button variant="primary" onClick={handleInstall}>
              Download Model
            </Button>
          </>
        )}

        {phase === "installing" && (
          <>
            <Spinner />
            <p className="text-[11px] text-dim">Downloading Whisper model...</p>
            <p className="text-[9px] text-muted">This may take a minute on first install</p>
          </>
        )}

        {phase === "ready" && (
          <>
            <div className="w-16 h-16 rounded-full bg-elevated flex items-center justify-center">
              <MicIcon size={32} />
            </div>
            <p className="text-[11px] text-muted text-center">
              Describe your task by voice. Whisper will transcribe it and Claude will structure it into a task.
            </p>
            {devices.length > 0 && (
              <Select
                label="Microphone"
                options={devices.map((d) => ({ value: d.deviceId, label: d.label }))}
                value={selectedDeviceId || devices[0]?.deviceId || ""}
                onChange={(e) => setSelectedDeviceId(e.target.value)}
              />
            )}
            <Button variant="primary" onClick={handleRecord}>
              Start Recording
            </Button>
          </>
        )}

        {phase === "recording" && (
          <>
            <WaveformBars />
            <p className="text-[15px] font-mono text-primary tabular-nums">
              {formatDuration(duration)}
            </p>
            <p className="text-[10px] text-muted">Listening...</p>
            {devices.length > 0 && (
              <div className="w-full opacity-70">
                <Select
                  label="Microphone"
                  options={devices.map((d) => ({ value: d.deviceId, label: d.label }))}
                  value={selectedDeviceId || devices[0]?.deviceId || ""}
                  onChange={(e) => setSelectedDeviceId(e.target.value)}
                  disabled
                />
              </div>
            )}
            <Button variant="primary" onClick={handleStop}>
              Stop Recording
            </Button>
          </>
        )}

        {phase === "error" && (
          <>
            <p className="text-[11px] text-danger">{error}</p>
            <div className="flex gap-2">
              <Button variant="ghost" onClick={handleClose}>Close</Button>
              <Button variant="primary" onClick={() => { checkStatus(); setError(""); }}>
                Try Again
              </Button>
            </div>
          </>
        )}
      </div>
    </Modal>
  );
}

const BAR_DELAYS = [0, 0.15, 0.3, 0.12, 0.25];

function WaveformBars() {
  return (
    <div className="flex items-center justify-center gap-[3px]" style={{ height: 48 }}>
      {BAR_DELAYS.map((delay, i) => (
        <div
          key={i}
          className="bg-danger"
          style={{
            width: 3,
            height: 8,
            animation: `voice-wave 1.2s ease-in-out ${delay}s infinite`,
          }}
        />
      ))}
    </div>
  );
}

export function MicIcon({ size = 24, className = "" }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden>
      <rect x="9" y="2" width="6" height="12" rx="3" />
      <path d="M5 11a1 1 0 012 0 5 5 0 0010 0 1 1 0 012 0 7 7 0 01-6 6.93V21h3v2H8v-2h3v-3.07A7 7 0 015 11z" />
    </svg>
  );
}

function DownloadIcon({ size = 24, className = "" }: { size?: number; className?: string }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} aria-hidden>
      <path d="M21 15v4a2 2 0 01-2 2H5a2 2 0 01-2-2v-4" />
      <polyline points="7 10 12 15 17 10" />
      <line x1="12" y1="15" x2="12" y2="3" />
    </svg>
  );
}
