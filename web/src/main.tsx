import { type ComponentType, render } from "preact";
import { lazy, Suspense } from "preact/compat";
import { useEffect, useState } from "preact/hooks";
import "./theme/steel.css";
import "./app.css";
import { api, initAuth } from "./auth";
import { Header } from "./header";
import { ROUTES, type RouteId, useRoute } from "./router";

const SCREENS: Record<RouteId, ComponentType> = {
  sessions: lazy(() => import("./screens/sessions")),
  decisions: lazy(() => import("./screens/decisions")),
  agents: lazy(() => import("./screens/agents")),
  board: lazy(() => import("./screens/board")),
};

type Auth = { state: "checking" } | { state: "ok" } | { state: "denied" } | { state: "down"; error: string };

function useAuth(): Auth {
  const [a, set] = useState<Auth>({ state: "checking" });
  useEffect(() => {
    api("/v1/state")
      .then((r) => set(r.ok ? { state: "ok" } : r.status === 401 ? { state: "denied" } : { state: "down", error: `HTTP ${r.status}` }))
      .catch((e) => set({ state: "down", error: String(e) }));
  }, []);
  return a;
}

function Gate({ auth }: { auth: Auth }) {
  if (auth.state === "denied")
    return (
      <p class="msg" role="alert">
        <span class="g wait">?</span>Not signed in. Run <code>smithy web</code> and open the link it prints.
      </p>
    );
  if (auth.state === "down")
    return (
      <p class="msg" role="alert">
        <span class="g fail">✗</span>The daemon did not answer ({auth.error}). Is <code>smithy daemon</code> running?
      </p>
    );
  return null;
}

function App() {
  const r = useRoute();
  const auth = useAuth();
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      if (e.altKey && !e.ctrlKey && !e.metaKey) {
        const t = ROUTES.find((x) => x.key === e.key);
        if (t) {
          e.preventDefault();
          location.hash = "#/" + t.id;
        }
      }
    };
    addEventListener("keydown", on);
    return () => removeEventListener("keydown", on);
  }, []);
  const Screen = SCREENS[r];
  return (
    <>
      <a class="skip" href="#main" onClick={(e) => { e.preventDefault(); document.getElementById("main")?.focus(); }}>
        Skip to content
      </a>
      <Header />
      <header class="bar">
        <span class="brand">smithy</span>
        <nav class="nav" aria-label="Screens">
          {ROUTES.map((x) => (
            <a href={"#/" + x.id} aria-current={x.id === r ? "page" : undefined} aria-keyshortcuts={"Alt+" + x.key}>
              <kbd>{x.key}</kbd>
              {x.label}
            </a>
          ))}
        </nav>
        <span class="who">alt+1–4</span>
      </header>
      <main id="main" tabIndex={-1}>
        {auth.state === "ok" ? (
          <Suspense fallback={null}>
            <Screen />
          </Suspense>
        ) : (
          <Gate auth={auth} />
        )}
      </main>
    </>
  );
}

initAuth();
render(<App />, document.getElementById("app")!);
