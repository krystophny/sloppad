package web

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/sloppy-org/slopshell/internal/store"
)

type sloptoolsVaultConfig struct {
	Vaults []sloptoolsVault `toml:"vault"`
}

type sloptoolsVault struct {
	Sphere string `toml:"sphere"`
	Root   string `toml:"root"`
	Brain  string `toml:"brain"`
}

func sloptoolsVaultConfigPath() string {
	if path := strings.TrimSpace(os.Getenv("SLOPTOOLS_VAULT_CONFIG")); path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "sloptools", "vaults.toml")
}

func loadSloptoolsBrainRoots() map[string]string {
	path := strings.TrimSpace(sloptoolsVaultConfigPath())
	if path == "" {
		return nil
	}
	var cfg sloptoolsVaultConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil
	}
	roots := map[string]string{}
	for _, vault := range cfg.Vaults {
		sphere := strings.ToLower(strings.TrimSpace(vault.Sphere))
		switch sphere {
		case store.SphereWork, store.SpherePrivate:
		default:
			continue
		}
		root := strings.TrimSpace(vault.Root)
		if root == "" {
			continue
		}
		brain := strings.TrimSpace(vault.Brain)
		if brain == "" {
			brain = "brain"
		}
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rootAbs = filepath.Clean(rootAbs)
		brainRoot := filepath.Join(rootAbs, filepath.FromSlash(brain))
		roots[sphere] = filepath.Clean(brainRoot)
	}
	return roots
}

func presetRootAvailable(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func configuredBrainPresetSphereOrder(activeSphere string) []string {
	switch strings.ToLower(strings.TrimSpace(activeSphere)) {
	case store.SphereWork:
		return []string{store.SphereWork, store.SpherePrivate}
	case store.SpherePrivate:
		return []string{store.SpherePrivate, store.SphereWork}
	default:
		return []string{store.SphereWork, store.SpherePrivate}
	}
}
