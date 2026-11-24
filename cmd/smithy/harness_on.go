//go:build harness

package main

import _ "github.com/Zovaris/smithy/internal/store"

// harnessBuild is true in the harness build (docs/adr/0009); its commands register themselves.
const harnessBuild = true
