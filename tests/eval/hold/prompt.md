In this Go package, `Stage` reports every item that has a `hold` key as held, even when the
value is `false`, `nil` or an empty string. An item is on hold only when `hold` is `true` or a
non-empty string. Fix it and add a test.
