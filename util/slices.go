package util

// Range represents part of a slice
// High and low can be used with a golang slice [Low:High]
type Range struct {
	// Low is inclusive
	Low int
	// High is exclusive
	High int
}

// Partition calculates the indexes for splitting a slice of size length
// with a max partition size of size
func Partition(length int, size int) []Range {
	var result []Range
	for i := 0; i < length; i += size {
		upperBound := min(i+size, length)
		result = append(result, Range{Low: i, High: upperBound})
	}
	return result
}

func min(x int, y int) int {
	if x < y {
		return x
	}
	return y
}
