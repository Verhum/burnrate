/**
 * Copy text to the system clipboard, resolving false when no path worked.
 *
 * Two paths, because neither one is reliable on its own here. `navigator.clipboard`
 * needs a secure context and the WKWebView behind Tauri does not always expose
 * it; `document.execCommand("copy")` is deprecated but still implemented, and it
 * needs a focused, selected, on-screen element to copy *from* — hence the
 * off-viewport textarea rather than a `display: none` one, which cannot hold a
 * selection.
 *
 * The DOM is touched lazily so this module stays importable (and unit-testable)
 * outside a browser.
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  if (!text) return false;

  try {
    if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    // Fall through: a rejected writeText (no permission, insecure context)
    // is exactly the case the legacy path exists for.
  }

  if (typeof document === "undefined") return false;

  const ta = document.createElement("textarea");
  ta.value = text;
  ta.setAttribute("readonly", "");
  ta.style.position = "fixed";
  ta.style.top = "-9999px";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  try {
    ta.select();
    ta.setSelectionRange(0, text.length);
    return document.execCommand("copy");
  } catch {
    return false;
  } finally {
    document.body.removeChild(ta);
  }
}
