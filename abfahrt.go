package main

import (
	"fmt"
	"github.com/buckket/kindle-abfahrt/vbb"
	"github.com/llgcode/draw2d"
	"github.com/llgcode/draw2d/draw2dimg"
	"github.com/robfig/cron"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

type Abfahrt struct {
	vbb *vbb.VBB

	imgS, imgB, imgT image.Image

	dest *image.RGBA
	gc   draw2d.GraphicContext

	pagesSinceFullRefresh int

	wg sync.WaitGroup
}

func main() {
	abfahrt := Abfahrt{}
	abfahrt.vbb = vbb.New("",
		"")
	abfahrt.loadStaticImages()
	abfahrt.render()

	c := cron.New()
	c.AddFunc("0 * * * * *", func() {
		abfahrt.render()
	})
	c.Start()

	abfahrt.wg.Add(1)
	abfahrt.wg.Wait()
}

func (a *Abfahrt) loadStaticImages() {
	var err error

	sbahn, _ := os.Open("./images/sbahn.png")
	defer sbahn.Close()
	a.imgS, _, err = image.Decode(sbahn)
	if err != nil {
		log.Fatal(err)
	}

	bus, _ := os.Open("./images/bus.png")
	defer bus.Close()
	a.imgB, _, err = image.Decode(bus)
	if err != nil {
		log.Fatal(err)
	}

	tram, _ := os.Open("./images/tram.png")
	defer bus.Close()
	a.imgT, _, err = image.Decode(tram)
	if err != nil {
		log.Fatal(err)
	}
}

func (a *Abfahrt) render() {
	a.dest = image.NewRGBA(image.Rect(0, 0, 1072, 1448))
	draw.Draw(a.dest, a.dest.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)

	draw2d.SetFontFolder("./fonts")
	a.gc = draw2dimg.NewGraphicContext(a.dest)

	a.drawHeadline(15, a.imgS, "Schöneweide")
	d, ok := a.vbb.GetDepartures("900192001", 15*time.Minute)
	if !ok {
		a.drawAPIError(15)
	} else {
		d := a.vbb.SortDepartures(d, "S", "", 5)
		a.drawDepartures(15, d)
	}

	a.drawHeadline(480, a.imgT, "Wilhelminenhofstr. / Edisonstr")
	d, ok = a.vbb.GetDepartures("900181001", 10*time.Minute)
	if !ok {
		a.drawAPIError(480)
	} else {
		d2, ok := a.vbb.GetDepartures("900181701", 10*time.Minute)
		if !ok {
			a.drawAPIError(480)
		} else {
			d = append(d, d2...)
			d = a.vbb.SortDepartures(d, "T", "S Schöneweide", 5)
			a.drawDepartures(480, d)
		}
	}

	a.drawHeadline(950, a.imgB, "Siemensstr. / Nalepastr")
	d, ok = a.vbb.GetDepartures("900181008", 3*time.Minute)
	if !ok {
		a.drawAPIError(950)
	} else {
		d = a.vbb.SortDepartures(d, "B", "", 2)
		a.drawDepartures(950, d)
	}

	a.drawHeadline(1215, a.imgB, "Karlshorster Str.")
	d, ok = a.vbb.GetDepartures("900192507", 5*time.Minute)
	if !ok {
		a.drawAPIError(1215)
	} else {
		d = a.vbb.SortDepartures(d, "B", "", 2)
		a.drawDepartures(1215, d)
	}

	a.drawRefreshTime()

	saveImage(convertToGray(a.dest))

	if a.pagesSinceFullRefresh >= 10 {
		log.Printf("Updating full screen, pages was %d", a.pagesSinceFullRefresh)
		updateScreen(true)
		a.pagesSinceFullRefresh = 0
	} else {
		log.Printf("Updating partial screen, pages is %d", a.pagesSinceFullRefresh)
		updateScreen(false)
		a.pagesSinceFullRefresh++
	}
}

func (a *Abfahrt) drawDepartures(y float64, departures []vbb.Departure) {
	row := y + 140
	spacing := 70

	a.gc.SetFillColor(color.Black)
	a.gc.SetFontSize(35)

	for i, d := range departures {
		a.gc.SetFontData(draw2d.FontData{Name: "RobotoCondensed"})
		a.gc.FillStringAt(d.Product.Line, 20, row+float64(i*spacing))

		a.gc.SetFontData(draw2d.FontData{Name: "RobotoCondensedLight"})
		direction := d.Direction
		if len(d.Direction) > 36 {
			direction = d.Direction[:36]
		}
		a.gc.FillStringAt(direction, 130, row+float64(i*spacing))

		t, err := d.ParseDateTime(false)
		if err != nil {
			log.Fatal(err)
		}

		tRT, err := d.ParseDateTime(true)
		if err != nil {
			tRT = t
		} else {
			a.gc.SetFontSize(10)
			a.gc.SetFontData(draw2d.FontData{Name: "RobotoCondensed"})
			a.gc.FillStringAt("RT", 919, row+float64(i*spacing))
			if tRT.Unix() > t.Unix() {
				a.gc.FillStringAt("D", 908, row+float64(i*spacing)-20)
			}
		}

		a.gc.SetFontSize(35)
		a.gc.SetFontData(draw2d.FontData{Name: "RobotoCondensed"})
		a.gc.FillStringAt(fmt.Sprintf("%s / %02d", tRT.Format("15:04"), relativeTime(tRT)), 800, row+float64(i*spacing))
		a.gc.SetFontData(draw2d.FontData{Name: "RobotoCondensedLight"})
		a.gc.FillStringAt("min", 990, row+float64(i*spacing))
	}
}

func (a *Abfahrt) drawHeadline(y float64, img image.Image, text string) {
	sr := img.Bounds()
	dp := image.Pt(15, int(y))
	re := image.Rectangle{dp, dp.Add(sr.Size())}
	draw.Draw(a.dest, re, img, sr.Min, draw.Over)

	a.gc.SetFillColor(color.Black)
	a.gc.SetFontSize(50)
	a.gc.SetFontData(draw2d.FontData{Name: "RobotoCondensed"})
	a.gc.FillStringAt(text, 95, y+60)

	a.drawHorizontalLine(y + 75)
}

func (a *Abfahrt) drawAPIError(y float64) {
	row := y + 140
	a.gc.SetFillColor(color.Black)
	a.gc.SetFontSize(35)
	a.gc.SetFontData(draw2d.FontData{Name: "RobotoCondensed"})
	a.gc.FillStringAt("API ERROR :(", 20, row)
}

func (a *Abfahrt) drawRefreshTime() {
	a.gc.SetFillColor(color.Black)
	a.gc.SetFontSize(50)
	a.gc.SetFontData(draw2d.FontData{Name: "RobotoCondensedLight"})
	a.gc.FillStringAt(time.Now().Format("15:04"), 850, 75)
}

func (a *Abfahrt) drawHorizontalLine(y float64) {
	a.gc.SetStrokeColor(color.RGBA{0x00, 0x00, 0x00, 0xff})
	a.gc.SetLineWidth(2)
	a.gc.MoveTo(0, y)
	a.gc.LineTo(1072, y)
	a.gc.Close()
	a.gc.FillStroke()
}

func convertToGray(img image.Image) image.Image {
	grayImg := image.NewGray(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			grayImg.Set(x, y, img.At(x, y))
		}
	}
	return grayImg
}

func relativeTime(t time.Time) int {
	diff := t.Sub(time.Now())
	return int(math.Round(diff.Minutes()))
}

func saveImage(img image.Image) {
	out, err := os.Create("/tmp/abfahrt.png")
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(out, img); err != nil {
		log.Fatal("unable to encode image")
	}
	return
}

func updateScreen(full bool) {
	if runtime.GOARCH != "arm" {
		return
	}
	args := []string{"-g", "/tmp/abfahrt.png"}
	if full {
		args = append(args, "-f")
	}
	cmd := exec.Command("eips", args...)
	err := cmd.Run()
	if err != nil {
		log.Fatalf("cmd.Run() failed with %s\n", err)
	}
}
