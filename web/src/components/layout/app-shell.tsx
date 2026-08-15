"use client";

import { useState, useEffect, useCallback, useMemo } from "react";
import { Header } from "./header";
import { KeyboardHelp } from "./keyboard-help";
import { TaskList } from "@/components/tasks/task-list";
import { RunList } from "@/components/runs/run-list";
import { UsageDashboard } from "@/components/usage/usage-dashboard";
import { StatusPanel } from "@/components/usage/status-panel";
import { ConfigPanel } from "@/components/settings/config-panel";
import { AccountSelector } from "@/components/settings/account-selector";
import { BurnRateBar } from "@/components/usage/burn-rate-bar";
import { WeeklyBurn } from "@/components/usage/weekly-burn";
import { OnboardingTour } from "@/components/onboarding/onboarding-tour";
import { VoiceRecorder, processVoiceInBackground } from "@/components/tasks/voice-recorder";
import { ConfirmDialog, Toaster } from "@/components/ui";
import { useRecorder } from "@/lib/use-recorder";
import { TOUR_STEPS } from "@/components/onboarding/tour-steps";
import { useUsageStore } from "@/stores/usage-store";
import { useTaskStore } from "@/stores/task-store";
import { useConfigStore } from "@/stores/config-store";
import { useOnboardingStore } from "@/stores/onboarding-store";
import { useKeyboard } from "@/hooks/use-keyboard";
import { usePendingRequests } from "@/hooks/use-pending-requests";
import { apiReady } from "@/lib/api/client";
import { shouldOpenInShell } from "@/lib/external-link";
import { WizardPanel, WizardTabLabel } from "@/components/wizard/wizard-panel";

const TABS = [
  { id: "tasks", label: "TASKS" },
  { id: "runs", label: "RUNS" },
  { id: "usage", label: "USAGE" },
  { id: "config", label: "CONFIG" },
  { id: "wizard", label: "WIZARD" },
] as const;

export function AppShell() {
  const [activeTab, setActiveTab] = useState("tasks");
  const [addRequested, setAddRequested] = useState(0);
  const [helpOpen, setHelpOpen] = useState(false);
  const [focusIndex, setFocusIndex] = useState(-1);
  // The `r` shortcut goes through the same confirmation as the button, so one
  // action doesn't behave differently by input method.
  const [runNowTaskId, setRunNowTaskId] = useState<number | null>(null);
  const [voiceOpen, setVoiceOpen] = useState(false);
  const recorder = useRecorder();
  // Pending human requests: initial load plus a refetch on every window focus,
  // since the SSE hub's buffer is bounded and drops rather than blocks.
  usePendingRequests();

  useEffect(() => {
    if (!recorder.audioBlob) return;
    processVoiceInBackground(recorder.audioBlob);
    recorder.reset();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- close modal after background processing starts
    setVoiceOpen(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [recorder.audioBlob]);

  useEffect(() => {
    const handler = () => setVoiceOpen(true);
    window.addEventListener("burnrate-voice-open", handler);
    return () => window.removeEventListener("burnrate-voice-open", handler);
  }, []);

  const fetchUsage = useUsageStore((s) => s.fetchUsage);
  const fetchStatus = useUsageStore((s) => s.fetchStatus);
  const fetchTasks = useTaskStore((s) => s.fetchTasks);
  const selectTask = useTaskStore((s) => s.selectTask);
  const selectedTask = useTaskStore((s) => s.selectedTask);
  const tasks = useTaskStore((s) => s.tasks);
  const filter = useTaskStore((s) => s.filter);
  const setFilter = useTaskStore((s) => s.setFilter);
  const changeStatus = useTaskStore((s) => s.changeStatus);
  const runNow = useTaskStore((s) => s.runNow);
  const config = useConfigStore((s) => s.config);
  const fetchConfig = useConfigStore((s) => s.fetchConfig);
  const setConfigValue = useConfigStore((s) => s.setConfigValue);
  const tourOpen = useOnboardingStore((s) => s.open);
  const tourStep = useOnboardingStore((s) => s.step);
  const tourReturnTab = useOnboardingStore((s) => s.returnTab);
  const tourNext = useOnboardingStore((s) => s.next);
  const tourBack = useOnboardingStore((s) => s.back);
  const tourGoTo = useOnboardingStore((s) => s.goTo);
  const tourClose = useOnboardingStore((s) => s.close);
  const tourAutoStart = useOnboardingStore((s) => s.autoStart);

  useEffect(() => {
    apiReady.then(() => {
      fetchUsage();
      fetchStatus();
      fetchTasks();
      fetchConfig();
    });
  }, [fetchUsage, fetchStatus, fetchTasks, fetchConfig]);

  // First open on a fresh install: config has no onboarding flag yet.
  useEffect(() => {
    if (!config) return;
    tourAutoStart(config.onboarding_completed === true, activeTab);
    // activeTab is read once, at auto-start time; it must not retrigger this.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [config, tourAutoStart]);

  // Each step can pull the UI to the surface it is describing.
  useEffect(() => {
    if (!tourOpen) return;
    const tab = TOUR_STEPS[tourStep]?.tab;
    if (!tab) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- tour drives the tab
    setActiveTab(tab);
    if (tab === "tasks") selectTask(null);
  }, [tourOpen, tourStep, selectTask]);

  const endTour = useCallback(() => {
    tourClose();
    if (tourReturnTab) setActiveTab(tourReturnTab);
    setConfigValue("onboarding_completed", true).catch(() => {
      // Non-fatal: the tour just offers itself again next launch.
    });
  }, [tourClose, tourReturnTab, setConfigValue]);

  useEffect(() => {
    function handleExternalLink(e: MouseEvent) {
      const anchor = (e.target as HTMLElement).closest("a");
      if (!anchor) return;
      const href = anchor.getAttribute("href");
      if (!shouldOpenInShell(href)) return;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const internals = (window as any).__TAURI_INTERNALS__;
      if (!internals) return;
      e.preventDefault();
      e.stopPropagation();
      internals.invoke("plugin:shell|open", { path: href });
    }
    document.addEventListener("click", handleExternalLink, true);
    return () =>
      document.removeEventListener("click", handleExternalLink, true);
  }, []);

  function handleTabChange(id: string) {
    setActiveTab(id);
    if (id === "tasks") {
      selectTask(null);
      // The filter lives in a module-level store, so it outlives the tab
      // switch. Coming back to Tasks should land on the working queue rather
      // than on whatever archive filter was left selected.
      setFilter("active");
    }
    setFocusIndex(-1);
  }

  const handleAddTask = useCallback(() => {
    setActiveTab("tasks");
    selectTask(null);
    setAddRequested((n) => n + 1);
  }, [selectTask]);

  const filteredTasks = useMemo(() => {
    return tasks.filter((t) => {
      switch (filter) {
        case "active":
          return ["queued", "running", "resumable", "paused"].includes(t.status);
        case "backlog":
          return t.status === "backlog";
        case "completed":
          return t.status === "done";
        case "dismissed":
          return t.status === "dismissed";
        case "failed":
          return t.status === "failed";
        default:
          return true;
      }
    });
  }, [tasks, filter]);

  const kbActions = useMemo(
    () => ({
      onNewTask: () => handleAddTask(),
      onNavigateUp: () => {
        if (activeTab !== "tasks" || selectedTask) return;
        setFocusIndex((i) => Math.max(0, i - 1));
      },
      onNavigateDown: () => {
        if (activeTab !== "tasks" || selectedTask) return;
        setFocusIndex((i) => Math.min(filteredTasks.length - 1, i + 1));
      },
      onOpenSelected: () => {
        if (activeTab !== "tasks" || selectedTask) return;
        const t = filteredTasks[focusIndex];
        if (t) selectTask(t);
      },
      onGoBack: () => {
        if (helpOpen) {
          setHelpOpen(false);
          return;
        }
        if (selectedTask) {
          selectTask(null);
          return;
        }
      },
      onSwitchTab: (tab: string) => handleTabChange(tab),
      onSetFilter: (f: string) => {
        if (activeTab === "tasks") {
          setFilter(f);
          setFocusIndex(-1);
        }
      },
      onSetStatus: (status: string) => {
        if (selectedTask) {
          changeStatus(selectedTask.id, status as import("@/lib/api/types").TaskStatus);
        }
      },
      onRunNow: () => {
        if (selectedTask) {
          setRunNowTaskId(selectedTask.id);
        }
      },
      onToggleHelp: () => setHelpOpen((v) => !v),
      hasSelectedTask: !!selectedTask,
      hasDetailView: !!selectedTask,
      disabled: tourOpen,
    }),
    [
      activeTab,
      selectedTask,
      filteredTasks,
      focusIndex,
      helpOpen,
      handleAddTask,
      selectTask,
      setFilter,
      changeStatus,
      tourOpen,
    ]
  );

  useKeyboard(kbActions);

  const runNowTask =
    runNowTaskId != null ? tasks.find((t) => t.id === runNowTaskId) ?? null : null;

  return (
    <div className="min-h-screen">
      <Header onAddTask={handleAddTask} onVoiceTask={() => setVoiceOpen(true)} isRecording={recorder.state === "recording"} />
      <BurnRateBar />

      <nav className="grid grid-cols-5 mt-0.5" style={{ gap: "2px" }} data-tour="tabs">
        {TABS.map((t) => (
          <button
            key={t.id}
            onClick={() => handleTabChange(t.id)}
            className={`py-2.5 text-[10px] font-bold uppercase tracking-widest text-center
              cursor-pointer border-none font-mono transition-colors overflow-hidden
              ${activeTab === t.id ? "bg-elevated text-primary" : "bg-surface text-dim hover:bg-elevated hover:text-primary"}`}
          >
            {t.id === "wizard" ? <WizardTabLabel active={activeTab === t.id} /> : t.label}
          </button>
        ))}
      </nav>

      <main className="mt-0.5">
        {activeTab === "tasks" && (
          <TaskList addRequested={addRequested} focusIndex={focusIndex} />
        )}
        {activeTab === "runs" && (
          <div data-tour="runs-panel">
            <RunList />
          </div>
        )}
        {activeTab === "usage" && (
          <div className="flex flex-col" style={{ gap: "2px" }} data-tour="usage-panel">
            <UsageDashboard />
            <StatusPanel />
          </div>
        )}
        {activeTab === "config" && (
          <div className="flex flex-col" style={{ gap: "2px" }} data-tour="config-panel">
            <ConfigPanel />
            <AccountSelector />
          </div>
        )}
        {activeTab === "wizard" && <WizardPanel />}
      </main>

      <WeeklyBurn />
      <ConfirmDialog
        open={runNowTask !== null}
        title="Run now"
        message={
          runNowTask
            ? `Launch ${runNowTask.display_id || `BR${runNowTask.id}`} immediately, ahead of the queue?`
            : ""
        }
        confirmLabel="Run now"
        onConfirm={() => {
          if (runNowTask) runNow(runNowTask.id);
          setRunNowTaskId(null);
        }}
        onCancel={() => setRunNowTaskId(null)}
      />
      <VoiceRecorder
        open={voiceOpen}
        onClose={() => setVoiceOpen(false)}
        recorder={recorder}
      />
      <Toaster />
      <KeyboardHelp open={helpOpen} onClose={() => setHelpOpen(false)} />
      <OnboardingTour
        open={tourOpen}
        step={tourStep}
        onNext={tourNext}
        onBack={tourBack}
        onGoTo={tourGoTo}
        onDismiss={endTour}
      />
    </div>
  );
}
