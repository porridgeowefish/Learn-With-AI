package workspaceservice

import (
	"errors"
	"github.com/xmz14/lll/backend-go/internal/platform/config"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
)

var errReopenWorkspace = errors.New("reopen relocated workspace")

// OpenRuntime finishes interrupted transactions before any module can write.
func OpenRuntime(opts config.Options, defaultRoot string) (config.Config, *paths.Lease, error) {
	for {
		cfg, lease, err := config.OpenRuntime(opts, defaultRoot, func(root string) error { _, _, err := entries(root); return err })
		if err != nil {
			return cfg, nil, err
		}
		err = RecoverMove(lease.Root)
		if err == nil {
			err = RecoverImport(lease.Root)
		}
		if err == nil {
			return cfg, lease, nil
		}
		lease.Close()
		if !errors.Is(err, errReopenWorkspace) {
			return cfg, nil, err
		}
	}
}
