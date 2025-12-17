package procscan

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Reidond/ccdbind/internal/topology"
)

type CPUConstraint struct {
	PID         int
	StartTime   uint64
	Exe         string
	AllowedCPUs string
}

func AllowedCPUs(pid int) (string, error) {
	return allowedCPUsAt("/proc", pid)
}

func ScanUserCPUConstraints(uid int) ([]CPUConstraint, error) {
	return scanUserCPUConstraintsAt("/proc", uid)
}

func scanUserCPUConstraintsAt(procRoot string, uid int) ([]CPUConstraint, error) {
	ents, err := os.ReadDir(procRoot)
	if err != nil {
		return nil, err
	}
	results := make([]CPUConstraint, 0, 128)
	for _, ent := range ents {
		if !ent.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(ent.Name())
		if err != nil || pid <= 0 {
			continue
		}
		owned, err := isOwnedByUIDAt(procRoot, pid, uid)
		if err != nil || !owned {
			continue
		}

		exe := exeBasenameLowerAt(procRoot, pid)
		if exe == "" {
			continue
		}

		allowed, err := allowedCPUsAt(procRoot, pid)
		if err != nil || strings.TrimSpace(allowed) == "" {
			continue
		}

		startTime, err := procStartTimeAt(procRoot, pid)
		if err != nil {
			startTime = 0
		}
		results = append(results, CPUConstraint{PID: pid, StartTime: startTime, Exe: exe, AllowedCPUs: allowed})
	}
	return results, nil
}

func allowedCPUsAt(procRoot string, pid int) (string, error) {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "status"))
	if err != nil {
		return "", err
	}
	allowed, ok := allowedCPUsFromStatus(data)
	if !ok {
		return "", fmt.Errorf("Cpus_allowed_list not found")
	}
	canonical, _, err := topology.CanonicalizeCPUList(allowed)
	if err != nil {
		// Keep the raw value if it can't be canonicalized.
		return strings.TrimSpace(allowed), nil
	}
	return canonical, nil
}

func allowedCPUsFromStatus(data []byte) (string, bool) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Cpus_allowed_list:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return "", false
		}
		return strings.TrimSpace(fields[1]), true
	}
	return "", false
}

func procStartTimeAt(procRoot string, pid int) (uint64, error) {
	path := filepath.Join(procRoot, strconv.Itoa(pid), "stat")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	line := strings.TrimSpace(string(data))
	if line == "" {
		return 0, fmt.Errorf("empty stat")
	}
	idx := strings.LastIndexByte(line, ')')
	if idx == -1 {
		return 0, fmt.Errorf("invalid stat format")
	}
	if idx+2 >= len(line) {
		return 0, fmt.Errorf("invalid stat format")
	}
	fields := strings.Fields(line[idx+2:])
	if len(fields) <= 19 {
		return 0, fmt.Errorf("stat too short")
	}
	return strconv.ParseUint(fields[19], 10, 64)
}

func exeBasenameLowerAt(procRoot string, pid int) string {
	path := filepath.Join(procRoot, strconv.Itoa(pid), "exe")
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	base := filepath.Base(target)
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == "/" {
		return ""
	}
	return strings.ToLower(base)
}

func isOwnedByUIDAt(procRoot string, pid int, uid int) (bool, error) {
	path := filepath.Join(procRoot, strconv.Itoa(pid), "status")
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return false, fmt.Errorf("unexpected Uid line: %q", line)
		}
		parsed, err := strconv.Atoi(fields[1])
		if err != nil {
			return false, err
		}
		return parsed == uid, nil
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, fmt.Errorf("Uid line not found")
}
