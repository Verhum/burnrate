## Level of Effort

Level of effort is **how far you carry this task before calling it done** — not how hard you
think, not which model or reasoning budget you use. There are four levels, and each one
includes everything below it:

1. **Investigate** — Research and report. Read the code, find the cause or map the options,
   and write up what you found with the `file:line` evidence behind it. Do not ship a
   speculative implementation.
2. **Write the code** — Investigate, then implement the change. It compiles and it is
   committed, but you have not proven it behaves correctly.
3. **Verify** — Write the code, then show it is right: reason through the edge cases,
   add or update unit tests that would fail without your change, and run the repo's standard
   checks (build, lint, the existing test suite). Report the actual command output — a test
   you did not run is not a verified test.
4. **Validate end to end** — Verify, then wholeheartedly attempt the closest thing to an
   integration test this repo allows. Exercise the change through its real entry point —
   start the server and hit the route, run the built binary, drive the UI, run the migration
   against a scratch database — and show the output that proves the whole path works, not
   just the unit you touched. If a true integration test is impossible here, say why, then
   get as close as the repo permits. **Level 4 is opt-in only — see below.**

**Level 3 is the default.** Aim there unless the task tells you otherwise or its nature
clearly calls for another level:

- A task that asks you to research, compare, diagnose, or recommend is a **1** — its
  deliverable is the write-up, not a patch.
- **NEVER work at level 4 unless the user explicitly asked for it.** Level 4 is opt-in, and
  the only thing that opts in is a directive from the user — `LOE: 4`, "validate end to end",
  "level of effort 4" in the task description or a follow-up comment. Nothing about the change
  itself promotes you to it: not risk, not money, not auth, not a data migration, not crossing
  a process or service boundary. If you think a change warrants end-to-end validation and
  nobody asked, do the work at level 3 and **say so in one line in your final output** —
  recommending 4 is useful, taking it on your own is not.
- **Never drop below 3 on your own.** Level 2 is only correct when the task explicitly asks
  for it (a spike, a throwaway prototype, a repo with no runnable tests).

Whatever the level, the run still ends the same way: commit, push, and open the draft PR.
At level 1 that means committing the findings write-up as a Markdown file (under `docs/` if
the repo has one) rather than committing code you never validated — the investigation is the
deliverable, and it still ships as a reviewable PR.

**State the level you worked at in your final output**, in one line, with a word on why if
you departed from the default.
