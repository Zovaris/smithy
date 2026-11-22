package main

import (
	"github.com/Zovaris/smithy/internal/cards"
	"github.com/Zovaris/smithy/internal/decide"
)

func init() {
	register("decide", "optional decision model: rule flags and question impact", decide.Usage,
		"--base= --rules=", decide.Main)
	cards.RaiseImpact = decide.Raise
}
