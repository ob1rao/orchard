//go:build linux

package volumes

import (
	"bufio"
	"golang.org/x/sys/unix"
	"os"
	"strings"
)

func unescape(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}
func list() ([]Volume, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Volume
	seen := map[string]bool{}
	pseudo := map[string]bool{"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true, "tmpfs": true, "cgroup": true, "cgroup2": true, "securityfs": true, "pstore": true, "debugfs": true, "tracefs": true, "configfs": true, "mqueue": true, "hugetlbfs": true, "fusectl": true, "autofs": true, "binfmt_misc": true, "rpc_pipefs": true, "nsfs": true}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 1024*1024)
	for s.Scan() {
		parts := strings.SplitN(s.Text(), " - ", 2)
		if len(parts) != 2 {
			continue
		}
		a, b := strings.Fields(parts[0]), strings.Fields(parts[1])
		if len(a) < 5 || len(b) < 2 {
			continue
		}
		p := unescape(a[4])
		if seen[p] || p != "/" && pseudo[b[0]] {
			continue
		}
		var st unix.Statfs_t
		if unix.Statfs(p, &st) != nil || st.Blocks == 0 {
			continue
		}
		var info unix.Stat_t
		if unix.Stat(p, &info) != nil || info.Mode&unix.S_IFMT != unix.S_IFDIR {
			continue
		}
		seen[p] = true
		out = append(out, Volume{unescape(b[1]), p, b[0], st.Blocks * uint64(st.Bsize), st.Bfree * uint64(st.Bsize), st.Bavail * uint64(st.Bsize)})
	}
	return out, s.Err()
}
