package engine

import (
	"context"
	"fmt"

	"github.com/mhersson/contextmatrix-setup/internal/configsync"
	"github.com/mhersson/contextmatrix-setup/internal/repos"
	"github.com/mhersson/contextmatrix-setup/internal/state"
)

type RepoStatus struct {
	Name      string
	Installed string
	Cached    string
}

type ServiceStatus struct {
	Name   string
	Active bool
	Unit   string
}

type Status struct {
	Repos      []RepoStatus
	Services   []ServiceStatus
	Ports      map[string]int
	Docker     bool
	DockerHint string
	Images     map[string]string
	Manager    string
}

func (e *Engine) Status(ctx context.Context) (Status, error) {
	st, _, err := state.Load(e.L.StateFile())
	if err != nil {
		return Status{}, err
	}

	e.useRecordedManager(st.ServiceManager)

	s := Status{Ports: map[string]int{}, Images: map[string]string{}, Docker: e.Host.Docker, DockerHint: e.Host.DockerHint, Manager: e.Services.Kind()}

	for _, repo := range repos.Apps {
		cached, _ := e.Git.Head(ctx, e.L.SrcDir(repo))
		s.Repos = append(s.Repos, RepoStatus{Name: repo, Installed: st.Repos[repo].Commit, Cached: cached})

		active, _ := e.Services.Active(ctx, repo)
		s.Services = append(s.Services, ServiceStatus{Name: repo, Active: active, Unit: e.Services.UnitPath(repo)})

		if tree, _, err := configsync.LoadFile(e.L.ConfigFor(repo)); err == nil {
			if v, ok := configsync.Get(tree, "port"); ok {
				if n, ok := v.(int); ok {
					s.Ports[repo] = n
				}
			}
		}
	}

	for family, img := range st.Images {
		s.Images[family] = img.Tag
	}

	return s, nil
}

func (e *Engine) PrintStatus(s Status) {
	e.logf("%-22s %-9s %-9s %-8s %s", "component", "installed", "cached", "port", "service")

	for i, r := range s.Repos {
		svc := s.Services[i]
		active := "stopped"

		if svc.Active {
			active = "running"
		}

		if s.Manager == "none" {
			active = "unmanaged"
		}

		e.logf("%-22s %-9s %-9s %-8d %s", r.Name, repos.Short(r.Installed), repos.Short(r.Cached), s.Ports[r.Name], active)
	}

	e.logf("docker: %v   service manager: %s", s.Docker, s.Manager)

	if !s.Docker && s.DockerHint != "" {
		e.logf("%s", s.DockerHint)
	}

	for family, tag := range s.Images {
		e.logf("%-22s %s", family, tag)
	}
}

func (e *Engine) Uninstall(ctx context.Context) error {
	st, _, err := state.Load(e.L.StateFile())
	if err != nil {
		return err
	}

	e.useRecordedManager(st.ServiceManager)

	if e.Services.Kind() == "none" {
		e.logf("no services were installed; nothing to remove")
		e.logf("kept: %s, %s, binaries in %s", e.L.ConfigDir, e.L.StateDir, e.Host.GoBin)

		return nil
	}

	for _, repo := range repos.Apps {
		if err := e.Services.Remove(ctx, repo); err != nil {
			return fmt.Errorf("remove %s service: %w", repo, err)
		}

		e.logf("%-22s service removed", repo)
	}

	e.logf("kept: %s, %s, binaries in %s", e.L.ConfigDir, e.L.StateDir, e.Host.GoBin)

	return nil
}
