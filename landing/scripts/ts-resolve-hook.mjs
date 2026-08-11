/**
 * Module resolve hook for `node --test`.
 *
 * Node runs the `.ts` sources directly (type stripping), but it wants fully
 * specified ESM paths, whereas the app code uses bundler-style extensionless
 * imports and the `@/` alias from tsconfig.json. This hook teaches Node both, so
 * tests need no bundler, transpiler, or test-framework dependency.
 */

import { registerHooks } from "node:module";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const ROOT = path.resolve(fileURLToPath(new URL("..", import.meta.url)));
// `.js` is for package subpaths like `next/server`, which Next publishes as a
// real file rather than an exports-map entry.
const CANDIDATE_SUFFIXES = [".ts", ".tsx", "/index.ts", ".js"];

registerHooks({
  resolve(specifier, context, nextResolve) {
    const target = specifier.startsWith("@/")
      ? pathToFileURL(path.join(ROOT, specifier.slice(2))).href
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
