package ui

import "testing"

// TestCommandRegistryComplete locks the registry as the single source of
// command behavior: every entry must be runnable and uniquely named.
func TestCommandRegistryComplete(t *testing.T) {
	names := make(map[string]bool)
	quickKeys := make(map[string]string)
	for _, cmd := range commandRegistry {
		if cmd.Run == nil {
			t.Errorf("command %q has no Run implementation", cmd.Name)
		}
		if names[cmd.Name] {
			t.Errorf("duplicate command name %q", cmd.Name)
		}
		names[cmd.Name] = true
		if cmd.QuickKey != "" {
			if owner, taken := quickKeys[cmd.QuickKey]; taken {
				t.Errorf("quick key %q claimed by both %q and %q", cmd.QuickKey, owner, cmd.Name)
			}
			quickKeys[cmd.QuickKey] = cmd.Name
		}
	}
}

// TestGlobalKeyCommandsResolve pins the key→command mapping: every global
// keybinding that delegates to executeCommand must name a registry command.
func TestGlobalKeyCommandsResolve(t *testing.T) {
	for _, kc := range globalKeyCommands {
		if findCommand(kc.command) == nil {
			t.Errorf("keybinding %v delegates to unknown command %q", kc.binding.Keys(), kc.command)
		}
	}
}
