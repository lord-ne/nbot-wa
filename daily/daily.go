package daily

import (
	"cmp"
	"nbot-wa/util"
	"slices"
	"time"
)

type TimeOfDay struct {
	Hours uint8
	Minutes uint8
}

func (t TimeOfDay) OnDate(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), int(t.Hours), int(t.Minutes), 0, 0, d.Location())
}

func AsTimeOfDay(t time.Time) TimeOfDay {
	return TimeOfDay{uint8(t.Hour()), uint8(t.Minute())}
}

type Event struct {
	TimeToRun TimeOfDay
	CallbackFunc func(scheduledTime time.Time)
	RunOnce bool
}

type DailyRunner struct {
	ticker *time.Ticker
	events []*Event
	interval time.Duration
	lastTick time.Time
}

func (runner *DailyRunner) AddEvent(e *Event) {
	runner.events = append(runner.events, e)
}

func closestRun(timeToRun TimeOfDay, currTime time.Time) time.Time {
	candidateTimes := []time.Time{
		timeToRun.OnDate(currTime),
		timeToRun.OnDate(currTime.AddDate(0, 0, -1)),
		timeToRun.OnDate(currTime.AddDate(0, 0, 1)),
	}

	closestTime := slices.MinFunc(candidateTimes, func(a, b time.Time) int {
		return cmp.Compare(currTime.Sub(a).Abs(), currTime.Sub(b).Abs())
	})

	return closestTime
}

func isReadyToRun(closestRunTime time.Time, currTime time.Time, interval time.Duration) bool {
	// Only run if we are within 2 intervals of when the event should have gone off
	return !closestRunTime.After(currTime) && (currTime.Sub(closestRunTime) <= interval * 2)
}

func (runner *DailyRunner) waitForTicks() {

	for currTick := range runner.ticker.C {
		currTick = currTick.Local()

		lastTick := runner.lastTick
		runner.lastTick = currTick

		toBeRemoved := []int{}

		for i, event := range runner.events {
			closestRunTimeToNow := closestRun(event.TimeToRun, currTick)

			readyToRunNow := isReadyToRun(closestRunTimeToNow, currTick, runner.interval)
			readyToRunLastTime := (lastTick != time.Time{}) && isReadyToRun(closestRun(event.TimeToRun, lastTick), lastTick, runner.interval)

			if readyToRunNow && !readyToRunLastTime {
				event.CallbackFunc(closestRunTimeToNow)
				if event.RunOnce {
					toBeRemoved = append(toBeRemoved, i)
				}
			}
		}

		for _, index := range toBeRemoved {
			util.Remove(&runner.events, index)
		}
	}
}

func MakeDailyRunner(interval time.Duration) *DailyRunner {
	runner := &DailyRunner{
		ticker: time.NewTicker(interval),
		interval: interval,
		events: []*Event{},
		lastTick: time.Time{},
	}

	go runner.waitForTicks()

	return runner
}