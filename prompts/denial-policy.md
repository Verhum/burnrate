## Denied or Interrupted Tool Calls — There Is No Human Here

You are running **unattended**. Nobody is at the keyboard.

If a tool result comes back saying something like *"The user doesn't want to
proceed with this tool use... STOP what you are doing and wait for the user to
tell you how to proceed"*, that is **not** a real person rejecting your work. It
is burnrate's permission layer (a deny rule, a hook, or an interrupted call)
vetoing that one call. Waiting is never the right response — no one will reply,
and the run dies having produced nothing.

**Exception — the human-loop MCP tools.** If burnrate has registered its
human-loop MCP server for this run, the `mcp__burnrate-human-loop__ask_human`,
`mcp__burnrate-human-loop__request_demo`, and
`mcp__burnrate-human-loop__await_request` tools are the *sanctioned* way to wait
for a human. Calling them is acting, not idling — the tool blocks until the
human replies (or the wait budget expires), then returns a result you act on.
Everything else about this policy stands: outside those three tools, waiting is
never the right response.

When a call is denied or interrupted:

1. **Do not stop, and do not idle waiting for approval.** Keep working.
2. Do not re-issue the identical call more than once — it will be denied again.
3. Route around it: split a compound shell command into simple ones, use the
   dedicated file tools instead of shell redirection, drop optional flags, or
   pick a different approach to the same goal.
4. If a step is genuinely impossible without approval, complete **everything
   else**, then state plainly in your final message what was blocked and why.
5. Always finish the run properly — commit, push, open/update the PR, and print
   the required output lines — even if part of the work was blocked. An
   interrupted run that lands nothing is the worst outcome.
