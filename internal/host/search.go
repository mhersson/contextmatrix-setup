package host

import (
	"os"
	"path/filepath"
)

// SearchTools is what the architect's grep and glob tools will find: grep
// execs rg, glob takes fd, fdfind or rg. RG and FD are resolved on the
// service PATH, the only PATH a unit has; ShellRG is an rg the installer's
// own PATH finds where the service will not.
type SearchTools struct {
	RG      string
	FD      string
	ShellRG string
}

// ServicePath is the PATH every unit runs with. It is fixed rather than
// copied from the installer's environment: the unit text decides restarts
// byte for byte, and the shell an update runs from must not change it. The
// architect also gets ~/.local/bin when the native claude is there.
func (i Info) ServicePath() string {
	if i.OS == "darwin" {
		return i.GoBin + ":/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
	}

	return i.GoBin + ":/usr/local/bin:/usr/bin:/bin"
}

// ProbeSearchTools resolves the search tools on servicePath the way the
// service will, and reports an rg only lookPath, the installer's own PATH,
// can see.
func ProbeSearchTools(servicePath string, isExec func(string) bool, lookPath func(string) (string, bool)) SearchTools {
	s := SearchTools{RG: findOn(servicePath, "rg", isExec)}

	for _, name := range []string{"fd", "fdfind"} {
		if p := findOn(servicePath, name, isExec); p != "" {
			s.FD = p

			break
		}
	}

	if s.RG == "" {
		if p, ok := lookPath("rg"); ok {
			s.ShellRG = p
		}
	}

	return s
}

func findOn(path, name string, isExec func(string) bool) string {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}

		if p := filepath.Join(dir, name); isExec(p) {
			return p
		}
	}

	return ""
}

func isExecutable(p string) bool {
	info, err := os.Stat(p)

	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
}

// ClaudeBinary is where Claude Code's claude binary is: on the service PATH,
// in the native installer's ~/.local/bin, or only on the installer's own
// PATH, which no unit sees.
type ClaudeBinary struct {
	OnServicePath string
	Native        string
	ShellPath     string
}

// ProbeClaude resolves claude on servicePath, then at the native installer's
// fixed location, so the result never depends on the shell an update runs
// from; lookPath only names a claude no unit could see.
func ProbeClaude(servicePath, home string, isExec func(string) bool, lookPath func(string) (string, bool)) ClaudeBinary {
	if p := findOn(servicePath, "claude", isExec); p != "" {
		return ClaudeBinary{OnServicePath: p}
	}

	if p := filepath.Join(home, ".local", "bin", "claude"); home != "" && isExec(p) {
		return ClaudeBinary{Native: p}
	}

	if p, ok := lookPath("claude"); ok {
		return ClaudeBinary{ShellPath: p}
	}

	return ClaudeBinary{}
}
