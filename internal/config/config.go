package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Interval         time.Duration
	EnvKeys          []string
	ExeAllowlist     []string
	IgnoreExe        []string
	IgnoreFile       string
	PinSessionSlice  bool
	PinSlices        []string
	OSCPUsOverride   string
	GameCPUsOverride string
}

type tomlConfig struct {
	Interval         string   `toml:"interval"`
	EnvKeys          []string `toml:"env_keys"`
	ExeAllowlist     []string `toml:"exe_allowlist"`
	IgnoreExe        []string `toml:"ignore_exe"`
	IgnoreFile       string   `toml:"ignore_file"`
	PinSessionSlice  *bool    `toml:"pin_session_slice"`
	PinSlices        []string `toml:"pin_slices"`
	OSCPUsOverride   string   `toml:"os_cpus"`
	GameCPUsOverride string   `toml:"game_cpus"`
}

func Default() Config {
	return Config{
		Interval: 2 * time.Second,
		EnvKeys: []string{
			"SteamAppId",
			"SteamGameId",
			"STEAM_COMPAT_APP_ID",
		},
		ExeAllowlist: nil,
		IgnoreExe: []string{
			"steam",
			"steamwebhelper",
			"steamservice",
			"steam-runtime-launcher-interface-0",
			"steam-runtime-supervisor",
			"pressure-vessel",
			"pressure-vessel-wrap",
			"wineserver",
			"wine64",
			"wine",
			"services.exe",
			"explorer.exe",
			"conhost.exe",
			"rpcss.exe",
			"winedevice.exe",
			"plugplay.exe",
			"svchost.exe",
			"winedbg",
			"gameoverlayui",
			"gameoverlayui.exe",
			"steam_monitor",
			"reaper",
		},
		PinSessionSlice: false,
		PinSlices: []string{
			"app.slice",
			"background.slice",
		},
	}
}

func DefaultConfigPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "ccdbind", "config.toml"), nil
}

func DefaultIgnorePath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "ccdbind", "ignore.txt"), nil
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return Config{}, err
			}
		} else {
			var tc tomlConfig
			if _, err := toml.Decode(string(data), &tc); err != nil {
				return Config{}, err
			}

			if tc.Interval != "" {
				d, err := time.ParseDuration(tc.Interval)
				if err != nil {
					return Config{}, fmt.Errorf("invalid interval %q: %w", tc.Interval, err)
				}
				cfg.Interval = d
			}
			if len(tc.EnvKeys) > 0 {
				cfg.EnvKeys = dedupeNonEmpty(tc.EnvKeys, nil)
			}
			if len(tc.ExeAllowlist) > 0 {
				cfg.ExeAllowlist = dedupeNonEmpty(tc.ExeAllowlist, strings.ToLower)
			}
			if len(tc.IgnoreExe) > 0 {
				cfg.IgnoreExe = dedupeNonEmpty(tc.IgnoreExe, strings.ToLower)
			}
			if tc.IgnoreFile != "" {
				cfg.IgnoreFile = strings.TrimSpace(tc.IgnoreFile)
			}
			if tc.PinSessionSlice != nil {
				cfg.PinSessionSlice = *tc.PinSessionSlice
			}
			if len(tc.PinSlices) > 0 {
				cfg.PinSlices = dedupeNonEmpty(tc.PinSlices, nil)
			}
			if tc.OSCPUsOverride != "" {
				cfg.OSCPUsOverride = strings.TrimSpace(tc.OSCPUsOverride)
			}
			if tc.GameCPUsOverride != "" {
				cfg.GameCPUsOverride = strings.TrimSpace(tc.GameCPUsOverride)
			}
		}
	}

	if strings.TrimSpace(cfg.IgnoreFile) == "" {
		ignorePath, err := DefaultIgnorePath()
		if err != nil {
			return Config{}, err
		}
		cfg.IgnoreFile = ignorePath
	}
	cfg.IgnoreFile = expandTilde(cfg.IgnoreFile)

	if extra, err := loadIgnoreFile(cfg.IgnoreFile); err == nil {
		cfg.IgnoreExe = dedupeNonEmpty(append(cfg.IgnoreExe, extra...), strings.ToLower)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, err
	}

	return cfg, nil
}

func dedupeNonEmpty(in []string, transform func(string) string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if transform != nil {
			s = transform(s)
		}
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func loadIgnoreFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}

func expandTilde(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		return filepath.Join(home, rest)
	}
	if rest, ok := strings.CutPrefix(path, "~"); ok {
		return filepath.Join(home, rest)
	}
	return path
}
