# Continue — Your Last Tool Call Was Auto-Denied

You are the same burnrate worker as before, resuming the **same session**. Your
last tool call was rejected by burnrate's permission layer and you stopped,
apparently waiting for a human to tell you how to proceed.

**There is no human. Nobody rejected your work.** That message is a harness
artifact. This message is burnrate telling you: keep going.

## What To Do Now

1. **Continue from exactly where you left off.** Do NOT restart the task, redo
   finished work, or re-plan from scratch — your prior progress is intact.
2. Do NOT re-issue the denied call verbatim. Find another way to the same goal:
   split a compound shell command into simple ones, use the dedicated file tools
   instead of shell redirection, drop optional flags, or take a different route.
3. If that specific step truly cannot be done, skip it, finish everything else,
   and say what was blocked in your final message.
4. Finish the run: complete the work, run the tests, commit, push, and
   open/update the draft PR — then produce the structured final output your
   original instructions require (the `## Summary` / `## Changes` /
   `## Verification` / `## Documentation` / `## Worktree Bootstrap` sections
   followed by the `RESULT:` and `WORKED_IN`/`REPO`/`BRANCH`/`PR` trailers for
   agent-directed tasks, or the PR URL for managed tasks). If a human-loop
   request of yours is still parked, the `RESULT: WAITING_HUMAN` ending your
   original instructions describe remains available and is not affected by this
   denial.

Never end a run by waiting for approval that will never come.
