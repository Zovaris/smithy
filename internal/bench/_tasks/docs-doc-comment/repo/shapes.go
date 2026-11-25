package p

type Rect struct{ W, H float64 }

func (r Rect) Area() float64 { return r.W * r.H }

func Square(side float64) Rect { return Rect{side, side} }
