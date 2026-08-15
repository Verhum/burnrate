import { create } from "zustand";
import { useRequestStore } from "./request-store";
import { useTaskStore } from "./task-store";

const SERVICE_UUID = "cb0a0001-5b20-4c5a-9b3d-8a7e6f1d2c3b";
const BURNRATE_CHAR_UUID = "cb0a0002-5b20-4c5a-9b3d-8a7e6f1d2c3b";
const MIC_CHAR_UUID = "cb0a0003-5b20-4c5a-9b3d-8a7e6f1d2c3b";

export interface WizardNote {
  id: string;
  text: string;
  createdAt: number;
}

const NOTES_KEY = "wizard-notes";
const WHISPER_URL_KEY = "wizard-whisper-url";
const DEFAULT_WHISPER_URL = "http://localhost:8080/v1/audio/transcriptions";

function loadNotes(): WizardNote[] {
  if (typeof window === "undefined") return [];
  try {
    const s = localStorage.getItem(NOTES_KEY);
    return s ? JSON.parse(s) : [];
  } catch {
    return [];
  }
}

function persistNotes(notes: WizardNote[]) {
  localStorage.setItem(NOTES_KEY, JSON.stringify(notes));
}

export function getWhisperUrl(): string {
  if (typeof window === "undefined") return DEFAULT_WHISPER_URL;
  return localStorage.getItem(WHISPER_URL_KEY) || DEFAULT_WHISPER_URL;
}

export function setWhisperUrl(url: string) {
  localStorage.setItem(WHISPER_URL_KEY, url);
}

const ORB_COLORS_RGB565 = [
  0xf800, 0x07e0, 0x001f, 0xffe0, 0xf81f, 0x07ff, 0xfd20, 0xd01f,
];

export const ORB_COLORS_CSS = [
  "#f00", "#0f0", "#00f", "#ff0", "#f0f", "#0ff", "#f90", "#c0f",
];

type WizardStatus = "disconnected" | "connecting" | "connected" | "error";

interface WizardState {
  status: WizardStatus;
  error: string | null;
  deviceName: string | null;
  lastSyncAt: number | null;
  syncCount: number;
  wandUp: boolean;
  isSyncing: boolean;
  notes: WizardNote[];

  connect: () => Promise<void>;
  disconnect: () => void;
  syncRequests: () => Promise<void>;
  sendCommand: (cmd: string, orbs?: number[]) => Promise<void>;
  raiseWand: () => Promise<void>;
  lowerWand: () => Promise<void>;
  deleteNote: (id: string) => void;
}

let device: BluetoothDevice | null = null;
let burnrateChar: BluetoothRemoteGATTCharacteristic | null = null;
let micChar: BluetoothRemoteGATTCharacteristic | null = null;
let syncInterval: ReturnType<typeof setInterval> | null = null;

// ── BLE audio transfer state machine ──
let xferState: "idle" | "receiving" = "idle";
let xferExpectedSize = 0;
let xferChunks: Uint8Array[] = [];
let xferReceived = 0;
let xferFilename = "";

async function transcribeAndSave(blob: Blob, filename: string) {
  const url = getWhisperUrl();
  try {
    const form = new FormData();
    form.append("file", blob, filename);
    form.append("model", "whisper-1");
    const res = await fetch(url, { method: "POST", body: form });
    if (!res.ok) throw new Error(`Whisper ${res.status}`);
    const data = await res.json();
    const text = data.text?.trim();
    if (!text) return;
    const note: WizardNote = {
      id: crypto.randomUUID(),
      text,
      createdAt: Date.now(),
    };
    const notes = [note, ...useWizardStore.getState().notes];
    persistNotes(notes);
    useWizardStore.setState({ notes });
  } catch (e) {
    console.error("Transcription failed:", e);
    const note: WizardNote = {
      id: crypto.randomUUID(),
      text: `[transcription failed: ${filename}]`,
      createdAt: Date.now(),
    };
    const notes = [note, ...useWizardStore.getState().notes];
    persistNotes(notes);
    useWizardStore.setState({ notes });
  }
}

function handleMicNotification(event: Event) {
  const target = event.target as unknown as BluetoothRemoteGATTCharacteristic;
  const raw = new Uint8Array(target.value!.buffer);

  if (xferState === "idle") {
    const text = new TextDecoder().decode(raw);
    if (text.startsWith("WAV:")) {
      const parts = text.split(":");
      xferFilename = parts[1];
      xferExpectedSize = parseInt(parts[2], 10);
      xferChunks = [];
      xferReceived = 0;
      xferState = "receiving";
      useWizardStore.setState({ isSyncing: true });
    } else if (text === "SYNC_DONE") {
      useWizardStore.setState({ isSyncing: false });
    }
    return;
  }

  // receiving state — collect binary chunks
  xferChunks.push(new Uint8Array(raw));
  xferReceived += raw.length;

  if (xferReceived >= xferExpectedSize) {
    const blob = new Blob(xferChunks as BlobPart[], { type: "audio/wav" });
    xferState = "idle";
    transcribeAndSave(blob, xferFilename);
  }
}

function stopSync() {
  if (syncInterval) {
    clearInterval(syncInterval);
    syncInterval = null;
  }
}

async function writeBLE(payload: string) {
  if (!burnrateChar) return;
  const encoder = new TextEncoder();
  await burnrateChar.writeValue(encoder.encode(payload));
}

function getTaskOrbs(): number[] {
  const tasks = useTaskStore.getState().tasks;
  const active = tasks.filter((t) =>
    ["queued", "running", "resumable", "paused"].includes(t.status)
  );
  return active.slice(0, 8).map((_, i) => ORB_COLORS_RGB565[i % ORB_COLORS_RGB565.length]);
}

export const useWizardStore = create<WizardState>((set, get) => ({
  status: "disconnected",
  error: null,
  deviceName: null,
  lastSyncAt: null,
  syncCount: 0,
  wandUp: false,
  isSyncing: false,
  notes: loadNotes(),

  connect: async () => {
    if (!navigator.bluetooth) {
      set({ status: "error", error: "Web Bluetooth not available" });
      return;
    }

    set({ status: "connecting", error: null });

    try {
      device = await navigator.bluetooth.requestDevice({
        filters: [{ services: [SERVICE_UUID] }],
        optionalServices: [SERVICE_UUID],
      });

      device.addEventListener("gattserverdisconnected", () => {
        stopSync();
        burnrateChar = null;
        micChar = null;
        set({ status: "disconnected", deviceName: null, wandUp: false, isSyncing: false });
      });

      const server = await device.gatt!.connect();
      const service = await server.getPrimaryService(SERVICE_UUID);

      burnrateChar = await service.getCharacteristic(BURNRATE_CHAR_UUID);
      micChar = await service.getCharacteristic(MIC_CHAR_UUID);
      micChar.addEventListener("characteristicvaluechanged", handleMicNotification);
      await micChar.startNotifications();

      set({
        status: "connected",
        deviceName: device.name ?? "wizardboi",
        error: null,
      });

      await get().syncRequests();

      syncInterval = setInterval(() => {
        if (get().status === "connected") get().syncRequests();
      }, 3000);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      if (msg.includes("cancelled") || msg.includes("canceled")) {
        set({ status: "disconnected", error: null });
      } else {
        set({ status: "error", error: msg });
      }
    }
  },

  disconnect: () => {
    stopSync();
    xferState = "idle";
    if (micChar) {
      micChar.removeEventListener("characteristicvaluechanged", handleMicNotification);
    }
    if (device?.gatt?.connected) {
      device.gatt.disconnect();
    }
    device = null;
    burnrateChar = null;
    micChar = null;
    set({ status: "disconnected", deviceName: null, error: null, wandUp: false, isSyncing: false });
  },

  sendCommand: async (cmd, orbs) => {
    const pending = useRequestStore.getState().pending;
    const payload: Record<string, unknown> = {
      req: pending.length,
      running: 0,
      pct: 0,
      requests: pending.slice(0, 8).map((r) => ({
        id: String(r.id),
        text: r.title.slice(0, 120),
      })),
      cmd,
    };
    if (orbs) payload.orbs = orbs;
    try {
      await writeBLE(JSON.stringify(payload));
      set({ lastSyncAt: Date.now(), syncCount: get().syncCount + 1 });
    } catch {
      // transient
    }
  },

  raiseWand: async () => {
    const taskOrbs = getTaskOrbs();
    await get().sendCommand("wand", taskOrbs);
    set({ wandUp: true });
  },

  lowerWand: async () => {
    await get().sendCommand("idle");
    set({ wandUp: false });
  },

  syncRequests: async () => {
    if (!burnrateChar) return;

    const pending = useRequestStore.getState().pending;
    const state = get();

    const payload: Record<string, unknown> = {
      req: pending.length,
      running: 0,
      pct: 0,
      requests: pending.slice(0, 8).map((r) => ({
        id: String(r.id),
        text: r.title.slice(0, 120),
      })),
    };

    if (state.wandUp) {
      payload.cmd = "wand";
      payload.orbs = getTaskOrbs();
    }

    try {
      await writeBLE(JSON.stringify(payload));
      set({ lastSyncAt: Date.now(), syncCount: state.syncCount + 1 });
    } catch {
      // transient
    }
  },

  deleteNote: (id) => {
    const notes = get().notes.filter((n) => n.id !== id);
    persistNotes(notes);
    set({ notes });
  },
}));
