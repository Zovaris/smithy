package p

// Leap reports whether year is a leap year in the Gregorian calendar.
func Leap(year int) bool {
	return year%4 == 0
}
