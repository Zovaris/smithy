// `smithy stats` for the web, drawn with Sephiro's text-glyph charts: the TUI's look.
import { GlyphBars, GlyphSparkline } from "@zovaris/sephiro/glyph-chart";
import "@zovaris/sephiro/css/glyph-chart.css";
import { count, type Stats, usd } from "./stats";

export { glyphSparkline as spark } from "@zovaris/sephiro/glyph-chart";

// StatsCharts is cost, tokens, tok/s and runs per hour.
export function StatsCharts({ s }: { s: Stats }) {
  return (
    <div class="charts">
      <figure class="gc">
        <figcaption>Runs per hour, last 24 h</figcaption>
        <GlyphSparkline label="Runs per hour, last 24 h" values={s.hourly} valueText={String(s.hourly.reduce((a, b) => a + b, 0))} empty="No runs yet." />
      </figure>
      <figure class="gc">
        <figcaption>tok/s by run</figcaption>
        <GlyphSparkline label="tok/s by run" values={s.tpsSeries} valueText={s.tps == null ? "?" : s.tps.toFixed(0) + " avg"} empty="No finished runs yet." />
      </figure>
      <figure class="gc">
        <figcaption>Cost · {usd(s.cost)}</figcaption>
        <GlyphBars label="Cost by session" items={s.costBy} width={16} formatValue={(n) => usd(n)} empty="No cost reported yet." />
      </figure>
      <figure class="gc">
        <figcaption>Tokens · {count(s.input)} in · {count(s.output)} out</figcaption>
        <GlyphBars label="Tokens by session" items={s.tokensBy} width={16} formatValue={count} empty="No tokens yet." />
      </figure>
    </div>
  );
}
