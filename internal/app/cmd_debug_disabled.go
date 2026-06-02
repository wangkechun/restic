//go:build !debug

package app

import (
	"github.com/restic/restic/internal/global"
	"github.com/spf13/cobra"
)

func registerDebugCommand(_ *cobra.Command, _ *global.Options) {
	// No commands to register in non-debug mode
}
