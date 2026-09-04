package configsync

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/mhersson/contextmatrix-setup/internal/run"
)

func Validate(ctx context.Context, r run.Runner, binary, file string) error {
	res, err := r.Run(ctx, run.Cmd{Name: binary, Args: []string{"config", "validate", file}})
	if err != nil {
		return err
	}

	if res.ExitCode != 0 {
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(res.Stdout)
		}

		return fmt.Errorf("%s: %s", file, strings.TrimPrefix(msg, "error: "))
	}

	return nil
}

// backendCheck names one server.yaml backend and the sibling file whose
// port and contextmatrix_url must agree with it.
type backendCheck struct {
	file    string
	backend string
	tree    Tree
}

// CrossCheck reports value pairs that must agree across the three files.
// It never edits; both locations are named so the user can pick which one
// to fix.
func CrossCheck(server, agent, chat Tree) []string {
	var out []string

	check := func(aFile, aPath string, aTree Tree, bFile, bPath string, bTree Tree) {
		a, _ := Get(aTree, aPath)
		b, _ := Get(bTree, bPath)

		if fmt.Sprint(a) != fmt.Sprint(b) {
			out = append(out, fmt.Sprintf("%s %s differs from %s %s", aFile, aPath, bFile, bPath))
		}
	}

	check("agent.yaml", "api_key", agent, "server.yaml", "backends.agent.api_key", server)
	check("chat.yaml", "api_key", chat, "server.yaml", "backends.chat.api_key", server)
	check("agent.yaml", "mcp_api_key", agent, "server.yaml", "mcp_api_key", server)

	serverPort, _ := Get(server, "port")

	for _, pair := range []backendCheck{
		{file: "agent.yaml", backend: "agent", tree: agent},
		{file: "chat.yaml", backend: "chat", tree: chat},
	} {
		if u, ok := Get(server, "backends."+pair.backend+".url"); ok {
			if p, _ := Get(pair.tree, "port"); portOf(u) != fmt.Sprint(p) {
				out = append(out, fmt.Sprintf("%s port %v differs from server.yaml backends.%s.url %v", pair.file, p, pair.backend, u))
			}
		}

		if u, ok := Get(pair.tree, "contextmatrix_url"); ok && portOf(u) != fmt.Sprint(serverPort) {
			out = append(out, fmt.Sprintf("%s contextmatrix_url %v does not use server.yaml port %v", pair.file, u, serverPort))
		}
	}

	return out
}

func portOf(v any) string {
	u, err := url.Parse(fmt.Sprint(v))
	if err != nil {
		return ""
	}

	if p := u.Port(); p != "" {
		return p
	}

	if u.Scheme == "https" {
		return strconv.Itoa(443)
	}

	return strconv.Itoa(80)
}
