package engine

import (
	"github.com/mhersson/contextmatrix-setup/internal/host"
	"github.com/mhersson/contextmatrix-setup/internal/ui"
)

// searchNotice is the warning block for a host whose service PATH lacks rg:
// the architect's grep tool fails in every code stage until rg is there.
// The installer never installs it, and the architect still runs. Empty when
// rg is on the service PATH.
func searchNotice(info host.Info) []string {
	s := info.Search
	if s.RG != "" {
		return nil
	}

	lines := []string{
		"!!! ripgrep (rg) is not on the service PATH: " + info.ServicePath(),
		"!!! the architect's grep tool fails in every code stage until it is; the architect still runs",
	}

	if s.FD == "" {
		lines = append(lines, "!!! fd is missing as well, so the glob tool fails too")
	}

	if s.ShellRG != "" {
		lines = append(lines, "!!! the service cannot see "+s.ShellRG+": ln -s "+s.ShellRG+" "+info.GoBin+"/rg")
	} else {
		lines = append(lines, "!!! install it: "+rgInstallHint(info.OS))
	}

	return append(lines, "!!! no restart is needed: the next stage run finds it")
}

func rgInstallHint(goos string) string {
	if goos == "darwin" {
		return "brew install ripgrep"
	}

	return "your package manager's ripgrep package (apt install ripgrep, dnf install ripgrep, pacman -S ripgrep)"
}

func (e *Engine) printSearchNotice() {
	for _, line := range searchNotice(e.Host) {
		e.logf("%s", line)
	}
}

func (e *Engine) showSearchNotice(c *ui.Console) {
	for _, line := range searchNotice(e.Host) {
		c.Println(c.Theme().Caution(line))
	}
}
