// Keys is the TUI's key-hint line (↑↓ move · enter save · esc cancel) for a modal's footer.
export function Keys({ k }: { k: [string, string][] }) {
  return (
    <span class="keys" aria-hidden="true">
      {k.map(([key, what]) => (
        <span><kbd>{key}</kbd> {what}</span>
      ))}
    </span>
  );
}
