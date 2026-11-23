package alert

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"time"
	_ "time/tzdata"
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

func chartZoneLabel() string {
	return time.Now().In(chartLocation()).Format("MST")
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
}

const glyphScale = 2

func textWidth(s string) int {
	if s == "" {
		return 0
	}
	return len([]rune(s))*6*glyphScale - glyphScale
}

func drawText(img *image.RGBA, x, y int, s string, c color.RGBA) {
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
				for dy := 0; dy < glyphScale; dy++ {
					for dx := 0; dx < glyphScale; dx++ {
						set(img, x+col*glyphScale+dx, y+row*glyphScale+dy, c)
					}
				}
			}
		}
		x += 6 * glyphScale
	}
}
