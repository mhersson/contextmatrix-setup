package engine

import (
	"github.com/mhersson/contextmatrix-setup/internal/configsync"
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
	for _, line := range e.hostNotices() {
		e.logf("%s", line)
	}
}

func (e *Engine) showSearchNotice(c *ui.Console) {
	for _, line := range e.hostNotices() {
		c.Println(c.Theme().Caution(line))
	}
}

func (e *Engine) hostNotices() []string {
	return append(searchNotice(e.Host), claudeNotice(e.Host, e.claudeTokenSet())...)
}

// claudeNotice is the warning block for a host with a Claude Code token but
// no claude binary: blueprints cannot run on the subscription until it is
// installed. The installer never installs it, and the architect still runs.
func claudeNotice(info host.Info, tokenSet bool) []string {
	c := info.Claude
	if !tokenSet || c.OnServicePath != "" || c.Native != "" {
		return nil
	}

	lines := []string{"!!! claude (Claude Code) was not found where the architect can run it: blueprints cannot run on the Claude Code subscription until it is"}

	if c.ShellPath != "" {
		lines = append(lines, "!!! the architect cannot see "+c.ShellPath+": ln -s "+c.ShellPath+" "+info.GoBin+"/claude")
	}

	return append(lines, "!!! the architect still runs; after adding claude, run contextmatrix-setup update so its unit can find it")
}

// claudeTokenSet reports whether server.yaml holds a Claude Code token.
func (e *Engine) claudeTokenSet() bool {
	v, _ := configsync.Get(e.loadTrees().Server, claudeCodeTokenKey)
	s, _ := v.(string)

	return s != ""
}
