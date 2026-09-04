// Package host detects what the machine offers: OS, tools, docker, service
// manager, and where go install puts binaries.
package host

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mhersson/contextmatrix-setup/internal/run"
)

type Info struct {
	OS             string
	Hostname       string
	UID            int
	GoBin          string
	Tools          map[string]string
	Docker         bool
	ServiceManager string
}

//nolint:gochecknoglobals // Required is part of the exported contract; the wizard reads it.
var Required = []string{"git", "go", "make", "node", "npm"}

//nolint:gochecknoglobals // internal detection list, not part of the exported contract.
var optional = []string{"docker", "systemctl", "launchctl", "xdg-open", "open"}

func Detect(ctx context.Context, r run.Runner, goos string, getenv func(string) string) (Info, error) {
	info := Info{OS: goos, Tools: map[string]string{}, UID: os.Getuid()}

	all := make([]string, 0, len(Required)+len(optional))
	all = append(all, Required...)
	all = append(all, optional...)

	for _, name := range all {
		if p, ok := r.LookPath(name); ok {
			info.Tools[name] = p
		}
	}

	if _, ok := info.Tools["go"]; !ok {
		return info, errors.New("go is required: install Go 1.26 or newer and rerun")
	}

	goBin, err := goBinDir(ctx, r, getenv)
	if err != nil {
		return info, err
	}

	info.GoBin = goBin

	if _, ok := info.Tools["docker"]; ok {
		res, err := r.Run(ctx, run.Cmd{Name: "docker", Args: []string{"info", "--format", "{{.ServerVersion}}"}})
		info.Docker = err == nil && res.ExitCode == 0
	}

	info.ServiceManager = detectServiceManager(ctx, r, goos, info.Tools)

	if h, err := os.Hostname(); err == nil {
		info.Hostname = h
	}

	return info, nil
}

func goBinDir(ctx context.Context, r run.Runner, getenv func(string) string) (string, error) {
	if v := getenv("GOBIN"); v != "" {
		return v, nil
	}

	res, err := r.Run(ctx, run.Cmd{Name: "go", Args: []string{"env", "GOBIN"}})
	if err != nil {
		return "", err
	}

	if v := strings.TrimSpace(res.Stdout); v != "" {
		return v, nil
	}

	res, err = r.Run(ctx, run.Cmd{Name: "go", Args: []string{"env", "GOPATH"}})
	if err != nil {
		return "", err
	}

	gopath := strings.TrimSpace(res.Stdout)
	if gopath == "" {
		return "", errors.New("go env GOPATH is empty")
	}

	// GOPATH may be a list; go install uses the first entry.
	return filepath.Join(strings.Split(gopath, string(os.PathListSeparator))[0], "bin"), nil
}

func detectServiceManager(ctx context.Context, r run.Runner, goos string, tools map[string]string) string {
	switch goos {
	case "darwin":
		if _, ok := tools["launchctl"]; ok {
			return "launchd"
		}

	case "linux":
		if _, ok := tools["systemctl"]; !ok {
			return "none"
		}

		res, err := r.Run(ctx, run.Cmd{Name: "systemctl", Args: []string{"--user", "is-system-running"}})
		if err == nil {
			state := strings.TrimSpace(res.Stdout)
			// degraded means some unit failed, the manager itself is usable.
			if state == "running" || state == "degraded" {
				return "systemd"
			}
		}
	}

	return "none"
}

// Missing returns required tools not found, sorted.
func (i Info) Missing() []string {
	var out []string

	for _, name := range Required {
		if _, ok := i.Tools[name]; !ok {
			out = append(out, name)
		}
	}

	sort.Strings(out)

	return out
}

// Binary returns the path go install would place name at.
func (i Info) Binary(name string) string {
	return filepath.Join(i.GoBin, name)
}

// PortFree reports whether a listener can bind the port on all interfaces,
// which is how the server binds.
func PortFree(port int) bool {
	var lc net.ListenConfig

	ln, err := lc.Listen(context.Background(), "tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}

	_ = ln.Close()

	return true
}

func OpenBrowser(ctx context.Context, r run.Runner, goos, url string) error {
	name := "xdg-open"
	if goos == "darwin" {
		name = "open"
	}

	res, err := r.Run(ctx, run.Cmd{Name: name, Args: []string{url}})
	if err != nil {
		return err
	}

	if res.ExitCode != 0 {
		return fmt.Errorf("%s exited %d: %s", name, res.ExitCode, strings.TrimSpace(res.Stderr))
	}

	return nil
}
