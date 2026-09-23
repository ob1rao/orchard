package ui

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"sort"
	"strings"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
	"github.com/ob1rao/orchard/internal/treemap"
	"github.com/ob1rao/orchard/internal/volumes"
)

// Filesystem families keep a stable colour so the same kind of volume reads the
// same on every disk. Unrecognised types fall back to the treemap palette.
var fsColors = map[string]int32{
	"apfs": 0x4f6fb8, "hfs": 0x4f6fb8, "hfsplus": 0x4f6fb8,
	"ext2": 0x3f7f63, "ext3": 0x3f7f63, "ext4": 0x3f7f63,
	"xfs": 0x3f7f63, "btrfs": 0x3f7f63, "zfs": 0x3f7f63,
	"vfat": 0x8f7434, "msdos": 0x8f7434, "exfat": 0x8f7434, "ntfs": 0x8f7434, "ntfs3": 0x8f7434,
	"iso9660": 0x6b6378, "udf": 0x6b6378, "squashfs": 0x6b6378,
	"nfs": 0x3f6f7d, "smbfs": 0x3f6f7d, "cifs": 0x3f6f7d, "virtiofs": 0x3f6f7d, "9p": 0x3f6f7d, "afpfs": 0x3f6f7d,
}

// Free space is black here as it is in the treemap, so one colour convention
// carries across both screens.
const freeHex = 0x000000

func fsColor(fsType string) tcell.Color {
	name := strings.ToLower(fsType)
	if hex, ok := fsColors[name]; ok {
		return tcell.NewHexColor(hex)
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	// Keep the hash unsigned: converting to int can go negative on 32-bit Pis.
	return tcell.NewHexColor(palette[hash.Sum32()%uint32(len(palette))])
}

func diskKey(v volumes.Volume) string {
	if v.Disk != "" {
		return v.Disk
	}
	return v.Device
}

func fraction(part, whole uint64) float64 {
	if whole == 0 {
		return 0
	}
	return min(1, float64(part)/float64(whole))
}

// storagePool is capacity its members draw from together: a plain partition is
// a pool of one, while APFS volumes share a container's bytes between them.
type storagePool struct {
	container   string
	total, free uint64
	mounted     bool
	members     []int // indexes into the volume slice the pool was built from
}

// storageDisk is one physical disk, or one mount whose ancestry the system did
// not report. Grouping volumes under it lets the picker draw shared capacity
// as containment rather than as a note the reader has to join up.
type storageDisk struct {
	key      string
	capacity uint64
	reported bool
	pools    []storagePool
}

// groupStorage rebuilds the disk/pool/volume nesting the picker draws. It is
// cheap enough to redo each frame, which keeps it honest about the current
// volume list instead of caching an order that can fall out of step with it.
func groupStorage(vs []volumes.Volume) []storageDisk {
	var disks []storageDisk
	diskAt, poolAt := map[string]int{}, map[string]int{}
	for i, v := range vs {
		key := diskKey(v)
		if _, ok := diskAt[key]; !ok {
			diskAt[key] = len(disks)
			disks = append(disks, storageDisk{key: key})
		}
		disk := &disks[diskAt[key]]
		if v.DiskSize > disk.capacity {
			disk.capacity, disk.reported = v.DiskSize, true
		}
		name := v.Pool
		if name == "" {
			name = v.Device
		}
		if _, ok := poolAt[key+"\x00"+name]; !ok {
			poolAt[key+"\x00"+name] = len(disk.pools)
			disk.pools = append(disk.pools, storagePool{container: v.Pool})
		}
		pool := &disk.pools[poolAt[key+"\x00"+name]]
		pool.members = append(pool.members, i)
		pool.mounted = pool.mounted || v.Path != ""
		if v.Total > pool.total {
			pool.total, pool.free = v.Total, min(v.Free, v.Total)
		}
	}
	for d := range disks {
		disk := &disks[d]
		claimed := uint64(0)
		for p := range disk.pools {
			pool := &disk.pools[p]
			claimed += pool.total
			sort.SliceStable(pool.members, func(x, y int) bool {
				return owned(vs[pool.members[x]]) > owned(vs[pool.members[y]])
			})
		}
		// A disk smaller than the volumes on it cannot contain them; widen it
		// rather than draw a volume spilling outside its own disk.
		disk.capacity = max(disk.capacity, claimed)
		sort.SliceStable(disk.pools, func(x, y int) bool { return disk.pools[x].total > disk.pools[y].total })
	}
	holdsRoot := func(d storageDisk) bool {
		for _, p := range d.pools {
			for _, i := range p.members {
				if vs[i].Path == "/" {
					return true
				}
			}
		}
		return false
	}
	sort.SliceStable(disks, func(x, y int) bool {
		if holdsRoot(disks[x]) != holdsRoot(disks[y]) {
			return holdsRoot(disks[x])
		}
		return disks[x].capacity > disks[y].capacity
	})
	return disks
}

// owned is the capacity a volume holds by itself. Pool members report their
// container's size through statfs, so only a container report can size them.
func owned(v volumes.Volume) uint64 {
	if v.Pool != "" {
		return v.Owned
	}
	return v.Total
}

// storageOrder lists volumes grouped by the disk they sit on, so arrow keys
// walk the picker in the order the reader sees.
func storageOrder(groups ...[]storageDisk) []int {
	var out []int
	for _, disks := range groups {
		for _, d := range disks {
			for _, p := range d.pools {
				out = append(out, p.members...)
			}
		}
	}
	return out
}

// splitStorage separates disks the system described physically from mounts it
// could not trace to one. Only real disks belong in a map of physical storage:
// a handful of network or virtual mounts would otherwise size the map and
// squeeze the disks the reader came to look at down to nothing. When nothing
// has a reported ancestry, drawing what we do know beats drawing an empty map.
func splitStorage(disks []storageDisk) (physical, other []storageDisk) {
	for _, d := range disks {
		if d.reported {
			physical = append(physical, d)
		} else {
			other = append(other, d)
		}
	}
	if len(physical) == 0 {
		return other, nil
	}
	return physical, other
}

func reportedDisks(disks []storageDisk) int {
	n := 0
	for _, d := range disks {
		if d.reported {
			n++
		}
	}
	return n
}

func (d storageDisk) heading() string {
	name := strings.TrimPrefix(d.key, "/dev/")
	if !d.reported {
		return fmt.Sprintf("%s · %s seen", name, Bytes(d.capacity))
	}
	return fmt.Sprintf("%s · %s · %s", name, Bytes(d.capacity), plural(d.volumeCount(), "volume"))
}

// usage counts each pool once: volumes sharing a container all report the same
// bytes, so adding them up would multiply the disk's real occupancy.
func (d storageDisk) usage() (used, free uint64) {
	for _, p := range d.pools {
		if !p.mounted {
			continue
		}
		used += p.total - p.free
		free += p.free
	}
	used = min(used, d.capacity)
	return used, min(free, d.capacity-used)
}

func (d storageDisk) volumeCount() int {
	n := 0
	for _, p := range d.pools {
		n += len(p.members)
	}
	return n
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func (d storageDisk) holds(volume int) bool {
	for _, p := range d.pools {
		for _, i := range p.members {
			if i == volume {
				return true
			}
		}
	}
	return false
}

// drawStorageMap paints each physical disk as a frame with its volumes nested
// inside, so two volumes sharing a disk are visibly inside one box. Tile area
// is bytes: colour is occupied space, black is free, and hatching is capacity
// the system reports but attributes to no volume.
func (a *App) drawStorageMap(disks []storageDisk, r treemap.Rect) {
	if r.W < 12 || r.H < 5 {
		return
	}
	capacities := make([]uint64, len(disks))
	for i, d := range disks {
		capacities[i] = d.capacity
	}
	y := r.Y
	for i, height := range bandHeights(capacities, r.H, 6) {
		if height <= 0 {
			continue
		}
		a.drawDisk(disks[i], treemap.Rect{X: r.X, Y: y, W: r.W, H: height})
		y += height
	}
}

// Disks stack as bands whose height tracks capacity. Area alone cannot span
// the range between a memory card and a 4 TB disk without one of them
// disappearing, and a picker that hides a disk fails at its only job, so every
// band starts at a height that fits a frame and shares out what is left over.
func bandHeights(capacities []uint64, height, minimum int) []int {
	out := make([]int, len(capacities))
	n := len(capacities)
	if n == 0 || height <= 0 {
		return out
	}
	if height < n*2 {
		// Not even a usable row each: draw the disks listed first, which are
		// the system disk and then the largest.
		for i := 0; i < min(n, height); i++ {
			out[i] = 1
		}
		return out
	}
	total := uint64(0)
	for _, c := range capacities {
		total += c
	}
	remainder, rows := make([]float64, n), 0
	for i, c := range capacities {
		exact := float64(height) / float64(n)
		if total > 0 {
			exact = float64(height) * float64(c) / float64(total)
		}
		out[i] = int(exact)
		remainder[i] = exact - float64(out[i])
		rows += out[i]
	}
	// Largest remainder first, so the rounding loss lands where it is smallest
	// in proportion rather than always on the same band.
	for ; rows < height; rows++ {
		best := 0
		for i := range out {
			if remainder[i] > remainder[best] {
				best = i
			}
		}
		out[best]++
		remainder[best] = -1
	}
	// Only now raise the bands that came out too thin to draw, paying for them
	// from the tallest band. Disks in a comparable range keep their proportion.
	minimum = max(1, min(minimum, height/n))
	short := 0
	for i := range out {
		if out[i] < minimum {
			short += minimum - out[i]
			out[i] = minimum
		}
	}
	for ; short > 0; short-- {
		tallest := -1
		for i := range out {
			if out[i] > minimum && (tallest < 0 || out[i] > out[tallest]) {
				tallest = i
			}
		}
		if tallest < 0 {
			break
		}
		out[tallest]--
	}
	return out
}

func (a *App) drawDisk(d storageDisk, r treemap.Rect) {
	border := base.Foreground(muted)
	if d.holds(a.disk) {
		border = base.Foreground(fg)
	}
	title := strings.TrimPrefix(d.key, "/dev/") + " · " + Bytes(d.capacity)
	if !d.reported {
		title = strings.TrimPrefix(d.key, "/dev/") + " · " + Bytes(d.capacity) + " seen · disk unknown"
	}
	inner, framed := a.frame(r, title, border)
	if !framed {
		used, free := d.usage()
		a.gauge(r, fraction(used, used+free), tcell.NewHexColor(0x4f6fb8))
		return
	}
	claimed := uint64(0)
	for _, p := range d.pools {
		claimed += p.total
	}
	weights := make([]uint64, len(d.pools)+1)
	for i, p := range d.pools {
		weights[i] = p.total
	}
	weights[len(d.pools)] = d.capacity - min(d.capacity, claimed)
	for _, t := range treemap.Layout(weights, inner) {
		if t.Index == len(d.pools) {
			a.hatch(t.Rect, muted, "unallocated", Bytes(weights[t.Index]))
			continue
		}
		a.drawPool(d.pools[t.Index], t.Rect)
	}
}

func (a *App) drawPool(p storagePool, r treemap.Rect) {
	if p.container == "" {
		a.drawVolumeTile(p.members[0], r)
		return
	}
	selected := false
	for _, i := range p.members {
		selected = selected || i == a.disk
	}
	border := base.Foreground(muted)
	if selected {
		border = base.Foreground(fg)
	}
	name := strings.TrimPrefix(p.container, "/dev/")
	inner, framed := a.frame(r, name+" · shared "+Bytes(p.total), border)
	if !framed {
		// Too small to divide: show the container's own level rather than
		// crediting every byte in it to whichever volume is listed first.
		color := fsColor(a.volumes[p.members[0]].Type)
		level := fraction(p.total-p.free, p.total)
		a.gauge(r, level, color)
		a.pickerTiles = append(a.pickerTiles, pickerTile{r, p.members[0]})
		a.tile(r, name, Bytes(p.total), func(y int) tcell.Color { return a.gaugeRowColor(r, level, color, y) }, selected)
		return
	}
	// Inside a container, a volume's tile is the bytes it alone holds and the
	// free space is one tile they all draw from, which is what "shared" means.
	held := uint64(0)
	for _, i := range p.members {
		held += owned(a.volumes[i])
	}
	used := p.total - p.free
	weights := make([]uint64, len(p.members)+2)
	for i, index := range p.members {
		weights[i] = owned(a.volumes[index])
	}
	weights[len(p.members)] = used - min(used, held)
	weights[len(p.members)+1] = p.free
	for _, t := range treemap.Layout(weights, inner) {
		switch t.Index {
		case len(p.members):
			// Without a per-volume report every member reports the container's
			// own occupancy, so its bytes can only be shown as one block.
			label := "system & snapshots"
			if held == 0 {
				label = "in use"
			}
			a.hatch(t.Rect, fg, label, Bytes(weights[t.Index]))
		case len(p.members) + 1:
			a.free(t.Rect, "free", Bytes(p.free))
		default:
			a.drawVolumeTile(p.members[t.Index], t.Rect)
		}
	}
}

func (a *App) drawVolumeTile(index int, r treemap.Rect) {
	v := a.volumes[index]
	color := fsColor(v.Type)
	a.pickerTiles = append(a.pickerTiles, pickerTile{r, index})
	used, detail := 0.0, Bytes(v.Total)
	switch {
	case v.Path == "":
		detail = "unmounted"
		a.fillHatch(r, color)
	case v.Pool != "":
		// A pool member's tile already is its own bytes: all of it is in use.
		used, detail = 1, Bytes(owned(v))
		a.gauge(r, used, color)
	default:
		used = fraction(v.Total-min(v.Free, v.Total), v.Total)
		a.gauge(r, used, color)
	}
	a.tile(r, volumeName(v), detail, func(y int) tcell.Color { return a.gaugeRowColor(r, used, color, y) }, index == a.disk)
}

func volumeName(v volumes.Volume) string {
	if v.Path != "" {
		return v.Path
	}
	if v.Label != "" {
		return v.Label
	}
	return strings.TrimPrefix(v.Device, "/dev/")
}

// frame draws a rounded box with its title set into the top edge and returns
// the interior. A box too small to carry a border is left to the caller.
func (a *App) frame(r treemap.Rect, title string, border tcell.Style) (treemap.Rect, bool) {
	if r.W < 5 || r.H < 4 {
		return treemap.Rect{}, false
	}
	a.screen.FillArea(r.X, r.Y, r.W, r.H, ' ', base)
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		a.screen.PutStrStyled(x, r.Y, "─", border)
		a.screen.PutStrStyled(x, r.Y+r.H-1, "─", border)
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		a.screen.PutStrStyled(r.X, y, "│", border)
		a.screen.PutStrStyled(r.X+r.W-1, y, "│", border)
	}
	a.screen.PutStrStyled(r.X, r.Y, "╭", border)
	a.screen.PutStrStyled(r.X+r.W-1, r.Y, "╮", border)
	a.screen.PutStrStyled(r.X, r.Y+r.H-1, "╰", border)
	a.screen.PutStrStyled(r.X+r.W-1, r.Y+r.H-1, "╯", border)
	if r.W >= 10 {
		a.text(r.X+2, r.Y, r.W-4, " "+title+" ", border.Bold(true))
	}
	return treemap.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}, true
}

// gauge fills a tile from the bottom in proportion to the space in use,
// leaving the rest black. Half blocks give the level twice the vertical
// resolution of whole cells, so a nearly empty volume still shows a rise.
func (a *App) gauge(r treemap.Rect, used float64, color tcell.Color) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	free := tcell.NewHexColor(freeHex)
	a.screen.FillArea(r.X, r.Y, r.W, r.H, ' ', base.Background(free))
	halves := gaugeHalves(r.H, used)
	if full := halves / 2; full > 0 {
		a.screen.FillArea(r.X, r.Y+r.H-full, r.W, full, ' ', base.Background(color))
	}
	if halves%2 == 1 {
		y := r.Y + r.H - halves/2 - 1
		for x := r.X; x < r.X+r.W; x++ {
			a.screen.PutStrStyled(x, y, "▄", base.Foreground(color).Background(free))
		}
	}
}

// A volume holding any bytes at all keeps at least a half cell, so a small
// volume on a large disk reads as occupied rather than as empty.
func gaugeHalves(height int, used float64) int {
	halves := min(height*2, max(0, int(used*float64(height)*2+0.5)))
	if used > 0 {
		halves = max(1, halves)
	}
	return halves
}

// gaugeRowColor reports the background a row ended up with, so a label can be
// written over the gauge without a style of its own erasing it.
func (a *App) gaugeRowColor(r treemap.Rect, used float64, color tcell.Color, y int) tcell.Color {
	if y >= r.Y+r.H-gaugeHalves(r.H, used)/2 {
		return color
	}
	return tcell.NewHexColor(freeHex)
}

func (a *App) fillHatch(r treemap.Rect, color tcell.Color) {
	if r.W > 0 && r.H > 0 {
		a.screen.FillArea(r.X, r.Y, r.W, r.H, '░', base.Foreground(color).Background(tcell.NewHexColor(freeHex)))
	}
}

// hatch marks capacity that is reported but not measurable: gaps between
// partitions, or a container's occupied bytes with no volume claiming them.
func (a *App) hatch(r treemap.Rect, color tcell.Color, name, detail string) {
	a.fillHatch(r, color)
	a.tile(r, name, detail, onBlack, false)
}

func (a *App) free(r treemap.Rect, name, detail string) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	a.screen.FillArea(r.X, r.Y, r.W, r.H, ' ', base.Background(tcell.NewHexColor(freeHex)))
	a.tile(r, name, detail, onBlack, false)
}

func onBlack(int) tcell.Color { return tcell.NewHexColor(freeHex) }

// label writes a name, and a size below it when the tile is tall enough. Each
// line takes the background its own row was filled with, so writing over a
// gauge neither erases the level nor leaves half blocks behind the text.
func (a *App) label(r treemap.Rect, name, detail string, background func(int) tcell.Color, marked bool) {
	if r.W < 5 || r.H < 1 {
		return
	}
	style := base.Background(background(r.Y))
	if marked {
		style = style.Foreground(accent).Bold(true)
		name = "› " + name
	}
	if displaywidth.String(name) > r.W-2 && strings.Contains(name, "/") {
		name = filepath.Base(name)
	}
	// Padding the text carries its row's background one cell past each end, so
	// a label that lands on the gauge's half-block row is not crowded by it.
	a.text(r.X, r.Y, r.W, " "+name+" ", style)
	if r.H >= 2 {
		a.text(r.X, r.Y+1, r.W, " "+detail+" ", base.Background(background(r.Y+1)).Foreground(muted))
	}
}

// tile finishes a map rectangle the caller has filled: a border, so two nearly
// empty neighbours do not merge into one black area, and a label inside it.
// It borrows the treemap's own tile styling so both screens read alike.
func (a *App) tile(r treemap.Rect, name, detail string, background func(int) tcell.Color, selected bool) {
	if r.W <= 0 || r.H <= 0 {
		return
	}
	if r.W < 3 || r.H < 3 {
		// A name clipped to "/…" tells the reader nothing the list does not
		// already say, so a tile this small carries only the selection mark.
		if selected {
			a.text(r.X, r.Y, r.W, "›", base.Background(background(r.Y)).Foreground(accent).Bold(true))
		}
		return
	}
	border := base.Foreground(tcell.NewHexColor(0xa4b9cb))
	if selected {
		border = base.Foreground(accent).Bold(true)
	}
	// The border keeps whatever background its own row was filled with, so
	// drawing it does not punch a hole in the level underneath.
	style := func(y int) tcell.Style { return border.Background(background(y)) }
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		a.screen.PutStrStyled(x, r.Y, "─", style(r.Y))
		a.screen.PutStrStyled(x, r.Y+r.H-1, "─", style(r.Y+r.H-1))
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		a.screen.PutStrStyled(r.X, y, "│", style(y))
		a.screen.PutStrStyled(r.X+r.W-1, y, "│", style(y))
	}
	a.screen.PutStrStyled(r.X, r.Y, "╭", style(r.Y))
	a.screen.PutStrStyled(r.X+r.W-1, r.Y, "╮", style(r.Y))
	a.screen.PutStrStyled(r.X, r.Y+r.H-1, "╰", style(r.Y+r.H-1))
	a.screen.PutStrStyled(r.X+r.W-1, r.Y+r.H-1, "╯", style(r.Y+r.H-1))
	a.label(treemap.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: r.H - 2}, name, detail, background, selected)
}

// bar draws a fixed-width usage meter. Eighth-width blocks resolve a level
// that whole cells would round away at this size.
func bar(used float64, width int) (filled string, rest string) {
	if width <= 0 {
		return "", ""
	}
	eighths := min(width*8, max(0, int(used*float64(width)*8+0.5)))
	if used > 0 {
		eighths = max(1, eighths)
	}
	filled = strings.Repeat("█", eighths/8)
	if eighths%8 != 0 {
		filled += string([]rune("▏▎▍▌▋▊▉")[eighths%8-1])
	}
	return filled, strings.Repeat("░", width-len([]rune(filled)))
}
