package telemetry

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"

	"go.opentelemetry.io/otel/metric"
)

// cgroupDir is where a container sees its own cgroup v2 files.
const cgroupDir = "/sys/fs/cgroup"

// registerProcessMetrics reports the process's CPU time and the container's
// memory use and limit, which the dashboard divides into saturation.
func registerProcessMetrics(mp metric.MeterProvider) {
	m := mp.Meter("github.com/shopnexus/server/internal/platform/telemetry")
	cpu := Must(m.Float64ObservableCounter("process.cpu.time", metric.WithUnit("s"),
		metric.WithDescription("CPU time used by the process, user and system.")))
	usage := Must(m.Int64ObservableGauge("container.memory.usage", metric.WithUnit("By"),
		metric.WithDescription("Memory the container uses, from its cgroup.")))
	limit := Must(m.Int64ObservableGauge("container.memory.limit", metric.WithUnit("By"),
		metric.WithDescription("The container's memory limit, from its cgroup.")))
	Must(m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		var ru syscall.Rusage
		if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) == nil {
			o.ObserveFloat64(cpu, seconds(ru.Utime)+seconds(ru.Stime))
		}
		if n, ok := readCgroupBytes("memory.current"); ok {
			o.ObserveInt64(usage, n)
		}
		if n, ok := readCgroupBytes("memory.max"); ok {
			o.ObserveInt64(limit, n)
		}
		return nil
	}, cpu, usage, limit))
}

func seconds(t syscall.Timeval) float64 {
	return float64(t.Sec) + float64(t.Usec)/1e6
}

func readCgroupBytes(name string) (int64, bool) {
	b, err := os.ReadFile(cgroupDir + "/" + name)
	if err != nil {
		return 0, false
	}
	return parseCgroupBytes(string(b))
}

// parseCgroupBytes reads a cgroup v2 byte count; "max" means no limit.
func parseCgroupBytes(s string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
