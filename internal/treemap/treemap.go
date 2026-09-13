// Package treemap partitions a terminal rectangle into weighted, non-overlapping tiles.
package treemap

type Rect struct{ X, Y, W, H int }

func (r Rect) Contains(x, y int) bool { return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H }

type Tile struct {
	Rect
	Index int
}

// Layout uses balanced binary partitioning, correcting for cells being twice as
// tall as they are wide. Sub-cell entries are omitted, never inflated; the list
// alongside the map keeps every entry accessible.
func Layout(weights []uint64, r Rect) []Tile {
	type entry struct {
		i int
		v float64
	}
	entries := make([]entry, 0, len(weights))
	var total float64
	for i, w := range weights {
		if w > 0 {
			entries = append(entries, entry{i, float64(w)})
			total += float64(w)
		}
	}
	tiles := make([]Tile, 0, min(len(entries), max(0, r.W*r.H)))
	var split func([]entry, float64, Rect)
	split = func(es []entry, sum float64, r Rect) {
		if len(es) == 0 || r.W <= 0 || r.H <= 0 {
			return
		}
		if len(es) == 1 {
			tiles = append(tiles, Tile{r, es[0].i})
			return
		}
		var left float64
		k := 0
		for k < len(es)-1 {
			if k > 0 && left+es[k].v > sum/2 && sum/2-left < left+es[k].v-sum/2 {
				break
			}
			left += es[k].v
			k++
			if left >= sum/2 {
				break
			}
		}
		if r.W >= r.H*2 {
			cut := int(float64(r.W)*left/sum + 0.5)
			split(es[:k], left, Rect{r.X, r.Y, cut, r.H})
			split(es[k:], sum-left, Rect{r.X + cut, r.Y, r.W - cut, r.H})
		} else {
			cut := int(float64(r.H)*left/sum + 0.5)
			split(es[:k], left, Rect{r.X, r.Y, r.W, cut})
			split(es[k:], sum-left, Rect{r.X, r.Y + cut, r.W, r.H - cut})
		}
	}
	split(entries, total, r)
	return tiles
}
