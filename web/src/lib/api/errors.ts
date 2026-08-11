import { ApiError } from "./client";

/**
 * The human-readable sentence behind a failed request.
 *
 * The Go server writes every error as `{"error": "..."}` (`writeError` in
 * `internal/server/helpers.go`), and `ApiError` keeps that raw text in `.body`
 * while `.message` is the noisier `API error 409: {"error":...}`. So: parse the
 * body and return `.error`, falling back to the raw body, then the Error's
 * message, then a generic string.
 */
export function apiErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    const body = err.body?.trim();
    if (body) {
      try {
        const parsed: unknown = JSON.parse(body);
        if (parsed && typeof parsed === "object") {
          const detail = (parsed as { error?: unknown }).error;
          if (typeof detail === "string" && detail.trim()) {
            return detail.trim();
          }
        }
      } catch {
        // Not JSON (a proxy error page, a bare status text) — the raw body is
        // still the most specific thing we have.
      }
      return body;
    }
  }
  if (err instanceof Error && err.message) return err.message;
  if (typeof err === "string" && err.trim()) return err.trim();
  return "Something went wrong.";
}
