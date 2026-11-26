// Splash is the anvil over a dither field that fades in, the TUI's first-run splash on the web.
// Loaded lazily: only empty and first-run screens show it.
import { Dither } from "@zovaris/sephiro/dither";
import "@zovaris/sephiro/css/dither.css";
import { Anvil } from "./icons";

export default function Splash() {
  return <Dither class="splash" cell={6} duration={1100} intensity={0.4} sprite={<span class="splash-anvil"><Anvil /></span>} />;
}
