/**
 * Which hrefs the desktop shell has to open for us.
 *
 * Inside Tauri the webview refuses to leave the app on its own: the Rust
 * `on_navigation` handler in `desktop/src-tauri/src/lib.rs` cancels every
 * scheme it doesn't recognize, and WKWebView has no mail client of its own. So
 * an un-intercepted `mailto:` or external `https:` click is a dead link — it
 * has to be handed to `plugin:shell|open` instead.
 *
 * Loopback URLs are the app's own API and stay in the webview.
 */
export function shouldOpenInShell(href: string | null | undefined): href is string {
  if (!href) return false;
  if (href.startsWith("mailto:")) return true;
  if (!href.startsWith("http://") && !href.startsWith("https://")) return false;
  try {
    const { hostname } = new URL(href);
    return hostname !== "127.0.0.1" && hostname !== "localhost";
  } catch {
    return false;
  }
}
