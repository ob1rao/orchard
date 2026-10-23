package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Machine is a saved profile for a host Orchard can scan over SSH. Host is
// anything ssh accepts as a destination, including an alias from
// ~/.ssh/config, so keys, jump hosts and ports can live there instead.
type Machine struct {
	Name     string   `json:"name"`
	Host     string   `json:"host"`
	Port     int      `json:"port,omitempty"`
	Identity string   `json:"identity,omitempty"`
	Options  []string `json:"ssh_options,omitempty"`
	Paths    []string `json:"paths,omitempty"`
	// Binary is an orchard executable already on the remote machine. When
	// empty Orchard looks for one and, failing that, uploads itself.
	Binary string    `json:"remote_binary,omitempty"`
	Sudo   bool      `json:"sudo,omitempty"`
	Notes  string    `json:"notes,omitempty"`
	Added  time.Time `json:"added"`
}

// DefaultPath is where a scan of the machine starts when none is given.
func (m Machine) DefaultPath() string {
	if len(m.Paths) > 0 {
		return m.Paths[0]
	}
	return "/"
}

func (l *Library) machinesFile() string { return filepath.Join(l.Dir, "machines.json") }

func (l *Library) Machines() ([]Machine, error) {
	b, err := os.ReadFile(l.machinesFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ms []Machine
	if err := json.Unmarshal(b, &ms); err != nil {
		return nil, fmt.Errorf("%s: %w", l.machinesFile(), err)
	}
	return ms, nil
}

func (l *Library) Machine(name string) (Machine, error) {
	ms, err := l.Machines()
	if err != nil {
		return Machine{}, err
	}
	for _, m := range ms {
		if strings.EqualFold(m.Name, name) {
			return m, nil
		}
	}
	return Machine{}, fmt.Errorf("no machine named %q (see: orchard machine list)", name)
}

func validName(name string) error {
	if name == "" || SafeName(name) != name {
		return fmt.Errorf("machine name %q must use only letters, digits, '.', '-' and '_'", name)
	}
	return nil
}

// SaveMachine adds a profile or replaces the one with the same name.
func (l *Library) SaveMachine(m Machine) error {
	if err := validName(m.Name); err != nil {
		return err
	}
	if m.Host == "" {
		return errors.New("a machine needs an SSH host")
	}
	ms, err := l.Machines()
	if err != nil {
		return err
	}
	replaced := false
	for i := range ms {
		if strings.EqualFold(ms[i].Name, m.Name) {
			if m.Added.IsZero() {
				m.Added = ms[i].Added
			}
			ms[i], replaced = m, true
		}
	}
	if m.Added.IsZero() {
		m.Added = time.Now().UTC()
	}
	if !replaced {
		ms = append(ms, m)
	}
	return l.writeMachines(ms)
}

func (l *Library) RemoveMachine(name string) error {
	ms, err := l.Machines()
	if err != nil {
		return err
	}
	kept := ms[:0]
	for _, m := range ms {
		if !strings.EqualFold(m.Name, name) {
			kept = append(kept, m)
		}
	}
	if len(kept) == len(ms) {
		return fmt.Errorf("no machine named %q", name)
	}
	return l.writeMachines(kept)
}

func (l *Library) writeMachines(ms []Machine) error {
	sort.Slice(ms, func(i, j int) bool { return strings.ToLower(ms[i].Name) < strings.ToLower(ms[j].Name) })
	if err := os.MkdirAll(l.Dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(ms, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(l.machinesFile(), func(f *os.File) error {
		_, err := f.Write(append(b, '\n'))
		return err
	})
}
