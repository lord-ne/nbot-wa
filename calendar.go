package main

import (
	"errors"
	"nbot-wa/secrets"
	"nbot-wa/util"
	"slices"
	"time"

	"github.com/hebcal/hdate"
	"github.com/hebcal/hebcal-go/event"
	"github.com/hebcal/hebcal-go/hebcal"
	"github.com/hebcal/hebcal-go/zmanim"
)

var (
	minyanZmanimLocation = util.PanicIfNil(zmanim.LookupCity(secrets.MinyanZmanimLocation))
)

const (
	havdalahFlags       = event.YOM_TOV_ENDS | event.LIGHT_CANDLES_TZEIS
	candleLightingFlags = event.LIGHT_CANDLES
)

type EventType bool

const (
	EventType_CandleLighting EventType = true
	EventType_Havdalah       EventType = false
)

type CandleLightingOrHavdalah struct {
	Type EventType
	Time time.Time
}

func GetCandleLightingHavdalahForDateRange(start time.Time, end time.Time) ([]CandleLightingOrHavdalah, error) {
	events, err := hebcal.HebrewCalendar(&hebcal.CalOptions{
		Start:          hdate.FromTime(start),
		End:            hdate.FromTime(end),
		Location:       minyanZmanimLocation,
		NoHolidays:     true,
		CandleLighting: true,
		Mask:           event.LIGHT_CANDLES | event.YOM_TOV_ENDS,
	})

	if err != nil {
		return nil, err
	}

	rtnEventList := make([]CandleLightingOrHavdalah, len(events))

	for _, e := range events {
		if e, ok := e.(hebcal.TimedEvent); ok {
			if (e.Flags & havdalahFlags) != 0 {
				rtnEventList = append(rtnEventList, CandleLightingOrHavdalah{
					Type: EventType_Havdalah,
					Time: e.EventTime.In(start.Location()),
				})
			} else if (e.Flags & candleLightingFlags) != 0 {
				rtnEventList = append(rtnEventList, CandleLightingOrHavdalah{
					Type: EventType_CandleLighting,
					Time: e.EventTime.In(start.Location()),
				})
			}
		}
	}

	slices.SortStableFunc(rtnEventList, func(a, b CandleLightingOrHavdalah) int {
		rtn := a.Time.Compare(b.Time)
		if (rtn == 0) && (a.Type != b.Type) {
			if a.Type == EventType_Havdalah {
				// Sort havdalah first
				return -1
			}
			return 1
		}

		return rtn
	})

	return rtnEventList, nil
}

type YomTovTimes struct {
	CandleLighting time.Time
	Havdalah       time.Time
}

func CurrentOrUpcomingYomTov(date time.Time) (YomTovTimes, bool, error) {
	startTime := date.AddDate(0, 0, -10)
	endTime := date.AddDate(0, 0, 10)

	events, err := GetCandleLightingHavdalahForDateRange(startTime, endTime)
	if err != nil {
		return YomTovTimes{}, false, err
	}

	loopInPast := true
	loopActiveCandleLightingInPast := true
	var loopActiveCandleLighting *time.Time = nil
	for _, e := range events {
		if e.Time.After(date) {
			loopInPast = false
		}

		switch e.Type {
		case EventType_CandleLighting:
			if loopActiveCandleLighting != nil {
				continue
			}
			loopActiveCandleLighting = &e.Time
			loopActiveCandleLightingInPast = loopInPast

		case EventType_Havdalah:
			if loopActiveCandleLighting == nil {
				continue
			}

			if !loopInPast {
				// We have found a havdalah after the current time, we can return now
				return YomTovTimes{
					CandleLighting: *loopActiveCandleLighting,
					Havdalah:       e.Time,
				}, loopActiveCandleLightingInPast, nil
			}

			loopActiveCandleLighting = nil
		}
	}

	return YomTovTimes{}, false, errors.New("Did not find havdalah after the current date")
}
