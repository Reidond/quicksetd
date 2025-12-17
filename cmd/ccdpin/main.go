package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Reidond/ccdbind/internal/systemdctl"
	"github.com/Reidond/ccdbind/internal/topology"
)

const (
	envGameCPUs = "STEAM_CCD_GAME_CPUS"
	envOSCPUs   = "STEAM_CCD_OS_CPUS"
	envSwap     = "STEAM_CCD_SWAP"
	envNoOSPin  = "STEAM_CCD_NO_OS_PIN"
	envOSSlices = "STEAM_CCD_OS_SLICES"
	envDebug    = "STEAM_CCD_DEBUG"
)

// logFile is the global log file handle for crash logging.
var logFile *os.File

type options struct {
	print bool
	swap  bool

	noOSPin bool

	gameCPUs string
	osCPUs   string
}

type resolved struct {
	osCPUs   string
	gameCPUs string
	ccds     []string

	noOSPin  bool
	osSlices []string
	debug    bool
}

func main() {
	// Set up crash logging before anything else
	setupLogging()
	defer closeLogging()
	defer recoverPanic()

	opts, cmd, err := parseArgs(os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		fatal(err)
	}

	r, err := resolve(opts)
	if err != nil {
		fatal(err)
	}

	if opts.print {
		printTopology(r)
		return
	}
	if len(cmd) == 0 {
		fatal(errors.New("no command provided"))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigc := make(chan os.Signal, 2)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigc
		cancel()
	}()

	sys := systemdctl.Systemctl{}
	cleanup := func() {}
	if !r.noOSPin {
		pin, err := newSlicePinManager(sys, r.osSlices, r.osCPUs, r.debug)
		if err != nil {
			warnf("os slice pin disabled: %v", err)
		} else {
			c, err := pin.AcquireAndPin(ctx)
			if err != nil {
				warnf("failed to pin OS slices: %v", err)
			} else {
				cleanup = c
			}
		}
	}

	exitCode := runGame(ctx, sys, r.gameCPUs, cmd, r.debug)
	cleanup()
	os.Exit(exitCode)
}

func parseArgs(args []string, out io.Writer, errOut io.Writer) (options, []string, error) {
	fs := flag.NewFlagSet("ccdpin", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var opts options
	fs.BoolVar(&opts.print, "print", false, "print detected topology and selected CPU sets")
	fs.BoolVar(&opts.swap, "swap", false, "swap OS and GAME CPU assignments")
	fs.BoolVar(&opts.noOSPin, "no-os-pin", false, "do not pin OS slices")
	fs.StringVar(&opts.gameCPUs, "game-cpus", "", "override GAME CPU list")
	fs.StringVar(&opts.osCPUs, "os-cpus", "", "override OS CPU list")
	fs.Usage = func() {
		fmt.Fprintln(out, "usage: ccdpin [flags] [--] COMMAND [args...]")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "flags:")
		fs.PrintDefaults()
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "environment overrides (compat):")
		fmt.Fprintf(out, "  %s, %s, %s, %s, %s, %s\n", envGameCPUs, envOSCPUs, envSwap, envNoOSPin, envOSSlices, envDebug)
	}

	if err := fs.Parse(args); err != nil {
		return options{}, nil, err
	}
	return opts, fs.Args(), nil
}

func resolve(opts options) (resolved, error) {
	debug := parseBoolEnv(envDebug)
	noOSPin := opts.noOSPin || parseBoolEnv(envNoOSPin)
	swap := opts.swap || parseBoolEnv(envSwap)

	osSlices := parseSlicesEnv(os.Getenv(envOSSlices))
	if len(osSlices) == 0 {
		osSlices = []string{"app.slice", "background.slice", "session.slice"}
	}

	osCPUs := strings.TrimSpace(opts.osCPUs)
	if osCPUs == "" {
		osCPUs = strings.TrimSpace(os.Getenv(envOSCPUs))
	}
	gameCPUs := strings.TrimSpace(opts.gameCPUs)
	if gameCPUs == "" {
		gameCPUs = strings.TrimSpace(os.Getenv(envGameCPUs))
	}

	// Match the script behavior:
	// - If both OS+GAME are provided explicitly, use them.
	// - Otherwise auto-detect and fill missing.
	var det topology.Result
	needDetect := opts.print || osCPUs == "" || gameCPUs == "" || swap
	if needDetect {
		res, err := topology.Detect()
		if err != nil {
			return resolved{}, err
		}
		det = res
	}
	if osCPUs == "" {
		osCPUs = det.OSCPUs
	}
	if gameCPUs == "" {
		gameCPUs = det.GameCPUs
	}
	if strings.TrimSpace(gameCPUs) == "" {
		return resolved{}, fmt.Errorf("could not resolve GAME_CPUS")
	}

	var err error
	if strings.TrimSpace(osCPUs) != "" {
		osCPUs, _, err = topology.CanonicalizeCPUList(osCPUs)
		if err != nil {
			return resolved{}, fmt.Errorf("invalid OS CPU list %q: %w", osCPUs, err)
		}
	}
	gameCPUs, _, err = topology.CanonicalizeCPUList(gameCPUs)
	if err != nil {
		return resolved{}, fmt.Errorf("invalid GAME CPU list %q: %w", gameCPUs, err)
	}

	if swap {
		if strings.TrimSpace(osCPUs) == "" {
			return resolved{}, fmt.Errorf("cannot swap without OS_CPUS")
		}
		osCPUs, gameCPUs = gameCPUs, osCPUs
	}

	return resolved{osCPUs: osCPUs, gameCPUs: gameCPUs, ccds: det.Lists, noOSPin: noOSPin, osSlices: osSlices, debug: debug}, nil
}

func printTopology(r resolved) {
	if len(r.ccds) > 0 {
		fmt.Println("Detected CCD CPU groups:")
		for i, s := range r.ccds {
			fmt.Printf("  CCD[%d] = %s\n", i, strings.TrimSpace(s))
		}
		fmt.Println("")
	}
	fmt.Println("Selected:")
	if r.osCPUs != "" {
		fmt.Printf("  OS_CPUS   = %s\n", r.osCPUs)
	}
	fmt.Printf("  GAME_CPUS = %s\n", r.gameCPUs)
	if len(r.osSlices) > 0 {
		fmt.Printf("  OS_SLICES = %s\n", strings.Join(r.osSlices, " "))
	}
}

func parseSlicesEnv(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	fields := strings.Fields(v)
	out := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.HasSuffix(f, ".slice") {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}

func parseBoolEnv(k string) bool {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on", "enable", "enabled":
		return true
	case "0", "false", "no", "n", "off", "disable", "disabled":
		return false
	default:
		return true
	}
}

func runGame(ctx context.Context, sys systemdctl.Systemctl, gameCPUs string, cmd []string, debug bool) int {
	userSystemd := userSystemdAvailable(ctx)
	if userSystemd {
		ctx2, cancel := systemdctl.DefaultContext()
		_ = sys.StartUnit(ctx2, "game.slice")
		cancel()
	}

	if userSystemd && hasBinary("systemd-run") {
		args := []string{
			"--user",
			"--scope",
			"--wait",
			"--quiet",
			"--slice=game.slice",
			"-p", "AllowedCPUs=" + gameCPUs,
		}
		args = append(args, systemdRunSetenvArgs()...)
		args = append(args, "--")
		if hasBinary("taskset") {
			args = append(args, "taskset", "-c", gameCPUs, "--")
			args = append(args, cmd...)
			return runCmd(ctx, "systemd-run", args, debug)
		}
		args = append(args, cmd...)
		return runCmd(ctx, "systemd-run", args, debug)
	}

	if hasBinary("taskset") {
		args := append([]string{"-c", gameCPUs, "--"}, cmd...)
		return runCmd(ctx, "taskset", args, debug)
	}

	warnf("neither systemd-run nor taskset available; running without pin")
	return runCmd(ctx, cmd[0], cmd[1:], debug)
}

func systemdRunSetenvArgs() []string {
	// Ensure the launched scope sees the same environment as this process.
	// This matters for Steam/Proton usage (e.g. PROTON_* variables).
	env := os.Environ()
	out := make([]string, 0, len(env))
	seen := map[string]struct{}{}
	for _, kv := range env {
		if kv == "" {
			continue
		}
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		k := kv[:i]
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, "--setenv="+kv)
	}
	return out
}

func userSystemdAvailable(ctx context.Context) bool {
	if !hasBinary("systemctl") {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", "--user", "show", "-p", "Version", "--value")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func runCmd(ctx context.Context, bin string, args []string, debug bool) int {
	debugf(debug, "exec: %s %s", bin, strings.Join(args, " "))
	c := exec.CommandContext(ctx, bin, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok {
				if ws.Signaled() {
					return 128 + int(ws.Signal())
				}
				return ws.ExitStatus()
			}
			return 1
		}
		warnf("exec failed: %v", err)
		return 1
	}
	return 0
}

func hasBinary(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// logDir returns the directory for ccdpin log files.
func logDir() (string, error) {
	// Use XDG state dir if available, otherwise fall back to cache
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "ccdpin"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "ccdpin"), nil
}

// setupLogging initializes crash logging to a file.
func setupLogging() {
	dir, err := logDir()
	if err != nil {
		return // silently skip if we can't determine log dir
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	logPath := filepath.Join(dir, "ccdpin.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	logFile = f

	// Configure log package to write to file
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("ccdpin started, pid=%d, args=%v", os.Getpid(), os.Args)
}

// closeLogging closes the log file handle.
func closeLogging() {
	if logFile != nil {
		log.Printf("ccdpin exiting normally")
		logFile.Close()
	}
}

// recoverPanic captures panic information and writes it to the log file.
func recoverPanic() {
	if r := recover(); r != nil {
		stack := debug.Stack()
		msg := fmt.Sprintf("PANIC: %v\n%s", r, stack)

		// Write to log file if available
		if logFile != nil {
			log.Printf("%s", msg)
			logFile.Sync()
		}

		// Also write to stderr
		fmt.Fprintf(os.Stderr, "ccdpin: %s\n", msg)
		os.Exit(2)
	}
}

// logError writes an error to the log file (if available) and stderr.
func logError(err error) {
	if logFile != nil {
		log.Printf("ERROR: %v", err)
	}
}

func fatal(err error) {
	logError(err)
	fmt.Fprintln(os.Stderr, "ccdpin:", err)
	os.Exit(2)
}

func warnf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if logFile != nil {
		log.Printf("WARN: %s", msg)
	}
	fmt.Fprintf(os.Stderr, "ccdpin: %s\n", msg)
}

func debugf(debug bool, format string, args ...any) {
	if !debug {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if logFile != nil {
		log.Printf("DEBUG: %s", msg)
	}
	fmt.Fprintf(os.Stderr, "ccdpin: %s\n", msg)
}

type pinState struct {
	Version             int               `json:"version"`
	Instances           map[string]uint64 `json:"instances"`
	OriginalAllowedCPUs map[string]string `json:"original_allowed_cpus"`
	OSCPUs              string            `json:"os_cpus"`
	Slices              []string          `json:"slices"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

type slicePinManager struct {
	sys    systemdctl.Systemctl
	osCPUs string
	slices []string
	debug  bool

	pid     int
	startTS uint64

	stateDir  string
	statePath string
	lockPath  string
}

func newSlicePinManager(sys systemdctl.Systemctl, slices []string, osCPUs string, debug bool) (*slicePinManager, error) {
	if strings.TrimSpace(osCPUs) == "" {
		return nil, fmt.Errorf("empty os cpus")
	}
	if len(slices) == 0 {
		return nil, fmt.Errorf("no slices configured")
	}
	stateDir, err := defaultStateDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, err
	}

	pid := os.Getpid()
	startTS, _ := procStartTime(pid)
	return &slicePinManager{
		sys:       sys,
		osCPUs:    osCPUs,
		slices:    append([]string{}, slices...),
		debug:     debug,
		pid:       pid,
		startTS:   startTS,
		stateDir:  stateDir,
		statePath: filepath.Join(stateDir, "state.json"),
		lockPath:  filepath.Join(stateDir, "lock"),
	}, nil
}

func defaultStateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "ccdpin"), nil
}

func (m *slicePinManager) AcquireAndPin(ctx context.Context) (func(), error) {
	unlock, st, err := m.lockAndLoad()
	if err != nil {
		return nil, err
	}

	changed := false
	defer func() {
		if !changed {
			unlock()
		}
	}()

	st = pruneDeadInstances(st)
	if st.Instances == nil {
		st.Instances = map[string]uint64{}
	}
	instKey := strconv.Itoa(m.pid)
	st.Instances[instKey] = m.startTS

	if len(st.Instances) == 1 {
		if err := m.pinSlicesLocked(ctx, &st); err != nil {
			delete(st.Instances, instKey)
			_ = m.saveLocked(st)
			unlock()
			return nil, err
		}
	}

	st.UpdatedAt = time.Now()
	if err := m.saveLocked(st); err != nil {
		unlock()
		return nil, err
	}
	unlock()
	changed = true

	return func() { m.releaseAndRestore(context.Background()) }, nil
}

func (m *slicePinManager) lockAndLoad() (func(), pinState, error) {
	f, err := os.OpenFile(m.lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, pinState{}, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, pinState{}, err
	}
	unlock := func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}

	data, err := os.ReadFile(m.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return unlock, pinState{Version: 1}, nil
		}
		unlock()
		return nil, pinState{}, err
	}
	var st pinState
	if err := json.Unmarshal(data, &st); err != nil {
		unlock()
		return nil, pinState{}, err
	}
	if st.Version == 0 {
		st.Version = 1
	}
	return unlock, st, nil
}

func (m *slicePinManager) saveLocked(st pinState) error {
	if st.Version == 0 {
		st.Version = 1
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.statePath)
}

func pruneDeadInstances(st pinState) pinState {
	if len(st.Instances) == 0 {
		return st
	}
	out := map[string]uint64{}
	for k, startTS := range st.Instances {
		pid, err := strconv.Atoi(k)
		if err != nil || pid <= 0 {
			continue
		}
		liveStart, err := procStartTime(pid)
		if err != nil {
			continue
		}
		if startTS != 0 && liveStart != 0 && liveStart != startTS {
			continue
		}
		out[k] = startTS
	}
	st.Instances = out
	return st
}

func (m *slicePinManager) pinSlicesLocked(_ context.Context, st *pinState) error {
	// Mimic script behavior: skip slices that don't exist.
	pinned := make([]string, 0, len(m.slices))
	current := map[string]string{}
	for _, unit := range m.slices {
		ctx2, cancel := systemdctl.DefaultContext()
		val, err := m.sys.GetAllowedCPUs(ctx2, unit)
		cancel()
		if err != nil {
			debugf(m.debug, "skipping slice %s: %v", unit, err)
			continue
		}
		pinned = append(pinned, unit)
		current[unit] = val
	}
	if len(pinned) == 0 {
		return fmt.Errorf("no OS slices could be pinned")
	}

	st.OriginalAllowedCPUs = make(map[string]string, len(current))
	for unit, val := range current {
		st.OriginalAllowedCPUs[unit] = val
	}
	st.OSCPUs = m.osCPUs
	st.Slices = append([]string{}, pinned...)

	for _, unit := range pinned {
		ctx2, cancel := systemdctl.DefaultContext()
		err := m.sys.SetAllowedCPUs(ctx2, unit, m.osCPUs)
		cancel()
		if err != nil {
			// Best-effort rollback.
			for _, u2 := range pinned {
				orig, ok := st.OriginalAllowedCPUs[u2]
				if !ok {
					continue
				}
				ctx3, cancel3 := systemdctl.DefaultContext()
				_ = m.sys.SetAllowedCPUs(ctx3, u2, orig)
				cancel3()
			}
			return err
		}
	}
	return nil
}

func (m *slicePinManager) releaseAndRestore(_ context.Context) {
	unlock, st, err := m.lockAndLoad()
	if err != nil {
		warnf("release lock: %v", err)
		return
	}
	defer unlock()

	st = pruneDeadInstances(st)
	if st.Instances != nil {
		key := strconv.Itoa(m.pid)
		if startTS, ok := st.Instances[key]; ok {
			if startTS == 0 || m.startTS == 0 || startTS == m.startTS {
				delete(st.Instances, key)
			}
		}
	}

	if len(st.Instances) == 0 && len(st.OriginalAllowedCPUs) > 0 {
		for _, unit := range st.Slices {
			orig := st.OriginalAllowedCPUs[unit]
			ctx2, cancel := systemdctl.DefaultContext()
			_ = m.sys.SetAllowedCPUs(ctx2, unit, orig)
			cancel()
		}
		st.OriginalAllowedCPUs = nil
		st.OSCPUs = ""
		st.Slices = nil
	}

	st.UpdatedAt = time.Now()
	_ = m.saveLocked(st)
}

func procStartTime(pid int) (uint64, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	line := strings.TrimSpace(string(data))
	idx := strings.LastIndexByte(line, ')')
	if idx == -1 || idx+2 >= len(line) {
		return 0, fmt.Errorf("invalid stat")
	}
	fields := strings.Fields(line[idx+2:])
	if len(fields) <= 19 {
		return 0, fmt.Errorf("stat too short")
	}
	return strconv.ParseUint(fields[19], 10, 64)
}
