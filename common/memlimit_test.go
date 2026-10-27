// Copyright (c) 2019-present The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package common

import (
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"
)

// fakeCgroup builds a cgroup filesystem under a temporary directory: procSelf
// becomes the contents of /proc/self/cgroup, and limits maps a pathname
// relative to the v2 root to that file's contents.
func fakeCgroup(t *testing.T, procSelf string, limits map[string]string) {
	t.Helper()
	saved := [3]string{procSelfCgroup, cgroupV2Root, cgroupV1Root}
	t.Cleanup(func() {
		procSelfCgroup, cgroupV2Root, cgroupV1Root = saved[0], saved[1], saved[2]
	})
	root := t.TempDir()
	procSelfCgroup = filepath.Join(root, "proc-self-cgroup")
	cgroupV2Root = filepath.Join(root, "sys/fs/cgroup")
	cgroupV1Root = filepath.Join(cgroupV2Root, "memory")
	if err := os.WriteFile(procSelfCgroup, []byte(procSelf), 0644); err != nil {
		t.Fatal(err)
	}
	for name, contents := range limits {
		pathname := filepath.Join(cgroupV2Root, name)
		if err := os.MkdirAll(filepath.Dir(pathname), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(pathname, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

const (
	// What a container with its own cgroup namespace sees.
	inNamespace = "0::/\n"
	// What a process on the host sees, under two levels of slice.
	onHost = "0::/user.slice/lwd.service\n"
	// A cgroup v1 host, which lists one line per controller.
	v1OnHost = "9:memory:/docker/abc\n8:cpu,cpuacct:/docker/abc\n"
)

func TestCgroupMemoryLimit(t *testing.T) {
	for _, tc := range []struct {
		name     string
		procSelf string
		limits   map[string]string
		want     uint64
	}{{
		name:     "v2 in a cgroup namespace",
		procSelf: inNamespace,
		limits:   map[string]string{"memory.max": "268435456\n"},
		want:     268435456,
	}, {
		// The bug this replaced: reading the root's memory.max on a host
		// finds nothing, because the limit is on the process's own cgroup.
		name:     "v2 on a host, limit on the leaf",
		procSelf: onHost,
		limits: map[string]string{
			"memory.max":                        "max\n",
			"user.slice/memory.max":             "max\n",
			"user.slice/lwd.service/memory.max": "268435456\n",
		},
		want: 268435456,
	}, {
		// An ancestor's limit constrains this process just as much.
		name:     "v2 limit on an ancestor only",
		procSelf: onHost,
		limits: map[string]string{
			"user.slice/memory.max":             "134217728\n",
			"user.slice/lwd.service/memory.max": "max\n",
		},
		want: 134217728,
	}, {
		name:     "v2 most restrictive ancestor wins",
		procSelf: onHost,
		limits: map[string]string{
			"memory.max":                        "1073741824\n",
			"user.slice/memory.max":             "134217728\n",
			"user.slice/lwd.service/memory.max": "268435456\n",
		},
		want: 134217728,
	}, {
		name:     "v2 unlimited everywhere",
		procSelf: onHost,
		limits: map[string]string{
			"memory.max":                        "max\n",
			"user.slice/lwd.service/memory.max": "max\n",
		},
		want: 0,
	}, {
		name:     "v1",
		procSelf: v1OnHost,
		limits:   map[string]string{"memory/docker/abc/memory.limit_in_bytes": "268435456\n"},
		want:     268435456,
	}, {
		// cgroup v1 spells "unlimited" as a value near 2^63.
		name:     "v1 unlimited",
		procSelf: v1OnHost,
		limits:   map[string]string{"memory/docker/abc/memory.limit_in_bytes": "9223372036854771712\n"},
		want:     0,
	}, {
		// An unlimited v2 hierarchy must not mask a v1 limit.
		name:     "v2 unlimited falls back to v1",
		procSelf: onHost + v1OnHost,
		limits: map[string]string{
			"memory.max":                              "max\n",
			"user.slice/lwd.service/memory.max":       "max\n",
			"memory/docker/abc/memory.limit_in_bytes": "134217728\n",
		},
		want: 134217728,
	}, {
		name:     "no cgroup filesystem",
		procSelf: onHost,
		want:     0,
	}, {
		name:     "unparsable /proc/self/cgroup",
		procSelf: "not a cgroup line\n",
		limits:   map[string]string{"memory.max": "268435456\n"},
		want:     0,
	}, {
		name:     "unparsable limit",
		procSelf: inNamespace,
		limits:   map[string]string{"memory.max": "not a number\n"},
		want:     0,
	}, {
		// Must be rejected rather than wrapped negative.
		name:     "too large for int64",
		procSelf: inNamespace,
		limits:   map[string]string{"memory.max": "18446744073709551615\n"},
		want:     0,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			fakeCgroup(t, tc.procSelf, tc.limits)
			limit, ok := cgroupMemoryLimit()
			if ok != (tc.want != 0) || (ok && limit != tc.want) {
				t.Fatalf("got (%d, %v), want (%d, %v)", limit, ok, tc.want, tc.want != 0)
			}
		})
	}
}

// An explicit GOMEMLIMIT is the operator's; SetMemoryLimit must not override it.
func TestSetMemoryLimitDefersToEnvironment(t *testing.T) {
	fakeCgroup(t, inNamespace, map[string]string{"memory.max": "268435456\n"})
	t.Setenv("GOMEMLIMIT", "64MiB")
	if limit := SetMemoryLimit(); limit != 0 {
		t.Fatalf("set limit %d, want 0", limit)
	}
}

func TestSetMemoryLimitFromCgroup(t *testing.T) {
	fakeCgroup(t, inNamespace, map[string]string{"memory.max": "268435456\n"})
	if was, ok := os.LookupEnv("GOMEMLIMIT"); ok {
		os.Unsetenv("GOMEMLIMIT")
		t.Cleanup(func() { os.Setenv("GOMEMLIMIT", was) })
	}
	t.Cleanup(func() { debug.SetMemoryLimit(math.MaxInt64) })
	// 90% of 256MiB.
	if limit := SetMemoryLimit(); limit != 241591910 {
		t.Fatalf("set limit %d, want 241591910", limit)
	}
}

// No cgroup limit means no Go limit, not a limit of zero.
func TestSetMemoryLimitNoCgroupLimit(t *testing.T) {
	fakeCgroup(t, inNamespace, map[string]string{"memory.max": "max\n"})
	if limit := SetMemoryLimit(); limit != 0 {
		t.Fatalf("set limit %d, want 0", limit)
	}
}
