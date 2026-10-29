// Package icon draws the menu bar image: an abstract spider lily. The app icon itself is
// the artwork in designs/icon/ccrouter.png, built by `make icon`.
package icon

import (
	"math"

	"github.com/progrium/darwinkit/macos/appkit"
	"github.com/progrium/darwinkit/macos/foundation"
)

func dir(deg float64) (float64, float64) {
	r := deg * math.Pi / 180
	return math.Cos(r), math.Sin(r)
}

// canvas maps a 100-unit design square, y pointing down, onto box in AppKit coordinates.
type canvas struct{ box foundation.Rect }

func (c canvas) u() float64 { return c.box.Size.Width / 100 }
func (c canvas) p(x, y float64) foundation.Point {
	return foundation.Point{X: c.box.Origin.X + x*c.u(), Y: c.box.Origin.Y + (100-y)*c.u()}
}
func (c canvas) curve(pts [4][2]float64, width float64, col appkit.Color) {
	b := appkit.NewBezierPath()
	b.MoveToPoint(c.p(pts[0][0], pts[0][1]))
	b.CurveToPointControlPoint1ControlPoint2(c.p(pts[3][0], pts[3][1]), c.p(pts[1][0], pts[1][1]), c.p(pts[2][0], pts[2][1]))
	b.SetLineWidth(width * c.u())
	b.SetLineCapStyle(appkit.LineCapStyleRound)
	b.SetLineJoinStyle(appkit.LineJoinStyleRound)
	col.SetStroke()
	b.Stroke()
}
func (c canvas) dot(x, y, r float64, col appkit.Color) {
	col.SetFill()
	appkit.BezierPath_BezierPathWithOvalInRect(foundation.Rect{Origin: c.p(x-r, y+r), Size: foundation.Size{Width: 2 * r * c.u(), Height: 2 * r * c.u()}}).Fill()
}

// ribbon fills a cubic Bézier spine as a ribbon tapered at both ends.
func (c canvas) ribbon(pts [4][2]float64, width float64, col appkit.Color) {
	at := func(t float64) (float64, float64) {
		m := 1 - t
		return m*m*m*pts[0][0] + 3*m*m*t*pts[1][0] + 3*m*t*t*pts[2][0] + t*t*t*pts[3][0],
			m*m*m*pts[0][1] + 3*m*m*t*pts[1][1] + 3*m*t*t*pts[2][1] + t*t*t*pts[3][1]
	}
	const steps = 20
	var l, r [][2]float64
	for i := 0; i <= steps; i++ {
		t := float64(i) / steps
		x, y := at(t)
		x2, y2 := at(math.Min(1, t+0.01))
		x1, y1 := at(math.Max(0, t-0.01))
		n := math.Hypot(x2-x1, y2-y1)
		w := width / 2 * math.Sin(math.Pi*math.Pow(t, 0.7))
		l = append(l, [2]float64{x - (y2-y1)/n*w, y + (x2-x1)/n*w})
		r = append(r, [2]float64{x + (y2-y1)/n*w, y - (x2-x1)/n*w})
	}
	b := appkit.NewBezierPath()
	b.MoveToPoint(c.p(l[0][0], l[0][1]))
	for _, q := range l[1:] {
		b.LineToPoint(c.p(q[0], q[1]))
	}
	for i := len(r) - 1; i >= 0; i-- {
		b.LineToPoint(c.p(r[i][0], r[i][1]))
	}
	b.ClosePath()
	col.SetFill()
	b.Fill()
}

// MenuBar returns the status item's template image: five stamens arching out of two
// curled petals on a short stem.
func MenuBar() appkit.Image {
	img := appkit.Image_ImageWithSizeFlippedDrawingHandler(foundation.Size{Width: 18, Height: 18}, false, func(foundation.Rect) bool {
		c := canvas{box: foundation.Rect{Size: foundation.Size{Width: 18, Height: 18}}}
		ink := appkit.Color_BlackColor()
		const x, y = 50.0, 58.0
		for i, deg := range []float64{198, 234, 270, 306, 342} {
			dx, dy := dir(deg)
			l := 40.0 - 6*math.Abs(dx)
			tx, ty := x+dx*l, y+dy*l*0.8-6
			c.curve([4][2]float64{{x + dx*6, y + dy*6}, {x + dx*l*0.6, y + dy*l*0.45 + 6}, {tx + dx*2, ty + l*0.38}, {tx, ty}}, 7, ink)
			c.dot(tx, ty, 6.5-float64(i%2), ink)
		}
		c.ribbon([4][2]float64{{x - 4, y + 2}, {x - 16, y + 3}, {x - 23, y + 12}, {x - 15, y + 15}}, 7, ink)
		c.ribbon([4][2]float64{{x + 4, y + 2}, {x + 16, y + 3}, {x + 23, y + 12}, {x + 15, y + 15}}, 7, ink)
		c.curve([4][2]float64{{x, y + 4}, {x, y + 16}, {x - 2, y + 28}, {x - 3, 97}}, 7, ink)
		return true
	})
	img.SetTemplate(true)
	return img
}
