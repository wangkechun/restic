//go:build !selfupdate

package app

import (
	"github.com/restic/restic/internal/global"
	"github.com/spf13/cobra"
)

func registerSelfUpdateCommand(_ *cobra.Command, _ *global.Options) {
	// No commands to register in non-selfupdate mode
}
