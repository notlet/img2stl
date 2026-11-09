package main

import (
	"strings"
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
)

type vec [3]float32
type triangle [3]vec

type builder struct {
	tris  []triangle
	px    float64 // millimetres per image pixel
	base  float64 // thickness of the flat part, mm
	raise float64 // extra height of the raised part, mm
}

// p makes a point from pixel-grid coordinates (x, y) and a height z in mm.
func (b *builder) p(x, y int, z float64) vec {
	return vec{float32(float64(x) * b.px), float32(float64(y) * b.px), float32(z)}
}

// tri adds one triangle. Vertices must be counter-clockwise seen from outside.
func (b *builder) tri(p0, p1, p2 vec) {
	b.tris = append(b.tris, triangle{p0, p1, p2})
}

func (b *builder) quad(p0, p1, p2, p3 vec) {
	b.tri(p0, p1, p2)
	b.tri(p0, p2, p3)
}

// strip fills the flat area between a lower edge (at row line y) and an upper
// edge (at row line y+1). Each edge is a list of x positions; the two lists may
// have different extra points on them, and the triangles connect them without
// leaving gaps. The triangles face up, or down if facingDown is set.
func (b *builder) strip(lower, upper []int, y int, z float64, facingDown bool) {
	add := b.tri
	if facingDown {
		add = func(p0, p1, p2 vec) { b.tri(p0, p2, p1) }
	}
	i, j := 0, 0
	for i < len(lower)-1 || j < len(upper)-1 {
		if i < len(lower)-1 && (j == len(upper)-1 || lower[i+1] <= upper[j+1]) {
			add(b.p(lower[i], y, z), b.p(lower[i+1], y, z), b.p(upper[j], y+1, z))
			i++
		} else {
			add(b.p(lower[i], y, z), b.p(upper[j+1], y+1, z), b.p(upper[j], y+1, z))
			j++
		}
	}
}

// chain returns lo, then every edge strictly between lo and hi, then hi.
func chain(edges []int, lo, hi int) []int {
	c := []int{lo}
	for _, e := range edges {
		if e > lo && e < hi {
			c = append(c, e)
		}
	}
	return append(c, hi)
}

// build creates the whole mesh. state[y][x] describes pixel (x, y):
//
//	0 = no material, 1 = flat base, 2 = raised
//
// y = 0 is the BOTTOM row of the image, so the model isn't mirrored when seen
// from above.
func (b *builder) build(state [][]uint8) {
	h, w := len(state), len(state[0])
	ht := [3]float64{0, b.base, b.base + b.raise} // height of each state

	// Every wall is split at these heights so that the corners of neighbouring
	// wall pieces line up exactly and the mesh stays watertight.
	levels := []float64{0}
	if b.base > 0 {
		levels = append(levels, b.base)
	}
	levels = append(levels, ht[2])

	get := func(y, x int) uint8 { // pixels outside the image have no material
		if y < 0 || y >= h || x < 0 || x >= w {
			return 0
		}
		return state[y][x]
	}

	// edges[y] holds the x positions where row y changes state, plus 0 and w.
	// Consecutive entries therefore delimit runs of identical pixels.
	edges := make([][]int, h)
	for y := range state {
		edges[y] = []int{0}
		for x := 1; x < w; x++ {
			if state[y][x] != state[y][x-1] {
				edges[y] = append(edges[y], x)
			}
		}
		edges[y] = append(edges[y], w)
	}
	rowEdges := func(y int) []int { // nil outside the image
		if y < 0 || y >= h {
			return nil
		}
		return edges[y]
	}

	// wall adds a vertical wall piece from height zA up to zB, walking from
	// (x0, y0) to (x1, y1). It faces the right-hand side of the walking direction.
	wall := func(x0, y0, x1, y1 int, zA, zB float64) {
		for i := 0; i+1 < len(levels); i++ {
			if levels[i] >= zA && levels[i+1] <= zB {
				b.quad(b.p(x0, y0, levels[i]), b.p(x1, y1, levels[i]),
					b.p(x1, y1, levels[i+1]), b.p(x0, y0, levels[i+1]))
			}
		}
	}

	// 1. Top and bottom faces, one strip per run of identical pixels. Using
	// runs instead of single pixels keeps the file small, and the extra points
	// taken from the neighbouring rows keep the mesh free of gaps.
	for y := range h {
		for k := 0; k+1 < len(edges[y]); k++ {
			x0, x1 := edges[y][k], edges[y][k+1]
			z := ht[state[y][x0]]
			if z == 0 { // no material here
				continue
			}
			lower, upper := chain(rowEdges(y-1), x0, x1), chain(rowEdges(y+1), x0, x1)
			b.strip(lower, upper, y, z, false)
			b.strip(lower, upper, y, 0, true)
		}
	}

	// 2. Walls where a row meets the row below it (including the image border).
	for y := 0; y <= h; y++ {
		start := 0
		for x := 1; x <= w; x++ {
			if x < w && get(y, x) == get(y, start) && get(y-1, x) == get(y-1, start) {
				continue
			}
			above, below := ht[get(y, start)], ht[get(y-1, start)]
			if above > below { // the wall faces -y
				wall(start, y, x, y, below, above)
			} else if below > above { // the wall faces +y
				wall(x, y, start, y, above, below)
			}
			start = x
		}
	}

	// 3. Walls where a pixel meets its left-hand neighbour (including the border).
	for y := 0; y < h; y++ {
		for x := 0; x <= w; x++ {
			left, right := ht[get(y, x-1)], ht[get(y, x)]
			if left > right { // the wall faces +x
				wall(x, y, x, y+1, right, left)
			} else if right > left { // the wall faces -x
				wall(x, y+1, x, y, left, right)
			}
		}
	}
}

// edt1d is the 1-D squared distance transform (Felzenszwalb & Huttenlocher).
// f holds 0 at source pixels and a huge number elsewhere.
func edt1d(f []float64) []float64 {
	n := len(f)
	out := make([]float64, n)
	v := make([]int, n)       // positions of the parabolas on the lower envelope
	z := make([]float64, n+1) // where each of those parabolas takes over
	k := 0
	z[0], z[1] = math.Inf(-1), math.Inf(1)
	for q := 1; q < n; q++ {
		var s float64
		for {
			p := v[k]
			s = ((f[q] + float64(q*q)) - (f[p] + float64(p*p))) / float64(2*q-2*p)
			if s > z[k] {
				break
			}
			k--
		}
		k++
		v[k] = q
		z[k] = s
		z[k+1] = math.Inf(1)
	}
	k = 0
	for q := 0; q < n; q++ {
		for z[k+1] < float64(q) {
			k++
		}
		d := float64(q - v[k])
		out[q] = d*d + f[v[k]]
	}
	return out
}

// distSq returns, for every pixel, the squared distance (in pixels) to the
// nearest dark pixel.
func distSq(dark [][]bool) [][]float64 {
	h, w := len(dark), len(dark[0])
	d := make([][]float64, h)
	for y := range d {
		d[y] = make([]float64, w)
		for x := range d[y] {
			if !dark[y][x] {
				d[y][x] = 1e20
			}
		}
	}
	col := make([]float64, h)
	for x := range w {
		for y := range h {
			col[y] = d[y][x]
		}
		res := edt1d(col)
		for y := range h {
			d[y][x] = res[y]
		}
	}
	for y := range h {
		d[y] = edt1d(d[y])
	}
	return d
}

// footprint returns the pixels within r pixels of a dark pixel, plus any gaps
// that are completely enclosed by them.
func footprint(dark [][]bool, r float64) [][]bool {
	d := distSq(dark)
	keep := make([][]bool, len(d))
	for y := range d {
		keep[y] = make([]bool, len(d[y]))
		for x, v := range d[y] {
			keep[y][x] = v <= r*r+1e-9
		}
	}
	fillHoles(keep)
	return keep
}

// fillHoles marks every unmarked pixel that cannot reach the edge of the image
// without crossing a marked pixel.
func fillHoles(keep [][]bool) {
	h, w := len(keep), len(keep[0])
	outside := make([][]bool, h)
	for y := range outside {
		outside[y] = make([]bool, w)
	}
	var stack [][2]int
	visit := func(x, y int) {
		if x < 0 || y < 0 || x >= w || y >= h || keep[y][x] || outside[y][x] {
			return
		}
		outside[y][x] = true
		stack = append(stack, [2]int{x, y})
	}
	for x := range w {
		visit(x, 0)
		visit(x, h-1)
	}
	for y := range h {
		visit(0, y)
		visit(w-1, y)
	}
	for len(stack) > 0 {
		pt := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		visit(pt[0]+1, pt[1])
		visit(pt[0]-1, pt[1])
		visit(pt[0], pt[1]+1)
		visit(pt[0], pt[1]-1)
	}
	for y := range keep {
		for x := range keep[y] {
			if !outside[y][x] {
				keep[y][x] = true
			}
		}
	}
}

// loadImage reads the image and returns which pixels are dark. Row 0 of the
// result is the BOTTOM row of the image.
func loadImage(path string) ([][]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("image is empty")
	}
	dark := make([][]bool, h)
	for y := range dark {
		dark[y] = make([]bool, w)
	}
	for j := range h { // j counts down from the top of the image
		for i := range w {
			r, g, bl, a := img.At(bounds.Min.X+i, bounds.Min.Y+j).RGBA()
			// Brightness on a 0..0xffff scale; transparent pixels count as white.
			lum := (299*r+587*g+114*bl)/1000 + (0xffff - a)
			dark[h-1-j][i] = lum < 0x8000
		}
	}
	return dark, nil
}

func normal(t triangle) vec {
	ux, uy, uz := float64(t[1][0]-t[0][0]), float64(t[1][1]-t[0][1]), float64(t[1][2]-t[0][2])
	vx, vy, vz := float64(t[2][0]-t[0][0]), float64(t[2][1]-t[0][1]), float64(t[2][2]-t[0][2])
	nx, ny, nz := uy*vz-uz*vy, uz*vx-ux*vz, ux*vy-uy*vx
	l := math.Sqrt(nx*nx + ny*ny + nz*nz)
	if l == 0 {
		return vec{}
	}
	return vec{float32(nx / l), float32(ny / l), float32(nz / l)}
}

// writeSTL saves the triangles as a binary STL file.
func writeSTL(path string, tris []triangle) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)

	header := make([]byte, 80)
	copy(header, "binary STL written by img2stl")
	w.Write(header)

	var buf [50]byte // per triangle: normal + 3 vertices (12 float32) + 2 spare bytes
	binary.LittleEndian.PutUint32(buf[:4], uint32(len(tris)))
	w.Write(buf[:4])
	for _, t := range tris {
		i := 0
		put := func(v vec) {
			for _, c := range v {
				binary.LittleEndian.PutUint32(buf[i:], math.Float32bits(c))
				i += 4
			}
		}
		put(normal(t))
		put(t[0])
		put(t[1])
		put(t[2])
		buf[48], buf[49] = 0, 0
		w.Write(buf[:])
	}
	return w.Flush()
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func main() {
	px := flag.Float64("px", 0.1, "scale of one image pixel in mm")
	base := flag.Float64("base", 1.0, "thickness of the white slab in mm")
	raise := flag.Float64("raise", 0.1, "thickness of black areas in mm")
	setback := flag.Float64("setback", -1, "shape the white slab to outline the black areas plus setback in mm")
	invert := flag.Bool("invert", false, "raise the white areas instead of the black ones")
	output := flag.String("o", "", "output file name (when empty same as input)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [options] input.png\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	
	if *px <= 0 || *base < 0 || *raise <= 0 {
		check(fmt.Errorf("-px and -raise must be greater than 0, and -base must not be negative"))
	}

	outname := *output
	if outname == "" {
		split := strings.Split(flag.Arg(0), ".")
		if len(split) > 1 { 
			split[len(split) - 1] = "stl" 
		} else { 
			split = append(split, "stl") 
		}

		outname = strings.Join(split, ".")
	}

	dark, err := loadImage(flag.Arg(0))
	check(err)
	h, w := len(dark), len(dark[0])

	// Which pixels may hold material at all? (nil = all of them.) The crop is
	// always measured from the black pixels, even with -invert.
	var inside [][]bool
	if *setback >= 0 {
		inside = footprint(dark, *setback / *px)
	}

	state := make([][]uint8, h)
	minX, maxX, minY, maxY := w, -1, h, -1
	for y := range state {
		state[y] = make([]uint8, w)
		for x := range state[y] {
			var s uint8
			switch {
			case inside != nil && !inside[y][x]:
				// cropped away: no material
			case dark[y][x] != *invert:
				s = 2 // raised
			case *base > 0:
				s = 1 // flat base
			}
			state[y][x] = s
			if s != 0 {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}

	b := &builder{px: *px, base: *base, raise: *raise}
	b.build(state)
	if len(b.tris) == 0 {
		check(fmt.Errorf("nothing to build: no pixel ended up with material (does the image have black pixels?)"))
	}
	check(writeSTL(outname, b.tris))

	fmt.Printf("wrote %s: %d triangles, %.1f x %.1f x %.2f mm\n",
		outname, len(b.tris),
		float64(maxX-minX+1)*b.px, float64(maxY-minY+1)*b.px, b.base+b.raise)
}
