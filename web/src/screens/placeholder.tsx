import { EmptyState } from "@zovaris/sephiro/empty-state";
import "@zovaris/sephiro/css/empty-state.css";

export function Placeholder({ title }: { title: string }) {
  return <EmptyState title={title} description="This screen arrives in the next v0.20 task." icon={<span aria-hidden="true">→</span>} />;
}
