package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func agentService() Service {
	return Service{
		Name:           Agent,
		Description:    "ContextMatrix Agent (task backend)",
		Binary:         "/home/u/go/bin/contextmatrix-agent",
		Args:           []string{"serve", "--config", "/home/u/.config/contextmatrix/agent.yaml"},
		Env:            map[string]string{"PATH": "/usr/local/bin:/usr/bin:/bin", "HOME": "/home/u"},
		WorkingDir:     "/home/u",
		ReadWritePaths: []string{"/home/u/.contextmatrix"},
		LogFile:        "/home/u/Library/Logs/contextmatrix/contextmatrix-agent.log",
	}
}

const wantSystemd = `[Unit]
Description=ContextMatrix Agent (task backend)
# After=docker.service is omitted on purpose: a user manager cannot order
# against system units. Restart=on-failure with backoff rides out a
# not-yet-ready dockerd at boot.

[Service]
Type=simple
WorkingDirectory=/home/u
ExecStart=/home/u/go/bin/contextmatrix-agent serve --config /home/u/.config/contextmatrix/agent.yaml
Environment=HOME=/home/u
Environment=PATH=/usr/local/bin:/usr/bin:/bin
KillMode=mixed
TimeoutStopSec=60

# Sandboxing
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=-/home/u/.contextmatrix
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
LockPersonality=yes
MemoryDenyWriteExecute=yes
RestrictRealtime=yes
RestrictNamespaces=yes
SystemCallArchitectures=native
SystemCallFilter=@system-service
SystemCallFilter=~@privileged @resources

# Resource limits
MemoryMax=2G
TasksMax=1024
LimitNOFILE=65536

# Restart with backoff
Restart=on-failure
RestartSec=10
RestartSteps=5
RestartMaxDelaySec=300

[Install]
WantedBy=default.target
`

const wantLaunchd = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.github.mhersson.contextmatrix-agent</string>

    <key>ProgramArguments</key>
    <array>
        <string>/home/u/go/bin/contextmatrix-agent</string>
        <string>serve</string>
        <string>--config</string>
        <string>/home/u/.config/contextmatrix/agent.yaml</string>
    </array>

    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>/home/u</string>
        <key>PATH</key>
        <string>/usr/local/bin:/usr/bin:/bin</string>
    </dict>

    <key>WorkingDirectory</key>
    <string>/home/u</string>

    <key>RunAtLoad</key>
    <true/>

    <key>KeepAlive</key>
    <dict>
        <key>SuccessfulExit</key>
        <false/>
        <key>Crashed</key>
        <true/>
    </dict>

    <key>ThrottleInterval</key>
    <integer>10</integer>

    <key>ProcessType</key>
    <string>Interactive</string>

    <key>StandardOutPath</key>
    <string>/home/u/Library/Logs/contextmatrix/contextmatrix-agent.log</string>

    <key>StandardErrorPath</key>
    <string>/home/u/Library/Logs/contextmatrix/contextmatrix-agent.log</string>
</dict>
</plist>
`

func TestRenderSystemd(t *testing.T) {
	assert.Equal(t, wantSystemd, string(RenderSystemd(agentService())))
}

func TestRenderLaunchd(t *testing.T) {
	assert.Equal(t, wantLaunchd, string(RenderLaunchd(agentService())))
}

func TestRenderEscapesPlistText(t *testing.T) {
	s := agentService()
	s.Env["DOCKER_HOST"] = "unix:///a&b"

	out := string(RenderLaunchd(s))
	assert.Contains(t, out, "<string>unix:///a&amp;b</string>")
}

func TestLabel(t *testing.T) {
	assert.Equal(t, "com.github.mhersson.contextmatrix", Label(Server))
}
