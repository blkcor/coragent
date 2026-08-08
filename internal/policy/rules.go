package policy

import "path/filepath"

// CommandPattern defines a matching rule for a command and its leading arguments.
// An empty ArgPatterns matches the command regardless of its arguments.
//
// Matching uses exact binary name + prefix arg comparison with simple glob
// support via filepath.Match. For example:
//
//	{Command: "git", ArgPatterns: []string{"push"}}
//
// matches "git push", "git push --force", "git push origin main", but not
// "git status" or "git".
//
// Classification determines the effect when this pattern matches. Rules are
// ordered in allRules so that dangerous patterns match before workspace,
// which match before safe — priority is encoded by position.
type CommandPattern struct {
	Command        string
	ArgPatterns    []string // prefix match with glob support; empty = match any args
	Classification EffectClassification
}

// matchRule returns true if cmd and args match the pattern.
func matchRule(cmd string, args []string, rule CommandPattern) bool {
	if rule.Command != cmd {
		return false
	}
	if len(rule.ArgPatterns) == 0 {
		return true
	}
	if len(args) < len(rule.ArgPatterns) {
		return false
	}
	for i, p := range rule.ArgPatterns {
		match, err := filepath.Match(p, args[i])
		if err != nil || !match {
			return false
		}
	}
	return true
}

// allRules defines the complete rule table. Rules are checked in order;
// the first match wins. Dangerous rules come first, then workspace, then
// safe — so a dangerous match always takes priority and cannot be
// overridden by a lower-tier rule for the same command.
var allRules = []CommandPattern{
	// === Dangerous: destructive / privilege / remote / system ===

	// Destructive file operations
	{Command: "rm", Classification: EffectDangerous},
	{Command: "rmdir", Classification: EffectDangerous},

	// Privilege escalation
	{Command: "sudo", Classification: EffectDangerous},
	{Command: "su", Classification: EffectDangerous},
	{Command: "doas", Classification: EffectDangerous},

	// Permission changes
	{Command: "chmod", Classification: EffectDangerous},
	{Command: "chown", Classification: EffectDangerous},
	{Command: "chgrp", Classification: EffectDangerous},

	// Remote access and data exfiltration
	{Command: "ssh", Classification: EffectDangerous},
	{Command: "scp", Classification: EffectDangerous},
	{Command: "sftp", Classification: EffectDangerous},
	{Command: "rsync", Classification: EffectDangerous},

	// Container and cluster orchestration
	{Command: "docker", Classification: EffectDangerous},
	{Command: "docker-compose", Classification: EffectDangerous},
	{Command: "kubectl", Classification: EffectDangerous},
	{Command: "helm", Classification: EffectDangerous},

	// System service management
	{Command: "systemctl", Classification: EffectDangerous},
	{Command: "service", Classification: EffectDangerous},
	{Command: "launchctl", Classification: EffectDangerous},
	{Command: "supervisorctl", Classification: EffectDangerous},

	// Filesystem and device operations
	{Command: "mount", Classification: EffectDangerous},
	{Command: "umount", Classification: EffectDangerous},
	{Command: "dd", Classification: EffectDangerous},
	{Command: "mkfs", Classification: EffectDangerous},

	// Code execution / dynamic evaluation
	{Command: "eval", Classification: EffectDangerous},
	{Command: "exec", Classification: EffectDangerous},
	{Command: "source", Classification: EffectDangerous},

	// Global package installation (escaping workspace)
	{Command: "pip", ArgPatterns: []string{"install"}, Classification: EffectDangerous},
	{Command: "pip3", ArgPatterns: []string{"install"}, Classification: EffectDangerous},
	{Command: "npm", ArgPatterns: []string{"install", "-g"}, Classification: EffectDangerous},
	{Command: "gem", ArgPatterns: []string{"install"}, Classification: EffectDangerous},

	// Firewall manipulation
	{Command: "iptables", Classification: EffectDangerous},
	{Command: "ufw", Classification: EffectDangerous},
	{Command: "firewall-cmd", Classification: EffectDangerous},

	// Process termination
	{Command: "kill", Classification: EffectDangerous},
	{Command: "pkill", Classification: EffectDangerous},
	{Command: "killall", Classification: EffectDangerous},

	// Destructive git operations
	{Command: "git", ArgPatterns: []string{"push"}, Classification: EffectDangerous},

	// === Workspace: project-local mutations ===

	// Git mutations
	{Command: "git", ArgPatterns: []string{"add"}, Classification: EffectWorkspace},
	{Command: "git", ArgPatterns: []string{"commit"}, Classification: EffectWorkspace},
	{Command: "git", ArgPatterns: []string{"rm"}, Classification: EffectWorkspace},
	{Command: "git", ArgPatterns: []string{"mv"}, Classification: EffectWorkspace},
	{Command: "git", ArgPatterns: []string{"checkout"}, Classification: EffectWorkspace},
	{Command: "git", ArgPatterns: []string{"restore"}, Classification: EffectWorkspace},
	{Command: "git", ArgPatterns: []string{"reset"}, Classification: EffectWorkspace},

	// Go build toolchain
	{Command: "go", ArgPatterns: []string{"build"}, Classification: EffectWorkspace},
	{Command: "go", ArgPatterns: []string{"test"}, Classification: EffectWorkspace},
	{Command: "go", ArgPatterns: []string{"run"}, Classification: EffectWorkspace},
	{Command: "go", ArgPatterns: []string{"mod"}, Classification: EffectWorkspace},
	{Command: "go", ArgPatterns: []string{"fmt"}, Classification: EffectWorkspace},

	// Rust build toolchain
	{Command: "cargo", ArgPatterns: []string{"build"}, Classification: EffectWorkspace},
	{Command: "cargo", ArgPatterns: []string{"test"}, Classification: EffectWorkspace},
	{Command: "cargo", ArgPatterns: []string{"fmt"}, Classification: EffectWorkspace},

	// JavaScript/Node package managers
	{Command: "npm", ArgPatterns: []string{"install"}, Classification: EffectWorkspace},
	{Command: "npm", ArgPatterns: []string{"test"}, Classification: EffectWorkspace},
	{Command: "npm", ArgPatterns: []string{"run"}, Classification: EffectWorkspace},
	{Command: "npx", Classification: EffectWorkspace},
	{Command: "pnpm", ArgPatterns: []string{"install"}, Classification: EffectWorkspace},
	{Command: "pnpm", ArgPatterns: []string{"test"}, Classification: EffectWorkspace},
	{Command: "yarn", ArgPatterns: []string{"install"}, Classification: EffectWorkspace},
	{Command: "yarn", ArgPatterns: []string{"test"}, Classification: EffectWorkspace},

	// Python testing
	{Command: "pytest", Classification: EffectWorkspace},

	// Task runners
	{Command: "make", Classification: EffectWorkspace},
	{Command: "just", Classification: EffectWorkspace},

	// File creation and manipulation (within workspace)
	{Command: "touch", Classification: EffectWorkspace},
	{Command: "mkdir", Classification: EffectWorkspace},
	{Command: "cp", Classification: EffectWorkspace},
	{Command: "mv", Classification: EffectWorkspace},
	{Command: "echo", Classification: EffectWorkspace},

	// In-place text editing
	{Command: "sed", ArgPatterns: []string{"-i"}, Classification: EffectWorkspace},
	{Command: "sd", Classification: EffectWorkspace},

	// === Safe: read-only, no side effects ===

	// File listing and navigation
	{Command: "ls", Classification: EffectSafe},
	{Command: "pwd", Classification: EffectSafe},
	{Command: "eza", Classification: EffectSafe},

	// File reading
	{Command: "cat", Classification: EffectSafe},
	{Command: "head", Classification: EffectSafe},
	{Command: "tail", Classification: EffectSafe},
	{Command: "less", Classification: EffectSafe},

	// Content search
	{Command: "grep", Classification: EffectSafe},
	{Command: "rg", Classification: EffectSafe},

	// File search
	{Command: "fd", Classification: EffectSafe},
	{Command: "find", Classification: EffectSafe},

	// Git read-only operations
	{Command: "git", ArgPatterns: []string{"status"}, Classification: EffectSafe},
	{Command: "git", ArgPatterns: []string{"diff"}, Classification: EffectSafe},
	{Command: "git", ArgPatterns: []string{"log"}, Classification: EffectSafe},
	{Command: "git", ArgPatterns: []string{"show"}, Classification: EffectSafe},
	{Command: "git", ArgPatterns: []string{"branch"}, Classification: EffectSafe},
	{Command: "git", ArgPatterns: []string{"tag"}, Classification: EffectSafe},

	// Text processing (read-only forms)
	{Command: "wc", Classification: EffectSafe},
	{Command: "sort", Classification: EffectSafe},
	{Command: "uniq", Classification: EffectSafe},
	{Command: "cut", Classification: EffectSafe},
	{Command: "tr", Classification: EffectSafe},

	// Static analysis
	{Command: "go", ArgPatterns: []string{"doc"}, Classification: EffectSafe},
	{Command: "go", ArgPatterns: []string{"vet"}, Classification: EffectSafe},
	{Command: "cargo", ArgPatterns: []string{"check"}, Classification: EffectSafe},

	// Command discovery
	{Command: "which", Classification: EffectSafe},
	{Command: "type", Classification: EffectSafe},
	{Command: "command", ArgPatterns: []string{"-v"}, Classification: EffectSafe},
	{Command: "command", ArgPatterns: []string{"-V"}, Classification: EffectSafe},
}
