import type { HumanRequest } from "@/lib/api/types";

/**
 * How to bring the thing under test back up, when the agent's run has ended and
 * its dev server died with it. Every field is optional: the agent fills in what
 * it knows.
 */
export interface RevivalSteps {
  cwd?: string;
  command?: string;
  port?: number;
}

/**
 * A `demo` request's `body` is a JSON document rather than markdown — the agent
 * writes a structured brief so the UI can render steps as a list and the URL as
 * a link. Anything that does not parse into at least one recognized field is
 * treated as prose and rendered raw; see `parseDemoBody`.
 */
export interface DemoBrief {
  steps: string[];
  expected?: string;
  look_for?: string;
  url?: string;
  revival_steps?: RevivalSteps;
}

function stringField(obj: Record<string, unknown>, key: string): string | undefined {
  const v = obj[key];
  return typeof v === "string" && v.trim() !== "" ? v : undefined;
}

function parseRevival(value: unknown): RevivalSteps | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  const obj = value as Record<string, unknown>;
  const cwd = stringField(obj, "cwd");
  const command = stringField(obj, "command");
  const rawPort = obj.port;
  let port: number | undefined;
  if (typeof rawPort === "number" && Number.isFinite(rawPort)) {
    port = rawPort;
  } else if (typeof rawPort === "string" && rawPort.trim() !== "" && !isNaN(Number(rawPort))) {
    port = Number(rawPort);
  }
  if (cwd === undefined && command === undefined && port === undefined) return undefined;
  return { cwd, command, port };
}

/**
 * Parses a `demo` request body into a structured brief.
 *
 * Returns `null` — meaning "render the raw text instead" — for anything that is
 * not JSON, is not a JSON object, or carries none of the recognized fields. The
 * body is agent-authored, so a malformed one must degrade to visible prose
 * rather than to an empty card or a thrown render.
 */
export function parseDemoBody(body: string): DemoBrief | null {
  if (typeof body !== "string" || body.trim() === "") return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) return null;

  const obj = parsed as Record<string, unknown>;
  const steps = Array.isArray(obj.steps)
    ? obj.steps.filter((s): s is string => typeof s === "string" && s.trim() !== "")
    : [];
  const expected = stringField(obj, "expected");
  const look_for = stringField(obj, "look_for");
  const url = stringField(obj, "url");
  const revival_steps = parseRevival(obj.revival_steps);

  if (
    steps.length === 0 &&
    expected === undefined &&
    look_for === undefined &&
    url === undefined &&
    revival_steps === undefined
  ) {
    return null;
  }

  return { steps, expected, look_for, url, revival_steps };
}

/**
 * Queue order, per the PRD: requests whose agent is still long-polling sort
 * first (answering them unblocks work right now), then oldest-first. Pure and
 * non-mutating so it is safe to call inside a `useMemo` over store state.
 */
export function sortRequests(requests: HumanRequest[]): HumanRequest[] {
  return [...requests].sort((a, b) => {
    if (a.live !== b.live) return a.live ? -1 : 1;
    const at = a.created_at ? new Date(a.created_at).getTime() : 0;
    const bt = b.created_at ? new Date(b.created_at).getTime() : 0;
    if (at !== bt) return at - bt;
    return a.id - b.id;
  });
}

/** The pending requests belonging to one task, in queue order. */
export function requestsForTask(requests: HumanRequest[], taskId: number): HumanRequest[] {
  return sortRequests(requests.filter((r) => r.task_id === taskId));
}

/** How long a one-line summary may get before it is cut with an ellipsis. */
export const SUMMARY_MAX_CHARS = 100;

/**
 * Flattens one line of markdown to the words a human would read out loud.
 *
 * List surfaces need a label, not a document: `ask_human` writes its whole
 * markdown question into `title`, so rendering the title raw put `**bold**`
 * and `- ` bullets on screen verbatim. This strips the syntax rather than the
 * content — links keep their text, code keeps its code — and is deliberately
 * not a markdown parser: it runs on a single already-chosen line.
 */
export function stripMarkdown(text: string): string {
  if (typeof text !== "string") return "";

  let out = text
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1") // image -> alt text
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1") // link -> link text
    .replace(/`+([^`]*)`+/g, "$1"); // inline code -> its contents

  // Leading block markers can stack ("> - **note**"), so peel until stable.
  for (let i = 0; i < 4; i++) {
    const next = out.replace(/^\s{0,3}(?:>\s?|#{1,6}\s+|[-*+]\s+|\d+[.)]\s+)/, "");
    if (next === out) break;
    out = next;
  }

  return out
    .replace(/(\*\*|__)(.+?)\1/g, "$2")
    .replace(/(\*|_)(.+?)\1/g, "$2")
    .replace(/~~(.+?)~~/g, "$1")
    .replace(/\*\*|__/g, "") // unbalanced emphasis, e.g. a cut-off "**Q:"
    .replace(/\s+/g, " ")
    .trim();
}

/**
 * A one-line, syntax-free summary of a markdown document: its first line with
 * anything to say, truncated. Returns "" when the document has no prose in it.
 */
export function summarizeMarkdown(text: string, max = SUMMARY_MAX_CHARS): string {
  if (typeof text !== "string" || text.trim() === "") return "";

  let line = "";
  for (const raw of text.split("\n")) {
    // Fences and horizontal rules are structure, never a summary.
    if (/^\s*(```|~~~|---+|\*\*\*+|===+)\s*$/.test(raw)) continue;
    const stripped = stripMarkdown(raw);
    if (stripped !== "") {
      line = stripped;
      break;
    }
  }
  if (line === "") return "";
  if (line.length <= max) return line;

  const cut = line.slice(0, max);
  // Prefer a word boundary, but not one so early it loses the sentence.
  const space = cut.lastIndexOf(" ");
  const head = space > max * 0.6 ? cut.slice(0, space) : cut;
  return `${head.trimEnd()}…`;
}

/**
 * Short label for a request in a one-line list.
 *
 * `title` is not a title: `ask_human` sets it to the full question text, which
 * is also the head of `body`. So every list surface derives its own compact
 * line rather than trusting the field — never render a wall of raw markdown as
 * a heading.
 */
export function requestSummary(request: HumanRequest): string {
  const fromTitle = summarizeMarkdown(request.title ?? "");
  if (fromTitle) return fromTitle;

  if (request.kind === "demo") {
    const brief = parseDemoBody(request.body);
    if (brief) {
      const first =
        brief.steps[0] ?? brief.expected ?? brief.look_for ?? brief.url ?? "";
      const fromBrief = summarizeMarkdown(first);
      if (fromBrief) return fromBrief;
      return "Demo request";
    }
  }

  const fromBody = summarizeMarkdown(request.body ?? "");
  if (fromBody) return fromBody;

  if (request.kind === "question") return "Question";
  if (request.kind === "demo") return "Demo request";
  if (request.kind === "capture_approval") return "Screen capture approval";
  return request.kind;
}
