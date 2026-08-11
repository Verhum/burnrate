"use client";

import { useState, useRef, useCallback, useEffect } from "react";

export type RecorderState = "idle" | "recording" | "stopped";

export interface AudioDevice {
  deviceId: string;
  label: string;
}

export function useRecorder() {
  const [state, setState] = useState<RecorderState>("idle");
  const [audioBlob, setAudioBlob] = useState<Blob | null>(null);
  const [duration, setDuration] = useState(0);
  const [devices, setDevices] = useState<AudioDevice[]>([]);
  const [selectedDeviceId, setSelectedDeviceId] = useState("");
  const mediaRecorder = useRef<MediaRecorder | null>(null);
  const chunks = useRef<Blob[]>([]);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const streamRef = useRef<MediaStream | null>(null);

  const refreshDevices = useCallback(async () => {
    try {
      let all = await navigator.mediaDevices.enumerateDevices();
      let mics = all.filter((d) => d.kind === "audioinput");

      // WKWebView returns unlabeled placeholder devices before permission is
      // granted. Request a brief stream to unlock real labels and device IDs.
      if (mics.length > 0 && mics.every((d) => !d.label)) {
        try {
          const probe = await navigator.mediaDevices.getUserMedia({ audio: true });
          probe.getTracks().forEach((t) => t.stop());
          all = await navigator.mediaDevices.enumerateDevices();
          mics = all.filter((d) => d.kind === "audioinput");
        } catch {
          // permission denied — keep the unlabeled list
        }
      }

      setDevices(
        mics.map((d, i) => ({
          deviceId: d.deviceId,
          label: d.label || `Microphone ${i + 1}`,
        }))
      );
    } catch {
      // enumerateDevices not available
    }
  }, []);

  const cleanup = useCallback(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
    if (streamRef.current) {
      streamRef.current.getTracks().forEach((t) => t.stop());
      streamRef.current = null;
    }
    mediaRecorder.current = null;
  }, []);

  useEffect(() => cleanup, [cleanup]);

  const start = useCallback(async (deviceId?: string) => {
    try {
      const audioConstraints: MediaTrackConstraints | boolean =
        deviceId ? { deviceId: { exact: deviceId } } : true;
      const stream = await navigator.mediaDevices.getUserMedia({ audio: audioConstraints });
      streamRef.current = stream;
      chunks.current = [];
      setAudioBlob(null);
      setDuration(0);

      const mimeType = MediaRecorder.isTypeSupported("audio/webm;codecs=opus")
        ? "audio/webm;codecs=opus"
        : "audio/webm";

      const recorder = new MediaRecorder(stream, { mimeType });
      mediaRecorder.current = recorder;

      recorder.ondataavailable = (e) => {
        if (e.data.size > 0) chunks.current.push(e.data);
      };
      recorder.onstop = () => {
        const blob = new Blob(chunks.current, { type: mimeType });
        setAudioBlob(blob);
        setState("stopped");
        cleanup();
      };

      recorder.start(250);
      setState("recording");

      const startTime = Date.now();
      timerRef.current = setInterval(() => {
        setDuration(Math.floor((Date.now() - startTime) / 1000));
      }, 250);

      // Permission granted — refresh to get labels
      refreshDevices();
    } catch {
      setState("idle");
    }
  }, [cleanup, refreshDevices]);

  const stop = useCallback(() => {
    if (mediaRecorder.current?.state === "recording") {
      mediaRecorder.current.stop();
    }
  }, []);

  const reset = useCallback(() => {
    cleanup();
    setState("idle");
    setAudioBlob(null);
    setDuration(0);
    chunks.current = [];
  }, [cleanup]);

  return {
    state, audioBlob, duration, devices, selectedDeviceId,
    setSelectedDeviceId, start, stop, reset, refreshDevices,
  };
}
