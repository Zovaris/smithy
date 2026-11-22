// Package hold decides what a batch runner does with a work item.
package hold

// Stage returns "held" when the item is on hold, else "queued".
// An item sets "hold" to true, or to a reason string.
func Stage(item map[string]any) string {
	if _, ok := item["hold"]; ok {
		return "held"
	}
	return "queued"
}
