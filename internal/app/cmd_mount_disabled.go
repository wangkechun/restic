//go:build !darwin && !freebsd && !linux

package app

import (
	"github.com/restic/restic/internal/global"
	"github.com/spf13/cobra"
)

func registerMountCommand(_ *cobra.Command, _ *global.Options) {
	// Mount command not supported on these platforms
}
