package pdf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func MergePDFs(outputPath string, inputPaths ...string) error {
	if len(inputPaths) == 0 {
		return fmt.Errorf("no input files provided")
	}

	if len(inputPaths) == 1 {
		data, err := os.ReadFile(inputPaths[0])
		if err != nil {
			return err
		}
		return os.WriteFile(outputPath, data, 0644)
	}

	return api.MergeCreateFile(inputPaths, outputPath, false, nil)
}

func ConvertJPEGToPDF(jpegPath, pdfPath string) error {
	return api.ImportImagesFile([]string{jpegPath}, pdfPath, nil, nil)
}

func MergeJPEGsToPDF(outputPath string, jpegPaths []string) error {
	if len(jpegPaths) == 0 {
		return fmt.Errorf("no jpeg files provided")
	}

	return api.ImportImagesFile(jpegPaths, outputPath, nil, nil)
}

func PrependPage(pdfPath, pagePDFPath string) error {
	dir := filepath.Dir(pdfPath)
	base := filepath.Base(pdfPath)
	tempOutput := filepath.Join(dir, "merged_"+base)

	if err := api.MergeCreateFile([]string{pagePDFPath, pdfPath}, tempOutput, false, nil); err != nil {
		return err
	}

	if err := os.Rename(tempOutput, pdfPath); err != nil {
		return err
	}

	return nil
}

func GetPageCount(pdfPath string) (int, error) {
	ctx, err := api.ReadContextFile(pdfPath)
	if err != nil {
		return 0, err
	}
	return ctx.PageCount, nil
}

func ValidatePDF(pdfPath string) error {
	return api.ValidateFile(pdfPath, nil)
}

func OptimizePDF(inputPath, outputPath string) error {
	return api.OptimizeFile(inputPath, outputPath, nil)
}

func PDFToBytes(pdfPath string) ([]byte, error) {
	return os.ReadFile(pdfPath)
}

func BytesToPDF(data []byte, outputPath string) error {
	return os.WriteFile(outputPath, data, 0644)
}

func CreatePDFFromBuffer(buf *bytes.Buffer, outputPath string) error {
	return os.WriteFile(outputPath, buf.Bytes(), 0644)
}
