package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Reidond/ccdbind/internal/config"
	"github.com/Reidond/ccdbind/internal/procscan"
	"github.com/Reidond/ccdbind/internal/state"
	"github.com/Reidond/ccdbind/internal/systemdctl"
)

type statusSlice struct {
	Unit              string `json:"unit"`
	AllowedCPUs       string `json:"allowed_cpus"`
	OriginalAllowed   string `json:"original_allowed_cpus,omitempty"`
	ReadAllowedCPUErr string `json:"read_allowed_cpus_error,omitempty"`
}

type statusGameProc struct {
	PID         int    `json:"pid"`
	Exe         string `json:"exe"`
	GameID      string `json:"game_id"`
	IDSource    string `json:"id_source"`
	AllowedCPUs string `json:"allowed_cpus,omitempty"`
}

type statusProgramSummary struct {
	Exe         string `json:"exe"`
	Class       string `json:"class"` // os|game
	AllowedCPUs string `json:"allowed_cpus"`
	Count       int    `json:"count"`
	SamplePIDs  []int  `json:"sample_pids"`
}

type statusOutput struct {
	GeneratedAt time.Time `json:"generated_at"`
	Filter      string    `json:"filter"`

	ConfigPath string `json:"config_path"`
	StatePath  string `json:"state_path"`

	OSCPUs   string `json:"os_cpus,omitempty"`
	GameCPUs string `json:"game_cpus,omitempty"`

	State  state.File             `json:"state"`
	Slices []statusSlice          `json:"slices"`
	Games  []statusGameProc       `json:"games,omitempty"`
	All    []statusProgramSummary `json:"all,omitempty"`
	Errors []string               `json:"errors,omitempty"`
}

func runStatus(args []string) {
	fs := flag.NewFlagSet("ccdbind status", flag.ExitOnError)
	flagJSON := fs.Bool("json", false, "output JSON")
	flagFilter := fs.String("filter", "games", "process filter: games|all")
	flagOnlyGames := fs.Bool("only-games", false, "alias for --filter=games")
	flagAll := fs.Bool("all", false, "alias for --filter=all")
	flagConfig := fs.String("config", "", "config file path (TOML). Default: XDG config path")
	_ = fs.Parse(args)

	filter := strings.ToLower(strings.TrimSpace(*flagFilter))
	if *flagOnlyGames && *flagAll {
		fatal(fmt.Errorf("cannot use --only-games and --all together"))
	}
	if *flagOnlyGames {
		filter = "games"
	}
	if *flagAll {
		filter = "all"
	}
	if filter == "" {
		filter = "games"
	}
	if filter != "games" && filter != "all" {
		fatal(fmt.Errorf("invalid --filter=%q (expected games|all)", filter))
	}

	defaultCfgPath, err := config.DefaultConfigPath()
	if err != nil {
		fatal(err)
	}
	configPath := strings.TrimSpace(*flagConfig)
	if configPath == "" {
		configPath = defaultCfgPath
	}

	statePath, err := state.DefaultPath()
	if err != nil {
		fatal(err)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fatal(err)
	}

	st, err := state.Load(statePath)
	if err != nil {
		fatal(err)
	}

	osCPUs := strings.TrimSpace(st.OSCPUs)
	gameCPUs := strings.TrimSpace(st.GameCPUs)
	if osCPUs == "" || gameCPUs == "" {
		resOS, resGame, err := resolveCPUs(cfg)
		if err == nil {
			if osCPUs == "" {
				osCPUs = resOS
			}
			if gameCPUs == "" {
				gameCPUs = resGame
			}
		}
	}

	out := statusOutput{
		GeneratedAt: time.Now(),
		Filter:      filter,
		ConfigPath:  configPath,
		StatePath:   statePath,
		OSCPUs:      osCPUs,
		GameCPUs:    gameCPUs,
		State:       st,
	}

	sys := systemdctl.Systemctl{}
	slices := slicesToPin(cfg)
	for _, unit := range slices {
		ss := statusSlice{Unit: unit}
		if st.OriginalAllowedCPUs != nil {
			ss.OriginalAllowed = st.OriginalAllowedCPUs[unit]
		}
		ctx2, cancel := systemdctl.DefaultContext()
		val, err := sys.GetAllowedCPUs(ctx2, unit)
		cancel()
		if err != nil {
			ss.ReadAllowedCPUErr = err.Error()
		} else {
			ss.AllowedCPUs = val
		}
		out.Slices = append(out.Slices, ss)
	}

	uid := os.Getuid()
	{
		scanner := procscan.NewScanner(uid, cfg.EnvKeys, cfg.ExeAllowlist, cfg.IgnoreExe)
		games, err := scanner.Scan()
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("scan games: %v", err))
		} else {
			gameIDs := make([]string, 0, len(games))
			for id := range games {
				gameIDs = append(gameIDs, id)
			}
			sort.Strings(gameIDs)
			for _, gameID := range gameIDs {
				procs := games[gameID]
				sort.Slice(procs, func(i, j int) bool { return procs[i].PID < procs[j].PID })
				for _, gp := range procs {
					p := statusGameProc{PID: gp.PID, Exe: gp.Exe, GameID: gp.GameID, IDSource: gp.IDSource}
					if allowed, err := procscan.AllowedCPUs(gp.PID); err == nil {
						p.AllowedCPUs = allowed
					}
					out.Games = append(out.Games, p)
				}
			}
		}
	}

	if filter == "all" {
		all, err := procscan.ScanUserCPUConstraints(uid)
		if err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("scan all processes: %v", err))
		} else {
			type key struct {
				exe   string
				class string
			}
			groups := map[key]*statusProgramSummary{}
			for _, p := range all {
				class := ""
				switch {
				case osCPUs != "" && p.AllowedCPUs == osCPUs:
					class = "os"
				case gameCPUs != "" && p.AllowedCPUs == gameCPUs:
					class = "game"
				default:
					continue
				}
				k := key{exe: p.Exe, class: class}
				s, ok := groups[k]
				if !ok {
					s = &statusProgramSummary{Exe: p.Exe, Class: class, AllowedCPUs: p.AllowedCPUs}
					groups[k] = s
				}
				s.Count++
				if len(s.SamplePIDs) < 8 {
					s.SamplePIDs = append(s.SamplePIDs, p.PID)
				}
			}

			summaries := make([]statusProgramSummary, 0, len(groups))
			for _, v := range groups {
				summaries = append(summaries, *v)
			}
			sort.Slice(summaries, func(i, j int) bool {
				if summaries[i].Class != summaries[j].Class {
					return summaries[i].Class < summaries[j].Class
				}
				if summaries[i].Count != summaries[j].Count {
					return summaries[i].Count > summaries[j].Count
				}
				return summaries[i].Exe < summaries[j].Exe
			})
			out.All = summaries
		}
	}

	if *flagJSON {
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return
	}

	printStatusHuman(out)
}

func printStatusHuman(out statusOutput) {
	fmt.Printf("state: %s\n", out.StatePath)
	fmt.Printf("pin_applied: %v\n", out.State.PinApplied)
	if out.OSCPUs != "" {
		fmt.Printf("os_cpus: %s\n", out.OSCPUs)
	}
	if out.GameCPUs != "" {
		fmt.Printf("game_cpus: %s\n", out.GameCPUs)
	}

	if len(out.Slices) > 0 {
		fmt.Println("slices:")
		for _, s := range out.Slices {
			line := fmt.Sprintf("  %s: AllowedCPUs=%q", s.Unit, s.AllowedCPUs)
			if s.ReadAllowedCPUErr != "" {
				line = fmt.Sprintf("  %s: error=%s", s.Unit, s.ReadAllowedCPUErr)
			}
			if s.OriginalAllowed != "" || out.State.PinApplied {
				line += fmt.Sprintf(" (original=%q)", s.OriginalAllowed)
			}
			fmt.Println(line)
		}
	}

	if out.Filter == "games" || out.Filter == "all" {
		if len(out.Games) == 0 {
			fmt.Println("games: none")
		} else {
			fmt.Println("games:")
			for _, g := range out.Games {
				allowed := g.AllowedCPUs
				if allowed == "" {
					allowed = "?"
				}
				fmt.Printf("  pid=%d exe=%s game_id=%s src=%s allowed=%s\n", g.PID, g.Exe, g.GameID, g.IDSource, allowed)
			}
		}
	}

	if out.Filter == "all" {
		if len(out.All) == 0 {
			fmt.Println("affected: none")
		} else {
			fmt.Println("affected:")
			for _, s := range out.All {
				fmt.Printf("  class=%s exe=%s count=%d allowed=%s pids=%v\n", s.Class, s.Exe, s.Count, s.AllowedCPUs, s.SamplePIDs)
			}
		}
	}

	if len(out.Errors) > 0 {
		fmt.Println("errors:")
		for _, e := range out.Errors {
			fmt.Printf("  %s\n", e)
		}
	}
}
