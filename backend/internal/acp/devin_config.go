package acp

import (
	"os"
	"path/filepath"
	"strings"
)

// configureDevinPrompt hands Jaz's prompt to Devin the only way it reads one:
// the always-on global rules file under WINDSURF_CODEIUM_DIR. The view carries
// the user's rules with Jaz's prompt appended, as Devin's own rule files are
// composed, beside links to the user's other global rules.
func configureDevinPrompt(env map[string]string, prompt string) (cleanup func(), err error) {
	source := devinCodeiumDir(env)
	if source == "" {
		return func() {}, nil
	}
	view, err := os.MkdirTemp("", "jaz-devin-codeium-*")
	if err != nil {
		return nil, err
	}
	remove := func() { _ = os.RemoveAll(view) }
	defer func() {
		if err != nil {
			remove()
		}
	}()
	memories := filepath.Join(source, "memories")
	target := filepath.Join(view, "memories")
	if err = os.MkdirAll(target, 0o700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(memories)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	var rules []byte
	for _, entry := range entries {
		if entry.Name() == "global_rules.md" {
			if rules, err = os.ReadFile(filepath.Join(memories, entry.Name())); err != nil {
				return nil, err
			}
			continue
		}
		if err = os.Symlink(filepath.Join(memories, entry.Name()), filepath.Join(target, entry.Name())); err != nil {
			return nil, err
		}
	}
	combined := strings.TrimRight(string(rules), "\n")
	if prompt != "" {
		combined = strings.TrimSpace(combined + "\n\n" + prompt)
	}
	if err = os.WriteFile(filepath.Join(target, "global_rules.md"), []byte(combined+"\n"), 0o600); err != nil {
		return nil, err
	}
	env["WINDSURF_CODEIUM_DIR"] = view
	return remove, nil
}

func devinCodeiumDir(env map[string]string) string {
	if dir := strings.TrimSpace(env["WINDSURF_CODEIUM_DIR"]); dir != "" {
		return dir
	}
	home := firstNonEmpty(env["HOME"], env["USERPROFILE"])
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".codeium", "windsurf")
}
