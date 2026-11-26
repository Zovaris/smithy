// Playwright screenshots of what the web shares with the TUI, against `smithy daemon --demo`:
// Settings (typed keys, API keys, serve, doctor, servers), the session header, the model and effort
// dialog, `!cmd` output, /ask, the frost theme and the phone width.
// Usage: node scripts/parity-shots.mjs <url-with-#token> <repo-dir> <out-dir>
import { chromium } from "playwright";

const [url, repo, out] = process.argv.slice(2);
if (!url || !repo || !out) {
  console.error("usage: node scripts/parity-shots.mjs <url> <repo-dir> <out-dir>");
  process.exit(2);
}
const browser = await chromium.launch(process.env.DEMO_CHROME ? { executablePath: process.env.DEMO_CHROME } : {});
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, colorScheme: "dark" });
  const shot = (name, o = {}) => page.screenshot({ path: `${out}/web-parity-${name}.png`, ...o });
  await page.goto(url, { waitUntil: "domcontentloaded" });
  const settings = page.getByRole("region", { name: "Settings" });
  await settings.getByText("ready").first().waitFor({ timeout: 20000 });
  await settings.getByRole("button", { name: /Claude Code/ }).first().click();
  await settings.getByRole("button", { name: "Use for new sessions" }).click();
  await page.locator("#set-keys").waitFor({ timeout: 20000 });
  await page.locator("#set-theme").scrollIntoViewIfNeeded();
  await shot("settings-1440");
  await page.locator("#set-keys").scrollIntoViewIfNeeded();
  await shot("settings-keys-1440");
  await page.locator("#set-doctor").getByRole("button", { name: "Run" }).click();
  await page.locator("#set-doctor pre").waitFor({ timeout: 30000 });
  await page.locator("#set-servers").scrollIntoViewIfNeeded();
  await shot("settings-doctor-servers-1440");

  await page.goto(url.replace(/#.*/, "#/"), { waitUntil: "domcontentloaded" });
  await page.getByLabel("Folder").fill(repo);
  const box = page.locator("textarea");
  await box.fill("add a health endpoint with a test");
  await page.getByRole("button", { name: "Send" }).click();
  await page.locator(".decide:not(.done)").first().waitFor({ timeout: 20000 });
  await shot("session-1440");
  await page.locator(".decide:not(.done) [data-primary]").first().click();
  await page.locator(".meta-bar .st.done").first().waitFor({ timeout: 30000 });

  await box.fill("!ls -la");
  await box.press("Enter");
  await page.locator(".shell pre").first().waitFor({ timeout: 20000 });
  await box.fill("/ask Merge the health endpoint now?");
  await box.press("Enter");
  await page.locator(".meta-bar .note").waitFor({ timeout: 20000 });
  await page.waitForTimeout(800);
  await shot("shell-ask-1440");

  await page.getByRole("button", { name: /Claude Code/ }).first().click();
  await page.getByRole("dialog").waitFor({ timeout: 20000 });
  await page.getByRole("radio", { name: "low" }).click().catch(() => {});
  await shot("model-effort-1440");
  await page.keyboard.press("Escape");

  await box.fill("/theme frost");
  await box.press("Enter");
  await page.waitForTimeout(600);
  await shot("frost-1440");
  await box.fill("/theme steel");
  await box.press("Enter");
  await page.waitForTimeout(600);

  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(400);
  await shot("phone-390");
  await page.goto(url.replace(/#.*/, "#/setup"), { waitUntil: "domcontentloaded" });
  await page.locator("#set-settings").scrollIntoViewIfNeeded({ timeout: 20000 });
  await shot("phone-settings-390");
} catch (e) {
  await browser.contexts()[0]?.pages()[0]?.screenshot({ path: `${out}/web-parity-failed.png` }).catch(() => {});
  throw e;
} finally {
  await browser.close();
}
