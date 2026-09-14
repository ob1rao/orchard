package volumes

import "sort"

type Volume struct {
	Topology
	Device, Path, Type      string
	Total, Free, Available  uint64
	Label, UUID, MountIssue string
}

func List() ([]Volume, error) {
	v, err := list()
	sort.Slice(v, func(i, j int) bool {
		if v[i].Path == "/" {
			return true
		}
		if v[j].Path == "/" {
			return false
		}
		return v[i].Path < v[j].Path
	})
	return v, err
}
