package treemap

import (
	"math/rand"
	"testing"
)

func TestLayoutInvariants(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 400; trial++ {
		w, h := 1+rng.Intn(120), 1+rng.Intn(50)
		weights := make([]uint64, 1+rng.Intn(300))
		for i := range weights {
			weights[i] = uint64(rng.Intn(100000))
		}
		r := Rect{3, 5, w, h}
		tiles := Layout(weights, r)
		cells := map[[2]int]bool{}
		for _, tile := range tiles {
			if tile.W <= 0 || tile.H <= 0 || weights[tile.Index] == 0 {
				t.Fatalf("invalid tile %+v", tile)
			}
			for y := tile.Y; y < tile.Y+tile.H; y++ {
				for x := tile.X; x < tile.X+tile.W; x++ {
					if !r.Contains(x, y) {
						t.Fatal("out of bounds")
					}
					p := [2]int{x, y}
					if cells[p] {
						t.Fatal("overlapping tiles")
					}
					cells[p] = true
				}
			}
		}
		if len(cells) != w*h {
			t.Fatalf("unfilled cells %d != %d", len(cells), w*h)
		}
	}
}
func TestKnownProportions(t *testing.T) {
	tiles := Layout([]uint64{75, 25}, Rect{0, 0, 100, 20})
	if len(tiles) != 2 || tiles[0].W*tiles[0].H != 1500 || tiles[1].W*tiles[1].H != 500 {
		t.Fatalf("wrong proportions: %+v", tiles)
	}
	if len(Layout([]uint64{0, 0}, Rect{0, 0, 100, 20})) != 0 {
		t.Fatal("zero size tiles")
	}
	if len(Layout([]uint64{1}, Rect{})) != 0 {
		t.Fatal("empty viewport")
	}
}
func TestExtremeWeights(t *testing.T) {
	for _, weights := range [][]uint64{{^uint64(0), 1, 1}, {1, 1, ^uint64(0)}} {
		tiles := Layout(weights, Rect{0, 0, 80, 30})
		if len(tiles) == 0 {
			t.Fatal("no tiles")
		}
		for _, tile := range tiles {
			if tile.W < 1 || tile.H < 1 || tile.W > 80 || tile.H > 30 {
				t.Fatalf("invalid: %+v", tile)
			}
		}
	}
}
func BenchmarkLayout(b *testing.B) {
	weights := make([]uint64, 10000)
	for i := range weights {
		weights[i] = uint64(10000 - i)
	}
	for i := 0; i < b.N; i++ {
		Layout(weights, Rect{0, 0, 160, 50})
	}
}
