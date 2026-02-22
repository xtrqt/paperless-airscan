package scanner

import "io"

type ScanResult struct {
	Pages []io.Reader
	Count int
}

type Scanner interface {
	Scan() (*ScanResult, error)
	IsAvailable() bool
}

type ColorMode string

const (
	ColorGrayscale ColorMode = "grayscale"
	ColorRGB24     ColorMode = "rgb24"
)

type Source string

const (
	SourceADF     Source = "adf"
	SourceFlatbed Source = "flatbed"
)
