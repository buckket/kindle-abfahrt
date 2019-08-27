package vbb

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type ByTime []Departure

func (d ByTime) Len() int      { return len(d) }
func (d ByTime) Swap(i, j int) { d[i], d[j] = d[j], d[i] }
func (d ByTime) Less(i, j int) bool {
	it, err := d[i].ParseDateTime(true)
	if err != nil {
		it, err = d[i].ParseDateTime(false)
		if err != nil {
			log.Fatal(err)
		}
	}
	jt, err := d[j].ParseDateTime(true)
	if err != nil {
		jt, err = d[j].ParseDateTime(false)
		if err != nil {
			log.Fatal(err)
		}
	}
	return it.Before(jt)
}

type VBB struct {
	accessID string
	baseURL  string
}

type JourneyDetailRef struct {
	Ref string `json:"ref"`
}

type Product struct {
	Name string `json:"name"`
	Line string `json:"line"`
}

type DepartureResult struct {
	Departures     []Departure `json:"Departure"`
	ServerVersion  string      `json:"serverVersion"`
	DialectVersion string      `json:"dialectVersion"`
	PlanRtTs       int64       `json:"planRtTs"`
	ErrorCode      string      `json:"errorCode"`
	ErrorText      string      `json:"errorText"`
}

type Departure struct {
	Name             string           `json:"name"`
	Type             string           `json:"type"`
	Time             string           `json:"time"`
	Date             string           `json:"date"`
	RtTime           string           `json:"rtTime"`
	RtDate           string           `json:"rtDate"`
	TrainNumber      string           `json:"trainNumber"`
	TrainCategory    string           `json:"trainCategory"`
	Direction        string           `json:"direction"`
	Product          Product          `json:"Product"`
	JourneyDetailRef JourneyDetailRef `json:"JourneyDetailRef"`
}

func (d *Departure) ParseDateTime(rt bool) (t time.Time, err error) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		log.Fatal(err)
	}

	const tFormat = "2006-01-02 15:04:05"
	if rt {
		t, err = time.ParseInLocation(tFormat, fmt.Sprintf("%s %s", d.RtDate, d.RtTime), loc)
	} else {
		t, err = time.ParseInLocation(tFormat, fmt.Sprintf("%s %s", d.Date, d.Time), loc)
	}
	return t, err
}

func New(accessID, baseURL string) *VBB {
	return &VBB{accessID: accessID, baseURL: baseURL}
}

func (vbb *VBB) GetDepartures(extId string, offset time.Duration) ([]Departure, bool) {
	log.Printf("New VBB API request for %s", extId)

	t := time.Now().Add(offset)

	v := url.Values{}
	v.Add("format", "json")
	v.Add("accessId", vbb.accessID)
	v.Add("extId", extId)
	v.Add("date", t.Format("2006-01-02"))
	v.Add("time", t.Format("15:04"))

	client := http.Client{Timeout: time.Duration(10 * time.Second)}
	resp, err := client.Get(fmt.Sprintf("%s?%s", vbb.baseURL, v.Encode()))
	if err != nil {
		log.Print(err)
		return []Departure{}, false
	}
	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)

	dr := DepartureResult{}
	err = decoder.Decode(&dr)
	if err != nil {
		log.Print(err)
		return []Departure{}, false
	}
	if len(dr.ErrorCode) >= 0 && len(dr.ErrorText) > 0 {
		log.Printf("vbb API error %s: %s", dr.ErrorCode, dr.ErrorText)
		return []Departure{}, false
	}

	return dr.Departures, true
}

func (vbb *VBB) SortDepartures(ds []Departure, ttype, exclude string, offset time.Duration, limit int) []Departure {
	keys := make(map[string]bool)
	res := make([]Departure, 0)

	sort.Sort(ByTime(ds))

	for _, d := range ds {
		if _, value := keys[d.JourneyDetailRef.Ref]; value {
			continue
		}
		if strings.HasPrefix(d.TrainCategory, ttype) && d.Direction != exclude {
			t, err := d.ParseDateTime(true)
			if err != nil {
				t, err = d.ParseDateTime(false)
				if err != nil {
					log.Fatal(err)
				}
			}
			if t.Before(time.Now().Add(offset)) {
				continue
			}
			res = append(res, d)
			keys[d.JourneyDetailRef.Ref] = true
		}
		if len(res) >= limit {
			break
		}
	}

	return res
}
