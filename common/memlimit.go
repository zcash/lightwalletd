// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
)

// memLimitFraction leaves headroom for the memory Go doesn't count as heap.
const memLimitFraction = 0.9

// Pathnames of the cgroup filesystem, variables so tests can redirect them.
var (
	procSelfCgroup = "/proc/self/cgroup"
	cgroupV2Root   = "/sys/fs/cgroup"
	cgroupV1Root   = "/sys/fs/cgroup/memory"
)

// SetMemoryLimit sets Go's soft memory limit to a fraction of the cgroup
// memory limit, and returns the limit it set (zero if it set none).
func SetMemoryLimit() int64 {
	if _, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		// Explicitly configured; the runtime has already applied it.
		return 0
	}
	max, ok := cgroupMemoryLimit()
	if !ok {
		return 0
	}
	limit := int64(float64(max) * memLimitFraction)
	debug.SetMemoryLimit(limit)
	return limit
}

// cgroupMemoryLimit returns the memory limit in bytes that applies to this
// process, and whether there is one.
func cgroupMemoryLimit() (uint64, bool) {
	if path, ok := cgroupPath(""); ok {
		if limit, ok := lowestLimit(cgroupV2Root, path, "memory.max"); ok {
			return limit, true
		}
	}
	if path, ok := cgroupPath("memory"); ok {
		return lowestLimit(cgroupV1Root, path, "memory.limit_in_bytes")
	}
	return 0, false
}

// cgroupPath returns this process's cgroup path relative to the root of the
// hierarchy: the unified one if controller is empty, else the v1 hierarchy
// that controller belongs to. A container that has its own cgroup namespace
// sees "/" here; a process on the host sees where it actually sits.
func cgroupPath(controller string) (string, bool) {
	contents, err := os.ReadFile(procSelfCgroup)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(contents), "\n") {
		// Each line is hierarchy-ID:controller-list:path.
		fields := strings.SplitN(line, ":", 3)
		if len(fields) != 3 {
			continue
		}
		if controller == "" {
			if fields[0] == "0" && fields[1] == "" {
				return fields[2], true
			}
			continue
		}
		for _, name := range strings.Split(fields[1], ",") {
			if name == controller {
				return fields[2], true
			}
		}
	}
	return "", false
}

// lowestLimit returns the most restrictive limit named by file in the cgroup
// at path under root or in any of its ancestors, since a limit on an ancestor
// constrains this process just as much as one on its own cgroup.
func lowestLimit(root, path, file string) (uint64, bool) {
	dir := filepath.Join(root, path)
	if !strings.HasPrefix(dir, root) {
		// The kernel doesn't produce such a path; don't walk out of the root.
		return 0, false
	}
	var lowest uint64
	found := false
	for {
		if limit, ok := readLimit(filepath.Join(dir, file)); ok {
			if !found || limit < lowest {
				lowest, found = limit, true
			}
		}
		parent := filepath.Dir(dir)
		if dir == root || parent == dir {
			break
		}
		dir = parent
	}
	return lowest, found
}

// readLimit reads one cgroup memory limit file, reporting whether it names a
// limit that is actually a limit.
func readLimit(pathname string) (uint64, bool) {
	contents, err := os.ReadFile(pathname)
	if err != nil {
		return 0, false
	}
	limit, err := strconv.ParseUint(strings.TrimSpace(string(contents)), 10, 64)
	if err != nil {
		// cgroup v2 writes "max" when there is no limit.
		return 0, false
	}
	if limit > uint64(math.MaxInt64)/2 {
		// cgroup v1 writes a value near 2^63 when there is no limit.
		return 0, false
	}
	return limit, true
}
