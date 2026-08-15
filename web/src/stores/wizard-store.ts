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

const ORB_COLORS_RGB565 = [
  0xf800, // red
  0x07e0, // green
  0x001f, // blue
  0xffe0, // yellow
  0xf81f, // magenta
  0x07ff, // cyan
  0xfd20, // orange
  0xd01f, // purple
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
  isRecording: boolean;
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

// eslint-disable-next-line @typescript-eslint/no-explicit-any
let recognition: any = null;
let currentTranscript = "";

function startRecording() {
  const SpeechRecognition =
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (window as any).SpeechRecognition || (window as any).webkitSpeechRecognition;
  if (!SpeechRecognition) {
    console.warn("SpeechRecognition not available");
    return;
  }
  currentTranscript = "";
  recognition = new SpeechRecognition();
  recognition.continuous = true;
  recognition.interimResults = false;
  recognition.lang = "en-US";
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  recognition.onresult = (e: any) => {
    for (let i = e.resultIndex; i < e.results.length; i++) {
      if (e.results[i].isFinal) {
        currentTranscript += e.results[i][0].transcript;
      }
    }
  };
  recognition.onerror = (e: any) => console.warn("Speech error:", e.error); // eslint-disable-line @typescript-eslint/no-explicit-any
  recognition.start();
  useWizardStore.setState({ isRecording: true });
}

function stopRecording() {
  if (recognition) {
    recognition.stop();
    recognition = null;
  }
  useWizardStore.setState({ isRecording: false });
  const text = currentTranscript.trim();
  if (!text) return;
  const note: WizardNote = {
    id: crypto.randomUUID(),
    text,
    createdAt: Date.now(),
  };
  const notes = [note, ...useWizardStore.getState().notes];
  persistNotes(notes);
  useWizardStore.setState({ notes });
}

function handleMicNotification(event: Event) {
  const target = event.target as unknown as BluetoothRemoteGATTCharacteristic;
  const value = new TextDecoder().decode(target.value!);
  if (value === "START") startRecording();
  else if (value === "STOP") stopRecording();
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
  isRecording: false,
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
        set({ status: "disconnected", deviceName: null, wandUp: false });
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
    if (recognition) stopRecording();
    if (micChar) {
      micChar.removeEventListener("characteristicvaluechanged", handleMicNotification);
    }
    if (device?.gatt?.connected) {
      device.gatt.disconnect();
    }
    device = null;
    burnrateChar = null;
    micChar = null;
    set({ status: "disconnected", deviceName: null, error: null, wandUp: false, isRecording: false });
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
