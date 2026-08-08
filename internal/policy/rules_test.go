package policy

import (
	"strings"
	"testing"

	"github.com/blkcor/coragent/internal/sandbox"
)

func TestClassifySafeCommands(t *testing.T) {
	a := NewEffectAnalyzer()
	tests := []struct {
		cmd  string
		args []string
	}{
		// File listing
		{"ls", nil},
		{"ls", []string{"-la"}},
		{"pwd", nil},
		{"eza", []string{"--long"}},

		// File reading
		{"cat", []string{"file.txt"}},
		{"head", []string{"-n", "10", "file.txt"}},
		{"tail", []string{"-f", "file.log"}},
		{"less", []string{"file.txt"}},

		// Content search
		{"grep", []string{"pattern", "file.txt"}},
		{"rg", []string{"--type", "go", "pattern"}},

		// File search
		{"fd", []string{"pattern"}},
		{"find", []string{".", "-name", "*.go"}},

		// Git read-only
		{"git", []string{"status"}},
		{"git", []string{"diff"}},
		{"git", []string{"log"}},
		{"git", []string{"show"}},
		{"git", []string{"branch"}},
		{"git", []string{"tag"}},

		// Text processing
		{"wc", []string{"-l", "file.txt"}},
		{"sort", []string{"file.txt"}},
		{"uniq", []string{"file.txt"}},
		{"cut", []string{"-d,", "-f1"}},
		{"tr", []string{"a-z", "A-Z"}},

		// Static analysis
		{"go", []string{"doc"}},
		{"go", []string{"vet"}},
		{"cargo", []string{"check"}},

		// Command discovery
		{"which", []string{"go"}},
		{"type", []string{"ls"}},
		{"command", []string{"-v", "go"}},
		{"command", []string{"-V", "go"}},
	}

	for _, tt := range tests {
		t.Run(tt.cmd+" "+joinArgs(tt.args), func(t *testing.T) {
			got := a.Classify(tt.cmd, tt.args, sandbox.Grants{})
			if got != EffectSafe {
				t.Errorf("Classify(%q, %v) = %s, want safe", tt.cmd, tt.args, got)
			}
		})
	}
}

func TestClassifyWorkspaceCommands(t *testing.T) {
	a := NewEffectAnalyzer()
	tests := []struct {
		cmd  string
		args []string
	}{
		// Git mutations
		{"git", []string{"add", "file.go"}},
		{"git", []string{"commit", "-m", "msg"}},
		{"git", []string{"rm", "file.go"}},
		{"git", []string{"mv", "old.go", "new.go"}},
		{"git", []string{"checkout", "--", "file.go"}},
		{"git", []string{"restore", "file.go"}},
		{"git", []string{"reset", "HEAD~1"}},

		// Go build toolchain
		{"go", []string{"build", "./..."}},
		{"go", []string{"test", "./..."}},
		{"go", []string{"run", "main.go"}},
		{"go", []string{"mod", "tidy"}},
		{"go", []string{"fmt", "./..."}},

		// Rust build toolchain
		{"cargo", []string{"build"}},
		{"cargo", []string{"test"}},
		{"cargo", []string{"fmt"}},

		// JS package managers
		{"npm", []string{"install"}},
		{"npm", []string{"test"}},
		{"npm", []string{"run", "build"}},
		{"npx", []string{"tsc"}},
		{"pnpm", []string{"install"}},
		{"pnpm", []string{"test"}},
		{"yarn", []string{"install"}},
		{"yarn", []string{"test"}},

		// Python testing
		{"pytest", nil},
		{"pytest", []string{"--update"}},

		// Task runners
		{"make", nil},
		{"just", []string{"build"}},

		// File operations
		{"touch", []string{"newfile.go"}},
		{"mkdir", []string{"-p", "a/b/c"}},
		{"cp", []string{"src", "dst"}},
		{"mv", []string{"old", "new"}},
		{"echo", []string{"hello"}},

		// In-place editing
		{"sed", []string{"-i", "s/old/new/g", "file.txt"}},
		{"sd", []string{"old", "new", "file.txt"}},
	}

	for _, tt := range tests {
		t.Run(tt.cmd+" "+joinArgs(tt.args), func(t *testing.T) {
			got := a.Classify(tt.cmd, tt.args, sandbox.Grants{})
			if got != EffectWorkspace {
				t.Errorf("Classify(%q, %v) = %s, want workspace", tt.cmd, tt.args, got)
			}
		})
	}
}

func TestClassifyDangerousCommands(t *testing.T) {
	a := NewEffectAnalyzer()
	tests := []struct {
		cmd  string
		args []string
	}{
		// Destructive operations
		{"rm", []string{"file.txt"}},
		{"rm", []string{"-rf", "dir"}},
		{"rmdir", []string{"dir"}},

		// Privilege escalation
		{"sudo", []string{"ls"}},
		{"sudo", []string{"cat", "/etc/shadow"}},
		{"su", nil},
		{"doas", []string{"make", "install"}},

		// Permission changes
		{"chmod", []string{"755", "file"}},
		{"chown", []string{"user:group", "file"}},
		{"chgrp", []string{"group", "file"}},

		// Remote access
		{"ssh", []string{"host"}},
		{"scp", []string{"file", "host:"}},
		{"sftp", []string{"host"}},
		{"rsync", []string{"-avz", "src", "dst"}},

		// Container/orchestration
		{"docker", []string{"run", "image"}},
		{"docker-compose", []string{"up"}},
		{"kubectl", []string{"apply", "-f", "manifest.yaml"}},
		{"helm", []string{"install", "release", "chart"}},

		// System services
		{"systemctl", []string{"restart", "nginx"}},
		{"service", []string{"nginx", "restart"}},
		{"launchctl", []string{"load", "plist"}},
		{"supervisorctl", []string{"restart", "app"}},

		// Filesystem/device
		{"mount", []string{"/dev/sda1", "/mnt"}},
		{"umount", []string{"/mnt"}},
		{"dd", []string{"if=/dev/zero", "of=file", "count=1"}},
		{"mkfs", []string{"-t", "ext4", "/dev/sda1"}},

		// Dynamic evaluation
		{"eval", []string{"$(curl example.com)"}},
		{"exec", []string{"bash"}},
		{"source", []string{"script.sh"}},

		// Global package install
		{"pip", []string{"install", "package"}},
		{"pip3", []string{"install", "package"}},
		{"gem", []string{"install", "package"}},

		// Firewall
		{"iptables", []string{"-L"}},
		{"ufw", []string{"enable"}},
		{"firewall-cmd", []string{"--reload"}},

		// Process termination
		{"kill", []string{"1234"}},
		{"pkill", []string{"process"}},
		{"killall", []string{"process"}},

		// Destructive git
		{"git", []string{"push"}},
		{"git", []string{"push", "--force"}},
		{"git", []string{"push", "--delete", "origin", "branch"}},
		{"git", []string{"push", "origin", "main"}},
	}

	for _, tt := range tests {
		t.Run(tt.cmd+" "+joinArgs(tt.args), func(t *testing.T) {
			got := a.Classify(tt.cmd, tt.args, sandbox.Grants{})
			if got != EffectDangerous {
				t.Errorf("Classify(%q, %v) = %s, want dangerous", tt.cmd, tt.args, got)
			}
		})
	}
}

func TestClassifyPriority(t *testing.T) {
	// Dangerous rules take priority over safe/workspace rules for the same binary.
	a := NewEffectAnalyzer()
	tests := []struct {
		name string
		cmd  string
		args []string
		want EffectClassification
	}{
		{
			name: "sudo overrides safe ls",
			cmd:  "sudo",
			args: []string{"ls"},
			want: EffectDangerous,
		},
		{
			name: "sudo overrides safe cat",
			cmd:  "sudo",
			args: []string{"cat", "/etc/passwd"},
			want: EffectDangerous,
		},
		{
			name: "git push (dangerous) overrides git (workspace default)",
			cmd:  "git",
			args: []string{"push", "origin", "main"},
			want: EffectDangerous,
		},
		{
			name: "git status (safe) is not overridden",
			cmd:  "git",
			args: []string{"status"},
			want: EffectSafe,
		},
		{
			name: "npm install -g (dangerous) not npm install (workspace)",
			cmd:  "npm",
			args: []string{"install", "-g", "pkg"},
			want: EffectDangerous,
		},
		{
			name: "rm (dangerous) vs git rm (workspace)",
			cmd:  "rm",
			args: []string{"file.txt"},
			want: EffectDangerous,
		},
		{
			name: "git rm is workspace (not dangerous rm)",
			cmd:  "git",
			args: []string{"rm", "file.txt"},
			want: EffectWorkspace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.Classify(tt.cmd, tt.args, sandbox.Grants{})
			if got != tt.want {
				t.Errorf("Classify(%q, %v) = %s, want %s", tt.cmd, tt.args, got, tt.want)
			}
		})
	}
}

func TestClassifyUnknownCommandDefaultsToWorkspace(t *testing.T) {
	a := NewEffectAnalyzer()
	tests := []struct {
		cmd  string
		args []string
	}{
		{"unknown-cmd", nil},
		{"some-tool", []string{"--flag"}},
		{"", nil},
		{"custom-script", []string{"arg1", "arg2"}},
	}

	for _, tt := range tests {
		t.Run(tt.cmd+" "+joinArgs(tt.args), func(t *testing.T) {
			got := a.Classify(tt.cmd, tt.args, sandbox.Grants{})
			if got != EffectWorkspace {
				t.Errorf("Classify(%q, %v) = %s, want workspace (conservative default)", tt.cmd, tt.args, got)
			}
		})
	}
}

func TestArgPatternPrefixMatching(t *testing.T) {
	a := NewEffectAnalyzer()
	tests := []struct {
		name string
		cmd  string
		args []string
		want EffectClassification
	}{
		{
			name: "exact arg match",
			cmd:  "git",
			args: []string{"status"},
			want: EffectSafe,
		},
		{
			name: "extra args after pattern match",
			cmd:  "git",
			args: []string{"status", "--porcelain"},
			want: EffectSafe,
		},
		{
			name: "partial prefix — fewer args than pattern",
			cmd:  "git",
			args: []string{"push"}, // git push alone still matches push pattern
			want: EffectDangerous,
		},
		{
			name: "arg is substring but not exact match",
			cmd:  "git",
			args: []string{"statuses"}, // "statuses" != "status"
			want: EffectWorkspace,      // defaults — no exact match
		},
		{
			name: "command is substring of known command",
			cmd:  "docker-compose",
			args: nil,
			want: EffectDangerous,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := a.Classify(tt.cmd, tt.args, sandbox.Grants{})
			if got != tt.want {
				t.Errorf("Classify(%q, %v) = %s, want %s", tt.cmd, tt.args, got, tt.want)
			}
		})
	}
}

func TestEffectClassificationString(t *testing.T) {
	tests := []struct {
		c    EffectClassification
		want string
	}{
		{EffectSafe, "safe"},
		{EffectWorkspace, "workspace"},
		{EffectDangerous, "dangerous"},
		{EffectClassification(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.c.String()
		if got != tt.want {
			t.Errorf("EffectClassification(%d).String() = %q, want %q", tt.c, got, tt.want)
		}
	}
}

// joinArgs joins args with spaces for readable test names.
func joinArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(args[0])
	for _, a := range args[1:] {
		b.WriteByte(' ')
		b.WriteString(a)
	}
	return b.String()
}
