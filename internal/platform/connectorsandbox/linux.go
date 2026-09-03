package connectorsandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

// ProbeReport is emitted by the deterministic emulator while it is inside the
// reference Linux sandbox. Visible=true is a security failure for each probe.
type ProbeReport struct {
	EnvironmentVisible     bool `json:"environment_visible"`
	FilesystemVisible      bool `json:"filesystem_visible"`
	DirectNetworkReachable bool `json:"direct_network_reachable"`
}

type SandboxProbeResult struct {
	Report    ProbeReport       `json:"report"`
	Usage     Usage             `json:"usage"`
	Isolation IsolationEvidence `json:"isolation"`
}

type LinuxSandbox struct {
	plan  pluginsecurity.AdmissionPlan
	slots chan struct{}
}

func NewLinuxSandbox(plan pluginsecurity.AdmissionPlan) (*LinuxSandbox, error) {
	if validatePlan(plan) != nil {
		return nil, ErrInvalidPlan
	}
	return &LinuxSandbox{plan: plan, slots: make(chan struct{}, plan.Limits.MaxConcurrentCalls)}, nil
}

func (sandbox *LinuxSandbox) Probe(ctx context.Context, emulatorExecutable string) (SandboxProbeResult, error) {
	started := time.Now()
	if runtime.GOOS != "linux" || emulatorExecutable == "" {
		return SandboxProbeResult{}, ErrSandboxUnavailable
	}
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		return SandboxProbeResult{}, ErrSandboxUnavailable
	}
	chroot, err := exec.LookPath("chroot")
	if err != nil {
		return SandboxProbeResult{}, ErrSandboxUnavailable
	}
	trueCommand, err := exec.LookPath("true")
	if err != nil || !linuxNamespacesAvailable(unshare, trueCommand) {
		return SandboxProbeResult{}, ErrSandboxUnavailable
	}
	info, err := os.Stat(emulatorExecutable)
	if err != nil || !info.Mode().IsRegular() {
		return SandboxProbeResult{}, ErrSandboxUnavailable
	}
	select {
	case sandbox.slots <- struct{}{}:
		defer func() { <-sandbox.slots }()
	case <-ctx.Done():
		return SandboxProbeResult{}, ctx.Err()
	}

	root, err := os.MkdirTemp("", "torgnexa-connector-sandbox-")
	if err != nil {
		return SandboxProbeResult{}, err
	}
	defer os.RemoveAll(root)
	bin := filepath.Join(root, "bin")
	// The staging directory must be writable while the host copies the
	// emulator. It is sealed below before entering the isolated namespace.
	if err := os.MkdirAll(bin, 0700); err != nil {
		return SandboxProbeResult{}, err
	}
	if err := copyExecutable(emulatorExecutable, filepath.Join(bin, "emulator")); err != nil {
		return SandboxProbeResult{}, err
	}
	// No /etc, /run, /home, /proc or production secret mount is created. Seal
	// both directories after staging so the child cannot write its root.
	if err := os.Chmod(bin, 0555); err != nil {
		return SandboxProbeResult{}, err
	}
	if err := os.Chmod(root, 0555); err != nil {
		return SandboxProbeResult{}, err
	}

	runctx, cancel := context.WithTimeout(ctx, time.Duration(sandbox.plan.Limits.WallTimeMS)*time.Millisecond)
	defer cancel()
	// Use the portable short options and an absolute chroot helper. The
	// qualification runs in both util-linux and BusyBox based build images;
	// BusyBox does not implement util-linux's --root/--wd options.
	command := exec.CommandContext(runctx, unshare, "-U", "-r", "-m", "-n", "-i", "-u", chroot, root, "/bin/emulator", "--isolation-probe")
	command.Env = []string{"LANG=C", "TZ=UTC", "PATH=/bin", "TORGNEXA_SANDBOX_MODE=test"}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return SandboxProbeResult{}, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return SandboxProbeResult{}, err
	}
	if err := command.Start(); err != nil {
		return SandboxProbeResult{}, fmt.Errorf("%w: %v", ErrSandboxUnavailable, err)
	}

	var reason atomic.Value
	reason.Store("")
	var peakRSS atomic.Int64
	var cpuMS atomic.Int64
	stop := make(chan struct{})
	var monitorWG sync.WaitGroup
	monitorWG.Add(1)
	go func() {
		defer monitorWG.Done()
		monitorProcess(command.Process.Pid, sandbox.plan.Limits, &peakRSS, &cpuMS, &reason, func() { _ = command.Process.Kill() }, stop)
	}()

	maxOutput := int64(sandbox.plan.Limits.MaxOutputBytes)
	outData, outExceeded := readBounded(stdout, maxOutput, func() { reason.CompareAndSwap("", "output_limit"); _ = command.Process.Kill() })
	// stderr is intentionally discarded/bounded so raw provider text never becomes a result.
	_, _ = readBounded(stderr, 4096, func() {})
	waitErr := command.Wait()
	close(stop)
	monitorWG.Wait()
	if outExceeded {
		reason.CompareAndSwap("", "output_limit")
	}
	if runctx.Err() == context.DeadlineExceeded {
		reason.CompareAndSwap("", "wall_time_limit")
	}
	if value, _ := reason.Load().(string); value != "" {
		return SandboxProbeResult{Usage: Usage{WallTimeMS: int64(sandbox.plan.Limits.WallTimeMS), CPUTimeMS: cpuMS.Load(), PeakRSSBytes: peakRSS.Load(), OutputBytes: int64(len(outData))}}, ErrResourceLimit
	}
	if waitErr != nil {
		return SandboxProbeResult{}, fmt.Errorf("sandbox probe failed")
	}
	var report ProbeReport
	decoder := json.NewDecoder(bytes.NewReader(outData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil {
		return SandboxProbeResult{}, fmt.Errorf("sandbox probe invalid output")
	}
	if report.EnvironmentVisible || report.FilesystemVisible || report.DirectNetworkReachable {
		return SandboxProbeResult{}, fmt.Errorf("sandbox isolation probe failed")
	}
	return SandboxProbeResult{
		Report:    report,
		Usage:     Usage{WallTimeMS: time.Since(started).Milliseconds(), CPUTimeMS: cpuMS.Load(), PeakRSSBytes: peakRSS.Load(), OutputBytes: int64(len(outData))},
		Isolation: IsolationEvidence{ProductionCredentialsBlocked: true, EnvironmentIsolated: true, FilesystemIsolated: true, DirectNetworkBlocked: true, EgressMediated: true, ResourceLimitsEnforced: true},
	}, nil
}

func linuxNamespacesAvailable(unshare, trueCommand string) bool {
	command := exec.Command(unshare, "-U", "-r", "-m", "-n", "-i", "-u", trueCommand)
	command.Env = []string{"LANG=C", "TZ=UTC", "PATH=/bin"}
	return command.Run() == nil
}

func copyExecutable(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0555)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func readBounded(reader io.Reader, max int64, onExceeded func()) ([]byte, bool) {
	var buffer bytes.Buffer
	limited := io.LimitReader(reader, max+1)
	_, _ = io.Copy(&buffer, limited)
	if int64(buffer.Len()) > max {
		onExceeded()
		return buffer.Bytes()[:max], true
	}
	return buffer.Bytes(), false
}

func monitorProcess(pid int, limits pluginsecurity.IsolationLimits, peakRSS, cpuMS *atomic.Int64, reason *atomic.Value, kill func(), stop <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	memoryLimit := int64(limits.MemoryMiB) << 20
	cpuLimit := int64(limits.CPUTimeMS)
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			rss := readRSS(pid)
			if rss > peakRSS.Load() {
				peakRSS.Store(rss)
			}
			cpu := readCPUTimeMS(pid)
			if cpu > cpuMS.Load() {
				cpuMS.Store(cpu)
			}
			if rss > memoryLimit && reason.CompareAndSwap("", "memory_limit") {
				kill()
				return
			}
			if cpu > cpuLimit && reason.CompareAndSwap("", "cpu_limit") {
				kill()
				return
			}
		}
	}
}

func readRSS(pid int) int64 {
	file, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 && fields[0] == "VmRSS:" {
			value, _ := strconv.ParseInt(fields[1], 10, 64)
			return value * 1024
		}
	}
	return 0
}

func readCPUTimeMS(pid int) int64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/schedstat", pid))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	ns, _ := strconv.ParseInt(fields[0], 10, 64)
	return ns / int64(time.Millisecond)
}
