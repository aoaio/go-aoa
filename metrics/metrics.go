package metrics

import (
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Aurorachain/go-Aurora/log"
	"github.com/rcrowley/go-metrics"
	"github.com/rcrowley/go-metrics/exp"
)

const MetricsEnabledFlag = "metrics"
const DashboardEnabledFlag = "dashboard"

var Enabled = false

func init() {
	for _, arg := range os.Args {
		if flag := strings.TrimLeft(arg, "-"); flag == MetricsEnabledFlag || flag == DashboardEnabledFlag {
			log.Info("Enabling metrics collection")
			Enabled = true
		}
	}
	exp.Exp(metrics.DefaultRegistry)
}

func NewCounter(name string) metrics.Counter {
	if !Enabled {
		return new(metrics.NilCounter)
	}
	return metrics.GetOrRegisterCounter(name, metrics.DefaultRegistry)
}

func NewTransactionCounter(name string) metrics.Counter {
	return metrics.GetOrRegisterCounter(name, metrics.DefaultRegistry)
}

func NewMeter(name string) metrics.Meter {
	if !Enabled {
		return new(metrics.NilMeter)
	}
	return metrics.GetOrRegisterMeter(name, metrics.DefaultRegistry)
}

func NewTimer(name string) metrics.Timer {
	if !Enabled {
		return new(metrics.NilTimer)
	}
	return metrics.GetOrRegisterTimer(name, metrics.DefaultRegistry)
}

func CollectProcessMetrics(refresh time.Duration) {

	if !Enabled {
		return
	}

	memstats := make([]*runtime.MemStats, 2)
	diskstats := make([]*DiskStats, 2)
	for i := 0; i < len(memstats); i++ {
		memstats[i] = new(runtime.MemStats)
		diskstats[i] = new(DiskStats)
	}

	memAllocs := metrics.GetOrRegisterMeter("system/memory/allocs", metrics.DefaultRegistry)
	memFrees := metrics.GetOrRegisterMeter("system/memory/frees", metrics.DefaultRegistry)
	memInuse := metrics.GetOrRegisterMeter("system/memory/inuse", metrics.DefaultRegistry)
	memPauses := metrics.GetOrRegisterMeter("system/memory/pauses", metrics.DefaultRegistry)

	var diskReads, diskReadBytes, diskWrites, diskWriteBytes metrics.Meter
	if err := ReadDiskStats(diskstats[0]); err == nil {
		diskReads = metrics.GetOrRegisterMeter("system/disk/readcount", metrics.DefaultRegistry)
		diskReadBytes = metrics.GetOrRegisterMeter("system/disk/readdata", metrics.DefaultRegistry)
		diskWrites = metrics.GetOrRegisterMeter("system/disk/writecount", metrics.DefaultRegistry)
		diskWriteBytes = metrics.GetOrRegisterMeter("system/disk/writedata", metrics.DefaultRegistry)
	} else {
		log.Debug("Failed to read disk metrics", "err", err)
	}

	for i := 1; ; i++ {
		runtime.ReadMemStats(memstats[i%2])
		memAllocs.Mark(int64(memstats[i%2].Mallocs - memstats[(i-1)%2].Mallocs))
		memFrees.Mark(int64(memstats[i%2].Frees - memstats[(i-1)%2].Frees))
		memInuse.Mark(int64(memstats[i%2].Alloc - memstats[(i-1)%2].Alloc))
		memPauses.Mark(int64(memstats[i%2].PauseTotalNs - memstats[(i-1)%2].PauseTotalNs))

		if ReadDiskStats(diskstats[i%2]) == nil {
			diskReads.Mark(diskstats[i%2].ReadCount - diskstats[(i-1)%2].ReadCount)
			diskReadBytes.Mark(diskstats[i%2].ReadBytes - diskstats[(i-1)%2].ReadBytes)
			diskWrites.Mark(diskstats[i%2].WriteCount - diskstats[(i-1)%2].WriteCount)
			diskWriteBytes.Mark(diskstats[i%2].WriteBytes - diskstats[(i-1)%2].WriteBytes)
		}
		time.Sleep(refresh)
	}
}
