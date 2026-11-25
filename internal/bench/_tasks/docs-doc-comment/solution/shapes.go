package p

// Rect is a rectangle of width W and height H.
type Rect struct{ W, H float64 }

// Area is the rectangle's area.
func (r Rect) Area() float64 { return r.W * r.H }

// Square returns a rectangle with equal sides.
func Square(side float64) Rect { return Rect{side, side} }
