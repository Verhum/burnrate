"use client";

import { useCallback, useEffect, useState } from "react";
import { useRecorder } from "@/lib/use-recorder";
import { client } from "@/lib/api/client";
import { apiErrorMessage } from "@/lib/api/errors";
import { toast } from "@/lib/toast";
import { MicIcon } from "@/components/tasks/voice-recorder";

interface VoiceDictationProps {
  onTranscript: (text: string) => void;
}

export function VoiceDictation({ onTranscript }: VoiceDictationProps) {
  const { state, audioBlob, start, stop, reset } = useRecorder();
  const [transcribing, setTranscribing] = useState(false);

  useEffect(() => {
    if (!audioBlob) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async transcription: setState after await
    setTranscribing(true);
    client
      .transcribeAudio(audioBlob)
      .then(({ text }) => {
        onTranscript(text);
      })
      .catch((err) => {
        toast.error("Transcription failed", apiErrorMessage(err));
      })
      .finally(() => {
        setTranscribing(false);
        reset();
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [audioBlob]);

  const toggle = useCallback(() => {
    if (state === "recording") {
      stop();
    } else {
      start();
    }
  }, [state, start, stop]);

  return (
    <button
      type="button"
      onClick={toggle}
      disabled={transcribing}
      title={state === "recording" ? "Stop dictation" : transcribing ? "Transcribing..." : "Dictate with voice"}
      className={`p-1.5 rounded cursor-pointer border-none transition-colors ${
        state === "recording"
          ? "text-danger bg-elevated animate-pulse"
          : transcribing
            ? "text-muted bg-elevated cursor-wait"
            : "text-muted bg-transparent hover:text-dim hover:bg-elevated"
      }`}
    >
      {transcribing ? (
        <div className="w-3.5 h-3.5 border border-current border-t-transparent rounded-full animate-spin" />
      ) : (
        <MicIcon size={14} />
      )}
    </button>
  );
}
