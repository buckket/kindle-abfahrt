package main

import (
	"bytes"
	"fmt"
	"github.com/buckket/kindle-abfahrt/vbb"
	"github.com/gobuffalo/packr/v2"
	"github.com/golang/freetype/truetype"
	"github.com/kelvins/sunrisesunset"
	"github.com/llgcode/draw2d"
	"github.com/llgcode/draw2d/draw2dimg"
	"github.com/robfig/cron"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

type Config struct {
	fileLocation    string
	pagesTillRedraw int
}

type CacheContent struct {
	timestamp  time.Time
	departures []vbb.Departure
	ok         bool
}

type Cache struct {
	data map[string]CacheContent
}

func (c *Cache) IsStale(key string) bool {
	cc, ok := c.data[key]
	if !ok {
		return true
	}
	n := time.Now()
	if n.Sub(cc.timestamp) >= 5*time.Minute {
		return true
	}
	return false
}

func (c *Cache) Get(key string) ([]vbb.Departure, bool) {
	cc, ok := c.data[key]
	if !ok {
		return []vbb.Departure{}, false
	}
	return cc.departures, true
}

func (c *Cache) Set(key string, departures []vbb.Departure, ok bool) {
	c.data[key] = CacheContent{
		timestamp:  time.Now(),
		departures: departures,
		ok:         ok,
	}
}

func (c *Cache) Init() {
	c.data = make(map[string]CacheContent)
}

type Abfahrt struct {
	vbb    *vbb.VBB
	config *Config

	cache Cache

	imgS, imgB, imgT image.Image
	font, fontLight  draw2d.FontData

	dest *image.RGBA
	gc   draw2d.GraphicContext

	activeUntil time.Time
	isActive    bool

	sunrise, sunset time.Time
}

func main() {
	abfahrt := Abfahrt{}
	abfahrt.config = &Config{
		fileLocation:    "/tmp/abfahrt.png",
		pagesTillRedraw: 60,
	}
	abfahrt.vbb = vbb.New("",
		"")
	abfahrt.initCache()
	abfahrt.loadStaticImages()
	abfahrt.loadFonts()
	abfahrt.updateSunrise()
	go abfahrt.clearScreen(true)
	go abfahrt.backlight(false)

	c := cron.New()
	c.AddFunc("0 * * * * *", func() {
		abfahrt.update()
	})
	c.AddFunc("@daily", func() {
		abfahrt.updateSunrise()
	})
	c.Start()

	http.HandleFunc("/", abfahrt.httpHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func (a *Abfahrt) initCache() {
	a.cache.Init()
}

func (a *Abfahrt) loadStaticImages() {
	box := packr.New("images", "./images")
	for k, v := range map[string]*image.Image{
		"sbahn.png": &a.imgS,
		"bus.png":   &a.imgB,
		"tram.png":  &a.imgT,
	} {
		img, err := box.Find(k)
		if err != nil {
			log.Fatal(err)
		}
		*v, _, err = image.Decode(bytes.NewReader(img))
		if err != nil {
			log.Fatal(err)
		}
	}
}

func (a *Abfahrt) loadFonts() {
	box := packr.New("fonts", "./fonts")
	a.font = draw2d.FontData{Name: "RobotoCondensed"}
	a.fontLight = draw2d.FontData{Name: "RobotoCondensedLight"}
	for k, v := range map[string]draw2d.FontData{
		"RobotoCondensed.ttf":      a.font,
		"RobotoCondensedLight.ttf": a.fontLight,
	} {
		f, err := box.Find(k)
		if err != nil {
			log.Fatal(err)
		}
		tt, err := truetype.Parse(f)
		if err != nil {
			log.Fatal(err)
		}
		draw2d.RegisterFont(v, tt)
	}
}

func (a *Abfahrt) httpHandler(w http.ResponseWriter, r *http.Request) {
	a.activeUntil = time.Now().Add(10 * time.Minute)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "running until %s", a.activeUntil)
	log.Printf("Incoming trigger, now active until %s", a.activeUntil.Format(time.RFC1123Z))
	a.update()
}

func (a *Abfahrt) updateSunrise() {
	p := sunrisesunset.Parameters{
		Latitude:  52.4545237,
		Longitude: 13.4956962,
		UtcOffset: 1.0,
		Date:      time.Now().UTC(),
	}
	sunrise, sunset, err := p.GetSunriseSunset()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Calculated sunrise: %s, sunset: %s", sunrise.Format("15:04"), sunset.Format("15:04"))
	a.sunrise = sunrise
	a.sunset = sunset
}

func (a *Abfahrt) backlight(enable bool) {
	if runtime.GOARCH != "arm" {
		return
	}
	if !enable {
		log.Printf("Disabling backlight if enabled")
		f, err := os.OpenFile("/sys/class/backlight/max77696-bl/brightness", os.O_WRONLY, 644)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		_, err = f.WriteString("0")
		if err != nil {
			log.Printf("Could not change brightness: %s", err)
		}
	} else {
		if a.sunset.Hour() < time.Now().Hour() || time.Now().Hour() < a.sunrise.Hour() {
			log.Printf("Night-time! Enabling backlight")
			f, err := os.OpenFile("/sys/class/backlight/max77696-bl/brightness", os.O_WRONLY, 644)
			if err != nil {
				log.Fatal(err)
			}
			defer f.Close()
			_, err = f.WriteString("100")
			if err != nil {
				log.Printf("Could not change brightness: %s", err)
			}
		}
	}
}

func (a *Abfahrt) update() {
	if time.Now().Before(a.activeUntil) {
		if !a.isActive {
			log.Printf("Display is now active (partial clear)")
			a.isActive = true
			go a.backlight(true)
		}
		a.render()
	} else {
		if a.isActive {
			log.Printf("Display is now disabled (full clear)")
			a.isActive = false
			go a.backlight(false)
			go a.clearScreen(true)
		}
	}
}

func (a *Abfahrt) render() {
	a.dest = image.NewRGBA(image.Rect(0, 0, 1072, 1448))
	draw.Draw(a.dest, a.dest.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)

	a.gc = draw2dimg.NewGraphicContext(a.dest)

	var d, d2 []vbb.Departure
	var ok bool

	a.drawHeadline(15, a.imgS, "Schöneweide")
	if a.cache.IsStale("900192001") {
		d, ok = a.vbb.GetDepartures("900192001", 15*time.Minute)
		a.cache.Set("900192001", d, ok)
	} else {
		d, ok = a.cache.Get("900192001")
	}
	if !ok {
		a.drawAPIError(15)
	} else {
		d = a.vbb.SortDepartures(d, "S", "", 15*time.Minute, 5)
		a.drawDepartures(15, d)
	}

	a.drawHeadline(480, a.imgT, "Wilhelminenhofstr. / Edisonstr")
	if a.cache.IsStale("900181001") {
		d, ok = a.vbb.GetDepartures("900181001", 10*time.Minute)
		a.cache.Set("900181001", d, ok)
	} else {
		d, ok = a.cache.Get("900181001")
	}
	if !ok {
		a.drawAPIError(480)
	} else {
		if a.cache.IsStale("900181701") {
			d2, ok = a.vbb.GetDepartures("900181701", 10*time.Minute)
			a.cache.Set("900181701", d2, ok)
		} else {
			d2, ok = a.cache.Get("900181701")
		}
		if !ok {
			a.drawAPIError(480)
		} else {
			d = append(d, d2...)
			d = a.vbb.SortDepartures(d, "T", "S Schöneweide", 10*time.Minute, 5)
			a.drawDepartures(480, d)
		}
	}

	a.drawHeadline(950, a.imgB, "Siemensstr. / Nalepastr")
	if h := time.Now().Hour(); h >= 6 && h < 20 {
		if a.cache.IsStale("900181008") {
			d, ok = a.vbb.GetDepartures("900181008", 3*time.Minute)
			a.cache.Set("900181008", d, ok)
		} else {
			d, ok = a.cache.Get("900181008")
		}
		if !ok {
			a.drawAPIError(950)
		} else {
			d = a.vbb.SortDepartures(d, "B", "", 3*time.Minute, 2)
			a.drawDepartures(950, d)
		}
	}

	a.drawHeadline(1215, a.imgB, "Karlshorster Str.")
	if a.cache.IsStale("900192507") {
		d, ok = a.vbb.GetDepartures("900192507", 5*time.Minute)
		a.cache.Set("900192507", d, ok)
	} else {
		d, ok = a.cache.Get("900192507")
	}
	if !ok {
		a.drawAPIError(1215)
	} else {
		d = a.vbb.SortDepartures(d, "B", "", 5*time.Minute, 2)
		a.drawDepartures(1215, d)
	}

	a.drawRefreshTime()

	a.saveImage(convertToGray(a.dest))

	log.Printf("Updating screen")
	a.updateScreen()
}

func (a *Abfahrt) drawDepartures(y float64, departures []vbb.Departure) {
	row := y + 140
	spacing := 70

	a.gc.SetFillColor(color.Black)

	for i, d := range departures {
		t, err := d.ParseDateTime(false)
		if err != nil {
			log.Fatal(err)
		}

		tRT, err := d.ParseDateTime(true)
		if err != nil {
			tRT = t
		} else {
			a.gc.SetFontSize(10)
			a.gc.SetFontData(a.font)
			a.gc.FillStringAt("RT", 919, row+float64(i*spacing))
			if tRT.After(t) {
				a.gc.FillStringAt("D", 908, row+float64(i*spacing)-20)
			}
		}

		a.gc.SetFontSize(35)
		a.gc.SetFontData(a.font)
		a.gc.FillStringAt(d.Product[0].Line, 20, row+float64(i*spacing))

		a.gc.SetFontSize(35)
		a.gc.SetFontData(a.fontLight)
		direction := d.Direction
		if len(d.Direction) > 36 {
			direction = d.Direction[:36]
		}
		a.gc.FillStringAt(direction, 130, row+float64(i*spacing))

		a.gc.SetFontSize(35)
		a.gc.SetFontData(a.font)
		a.gc.FillStringAt(fmt.Sprintf("%s / %02d", tRT.Format("15:04"), relativeTime(tRT)), 800, row+float64(i*spacing))
		a.gc.SetFontData(a.fontLight)
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
	a.gc.SetFontData(a.font)
	a.gc.FillStringAt(text, 95, y+60)

	a.drawHorizontalLine(y + 75)
}

func (a *Abfahrt) drawAPIError(y float64) {
	row := y + 140
	a.gc.SetFillColor(color.Black)
	a.gc.SetFontSize(35)
	a.gc.SetFontData(a.font)
	a.gc.FillStringAt("API ERROR :(", 20, row)
}

func (a *Abfahrt) drawRefreshTime() {
	a.gc.SetFillColor(color.Black)
	a.gc.SetFontSize(50)
	a.gc.SetFontData(a.fontLight)
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

func (a *Abfahrt) saveImage(img image.Image) {
	out, err := os.Create(a.config.fileLocation)
	if err != nil {
		log.Fatal(err)
	}
	if err := png.Encode(out, img); err != nil {
		log.Fatal("unable to encode image")
	}
	return
}

func (a *Abfahrt) updateScreen() {
	if runtime.GOARCH != "arm" {
		return
	}
	args := []string{"-g", a.config.fileLocation}
	cmd := exec.Command("eips", args...)
	err := cmd.Run()
	if err != nil {
		log.Printf("cmd.Run() failed with %s", err)
	}
}

func (a *Abfahrt) clearScreen(full bool) {
	if runtime.GOARCH != "arm" {
		return
	}
	args := []string{"-c"}
	if full {
		args = append(args, "-f")
	}
	cmd := exec.Command("eips", args...)
	err := cmd.Run()
	if err != nil {
		log.Printf("cmd.Run() failed with %s", err)
	}
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
