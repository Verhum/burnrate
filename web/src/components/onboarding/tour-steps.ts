export interface TourStep {
  /** Stable id, used as the React key and for the progress dots. */
  id: string;
  title: string;
  /** Paragraphs of body copy. */
  body: string[];
  /** Optional key/value rows rendered under the body. */
  facts?: { label: string; desc: string }[];
  /** Tab to switch to before the step is shown, so the spotlight has a target. */
  tab?: "tasks" | "runs" | "usage" | "config";
  /** `data-tour` value of the element to spotlight. Omit to center the card. */
  anchor?: string;
}

export const TOUR_STEPS: TourStep[] = [
  {
    id: "welcome",
    title: "Welcome to burnrate",
    body: [
      "burnrate keeps an ordered queue of coding tasks and spends your Claude Code subscription on them automatically — launching agents the moment your rate-limit window has room.",
      "This tour takes about a minute. You can replay it any time from the Config tab.",
    ],
    tab: "tasks",
  },
  {
    id: "burn-bar",
    title: "The burn bar",
    body: [
      "This is your 5-hour session window: percent consumed, and a countdown to the next reset. It turns red when the window is spent.",
      "The scheduler watches this number. Tasks launch while there is headroom and stop once the window saturates or is about to close.",
    ],
    anchor: "burn-bar",
  },
  {
    id: "tabs",
    title: "Four tabs",
    body: [
      "Everything lives behind these four tabs — press 1–4 to jump between them.",
    ],
    facts: [
      { label: "Tasks", desc: "the queue you control" },
      { label: "Runs", desc: "agent executions and their logs" },
      { label: "Usage", desc: "burn history, forecast, scheduler state" },
      { label: "Config", desc: "thresholds, accounts, this tutorial" },
    ],
    anchor: "tabs",
  },
  {
    id: "add-task",
    title: "Write a task",
    body: [
      "A task is a title plus a prompt — the same thing you would type into Claude Code, written so an agent can finish it unattended.",
      "Set a repo path and burnrate creates an isolated git worktree for the run. Leave it blank and the agent picks its own working directory (name the repo in the prompt).",
    ],
    tab: "tasks",
    anchor: "add-task",
  },
  {
    id: "queue",
    title: "Order the queue",
    body: [
      "Drag tasks to reorder them — the scheduler launches from the top down, up to your parallel limit.",
      "Queued tasks are eligible to run now; backlog tasks sit out until you promote them. Interrupted tasks come back as resumable and pick up their old session.",
    ],
    tab: "tasks",
    anchor: "task-list",
  },
  {
    id: "runs",
    title: "Watch the agents",
    body: [
      "Every launch is a run: live streamed logs, tool calls, cost, and turn count.",
      "If a run hits the rate limit or times out, burnrate marks it resumable and retries it in the next window instead of losing the work.",
    ],
    tab: "runs",
    anchor: "runs-panel",
  },
  {
    id: "review",
    title: "Review the output",
    body: [
      "A successful run pushes a branch and opens a draft PR, then the task moves to the review queue at the top of the Tasks tab.",
      "Open the task and leave a comment to send follow-up instructions — the next run reads them, on the same branch.",
    ],
    tab: "tasks",
    anchor: "task-list",
  },
  {
    id: "usage",
    title: "Know your burn",
    body: [
      "The Usage tab tracks the 5-hour and 7-day windows, daily spend, and per-model weekly limits.",
      "The status panel below shows what the scheduler is doing right now: what is running, what is blocked, and the ETA for everything queued.",
    ],
    tab: "usage",
    anchor: "usage-panel",
  },
  {
    id: "config",
    title: "Tune it, then let it run",
    body: [
      "Config holds the knobs worth knowing: parallel_n (concurrent agents), util_threshold and sevenday_threshold (when to stop burning), model, and default_repo_path.",
      "Accounts below let you pin burnrate to a specific Claude Code login. And Tutorial replays this tour whenever you want it.",
    ],
    tab: "config",
    anchor: "config-panel",
  },
  {
    id: "done",
    title: "You're set",
    body: [
      "Queue a task, leave burnrate running, and it will spend each window for you.",
      "Press ? at any time for keyboard shortcuts.",
    ],
    tab: "tasks",
  },
];
