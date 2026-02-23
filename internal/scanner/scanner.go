package scanner

import "io"

type ScanResult struct {
	Pages     []io.Reader
	Count     int
	TempDir   string
	FilePaths []string
}

type Scanner interface {
	Scan() (*ScanResult, error)
	IsAvailable() bool
}

type ColorMode string

const (
	ColorGrayscale ColorMode = "Grayscale8"
	ColorRGB24     ColorMode = "RGB24"
)

type Source string

const (
	SourceADF     Source = "adf"
	SourceFlatbed Source = "flatbed"
)

type ReorderMode string

const (
	ReorderNone       ReorderMode = ""
	ReorderInterleave ReorderMode = "interleave"
)
