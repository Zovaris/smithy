package main

import "github.com/Zovaris/smithy/internal/bench"

func init() {
	register("bench", "run smithy's benchmark task set with an agent", bench.Usage,
		"--agent= --model= --skills= --task= --timeout= --keep", bench.Main)
	registerSub("bench", "run", "--agent= --model= --skills= --task= --timeout= --keep")
	registerSub("bench", "list", "")
}
