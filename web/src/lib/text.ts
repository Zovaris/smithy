// plural renders a count with its noun, as internal/text.Plural: "1 run", "3 runs".
export const plural = (n: number, one: string, many: string): string => (n === 1 ? "1 " + one : n + " " + many);

// tail is the last n lines of a log, without the trailing blank line.
export const tail = (log: string, n: number): string => log.trimEnd().split("\n").slice(-n).join("\n");
