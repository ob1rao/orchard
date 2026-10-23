package library

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ob1rao/orchard/internal/snapshot"
)

// An archive is a plain tar file: snapshot files are already compressed, so
// compressing the container again would only cost time. A manifest lists the
// entries so an archive can be inspected without decoding every snapshot.
const manifestName = "orchard-archive.json"

type Manifest struct {
	Created   time.Time       `json:"created"`
	Snapshots []snapshot.Meta `json:"snapshots"`
}

// Export writes the given entries into an archive at dest.
func Export(dest string, entries []Entry) error {
	if len(entries) == 0 {
		return errors.New("nothing to archive")
	}
	return writeAtomic(dest, func(f *os.File) error {
		tw := tar.NewWriter(f)
		m := Manifest{Created: time.Now().UTC()}
		for _, e := range entries {
			m.Snapshots = append(m.Snapshots, e.Meta)
		}
		body, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: manifestName, Mode: 0o600, Size: int64(len(body)), ModTime: m.Created}); err != nil {
			return err
		}
		if _, err := tw.Write(body); err != nil {
			return err
		}
		for _, e := range entries {
			if err := addFile(tw, e); err != nil {
				return err
			}
		}
		return tw.Close()
	})
}

func addFile(tw *tar.Writer, e Entry) error {
	f, err := os.Open(e.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	name := path.Join("snapshots", SafeName(e.Machine), e.ID+Ext)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: e.ScannedAt}); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

// ReadManifest lists an archive's snapshots.
func ReadManifest(src string) (Manifest, error) {
	var m Manifest
	f, err := os.Open(src)
	if err != nil {
		return m, err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return m, errors.New("not an Orchard archive: no manifest")
		}
		if err != nil {
			return m, fmt.Errorf("not an Orchard archive: %w", err)
		}
		if h.Name == manifestName {
			err := json.NewDecoder(io.LimitReader(tr, 64<<20)).Decode(&m)
			return m, err
		}
	}
}

// ImportResult says what an archive import did.
type ImportResult struct {
	Added, Skipped []snapshot.Meta
}

// ImportArchive loads every snapshot in an archive into the library, skipping
// IDs it already holds.
func (l *Library) ImportArchive(src string) (ImportResult, error) {
	var res ImportResult
	f, err := os.Open(src)
	if err != nil {
		return res, err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	found := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return res, fmt.Errorf("not an Orchard archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg || !strings.HasSuffix(h.Name, Ext) {
			continue
		}
		found = true
		// The header is short; read it from a bounded prefix, then replay
		// that prefix ahead of the rest of the entry.
		prefix := make([]byte, min(h.Size, 2<<20))
		n, _ := io.ReadFull(tr, prefix)
		prefix = prefix[:n]
		meta, err := snapshot.ReadMeta(bytes.NewReader(prefix))
		if err != nil {
			return res, fmt.Errorf("%s: %w", h.Name, err)
		}
		if _, err := l.ByID(meta.ID); err == nil {
			res.Skipped = append(res.Skipped, meta)
			continue
		}
		if _, _, err := l.importReader(meta, io.MultiReader(bytes.NewReader(prefix), tr)); err != nil {
			return res, fmt.Errorf("%s: %w", h.Name, err)
		}
		res.Added = append(res.Added, meta)
	}
	if !found {
		return res, errors.New("archive holds no snapshots")
	}
	return res, nil
}
