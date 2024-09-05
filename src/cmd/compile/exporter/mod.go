package exporter

import (
	"cmd/compile/internal/base"
)

// NoUnusedErrorsFlagOn re-exports the internal base.Flag.NoUnusedErrors
func NoUnusedErrorsFlagOn() bool {
	return base.Flag.NoUnusedErrors
}
