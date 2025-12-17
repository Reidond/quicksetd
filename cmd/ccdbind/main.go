package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Reidond/ccdbind/internal/config"
	"github.com/Reidond/ccdbind/internal/procscan"
	"github.com/Reidond/ccdbind/internal/state"
	"github.com/Reidond/ccdbind/internal/systemdctl"
	"github.com/Reidond/ccdbind/internal/topology"
)

type runtime struct {
	dryRun bool

	osCPUs   string
	gameCPUs string

	pidToUnit map[int]pidRecord
}

type pidRecord struct {
	unit      string
	startTime uint64
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if len(os.Args) > 1 && os.Args[1] == "status" {
		runStatus(os.Args[2:])
		return
	}

	runDaemon(os.Args[1:])
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet("ccdbind", flag.ExitOnError)
	var (
		flagConfig    = fs.String("config", "", "config file path (TOML). Default: XDG config path")
		flagInterval  = fs.Duration("interval", 0, "poll interval override (e.g. 1s, 500ms)")
		flagPrintTopo = fs.Bool("print-topology", false, "print detected CPU topology and exit")
		flagDryRun    = fs.Bool("dry-run", false, "log actions without mutating systemd state")
		flagDumpState = fs.Bool("dump-state", false, "print persisted state JSON and exit")
	)
	_ = fs.Parse(args)

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

	if *flagDumpState {
		st, err := state.Load(statePath)
		if err != nil {
			fatal(err)
		}
		b, _ := json.MarshalIndent(st, "", "  ")
		fmt.Println(string(b))
		return
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fatal(err)
	}
	if *flagInterval > 0 {
		cfg.Interval = *flagInterval
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 2 * time.Second
	}

	r := &runtime{dryRun: *flagDryRun, pidToUnit: map[int]pidRecord{}}

	effectiveOS, effectiveGame, err := resolveCPUs(cfg)
	if err != nil {
		fatal(err)
	}
	r.osCPUs = effectiveOS
	r.gameCPUs = effectiveGame

	if *flagPrintTopo {
		fmt.Printf("OS_CPUS=%s\n", r.osCPUs)
		fmt.Printf("GAME_CPUS=%s\n", r.gameCPUs)
		return
	}

	uid := os.Getuid()
	slices := slicesToPin(cfg)

	sys := systemdctl.Systemctl{DryRun: r.dryRun}
	// Best-effort: ensure game.slice exists/loads.
	{
		ctx2, cancel := systemdctl.DefaultContext()
		_ = sys.StartUnit(ctx2, "game.slice")
		cancel()
	}

	mgr, err := systemdctl.NewUserManager(r.dryRun)
	if err != nil {
		fatal(fmt.Errorf("connect to user dbus: %w", err))
	}
	defer mgr.Close()

	scanner := procscan.NewScanner(uid, cfg.EnvKeys, cfg.ExeAllowlist, cfg.IgnoreExe)

	st, err := state.Load(statePath)
	if err != nil {
		fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := restoreIfNeeded(ctx, scanner, sys, statePath, &st, slices); err != nil {
		log.Printf("restoreIfNeeded: %v", err)
	}

	sigc := make(chan os.Signal, 2)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigc
		log.Printf("signal received; shutting down")
		cancel()
	}()

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	log.Printf("ccdbind started interval=%s os_cpus=%q game_cpus=%q dry_run=%v", cfg.Interval, r.osCPUs, r.gameCPUs, r.dryRun)
	for {
		select {
		case <-ctx.Done():
			if st.PinApplied {
				if err := restoreSlices(sys, slices, st.OriginalAllowedCPUs); err != nil {
					log.Printf("restore on exit: %v", err)
				} else {
					st.PinApplied = false
					st.LastSuccessfulRestore = time.Now()
					_ = state.Save(statePath, st)
				}
			}
			return
		case <-ticker.C:
			games, err := scanner.Scan()
			if err != nil {
				log.Printf("scan: %v", err)
				continue
			}
			if err := handleTick(ctx, r, sys, mgr, statePath, &st, slices, games); err != nil {
				log.Printf("tick: %v", err)
			}
		}
	}
}

func slicesToPin(cfg config.Config) []string {
	slices := append([]string{}, cfg.PinSlices...)
	if cfg.PinSessionSlice {
		slices = append(slices, "session.slice")
	}
	slices = dedupe(slices)
	if len(slices) == 0 {
		return []string{"app.slice", "background.slice"}
	}
	return slices
}

func resolveCPUs(cfg config.Config) (string, string, error) {
	if strings.TrimSpace(cfg.OSCPUsOverride) != "" && strings.TrimSpace(cfg.GameCPUsOverride) != "" {
		osCanonical, _, err := topology.CanonicalizeCPUList(cfg.OSCPUsOverride)
		if err != nil {
			return "", "", fmt.Errorf("invalid os_cpus override: %w", err)
		}
		gameCanonical, _, err := topology.CanonicalizeCPUList(cfg.GameCPUsOverride)
		if err != nil {
			return "", "", fmt.Errorf("invalid game_cpus override: %w", err)
		}
		return osCanonical, gameCanonical, nil
	}

	res, err := topology.Detect()
	if err != nil {
		return "", "", err
	}
	if res.GameCPUs == "" {
		return "", "", fmt.Errorf("topology detection found only one list: %v", res.Lists)
	}
	return res.OSCPUs, res.GameCPUs, nil
}

func restoreIfNeeded(ctx context.Context, scanner *procscan.Scanner, sys systemdctl.Systemctl, statePath string, st *state.File, slices []string) error {
	if !st.PinApplied {
		return nil
	}
	games, err := scanner.Scan()
	if err != nil {
		return err
	}
	if len(games) > 0 {
		return nil
	}
	if err := restoreSlices(sys, slices, st.OriginalAllowedCPUs); err != nil {
		return err
	}
	st.PinApplied = false
	st.LastSuccessfulRestore = time.Now()
	return state.Save(statePath, *st)
}

func handleTick(ctx context.Context, r *runtime, sys systemdctl.Systemctl, mgr *systemdctl.UserManager, statePath string, st *state.File, slices []string, games map[string][]procscan.GameProcess) error {
	if len(games) == 0 {
		if st.PinApplied {
			log.Printf("no games active; restoring slices")
			if err := restoreSlices(sys, slices, st.OriginalAllowedCPUs); err != nil {
				return err
			}
			st.PinApplied = false
			st.LastSuccessfulRestore = time.Now()
			if err := state.Save(statePath, *st); err != nil {
				return err
			}
			r.pidToUnit = map[int]pidRecord{}
		}
		return nil
	}

	currentAllowed, err := readAllowedCPUs(sys, slices)
	if err != nil {
		return err
	}

	reapplyNeeded := !st.PinApplied
	if st.PinApplied {
		for _, unit := range slices {
			if currentAllowed[unit] != r.osCPUs {
				reapplyNeeded = true
				break
			}
			if st.OriginalAllowedCPUs == nil {
				continue
			}
			if _, ok := st.OriginalAllowedCPUs[unit]; !ok {
				// If the unit is already pinned but we lack an original, don't blindly
				// snapshot the pinned value as an "original".
				if currentAllowed[unit] != r.osCPUs {
					reapplyNeeded = true
					break
				}
			}
		}
	}

	if reapplyNeeded {
		orig := st.OriginalAllowedCPUs
		if orig == nil {
			orig = map[string]string{}
		}
		if !st.PinApplied {
			orig = make(map[string]string, len(currentAllowed))
			for unit, val := range currentAllowed {
				orig[unit] = val
			}
		} else {
			for unit, val := range currentAllowed {
				if _, ok := orig[unit]; ok {
					continue
				}
				// Backfill originals only if the unit is not already pinned; otherwise
				// fall back to clearing AllowedCPUs on restore.
				if val != r.osCPUs {
					orig[unit] = val
				} else {
					orig[unit] = ""
				}
			}
		}

		msg := "games active; pinning"
		if st.PinApplied {
			msg = "games active; reapplying pin"
		}
		log.Printf("%s slices=%v to os_cpus=%q", msg, slices, r.osCPUs)
		for _, unit := range slices {
			ctx2, cancel := systemdctl.DefaultContext()
			err := sys.SetAllowedCPUs(ctx2, unit, r.osCPUs)
			cancel()
			if err != nil {
				return err
			}
		}
		st.PinApplied = true
		st.OriginalAllowedCPUs = orig
		st.OSCPUs = r.osCPUs
		st.GameCPUs = r.gameCPUs
		st.LastSuccessfulPinApply = time.Now()
		if err := state.Save(statePath, *st); err != nil {
			return err
		}
	}

	alive := make(map[int]struct{}, 32)
	gameIDs := make([]string, 0, len(games))
	for gameID := range games {
		gameIDs = append(gameIDs, gameID)
	}
	sort.Strings(gameIDs)

	for _, gameID := range gameIDs {
		procs := games[gameID]
		unit := systemdctl.UnitNameForGameID(gameID)
		if len(procs) == 0 {
			continue
		}

		pids := make([]int, 0, len(procs))
		newPIDs := make([]int, 0, len(procs))
		pidStarts := make(map[int]uint64, len(procs))
		for _, gp := range procs {
			alive[gp.PID] = struct{}{}
			pidStarts[gp.PID] = gp.StartTime

			pids = append(pids, gp.PID)

			rec, ok := r.pidToUnit[gp.PID]
			if !ok || rec.unit != unit {
				newPIDs = append(newPIDs, gp.PID)
				continue
			}
			if rec.startTime == 0 || gp.StartTime == 0 {
				newPIDs = append(newPIDs, gp.PID)
				continue
			}
			if rec.startTime != gp.StartTime {
				newPIDs = append(newPIDs, gp.PID)
			}
		}

		desc := fmt.Sprintf("ccdbind game %s", gameID)
		ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
		created, err := mgr.EnsureTransientScope(ctx2, unit, pids, "game.slice", desc)
		cancel()
		if err != nil {
			return fmt.Errorf("EnsureTransientScope %s: %w", unit, err)
		}

		ctx2, cancel = systemdctl.DefaultContext()
		err = sys.SetAllowedCPUs(ctx2, unit, r.gameCPUs)
		cancel()
		if err != nil {
			return fmt.Errorf("pin scope %s: %w", unit, err)
		}

		if created {
			for _, pid := range pids {
				r.pidToUnit[pid] = pidRecord{unit: unit, startTime: pidStarts[pid]}
			}
		} else if len(newPIDs) > 0 {
			ctx2, cancel = context.WithTimeout(ctx, 5*time.Second)
			err = mgr.AttachProcessesToUnit(ctx2, unit, "", newPIDs)
			cancel()
			if err != nil {
				return fmt.Errorf("AttachProcessesToUnit %s: %w", unit, err)
			}
			for _, pid := range newPIDs {
				r.pidToUnit[pid] = pidRecord{unit: unit, startTime: pidStarts[pid]}
			}
		}
	}

	for pid := range r.pidToUnit {
		if _, ok := alive[pid]; !ok {
			delete(r.pidToUnit, pid)
		}
	}

	return nil
}

func readAllowedCPUs(sys systemdctl.Systemctl, slices []string) (map[string]string, error) {
	out := make(map[string]string, len(slices))
	for _, unit := range slices {
		ctx2, cancel := systemdctl.DefaultContext()
		val, err := sys.GetAllowedCPUs(ctx2, unit)
		cancel()
		if err != nil {
			return nil, err
		}
		out[unit] = val
	}
	return out, nil
}

func restoreSlices(sys systemdctl.Systemctl, slices []string, originals map[string]string) error {
	for _, unit := range slices {
		val := originals[unit]
		ctx2, cancel := systemdctl.DefaultContext()
		err := sys.SetAllowedCPUs(ctx2, unit, val)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

func dedupe(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
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

func fatal(err error) {
	log.Printf("fatal: %v", err)
	os.Exit(1)
}
