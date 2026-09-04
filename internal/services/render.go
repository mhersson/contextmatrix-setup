package services

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
)

func sortedEnv(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

func RenderSystemd(s Service) []byte {
	var b bytes.Buffer

	fmt.Fprintf(&b, "[Unit]\nDescription=%s\n", s.Description)
	b.WriteString("# After=docker.service is omitted on purpose: a user manager cannot order\n")
	b.WriteString("# against system units. Restart=on-failure with backoff rides out a\n")
	b.WriteString("# not-yet-ready dockerd at boot.\n\n")

	b.WriteString("[Service]\nType=simple\n")
	fmt.Fprintf(&b, "WorkingDirectory=%s\n", s.WorkingDir)
	fmt.Fprintf(&b, "ExecStart=%s %s\n", s.Binary, strings.Join(s.Args, " "))

	for _, k := range sortedEnv(s.Env) {
		fmt.Fprintf(&b, "Environment=%s=%s\n", k, s.Env[k])
	}

	b.WriteString("KillMode=mixed\nTimeoutStopSec=60\n\n")

	b.WriteString("# Sandboxing\n")
	b.WriteString("NoNewPrivileges=yes\nProtectSystem=strict\nProtectHome=read-only\n")

	paths := make([]string, len(s.ReadWritePaths))
	for i, p := range s.ReadWritePaths {
		paths[i] = "-" + p
	}

	fmt.Fprintf(&b, "ReadWritePaths=%s\n", strings.Join(paths, " "))
	b.WriteString("PrivateTmp=yes\nPrivateDevices=yes\nProtectKernelTunables=yes\nProtectKernelModules=yes\n")
	b.WriteString("ProtectControlGroups=yes\nRestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX\nLockPersonality=yes\n")
	b.WriteString("MemoryDenyWriteExecute=yes\nRestrictRealtime=yes\nRestrictNamespaces=yes\nSystemCallArchitectures=native\n")
	b.WriteString("SystemCallFilter=@system-service\nSystemCallFilter=~@privileged @resources\n\n")

	b.WriteString("# Resource limits\nMemoryMax=2G\nTasksMax=1024\nLimitNOFILE=65536\n\n")
	b.WriteString("# Restart with backoff\nRestart=on-failure\nRestartSec=10\nRestartSteps=5\nRestartMaxDelaySec=300\n\n")
	b.WriteString("[Install]\nWantedBy=default.target\n")

	return b.Bytes()
}

func RenderLaunchd(s Service) []byte {
	var b bytes.Buffer

	esc := func(v string) string {
		var out bytes.Buffer

		_ = xml.EscapeText(&out, []byte(v))

		return out.String()
	}

	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">` + "\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	fmt.Fprintf(&b, "    <key>Label</key>\n    <string>%s</string>\n\n", Label(s.Name))

	b.WriteString("    <key>ProgramArguments</key>\n    <array>\n")
	fmt.Fprintf(&b, "        <string>%s</string>\n", esc(s.Binary))

	for _, a := range s.Args {
		fmt.Fprintf(&b, "        <string>%s</string>\n", esc(a))
	}

	b.WriteString("    </array>\n\n")

	b.WriteString("    <key>EnvironmentVariables</key>\n    <dict>\n")

	for _, k := range sortedEnv(s.Env) {
		fmt.Fprintf(&b, "        <key>%s</key>\n        <string>%s</string>\n", esc(k), esc(s.Env[k]))
	}

	b.WriteString("    </dict>\n\n")
	fmt.Fprintf(&b, "    <key>WorkingDirectory</key>\n    <string>%s</string>\n\n", esc(s.WorkingDir))
	b.WriteString("    <key>RunAtLoad</key>\n    <true/>\n\n")
	b.WriteString("    <key>KeepAlive</key>\n    <dict>\n        <key>SuccessfulExit</key>\n        <false/>\n        <key>Crashed</key>\n        <true/>\n    </dict>\n\n")
	b.WriteString("    <key>ThrottleInterval</key>\n    <integer>10</integer>\n\n")
	b.WriteString("    <key>ProcessType</key>\n    <string>Interactive</string>\n\n")
	fmt.Fprintf(&b, "    <key>StandardOutPath</key>\n    <string>%s</string>\n\n", esc(s.LogFile))
	fmt.Fprintf(&b, "    <key>StandardErrorPath</key>\n    <string>%s</string>\n", esc(s.LogFile))
	b.WriteString("</dict>\n</plist>\n")

	return b.Bytes()
}
