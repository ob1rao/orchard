package scan

import "runtime"

// Worker counts for storage whose behaviour differs from a local SSD.
// Concurrency hides latency but multiplies seeks, so the right number is a
// property of the device rather than of the processor.
const (
	// A network filesystem spends its time waiting for a server, so requests
	// in flight matter more than cores.
	remoteWorkers = 16
	// One userspace daemon answers every FUSE request; queueing more readers
	// against it mostly adds contention.
	fuseWorkers = 4
	// Concurrent readers turn one disk's sequential reads into seeks. The
	// user guide already suggests --workers 1 for seek-sensitive disks.
	rotatingWorkers = 2
)

// DefaultWorkers returns the count used when the storage is unremarkable or
// cannot be identified.
func DefaultWorkers() int { return min(8, max(2, runtime.NumCPU())) }

// WorkersFor returns a worker count suited to the storage behind path,
// falling back to DefaultWorkers when the platform cannot say.
func WorkersFor(path string) int {
	n := workersForPath(path)
	if n < 1 {
		n = DefaultWorkers()
	}
	return min(64, max(1, n))
}
