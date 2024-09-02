package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"nbot-wa/constants"
	"nbot-wa/daily"
	"nbot-wa/util"

	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/api/calendar/v3"
)

func parseEventDateTime(t *calendar.EventDateTime) (time.Time, error) {
	var rtnTime time.Time
	var err error
	var location *time.Location = constants.MinyanLocation()

	if len(t.TimeZone) > 0 {
		location, err = time.LoadLocation(t.TimeZone)

		if err != nil {
			return time.Time{}, err
		}
	}

	if len(t.DateTime) > 0 {
		rtnTime, err = time.ParseInLocation(time.RFC3339, t.DateTime, location)
	} else if len(t.Date) > 0 {
		rtnTime, err = time.ParseInLocation("2006-01-02", t.Date, location)
	} else {
		err = errors.New(fmt.Sprint("Fields not specified in received event time", t))
	}

	if err != nil {
		return time.Time{}, err
	}

	return rtnTime.In(constants.MinyanLocation()), nil
}

type ParsedEvent struct {
	Name string;
	DateTime time.Time;
}

func parseEvents(events []*calendar.Event) ([]ParsedEvent, error) {
	parsedEvents := []ParsedEvent{}

	for _, event := range events {
		t, err := parseEventDateTime(event.Start)
		if err != nil {
			return nil, err
		}

		parsedEvents = append(parsedEvents, ParsedEvent{
			Name: event.Summary,
			DateTime: t,
		})
	}

	slices.SortFunc(parsedEvents, func (a, b ParsedEvent) int {
		if a.DateTime.Before(b.DateTime) {
			return -1
		} else if b.DateTime.Before(a.DateTime) {
			return 1
		} else {
			return 0
		}
	})
	
	return parsedEvents, nil
}

func formatMinyanMessage(parsedEvents []ParsedEvent) (string, error) {
    var builder strings.Builder

	if (len(parsedEvents) <= 0) {
		return "(no times to show)", nil
	}

	for _, event := range parsedEvents {
		builder.WriteString(fmt.Sprintf(
			"\n*%v*: %v",
			event.Name,
			event.DateTime.In(constants.MinyanLocation()).Format(time.Kitchen),
		))
	}

	return builder.String(), nil
}

func (state *ProgramState) GetMinyanEventsForDate(date time.Time) (*calendar.Events, error) {
	dateLoc := date.In(constants.MinyanLocation())

	todayMidnight := time.Date(dateLoc.Year(), dateLoc.Month(), dateLoc.Day(), 0, 0, 0, 0, dateLoc.Location())
	tomorrowMidnight := todayMidnight.AddDate(0, 0, 1)

	return state.CalendarEventsService.List(constants.MinyanCalendarID).
		SingleEvents(true).
		TimeZone("America/New_York").
		TimeMin(todayMidnight.Format(time.RFC3339)).
		TimeMax(tomorrowMidnight.Format(time.RFC3339)).
		Do()
}

func (state *ProgramState) GetMinyanMessageForDate(date time.Time, includePassed bool) (string, error) {
	events, err := state.GetMinyanEventsForDate(date)
	if err != nil {
		return "", err
	}

	parsedEvents, err := parseEvents(events.Items)
	if err != nil {
		return "", err
	}

	cutoff := time.Now().In(constants.MinyanLocation()).Add(-5 * time.Minute)
	if (!includePassed) {
		parsedEvents = util.Filter(parsedEvents, func(event ParsedEvent) bool {
			return event.DateTime.After(cutoff)
		})
	}
	
	return formatMinyanMessage(parsedEvents)
}

func (state *ProgramState) SendMinyanTimes(date time.Time, header string, chat types.JID, shouldSendOnError bool, includePassed bool) {
	messageBody, err := state.GetMinyanMessageForDate(date, includePassed)

	if (err != nil) {
		errorMessage := fmt.Sprintf("Error in HandleMinyanMessage: %v", err)
		fmt.Println(errorMessage)
		state.QueueSimpleStringMessage(constants.ChatIDMe(), errorMessage)

		if shouldSendOnError {
			message := fmt.Sprintf("```There was an error retrieving the minyan times. Contact %s if the problem persists.```",
				constants.MaintainerName)
			
			state.QueueSimpleStringMessage(chat, message)
		}
		return
	}
	
	message := fmt.Sprintf("%s\n%s\n%s",
		header,
		date.Format("Monday, January 02"),
		messageBody)

	state.QueueSimpleStringMessage(chat, message)
}

func (state *ProgramState) RegisterDailyEvents() {
	// Send minyan times for today at 9:30
	state.DailyEventRunner.AddEvent(&daily.Event{
		TimeToRun: daily.TimeOfDay{
			Hours: 9,
			Minutes: 30,
		},
		CallbackFunc: func(scheduledTime time.Time) {
			state.SendMinyanTimes(
				scheduledTime.In(constants.MinyanLocation()),
				"*Upcoming minyan times for today*",
				constants.ChatIDMinyan(),
				false,
				false)
		},
	})

	// Send minyan times for tomorrow
	state.DailyEventRunner.AddEvent(&daily.Event{
		TimeToRun: daily.TimeOfDay{
			Hours: 20,
			Minutes: 30,
		},
		CallbackFunc: func(scheduledTime time.Time) {
			state.SendMinyanTimes(
				scheduledTime.In(constants.MinyanLocation()).AddDate(0, 0, 1),
				"*Minyan times for tomorrow*",
				constants.ChatIDMinyan(),
				false,
				true)
		},
	})
}

func (state *ProgramState) HandleMinyanMessage(v *events.Message) {
	switch util.NormalizeString(v.Message.GetConversation()) {
	
	case "!times":
		state.SendMinyanTimes(time.Now().Local(), "*Upcoming minyan times for today*", v.Info.Chat, true, false)
	case "!times today":
		state.SendMinyanTimes(time.Now().Local(), "*Minyan times for today*", v.Info.Chat, true, true)
	case "!times tomorrow":
		state.SendMinyanTimes(time.Now().Local().AddDate(0, 0, 1), "*Minyan times for tomorrow*", v.Info.Chat, true, true)
	}
}