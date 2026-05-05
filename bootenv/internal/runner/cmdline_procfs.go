//go:build !tinygo

package runner

import "github.com/prometheus/procfs"

func cmdlineFromProcFS(root string) ([]string, bool) {
	fs, err := procfs.NewFS(root)
	if err != nil {
		return nil, false
	}
	params, err := fs.CmdLine()
	if err != nil {
		return nil, false
	}
	return params, true
}
