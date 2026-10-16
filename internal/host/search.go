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
// byte for byte, and the shell an update runs from must not change it.
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
