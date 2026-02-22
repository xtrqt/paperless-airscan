package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"time"

	"github.com/signintech/gopdf"
)

type TitlePageConfig struct {
	Year      int
	Week      int
	StartDate time.Time
	EndDate   time.Time
}

func GenerateTitlePage(config TitlePageConfig) ([]byte, error) {
	pdf := gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.AddPage()

	width := 595.28
	height := 841.89

	titleText := fmt.Sprintf("WEEK %d, %d", config.Week, config.Year)
	dateText := fmt.Sprintf("%s - %s",
		config.StartDate.Format("2 January"),
		config.EndDate.Format("2 January 2006"),
	)

	pdf.SetFont("Helvetica-Bold", "", 48)
	titleWidth, _ := pdf.MeasureTextWidth(titleText)
	pdf.SetX((width - titleWidth) / 2)
	pdf.SetY(height/2 - 50)
	pdf.Cell(nil, titleText)

	pdf.SetFont("Helvetica", "", 24)
	dateWidth, _ := pdf.MeasureTextWidth(dateText)
	pdf.SetX((width - dateWidth) / 2)
	pdf.SetY(height/2 + 20)
	pdf.Cell(nil, dateText)

	var buf bytes.Buffer
	if err := pdf.Write(&buf); err != nil {
		return nil, fmt.Errorf("failed to write PDF: %w", err)
	}

	return buf.Bytes(), nil
}

func GenerateTitlePageSimple(config TitlePageConfig) ([]byte, error) {
	const (
		width  = 2480
		height = 3508
		dpi    = 300
	)

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	titleText := fmt.Sprintf("WEEK %d, %d", config.Week, config.Year)
	dateText := fmt.Sprintf("%s - %s",
		config.StartDate.Format("2 January"),
		config.EndDate.Format("2 January 2006"),
	)

	_ = titleText
	_ = dateText

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		return nil, fmt.Errorf("failed to encode JPEG: %w", err)
	}

	return buf.Bytes(), nil
}

func GetWeekBounds(t time.Time, weekStartsMonday bool) TitlePageConfig {
	year, week := t.ISOWeek()

	var startDate, endDate time.Time

	if weekStartsMonday {
		startDate = firstDayOfISOWeek(year, week, time.UTC)
		endDate = startDate.AddDate(0, 0, 6)
	} else {
		isoStart := firstDayOfISOWeek(year, week, time.UTC)
		startDate = isoStart.AddDate(0, 0, -1)
		endDate = startDate.AddDate(0, 0, 6)
	}

	return TitlePageConfig{
		Year:      year,
		Week:      week,
		StartDate: startDate,
		EndDate:   endDate,
	}
}

func firstDayOfISOWeek(year, week int, loc *time.Location) time.Time {
	date := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
	isoYear, isoWeek := date.ISOWeek()
	for date.Weekday() != time.Monday || isoYear != year || isoWeek != week {
		date = date.AddDate(0, 0, 1)
		isoYear, isoWeek = date.ISOWeek()
	}
	return date
}

func FormatWeekKey(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

func ParseWeekKey(key string) (year, week int, err error) {
	_, err = fmt.Sscanf(key, "%d-W%d", &year, &week)
	return
}
