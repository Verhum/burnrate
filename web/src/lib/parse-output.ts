export interface RunOutput {
  summary: string;
  changes: string;
  verify: string;
  docs: string;
  bootstrap: string;
  raw: string;
}

const SECTION_ALIASES: Record<string, keyof Omit<RunOutput, "raw">> = {
  summary: "summary",
  changes: "changes",
  "what changed": "changes",
  verification: "verify",
  verify: "verify",
  tests: "verify",
  "level of effort": "verify",
  documentation: "docs",
  docs: "docs",
  "worktree bootstrap": "bootstrap",
  bootstrap: "bootstrap",
};

const SECTION_HEADER_RE = /^(?:##\s+(.+)|([A-Z][A-Za-z ]+):)\s*$/gm;
const TRAILER_RE = /^(?:RESULT|WORKED_IN|REPO|BRANCH|PR):[ \t].*$/gm;

export function parseRunOutput(text: string): RunOutput {
  const out: RunOutput = {
    summary: "",
    changes: "",
    verify: "",
    docs: "",
    bootstrap: "",
    raw: text,
  };
  if (!text) return out;

  type Section = {
    field: keyof Omit<RunOutput, "raw">;
    headerStart: number;
    bodyStart: number;
  };

  const sections: Section[] = [];
  let m: RegExpExecArray | null;
  SECTION_HEADER_RE.lastIndex = 0;

  while ((m = SECTION_HEADER_RE.exec(text)) !== null) {
    const heading = (m[1] ?? m[2]).trim().toLowerCase();
    const field = SECTION_ALIASES[heading];
    if (!field) continue;
    sections.push({
      field,
      headerStart: m.index,
      bodyStart: m.index + m[0].length,
    });
  }

  if (sections.length === 0) return out;

  for (let i = 0; i < sections.length; i++) {
    const s = sections[i];
    let limit = i + 1 < sections.length ? sections[i + 1].headerStart : text.length;
    const trailerMatch = text.slice(s.bodyStart, limit).search(TRAILER_RE);
    if (trailerMatch >= 0) {
      limit = s.bodyStart + trailerMatch;
    }
    const body = text
      .slice(s.bodyStart, limit)
      .replace(TRAILER_RE, "")
      .trim();
    if (!body) continue;

    if (s.field === "verify" && out.verify) {
      out.verify += "\n" + body;
    } else {
      out[s.field] = body;
    }
  }

  return out;
}

export const SECTION_LABELS: Record<
  keyof Omit<RunOutput, "raw">,
  string
> = {
  summary: "Summary",
  changes: "Changes",
  verify: "Verification",
  docs: "Documentation",
  bootstrap: "Worktree Bootstrap",
};
