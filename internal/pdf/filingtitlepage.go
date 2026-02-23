package pdf

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	// A4 dimensions at 300 DPI
	a4Width300DPI  = 2480 // 210mm * 300dpi / 25.4
	a4Height300DPI = 3508 // 297mm * 300dpi / 25.4

	// Font size in points (52pt)
	fontSize = 52.0

	// Y positions for the 4 lines (in pixels at 300 DPI)
	// Line 1: 120mm = 1417px
	// Line 2: 135mm = 1594px
	// Fold line: 148.5mm = 1752px
	// Line 3: 162mm = 1913px
	// Line 4: 177mm = 2087px
	line1Y = 1417
	line2Y = 1594
	line3Y = 1913
	line4Y = 2087

	// Liberation Sans font path in Alpine Linux
	fontPath = "/usr/share/fonts/liberation/LiberationSans-Regular.ttf"
)

// GenerateFilingTitlePage creates an A4 PDF with 4 centered lines showing the filing ID
func GenerateFilingTitlePage(filingID string) ([]byte, error) {
	// Create A4 image at 300 DPI
	img := image.NewRGBA(image.Rect(0, 0, a4Width300DPI, a4Height300DPI))

	// Fill with white background
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	// Load Liberation Sans font
	fontBytes, err := loadFont()
	if err != nil {
		return nil, fmt.Errorf("load font: %w", err)
	}

	parsedFont, err := opentype.Parse(fontBytes)
	if err != nil {
		return nil, fmt.Errorf("parse font: %w", err)
	}

	// Create font face with 80pt size at 300 DPI
	face, err := opentype.NewFace(parsedFont, &opentype.FaceOptions{
		Size:    fontSize,
		DPI:     300,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("create font face: %w", err)
	}
	defer face.Close()

	// Create font drawer
	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.Black,
		Face: face,
	}

	// Draw the 4 lines
	lines := []struct {
		text string
		y    int
	}{
		{filingID, line1Y},
		{filingID, line2Y},
		{filingID, line3Y},
		{filingID, line4Y},
	}

	for _, line := range lines {
		// Measure text width to center it
		textWidth := font.MeasureString(face, line.text)
		textWidthPx := int(textWidth >> 6) // Convert from fixed.Int26_6 to pixels
		x := (a4Width300DPI - textWidthPx) / 2

		// Draw text
		drawer.Dot = fixed.Point26_6{
			X: fixed.I(x),
			Y: fixed.I(line.y),
		}
		drawer.DrawString(line.text)
	}

	// Save image to temporary PNG file
	tmpDir, err := ioutil.TempDir("", "filing-title-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	pngPath := filepath.Join(tmpDir, "title.png")
	pngFile, err := os.Create(pngPath)
	if err != nil {
		return nil, fmt.Errorf("create png file: %w", err)
	}

	if err := png.Encode(pngFile, img); err != nil {
		pngFile.Close()
		return nil, fmt.Errorf("encode png: %w", err)
	}
	pngFile.Close()

	// Convert PNG to PDF using pdfcpu
	pdfPath := filepath.Join(tmpDir, "title.pdf")
	if err := api.ImportImagesFile([]string{pngPath}, pdfPath, nil, nil); err != nil {
		return nil, fmt.Errorf("convert png to pdf: %w", err)
	}

	// Read PDF file
	pdfData, err := ioutil.ReadFile(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("read pdf: %w", err)
	}

	return pdfData, nil
}

// loadFont loads the Liberation Sans font from the system
func loadFont() ([]byte, error) {
	// Try Liberation Sans first
	if _, err := os.Stat(fontPath); err == nil {
		return ioutil.ReadFile(fontPath)
	}

	// Fallback: try other common font paths
	fallbackPaths := []string{
		"/usr/share/fonts/truetype/liberation/LiberationSans-Regular.ttf", // Debian/Ubuntu
		"/usr/share/fonts/TTF/LiberationSans-Regular.ttf",                 // Arch
		"/System/Library/Fonts/Helvetica.ttc",                             // macOS
	}

	for _, path := range fallbackPaths {
		if _, err := os.Stat(path); err == nil {
			return ioutil.ReadFile(path)
		}
	}

	return nil, fmt.Errorf("no suitable font found (tried %s and fallbacks)", fontPath)
}
