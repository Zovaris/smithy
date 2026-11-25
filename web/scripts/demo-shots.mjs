// Playwright screenshots of the v0.23 web demo (demo/run.sh runs it against `smithy daemon --demo`):
// first run, a session started from the UI, its tool calls, the decision answered, then phone width.
// Waits on selectors, never networkidle: the event stream keeps the network busy.
// Usage: node scripts/demo-shots.mjs <url-with-#token> <repo-dir> <out-dir>
import { chromium } from "playwright";

const [url, repo, out] = process.argv.slice(2);
if (!url || !repo || !out) {
  console.error("usage: node scripts/demo-shots.mjs <url> <repo-dir> <out-dir>");
  process.exit(2);
}
// DEMO_CHROME: a browser already installed (e.g. Chrome) instead of Playwright's own chromium.
const browser = await chromium.launch(process.env.DEMO_CHROME ? { executablePath: process.env.DEMO_CHROME } : {});
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: "dark" });
  const shot = (name) => page.screenshot({ path: `${out}/${name}.png` });
  await page.goto(url, { waitUntil: "domcontentloaded" });
  // First run: no default agent yet, so the web opens Settings with the detected agents.
  const settings = page.getByRole("region", { name: "Settings" });
  await settings.getByText("ready").first().waitFor({ timeout: 20000 });
  await shot("web-firstrun-1440");
  await settings.getByRole("button", { name: /Claude Code/ }).first().click();
  await settings.getByRole("button", { name: "Use for new sessions" }).click();
  await settings.getByText("default").first().waitFor({ timeout: 20000 });

  await page.goto(url.replace(/#.*/, "#/"), { waitUntil: "domcontentloaded" });
  await page.getByRole("radiogroup", { name: "Agent" }).locator(".row2").first().waitFor({ timeout: 20000 });
  await shot("web-new-1440");
  await page.getByLabel("Folder").fill(repo);
  const box = page.locator("textarea");
  await box.fill("add a health endpoint with a test");
  await page.getByRole("button", { name: "Send" }).click();
  await page.getByRole("log", { name: "Conversation" }).getByText("go test ./...").first().waitFor({ timeout: 20000 });
  const decision = page.locator(".decide:not(.done)").first();
  await decision.waitFor({ timeout: 20000 });
  await shot("web-session-1440");

  await decision.getByRole("button", { name: /commit/i }).first().click();
  await page.locator(".decide.done").first().waitFor({ timeout: 20000 });
  await page.getByText("you answered").first().waitFor({ timeout: 20000 });
  await shot("web-answered-1440");

  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("log", { name: "Conversation" }).waitFor();
  await shot("web-phone-390");
} catch (e) {
  await browser.contexts()[0]?.pages()[0]?.screenshot({ path: `${out}/failed.png` }).catch(() => {});
  throw e;
} finally {
  await browser.close();
}
