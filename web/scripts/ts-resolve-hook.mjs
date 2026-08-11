/**
 * Module resolve hook for `node --test`.
 *
 * Node runs the `.ts` sources directly (type stripping), but it wants fully
 * specified ESM paths, whereas the app code uses bundler-style extensionless
 * imports and the `@/` alias from tsconfig.json. This hook teaches Node both, so
 * tests need no bundler, transpiler, or test-framework dependency.
 *
 * Same hook as `landing/scripts/ts-resolve-hook.mjs`; `@/` resolves to `src/`
 * here rather than the package root.
 */

import { registerHooks } from "node:module";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const SRC = path.resolve(fileURLToPath(new URL("../src", import.meta.url)));
const CANDIDATE_SUFFIXES = [".ts", ".tsx", "/index.ts", ".js"];

registerHooks({
  resolve(specifier, context, nextResolve) {
    const target = specifier.startsWith("@/")
      ? pathToFileURL(path.join(SRC, specifier.slice(2))).href
      : specifier;

    try {
      return nextResolve(target, context);
    } catch (err) {
      if (path.extname(target) !== "") throw err;
      for (const suffix of CANDIDATE_SUFFIXES) {
        try {
          return nextResolve(target + suffix, context);
        } catch {
          // try the next suffix
        }
      }
      throw err;
    }
  },
});
