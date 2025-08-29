package main

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"nbot-wa/constants"
	"nbot-wa/util"

	"github.com/go-co-op/gocron/v2"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/api/calendar/v3"
)

var rSepharidic = regexp.MustCompile(`(?ims)^\s*se?(?:(?:f)|(?:ph))ara?dic?\s*(?:(?:\s)|(?:$))`)

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
	Name     string
	DateTime time.Time
}

func parseEvents(events []*calendar.Event) ([]ParsedEvent, error) {
	parsedEvents := []ParsedEvent{}

	for _, event := range events {
		t, err := parseEventDateTime(event.Start)
		if err != nil {
			return nil, err
		}

		parsedEvents = append(parsedEvents, ParsedEvent{
			Name:     event.Summary,
			DateTime: t,
		})
	}

	slices.SortFunc(parsedEvents, func(a, b ParsedEvent) int {
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

func formatMinyanMessage(command *TimesCommand, parsedEvents []ParsedEvent) (string, error) {
	var builder strings.Builder

	builder.WriteString(command.header)
	builder.WriteRune('\n')
	builder.WriteString(command.date.Format("Monday, January 2"))
	builder.WriteString(util.OrdinalSuffix(command.date.Day()))

	if command.date.Year() != time.Now().In(constants.MinyanLocation()).Year() {
		// Add year if different from current
		builder.WriteRune(' ')
		builder.WriteString(command.date.Format("2006"))
	}

	builder.WriteRune('\n')

	if len(parsedEvents) <= 0 {
		builder.WriteString("\n(no times to show)")
	} else {
		for _, event := range parsedEvents {
			timeString := event.DateTime.In(constants.MinyanLocation()).Format(time.Kitchen)
			// Narrow non-breaking space followed by small-caps AM/PM
			timeString = strings.Replace(timeString, "AM", "\u202F\u1D00\u1D0D", 1)
			timeString = strings.Replace(timeString, "PM", "\u202F\u1D18\u1D0D", 1)

			builder.WriteString(fmt.Sprintf(
				"\n*%v*: %v",
				event.Name,
				timeString,
			))
		}
	}

	message := builder.String()

	if command.sephardic {
		message = strings.ReplaceAll(message, "Shacharis", "Shaharit")
		message = strings.ReplaceAll(message, "shacharis", "shaharit")
		message = strings.ReplaceAll(message, "Mincha", "Minha")
		message = strings.ReplaceAll(message, "mincha", "minha")
		message = strings.ReplaceAll(message, "Maariv", "Arbit")
		message = strings.ReplaceAll(message, "maariv", "arbit")
		message = strings.ReplaceAll(message, "Slichot", "Selihot")
		message = strings.ReplaceAll(message, "slichot", "selihot")
	}

	return message, nil
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

func (state *ProgramState) GetMinyanMessage(command *TimesCommand) (string, error) {
	events, err := state.GetMinyanEventsForDate(command.date)
	if err != nil {
		return "", err
	}

	parsedEvents, err := parseEvents(events.Items)
	if err != nil {
		return "", err
	}

	cutoff := time.Now().In(constants.MinyanLocation()).Add(-5 * time.Minute)
	if !command.includePassed {
		parsedEvents = util.Filter(parsedEvents, func(event ParsedEvent) bool {
			return event.DateTime.After(cutoff)
		})
	}

	return formatMinyanMessage(command, parsedEvents)
}

func (state *ProgramState) SendMinyanTimes(command *TimesCommand, chat types.JID, shouldSendOnError bool) {
	message, err := state.GetMinyanMessage(command)

	if err != nil {
		if shouldSendOnError {
			state.QueueSimpleStringMessage(chat, "```There was an error retrieving the minyan times```")
		}
		state.ReportErrorToMe(err, "HandleMinyanMessage")

		return
	}

	state.QueueSimpleStringMessage(chat, message)
}

func (state *ProgramState) RegisterDailyEvents() {
	// Send minyan times for today at 9:30am
	state.MinyanScheduler.NewJob(
		gocron.DailyJob(1, gocron.NewAtTimes(gocron.NewAtTime(9, 30, 0))),
		gocron.NewTask(func() {
			now := time.Now().In(constants.MinyanLocation())

			_, isYomTov, err := CurrentOrUpcomingYomTov(now)

			if err != nil {
				state.ReportErrorToMe(err, "CurrentOrUpcomingYomTov")
				return
			}

			if isYomTov {
				fmt.Println("Scheduled event did not run since issur melacha is in effect", now)
				return
			}

			state.SendMinyanTimes(
				&TimesCommand{
					date:          now,
					header:        "*Upcoming minyan times for today*",
					sephardic:     false,
					includePassed: false,
				},
				constants.ChatIDMinyan(),
				false)
		}),
	)

	// Send minyan times for tomorrow at 8:30pm
	state.MinyanScheduler.NewJob(
		gocron.DailyJob(1, gocron.NewAtTimes(gocron.NewAtTime(20, 30, 0))),
		gocron.NewTask(func() {
			now := time.Now().In(constants.MinyanLocation())

			_, isYomTov, err := CurrentOrUpcomingYomTov(now)

			if err != nil {
				state.ReportErrorToMe(err, "CurrentOrUpcomingYomTov")
				return
			}

			if isYomTov {
				fmt.Println("Scheduled event did not run since issur melacha is in effect", now)
				return
			}

			state.SendMinyanTimes(
				&TimesCommand{
					date:          now.AddDate(0, 0, 1),
					header:        "*Minyan times for tomorrow*",
					sephardic:     false,
					includePassed: true,
				},
				constants.ChatIDMinyan(),
				false)
		}),
	)

	// Send a message every week (Sunday at noon) reminding me to log in to the bot account on my
	// phone so that the linked device doesn't expire.
	// This should really be in another file, but the scheduler is here so it's easier
	state.MinyanScheduler.NewJob(
		gocron.WeeklyJob(1, gocron.NewWeekdays(time.Sunday), gocron.NewAtTimes(gocron.NewAtTime(12, 0, 0))),
		gocron.NewTask(func() {
			state.QueueSimpleStringMessage(constants.ChatIDMe(),
				"*Reminder*: Please log in to the bot account to prevent the linked device from expiring")
		}),
	)
}

type ParsedDate struct {
	year  int
	month int
	day   int
}

// Copied from internal function in time package
func daysIn(m time.Month, year int) int {
	return time.Date(year, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func isDateValid(d *ParsedDate) bool {
	return ((d.year >= 1) &&
		(d.month >= 1) && (d.month <= 12) &&
		(d.day >= 1) && (d.day <= daysIn(time.Month(d.month), d.year)))
}

func tryParseAsDate(text string) (*time.Time, error) {
	var d *ParsedDate = nil
	var err error = nil

	parserFuncs := [](func(text string) (*ParsedDate, error)){helperParseSimpleDate}
	for _, f := range parserFuncs {
		d, err = f(text)

		if err != nil {
			return nil, err
		}

		if d != nil {
			break
		}
	}

	if d == nil {
		return nil, nil // No match, return nil
	}

	if !isDateValid(d) {
		return nil, fmt.Errorf("invalid date: %q (parsed from %q)", d, text)
	}

	return util.New(time.Date(d.year, time.Month(d.month), d.day,
		0, 0, 0, 0,
		constants.MinyanLocation())), nil
}

var rSimpleDate = regexp.MustCompile(`(?ims)^\s*(\d{1,2})/(\d{1,2})(?:/((?:\d{2})?\d{2}))?\s*$`)

func helperParseSimpleDate(text string) (*ParsedDate, error) {
	submatches := rSimpleDate.FindStringSubmatch(text)

	if submatches == nil {
		return nil, nil // No match, return nil
	}

	if len(submatches) != 4 {
		return nil, fmt.Errorf("wrong number of submatches (got %v, should be 3)", len(submatches))
	}

	month, err := strconv.Atoi(submatches[1])
	if err != nil {
		return nil, err
	}

	day, err := strconv.Atoi(submatches[2])
	if err != nil {
		return nil, err
	}

	var year int
	if len(submatches[3]) == 0 {
		now := time.Now().In(constants.MinyanLocation())
		if month > int(now.Month()) || ((month == int(now.Month())) && (day >= now.Day())) {
			year = now.Year()
		} else {
			// For dates before today, assume we are talking about next year
			year = now.Year() + 1
		}
	} else {
		year, err = strconv.Atoi(submatches[3])
		if err != nil {
			return nil, err
		}
		if len(submatches[3]) == 2 {
			year += 2000
		}
	}

	return &ParsedDate{
		year:  year,
		month: month,
		day:   day}, nil
}

type TimesCommand struct {
	date          time.Time
	header        string
	sephardic     bool
	includePassed bool
}

func parseTimeCommand(text string) (*TimesCommand, error) {

	var found bool
	text, found = strings.CutPrefix(text, "!times")
	if !found {
		return nil, errors.New("text does not start with '!times'")
	}

	text = strings.TrimSpace(text)

	var isSephardic bool = false
	text, isSephardic = util.RemoveAndCheckMatch(rSepharidic, text)

	if len(text) == 0 {
		// Plain "!times" command
		return &TimesCommand{
			date:          time.Now().In(constants.MinyanLocation()),
			header:        "*Upcoming minyan times for today*",
			sephardic:     isSephardic,
			includePassed: false,
		}, nil
	}

	if text == "today" {
		return &TimesCommand{
			date:          time.Now().In(constants.MinyanLocation()),
			header:        "*Minyan times for today*",
			sephardic:     isSephardic,
			includePassed: true,
		}, nil
	}

	if text == "tomorrow" {
		return &TimesCommand{
			date:          time.Now().In(constants.MinyanLocation()).AddDate(0, 0, 1),
			header:        "*Minyan times for tomorrow*",
			sephardic:     isSephardic,
			includePassed: true,
		}, nil
	}

	date, err := tryParseAsDate(text)
	if err != nil {
		return nil, err
	}
	if date != nil {
		return &TimesCommand{
			date:          *date,
			header:        "*Minyan times for date:*",
			sephardic:     isSephardic,
			includePassed: true,
		}, nil
	}

	return nil, fmt.Errorf("extra text '%s'", text)
}

func (state *ProgramState) HandleMinyanMessage(v *events.Message) {
	inputText := util.NormalizeString(v.Message.GetConversation())

	if strings.HasPrefix(inputText, "!times") {
		command, err := parseTimeCommand(inputText)
		if err != nil {
			state.QueueSimpleStringMessage(v.Info.Chat, "```Could not parse the command```")
			state.ReportErrorToMe(err, "HandleMinyanMessage")

			return
		}

		state.SendMinyanTimes(command, v.Info.Chat, true)
	}
}
