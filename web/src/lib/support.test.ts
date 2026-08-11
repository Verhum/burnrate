import assert from "node:assert/strict";
import { test } from "node:test";

import { SUPPORT_EMAIL } from "@/lib/support";

test("support address is the one users are told to write to", () => {
  assert.equal(SUPPORT_EMAIL, "support@ver-hum.com");
});
