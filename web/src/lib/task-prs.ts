import type { Task, TaskPR } from "@/lib/api/types";

// A task can span several repos, each with its own PR. Older tasks (and older
// servers) only carry latest_run_pr_url, so fall back to it when the per-repo
// list is absent.
export function taskPRs(task: Task): TaskPR[] {
  const prs = (task.prs ?? []).filter((p) => p.pr_url);
  if (prs.length > 0) return prs;
  if (task.latest_run_pr_url) {
    return [
      {
        id: 0,
        task_id: task.id,
        run_id: 0,
        repo: "",
        branch: "",
        pr_url: task.latest_run_pr_url,
        worked_in: "",
        created_at: "",
        lines_added: 0,
        lines_removed: 0,
        pr_state: "",
        pr_is_draft: false,
        pr_checked_at: "",
      },
    ];
  }
  return [];
}

const prNumberRe = /\/pull\/(\d+)/;

/** The PR number from its URL, or 0 when the URL isn't a GitHub PR link. */
export function prNumber(pr: TaskPR): number {
  const m = prNumberRe.exec(pr.pr_url);
  return m ? Number(m[1]) : 0;
}

export function prRepoName(pr: TaskPR): string {
  return pr.repo.split("/").filter(Boolean).pop() || "";
}

// A task can produce several PRs, sometimes more than one from the same repo,
// so the number is what actually identifies a button — never the repo alone.
export function prLabel(pr: TaskPR, fallback: string): string {
  const name = prRepoName(pr);
  const num = prNumber(pr);
  if (name && num) return `${name} #${num}`;
  if (num) return `#${num}`;
  return name || fallback;
}

export type PRVisualState = "merged" | "closed" | "draft" | "open" | "unknown";

// gh reports state as OPEN / MERGED / CLOSED with isDraft orthogonal to it.
// Draft only matters while the PR is still open — a merged draft is merged.
export function prVisualState(pr: TaskPR): PRVisualState {
  // ?? "" because an older daemon behind a newer desktop build omits the field.
  switch ((pr.pr_state ?? "").toUpperCase()) {
    case "MERGED":
      return "merged";
    case "CLOSED":
      return "closed";
    case "OPEN":
      return pr.pr_is_draft ? "draft" : "open";
    default:
      return "unknown";
  }
}

// Text color for a PR chip, keyed on probed state. Every token here is declared
// in globals.css @theme — the palette is closed, so an undeclared color would
// compile to no CSS at all. "unknown" keeps the pre-probe amber look.
const stateColor: Record<PRVisualState, string> = {
  merged: "text-merged",
  closed: "text-danger",
  draft: "text-dim",
  open: "text-sage",
  unknown: "text-amber",
};

export function prStateColor(pr: TaskPR): string {
  return stateColor[prVisualState(pr)];
}

export function prStateTitle(pr: TaskPR): string {
  const where = pr.repo ? `${pr.repo} ` : "";
  switch (prVisualState(pr)) {
    case "merged":
      return `${where}merged`;
    case "closed":
      return `${where}closed without merging`;
    case "draft":
      return `${where}open (draft)`;
    case "open":
      return `${where}open`;
    default:
      return `${where}state unknown — not probed yet`;
  }
}
