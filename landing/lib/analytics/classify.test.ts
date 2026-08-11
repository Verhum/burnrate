import assert from "node:assert/strict";
import test from "node:test";
import { isAutomated, isCrawler, isScriptedClient } from "./classify";

const SAFARI =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15";

test("a browser download counts", () => {
  assert.equal(isAutomated("download", SAFARI), false);
});

test("crawlers and unfurlers never count as downloads", () => {
  const bots = [
    "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
    "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
    "facebookexternalhit/1.1",
    "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)",
    "Twitterbot/1.0",
    "Discordbot/2.0",
    "Mozilla/5.0 (compatible; Bytespider; spider-feedback@bytedance.com)",
    "HeadlessChrome/126.0.0.0",
    "UptimeRobot/2.0",
  ];
  for (const ua of bots) {
    assert.equal(isCrawler(ua), true, ua);
    assert.equal(isAutomated("download", ua), true, ua);
    // A crawler is noise on every endpoint, including update checks.
    assert.equal(isAutomated("update_check", ua), true, ua);
  }
});

test("scripted fetches are automated downloads but legitimate update checks", () => {
  const scripts = ["curl/8.7.1", "Wget/1.21.4", "Go-http-client/2.0", "python-requests/2.32.3"];
  for (const ua of scripts) {
    assert.equal(isScriptedClient(ua), true, ua);
    assert.equal(isAutomated("download", ua), true, ua);
    // The desktop updater is a native HTTP client; excluding it would zero out
    // the active-install count.
    assert.equal(isAutomated("update_check", ua), false, ua);
  }
});

test("a missing user agent is a script downloading, but a plausible updater", () => {
  for (const ua of [null, undefined, ""]) {
    assert.equal(isAutomated("download", ua), true);
    assert.equal(isAutomated("update_check", ua), false);
  }
});

test("browser UAs are not mistaken for scripts", () => {
  const humans = [
    SAFARI,
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/127.0.0.0 Safari/537.36",
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 14.5; rv:128.0) Gecko/20100101 Firefox/128.0",
  ];
  for (const ua of humans) {
    assert.equal(isCrawler(ua), false, ua);
    assert.equal(isScriptedClient(ua), false, ua);
  }
});
