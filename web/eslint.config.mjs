import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // Override default ignores of eslint-config-next.
  globalIgnores([
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
    "sortable.min.js",
  ]),
  {
    // The desktop shell is Tauri -> wry -> WKWebView, and wry's WKUIDelegate
    // implements none of the three JavaScript dialog panels. WebKit therefore
    // shows nothing and answers `false`, so a `confirm()`-guarded action is a
    // dead button. Must be no-restricted-globals rather than a member-access
    // rule: the calls that shipped this bug used the bare globals.
    files: ["src/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-globals": [
        "error",
        {
          name: "confirm",
          message: "Returns false in the Tauri webview — use ConfirmDialog.",
        },
        {
          name: "alert",
          message: "Does nothing in the Tauri webview — use the toast store.",
        },
        {
          name: "prompt",
          message: "Does nothing in the Tauri webview — use a Modal.",
        },
      ],
    },
  },
]);

export default eslintConfig;
