package alert

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/holgstr/detector/internal/polymarket"
)

const (
	chartW = 960
	chartH = 540
)

func chartLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return time.UTC
	}
	return loc
}

// RenderCandles draws an OHLC chart of r.Candles over [start, end] (unix seconds).
func RenderCandles(r ChartReport, start, end int64) ([]byte, error) {
	if len(r.Candles) == 0 {
		return nil, ErrNoPriceHistory
	}
	if end <= start {
		end = start + 60
	}
	img := image.NewRGBA(image.Rect(0, 0, chartW, chartH))
	bg := color.RGBA{248, 249, 251, 255}
	grid := color.RGBA{226, 230, 236, 255}
	label := color.RGBA{71, 78, 90, 255}
	up := color.RGBA{22, 150, 82, 255}
	down := color.RGBA{208, 56, 62, 255}
	fill(img, bg)

	lo, hi := candleRange(r.Candles)
	top, bottom := 28, chartH-46
	left, right := 64, chartW-18
	plotH := bottom - top
	plotW := right - left
	if plotH < 1 {
		plotH = 1
	}
	if plotW < 1 {
		plotW = 1
	}

	yOf := func(p float64) int {
		y := top + int((hi-p)/(hi-lo)*float64(plotH))
		if y < top {
			return top
		}
		if y > bottom {
			return bottom
		}
		return y
	}

	for _, frac := range []float64{0, 0.25, 0.5, 0.75, 1} {
		price := lo + (hi-lo)*frac
		y := yOf(price)
		hline(img, left, right, y, grid)
		drawText(img, 8, y-6, fmt.Sprintf("%.0f", price*100), label)
	}
	vline(img, left, top, bottom, grid)
	hline(img, left, right, bottom, grid)

	bucket := int64(r.Bucket / time.Second)
	if bucket < 60 {
		bucket = 60
	}
	slot := int(float64(plotW) * float64(bucket) / float64(end-start))
	if slot < 3 {
		slot = 3
	}
	bodyW := slot * 2 / 3
	if bodyW < 3 {
		bodyW = 3
	}
	if bodyW > 16 {
		bodyW = 16
	}

	for _, c := range r.Candles {
		mid := c.Start + bucket/2
		x := left + int(float64(mid-start)/float64(end-start)*float64(plotW))
		if x < left {
			x = left
		}
		if x > right {
			x = right
		}
		col := up
		if c.Close < c.Open {
			col = down
		}
		yHigh := yOf(c.High)
		yLow := yOf(c.Low)
		yOpen := yOf(c.Open)
		yClose := yOf(c.Close)
		vline(img, x, yHigh, yLow, col)
		y0, y1 := yOpen, yClose
		if y1 < y0 {
			y0, y1 = y1, y0
		}
		if y1-y0 < 1 {
			y1 = y0 + 1
		}
		rect(img, x-bodyW/2, y0, x+bodyW/2, y1, col)
	}

	loc := chartLocation()
	span := time.Duration(end-start) * time.Second
	fracs := []float64{0, 0.33, 0.66, 1}
	for i, frac := range fracs {
		ts := time.Unix(start+int64(float64(end-start)*frac), 0).In(loc)
		text := formatAxisTime(ts, span)
		x := left + int(frac*float64(plotW))
		if i == len(fracs)-1 {
			x -= textWidth(text)
		} else if i > 0 {
			x -= textWidth(text) / 2
		}
		if x < 4 {
			x = 4
		}
		drawText(img, x, bottom+12, text, label)
	}

	drawBookOverlay(img, chartBookLines(r.Books), left, top, bottom, bg, label)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func candleRange(cs []Candle) (lo, hi float64) {
	lo, hi = cs[0].Low, cs[0].High
	for _, c := range cs {
		if c.Low < lo {
			lo = c.Low
		}
		if c.High > hi {
			hi = c.High
		}
	}
	span := hi - lo
	pad := span * 0.18
	if pad < 0.008 {
		pad = 0.008
	}
	lo -= pad
	hi += pad
	if hi-lo < 0.04 {
		mid := (cs[0].Low + cs[0].High) / 2
		lo = mid - 0.02
		hi = mid + 0.02
	}
	if lo < 0 {
		lo = 0
	}
	if hi > 1 {
		hi = 1
	}
	if hi <= lo {
		hi = lo + 0.01
		if hi > 1 {
			hi = 1
			lo = 0.99
		}
	}
	return lo, hi
}

// chartBookLines is the /ob ladder without the market name.
// An empty book is omitted so the chart corner stays clear.
func chartBookLines(books []polymarket.OutcomeBook) []string {
	if !booksHaveSize(books) {
		return nil
	}
	body := strings.Trim(formatOBBody(books), "\n")
	if body == "" || body == "No CLOB depth." {
		return nil
	}
	return strings.Split(body, "\n")
}

func booksHaveSize(books []polymarket.OutcomeBook) bool {
	for _, book := range booksToShow(books) {
		if len(levelsWithSize(book.Asks)) > 0 || len(levelsWithSize(book.Bids)) > 0 {
			return true
		}
	}
	return false
}

// drawBookOverlay paints a small /ob ladder in the top-left or bottom-left
// of the plot, whichever corner has more empty background.
func drawBookOverlay(img *image.RGBA, lines []string, left, top, bottom int, bg, ink color.RGBA) {
	if len(lines) == 0 {
		return
	}
	const scale = 2
	lineH := 8 * scale
	maxH := (bottom - top) * 2 / 5
	if maxH < lineH {
		return
	}
	if len(lines)*lineH > maxH {
		lines = lines[:maxH/lineH]
	}
	w := 0
	for _, ln := range lines {
		if tw := textWidthScale(ln, scale); tw > w {
			w = tw
		}
	}
	if w == 0 {
		return
	}
	pad := 2 * scale
	blockW := w + pad*2
	blockH := len(lines)*lineH - scale + pad*2
	bestX, bestY := bookOrigin(img, blockW, blockH, left, top, bottom, bg)
	rect(img, bestX, bestY, bestX+blockW, bestY+blockH, bg)
	y := bestY + pad
	for _, ln := range lines {
		drawTextScale(img, bestX+pad, y, ln, ink, scale)
		y += lineH
	}
}

func bookOrigin(img *image.RGBA, blockW, blockH, left, top, bottom int, bg color.RGBA) (x, y int) {
	x = left + 4
	topY := top + 4
	botY := bottom - 4 - blockH
	if botY < topY {
		botY = topY
	}
	bestX, bestY := x, topY
	bestInk := countInk(img, x, topY, x+blockW, topY+blockH, bg)
	if botY != topY {
		if inkN := countInk(img, x, botY, x+blockW, botY+blockH, bg); inkN < bestInk {
			bestX, bestY = x, botY
		}
	}
	return bestX, bestY
}

func countInk(img *image.RGBA, x0, y0, x1, y1 int, bg color.RGBA) int {
	b := img.Bounds()
	if x0 < b.Min.X {
		x0 = b.Min.X
	}
	if y0 < b.Min.Y {
		y0 = b.Min.Y
	}
	if x1 >= b.Max.X {
		x1 = b.Max.X - 1
	}
	if y1 >= b.Max.Y {
		y1 = b.Max.Y - 1
	}
	n := 0
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			i := img.PixOffset(x, y)
			if img.Pix[i] != bg.R || img.Pix[i+1] != bg.G || img.Pix[i+2] != bg.B {
				n++
			}
		}
	}
	return n
}

func formatAxisTime(t time.Time, window time.Duration) string {
	if window <= 36*time.Hour {
		return t.Format("15:04")
	}
	return t.Format("02/01")
}

func fill(img *image.RGBA, c color.RGBA) {
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = c.R
		img.Pix[i+1] = c.G
		img.Pix[i+2] = c.B
		img.Pix[i+3] = c.A
	}
}

func hline(img *image.RGBA, x0, x1, y int, c color.RGBA) {
	if y < 0 || y >= img.Bounds().Dy() {
		return
	}
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	for x := x0; x <= x1; x++ {
		set(img, x, y, c)
	}
}

func vline(img *image.RGBA, x, y0, y1 int, c color.RGBA) {
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	for y := y0; y <= y1; y++ {
		set(img, x, y, c)
	}
}

func rect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			set(img, x, y, c)
		}
	}
}

func set(img *image.RGBA, x, y int, c color.RGBA) {
	if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		return
	}
	i := img.PixOffset(x, y)
	img.Pix[i] = c.R
	img.Pix[i+1] = c.G
	img.Pix[i+2] = c.B
	img.Pix[i+3] = c.A
}

// 5x7 glyphs, rows left to right, '1' is ink.
var glyphs = map[rune][7]string{
	'0': {"01110", "10001", "10011", "10101", "11001", "10001", "01110"},
	'1': {"00100", "01100", "00100", "00100", "00100", "00100", "01110"},
	'2': {"01110", "10001", "00001", "00010", "00100", "01000", "11111"},
	'3': {"11110", "00001", "00001", "01110", "00001", "00001", "11110"},
	'4': {"00010", "00110", "01010", "10010", "11111", "00010", "00010"},
	'5': {"11111", "10000", "10000", "11110", "00001", "00001", "11110"},
	'6': {"01110", "10000", "10000", "11110", "10001", "10001", "01110"},
	'7': {"11111", "00001", "00010", "00100", "01000", "01000", "01000"},
	'8': {"01110", "10001", "10001", "01110", "10001", "10001", "01110"},
	'9': {"01110", "10001", "10001", "01111", "00001", "00001", "01110"},
	':': {"00000", "00100", "00100", "00000", "00100", "00100", "00000"},
	'/': {"00001", "00010", "00010", "00100", "01000", "01000", "10000"},
	'.': {"00000", "00000", "00000", "00000", "00000", "01100", "01100"},
	'-': {"00000", "00000", "00000", "11111", "00000", "00000", "00000"},
	' ': {"00000", "00000", "00000", "00000", "00000", "00000", "00000"},
	'¢': {"00100", "01111", "10100", "10100", "10100", "01111", "00100"},
	'k': {"10000", "10000", "10010", "10100", "11000", "10100", "10010"},
	'A': {"01110", "10001", "10001", "11111", "10001", "10001", "10001"},
	'B': {"11110", "10001", "10001", "11110", "10001", "10001", "11110"},
	'C': {"01110", "10001", "10000", "10000", "10000", "10001", "01110"},
	'D': {"11110", "10001", "10001", "10001", "10001", "10001", "11110"},
	'E': {"11111", "10000", "10000", "11110", "10000", "10000", "11111"},
	'F': {"11111", "10000", "10000", "11110", "10000", "10000", "10000"},
	'G': {"01110", "10001", "10000", "10111", "10001", "10001", "01111"},
	'H': {"10001", "10001", "10001", "11111", "10001", "10001", "10001"},
	'I': {"01110", "00100", "00100", "00100", "00100", "00100", "01110"},
	'J': {"00111", "00010", "00010", "00010", "10010", "10010", "01100"},
	'K': {"10001", "10010", "10100", "11000", "10100", "10010", "10001"},
	'L': {"10000", "10000", "10000", "10000", "10000", "10000", "11111"},
	'M': {"10001", "11011", "10101", "10101", "10001", "10001", "10001"},
	'N': {"10001", "11001", "10101", "10011", "10001", "10001", "10001"},
	'O': {"01110", "10001", "10001", "10001", "10001", "10001", "01110"},
	'P': {"11110", "10001", "10001", "11110", "10000", "10000", "10000"},
	'Q': {"01110", "10001", "10001", "10001", "10101", "10010", "01101"},
	'R': {"11110", "10001", "10001", "11110", "10100", "10010", "10001"},
	'S': {"01111", "10000", "10000", "01110", "00001", "00001", "11110"},
	'T': {"11111", "00100", "00100", "00100", "00100", "00100", "00100"},
	'U': {"10001", "10001", "10001", "10001", "10001", "10001", "01110"},
	'V': {"10001", "10001", "10001", "10001", "01010", "01010", "00100"},
	'W': {"10001", "10001", "10001", "10101", "10101", "10101", "01010"},
	'X': {"10001", "10001", "01010", "00100", "01010", "10001", "10001"},
	'Y': {"10001", "10001", "01010", "00100", "00100", "00100", "00100"},
	'Z': {"11111", "00001", "00010", "00100", "01000", "10000", "11111"},
}

const glyphScale = 2

func textWidth(s string) int {
	return textWidthScale(s, glyphScale)
}

func textWidthScale(s string, scale int) int {
	if s == "" || scale < 1 {
		return 0
	}
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	return n*6*scale - scale
}

func drawText(img *image.RGBA, x, y int, s string, c color.RGBA) {
	drawTextScale(img, x, y, s, c, glyphScale)
}

func drawTextScale(img *image.RGBA, x, y int, s string, c color.RGBA, scale int) {
	if scale < 1 {
		scale = 1
	}
	for _, r := range s {
		g, ok := glyphs[r]
		if !ok {
			g = glyphs[' ']
		}
		for row := 0; row < 7; row++ {
			bits := g[row]
			for col := 0; col < 5 && col < len(bits); col++ {
				if bits[col] != '1' {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						set(img, x+col*scale+dx, y+row*scale+dy, c)
					}
				}
			}
		}
		x += 6 * scale
	}
}
