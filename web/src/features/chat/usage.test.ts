import { describe, expect, it, vi } from "vitest";

vi.mock("../../lib/actions", () => ({
  tokenUsage: async () => ({ session: none, today: one, week: one, all: one }),
}));

const u = { input_tokens: 12000, output_tokens: 3400, cache_read_tokens: 56000, cache_write_tokens: 0, cost_usd: 1.25 };
const none = { total: { ...u, cost_usd: undefined }, agents: null, models: null, runs: 0 };
const one = { total: u, agents: { claude: u, codex: { ...u, input_tokens: 1, cost_usd: undefined } }, models: { haiku: u }, runs: 2 };

const { loadUsage, usageInfo } = await import("./usage");
const { took } = await import("./shellblock");

describe("/usage", () => {
  it("lays out each period with its agents and models, skipping empty ones", async () => {
    const i = await loadUsage("s1");
    expect(i.title).toBe("Usage");
    expect(i.lines).not.toContain("#THIS SESSION");
    expect(i.lines).toContain("#TODAY");
    expect(i.lines).toContain("#ALL TIME");
    expect(i.lines).toContain("agent claude\tin 12.0k · out 3.4k · cache 56.0k · $1.25");
    expect(i.lines).toContain("model haiku\tin 12.0k · out 3.4k · cache 56.0k · $1.25");
    expect(i.lines.indexOf("agent claude\tin 12.0k · out 3.4k · cache 56.0k · $1.25")).toBeLessThan(i.lines.findIndex((l) => l.startsWith("agent codex")));
  });
  it("says when nothing was recorded", () => {
    expect(usageInfo({ session: none, today: none, week: none, all: none }).lines[0]).toMatch(/no token use recorded/);
  });
});

describe("!cmd", () => {
  it("formats the run time", () => {
    expect(took(40)).toBe("40ms");
    expect(took(1500)).toBe("1.5s");
  });
});
