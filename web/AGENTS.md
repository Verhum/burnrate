<!-- BEGIN:nextjs-agent-rules -->
# This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` before writing any code. Heed deprecation notices.
<!-- END:nextjs-agent-rules -->

# `out/.gitkeep` is tracked on purpose — do not delete it

`web.go` declares `//go:embed all:out`, and Go fails the **compile** when an
embed pattern matches nothing:

```
web/web.go:9:12: pattern all:out: no matching files found
```

`out/` is generated and gitignored, so without a tracked file inside it a fresh
clone or `git worktree add` could not build or test three Go packages (`web`,
`internal/server`, `cmd/burnrate`) until someone ran a Node build. The zero-byte
`out/.gitkeep` is what keeps Go work independent of the frontend toolchain. Notes:

- The `all:` prefix is load-bearing. Plain `//go:embed out` skips names starting
  with `.` or `_` and still reports "contains no embeddable files".
- The ignore rule must be `/out/*`, not `/out/` — git does not descend into an
  ignored *directory*, so `!/out/.gitkeep` under the old pattern was dead.
- `next build` wipes `out/` first, so `npm run build` chains a
  `keep-embed-target` script to put the placeholder back. If you add another
  build path, restore it there too, or the deletion lands in `git status` and
  burnrate's `git add -A` checkpoint will commit it. `npx next build` on its own
  does *not* restore it; `scripts/bootstrap.sh` will.
- Keep it zero-byte. `touch` then restores it byte-identically and the tree stays
  clean; a file with content would show up as modified.
- Symlinking `out` instead is not an option — `//go:embed` rejects symlinks, both
  as the directory ("cannot embed irregular file out") and as files inside a real
  one ("contains no embeddable files").
- Nothing but the export may live under `out/`. In particular do not set
  `distDir: "out"` in `next.config.ts`: `output: "export"` already writes the
  export there, and pointing Next's *build* dir at it too means `next dev` churns
  `out/dev/_events_<pid>.json` files in and out of the embed tree. A `go build`
  racing that dies at compile time with
  `embed out/dev/_events_64167.json: no such file or directory` — which reads like
  a corrupt checkout, not a dev server. Default `distDir` keeps the churn in the
  gitignored `.next/`.

`web.Built()` reports whether a real export is embedded. When it is false the
server answers every path with a 503 explaining how to build, rather than a bare
404.

# `confirm()`, `alert()` and `prompt()` are dead in this app

They are not merely discouraged — they cannot work, and they fail *silently*. The desktop
shell is Tauri → wry → WKWebView, and wry's `WKUIDelegate` implements exactly three
callbacks (`runOpenPanelWithParameters`, `requestMediaCapturePermissionForOrigin`,
`createWebViewWithConfiguration`). The three JavaScript dialog panels —
`runJavaScriptAlertPanel…`, `runJavaScriptConfirmPanel…`, `runJavaScriptTextInputPanel…` —
are absent. When a `WKUIDelegate` doesn't implement the confirm panel, WebKit shows nothing
and returns `false`, so

```tsx
if (confirm("Delete this task?")) deleteTask(task.id);   // never runs
```

is a dead button. `tsc` is happy (`confirm` is validly typed `boolean`) and it works fine
in a browser, so this only shows up in the shipped app. It shipped twice.

Use instead:

- **`ConfirmDialog`** (`components/ui/confirm-dialog.tsx`) for a yes/no — built on
  `Modal size="sm"`, autofocuses its confirm button so Enter confirms and Escape cancels.
- **`toast`** (`lib/toast.ts`) to tell the user anything: `toast.success(...)`,
  `toast.error(title, apiErrorMessage(err))`, `toast.info(...)`. It is a plain module
  helper over a Zustand store, so **stores can call it too** — they are not components and
  they are where most failures surface. `<Toaster />` is mounted once, in `AppShell`.

`no-restricted-globals` in `eslint.config.mjs` fails the build on all three globals. It is
keyed on the bare global, which is what both original call sites used; a rule matching
`window.confirm` member access would have missed them.

Two constraints that are easy to trip over:

- The `Toaster` is `z-[60]` because `Modal` is `z-50` and renders **inline, not through a
  portal** — at an equal z-index the stacking would fall to DOM order, and a failure raised
  from inside a dialog has to stay visible.
- `Modal` keeps a module-level stack so Escape closes only the topmost one, and
  `use-keyboard` stands down entirely while any Modal is open (`hasOpenModal()`). Without
  that, the global `Enter` shortcut preventDefaults the keypress that activates the confirm
  button, and `Escape` navigates the view *behind* the dialog as well as closing it.

# A link out of the app takes two handlers, not zero

An `<a href>` that leaves the webview does nothing on its own in the desktop
build. Tauri's `on_navigation` handler (`desktop/src-tauri/src/lib.rs`) cancels
every scheme it doesn't explicitly allow, and WKWebView has no browser or mail
client behind it — so the click is simply swallowed, in the shipped app only.

Both layers already exist and both key off `lib/external-link.ts`:

- `AppShell` intercepts anchor clicks in the capture phase and hands the href to
  `plugin:shell|open`. It stands down when `__TAURI_INTERNALS__` is absent, so
  `next dev` in a real browser keeps native behavior.
- The Rust handler opens the same way for any navigation the interceptor misses.

`mailto:` is the case that looks like it should "just work" and doesn't. Note
also that tauri-plugin-shell validates `open` against
`^((mailto:\w+)|(tel:\w+)|(https?://\w+)).+` when the app declares no custom
scope — a href that fails that regex is rejected in Rust with no UI feedback.

# Tests: `npm test`, no framework

`node --test` runs the `.ts` sources directly through
`scripts/ts-resolve-hook.mjs`, which teaches Node the `@/` alias and
extensionless imports. No jest, no vitest, no `npm install` — which is the point:
`make web-test` works in a fresh worktree. It suits pure modules under `lib/`;
there is no DOM or renderer, so components are not testable this way.

# Tailwind: the palette is closed

`src/app/globals.css` opens its `@theme` block with `--color-*: initial`, which
deletes Tailwind's *entire* default palette. Only the colors declared right below
it exist. A utility referencing anything else — `bg-red-500`, `text-slate-400` —
compiles to **no CSS at all**: no error, no warning, the element just inherits.
If a color "isn't applying", check that the token is in `@theme` before
reaching for an inline `style`.

To add a color, declare `--color-<name>` in `@theme`. To check nothing has
silently dropped out, build and diff the emitted classes against the source:

```bash
npx next build
grep -o 'bg-black[^{]*{[^}]*}' out/_next/static/chunks/*.css   # empty => not emitted
```

# Canvas charts: never measure once

A canvas is sized in explicit pixels, so it cannot reflow. Measuring its
container on mount and drawing at that width leaves the canvas stuck there:
grow the window and the chart stays narrow, **shrink** the window and the chart
stays wide, spills out of its panel, and gives the whole page a horizontal
scrollbar. The panels around it keep their (correct) narrow width, which is
what makes the result look so broken.

Do not hand-roll this. Render through `components/usage/chart-canvas.tsx`:

```tsx
const draw = useCallback<DrawFn>((ctx, W, H) => { /* ... */ }, [data]);
return <ChartCanvas height={160} draw={draw} />;
```

It observes the container with `useElementWidth` and redraws on every change,
and it positions the canvas absolutely so the canvas's own pixel width can
never feed back into the layout it was measured from. `draw` receives a context
already scaled for `devicePixelRatio` and cleared, with coordinates in CSS
pixels — and it **must** be memoized, since it is an effect dependency.

Two things follow for the drawing code itself:

- Every width is possible, including tiny ones. Guard `plotW`/`plotH` above
  zero, and drop the axis gutters below a threshold rather than letting padding
  exceed the canvas.
- Axis labels have to thin out. Derive the label step from the measured width
  and skip any label that would collide with the previous one; a hardcoded
  "every other day" turns into overlapping mush in a narrow window.

Non-canvas panels need `min-w-0` on flex/grid children whose content has a
minimum width, or the same overflow reappears in CSS.
