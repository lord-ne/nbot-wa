package main

import (
	"errors"
	"fmt"
	"iter"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"nbot-wa/constants"
	"nbot-wa/util"

	"github.com/dlclark/regexp2"
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
			Name:     strings.TrimSpace(event.Summary),
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

func formatMinyanEventDate(builder *strings.Builder, date time.Time) {
	builder.WriteString(date.Format("Monday, January 2"))
	builder.WriteString(util.OrdinalSuffix(date.Day()))

	if date.Year() != time.Now().In(date.Location()).Year() {
		// Add year if different from current
		builder.WriteRune(' ')
		builder.WriteString(date.Format("2006"))
	}
}

func areSameDate(d1 time.Time, d2 time.Time) bool {
	d1 = d1.In(constants.MinyanLocation())
	d2 = d2.In(constants.MinyanLocation())

	return (d1.Year() == d2.Year()) && (d1.Month() == d2.Month()) && (d1.Day() == d2.Day())

}

func formatMinyanMessage(command *TimesCommand, parsedEvents []ParsedEvent) (string, error) {
	var builder strings.Builder

	singleDayRequested := areSameDate(command.dtStart, command.dtEnd)

	builder.WriteRune('*')
	builder.WriteString(command.header)
	builder.WriteString(":*")
	if len(parsedEvents) == 0 {
		if singleDayRequested {
			// If we are outputting times for a single day, show the date even when there are no times to show
			builder.WriteRune('\n')
			formatMinyanEventDate(&builder, command.dtStart)
		}

		builder.WriteString("\n(no times to show)")
	} else {
		singleDayReturned := areSameDate(parsedEvents[0].DateTime, parsedEvents[len(parsedEvents)-1].DateTime)
		prevDate := time.Time{}
		first := true
		for _, event := range parsedEvents {
			eventDateTime := event.DateTime.In(constants.MinyanLocation())

			currDate := startOfDate(eventDateTime)
			if first || currDate != prevDate {
				// If we have gone onto a new day, print it
				builder.WriteRune('\n')
				if !singleDayReturned || !first {
					// Blank line between dates, and before the first if we returned multiple
					builder.WriteRune('\n')
				}
				formatMinyanEventDate(&builder, currDate)
				prevDate = currDate
				first = false
			}

			timeString := eventDateTime.Format(time.Kitchen)
			// Narrow non-breaking space followed by small-caps AM/PM
			timeString = strings.Replace(timeString, "AM", "\u202F\u1D00\u1D0D", 1)
			timeString = strings.Replace(timeString, "PM", "\u202F\u1D18\u1D0D", 1)

			fmt.Fprintf(&builder, "\n- *%v*: %v",
				event.Name,
				timeString)
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

func datetimeRangeForDay(date time.Time) (time.Time, time.Time) {
	dateLoc := date.In(constants.MinyanLocation())
	todayMidnight := time.Date(dateLoc.Year(), dateLoc.Month(), dateLoc.Day(), 0, 0, 0, 0, dateLoc.Location())
	tomorrowMidnight := todayMidnight.AddDate(0, 0, 1).Add(-1 * time.Second)
	return todayMidnight, tomorrowMidnight
}

func plusOneWeek(date time.Time) time.Time {
	return date.AddDate(0, 0, 7).Add(-1 * time.Second)
}

func (state *ProgramState) GetMinyanEventsForDate(dtStart time.Time, dtEnd time.Time) (*calendar.Events, error) {
	return state.CalendarEventsService.List(constants.MinyanCalendarID).
		SingleEvents(true).
		TimeZone("America/New_York").
		TimeMin(dtStart.Format(time.RFC3339)).
		TimeMax(dtEnd.Format(time.RFC3339)).
		Do()
}

func (state *ProgramState) GetMinyanMessage(command *TimesCommand) (string, error) {
	events, err := state.GetMinyanEventsForDate(command.dtStart, command.dtEnd)
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
				upcomingMinyanTimesCommand(false),
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
				upcomingMinyanTimesCommand(false),
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

func startOfDate(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
}

func endOfDate(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, d.Location())
}

// Copied from internal function in time package
func daysIn(m time.Month, year int) int {
	return time.Date(year, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func isDateValid(year int, month int, day int) bool {
	return ((year >= 1) &&
		(month >= 1) && (month <= 12) &&
		(day >= 1) && (day <= daysIn(time.Month(month), year)))
}

func tryMakeDate(year int, month int, day int) (time.Time, error) {
	if !isDateValid(year, month, day) {
		return time.Time{}, fmt.Errorf("invalid date: %d/%d/%d", month, day, year)
	}

	return time.Date(year, time.Month(month), day,
		0, 0, 0, 0,
		constants.MinyanLocation()), nil
}

func nextOccurenceYear(basedate time.Time, month int, day int) int {
	year := basedate.Year()
	if month < int(basedate.Month()) || ((month == int(basedate.Month())) && (day < basedate.Day())) {
		// For dates before today, assume we are talking about next year
		year += 1
	}

	return year
}

var dayOfWeekMap = map[string]time.Weekday{
	"sun":       time.Sunday,
	"sunday":    time.Sunday,
	"mon":       time.Monday,
	"monday":    time.Monday,
	"tue":       time.Tuesday,
	"tues":      time.Tuesday,
	"tuesday":   time.Tuesday,
	"wed":       time.Wednesday,
	"wednesday": time.Wednesday,
	"thu":       time.Thursday,
	"thurs":     time.Thursday,
	"thursday":  time.Thursday,
	"fri":       time.Friday,
	"friday":    time.Friday,
	"sat":       time.Saturday,
	"saturday":  time.Saturday,
	"shab":      time.Saturday,
	"shabbat":   time.Saturday,
	"shabbos":   time.Saturday,
}

var monthMap = map[string]time.Month{
	"jan":       time.January,
	"january":   time.January,
	"feb":       time.February,
	"february":  time.February,
	"mar":       time.March,
	"march":     time.March,
	"apr":       time.April,
	"april":     time.April,
	"may":       time.May,
	"jun":       time.June,
	"june":      time.June,
	"jul":       time.July,
	"july":      time.July,
	"aug":       time.August,
	"august":    time.August,
	"sep":       time.September,
	"september": time.September,
	"oct":       time.October,
	"october":   time.October,
	"nov":       time.November,
	"november":  time.November,
	"dec":       time.December,
	"december":  time.December,
}

func joinRegexOptions(values iter.Seq[string]) string {
	var builder strings.Builder
	builder.WriteString("(?:")

	first := true
	for s := range values {
		if !first {
			builder.WriteRune('|')
		}
		first = false
		builder.WriteString("(?:")
		builder.WriteString(s)
		builder.WriteRune(')')
	}
	builder.WriteRune(')')

	return builder.String()
}

func singleDateRegex(prefix string) string {
	weekday_names := joinRegexOptions(maps.Keys(dayOfWeekMap))
	month_names := joinRegexOptions(maps.Keys(monthMap))

	// 1-2 digit number with (optional) correct ordinal suffix
	ordinal_suffix := joinRegexOptions(slices.Values([]string{
		`(?<=1)(?<!11)st`,
		`(?<=2)(?<!12)nd`,
		`(?<=3)(?<!13)rd`,
		`(?<=[04-9])th`,
		`(?<=1\d)th`}))

	template := joinRegexOptions(slices.Values([]string{
		// today|tomorrow
		`(?P<rel>(?:today)|(?:tomorrow))`,
		// mon|monday|tue|tuesday...
		`(?P<weekday>` + weekday_names + `)`,
		// (Month|Mon) [d]d[st|nd|rd|th][[,] [YY]YY]
		`(?P<long>(?P<long_M>` + month_names + `)\s+(?P<long_D>\d{1,2})(?:` + ordinal_suffix + `)?(?:\s*,?\s+(?P<long_Y>(?:\d{2})?\d{2}))?)`,
		// [M]M/[D]D/[[YY]YY]
		`(?P<short>(?P<short_M>\d{1,2})/(?P<short_D>\d{1,2})(?:/(?P<short_Y>(?:\d{2})?\d{2}))?)`,
	}))

	return strings.ReplaceAll(template, `(?P<`, `(?P<`+prefix)
}

type ParsedSingleDateType int

const (
	ParsedSingleDateType_Date ParsedSingleDateType = iota
	ParsedSingleDateType_Today
	ParsedSingleDateType_Tomorrow
	ParsedSingleDateType_Weekday
)

func parseSingleDateFromMatch(prefix string, basedate time.Time, matches map[string]string) (time.Time, ParsedSingleDateType, error) {
	if v, ok := matches[prefix+"rel"]; ok {
		daystring := strings.ToLower(v)
		switch daystring {
		case "today":
			d := time.Now().In(constants.MinyanLocation())
			return startOfDate(d), ParsedSingleDateType_Today, nil
		case "tomorrow":
			d := time.Now().In(constants.MinyanLocation()).AddDate(0, 0, 1)
			return startOfDate(d), ParsedSingleDateType_Tomorrow, nil
		}
		return time.Time{}, 0, fmt.Errorf("Unknown relative date %s", daystring)
	} else if v, ok := matches[prefix+"weekday"]; ok {
		weekday := dayOfWeekMap[strings.ToLower(v)]
		baseweekday := basedate.Weekday()

		offsetDays := int(weekday) - int(baseweekday)
		if offsetDays <= 0 {
			// Go to next week
			offsetDays += 7
		}

		return startOfDate(basedate.AddDate(0, 0, offsetDays)), ParsedSingleDateType_Weekday, nil
	} else if _, ok := matches[prefix+"short"]; ok {
		day, err := strconv.Atoi(matches[prefix+"short_D"])
		if err != nil {
			return time.Time{}, 0, err
		}

		month, err := strconv.Atoi(matches[prefix+"short_M"])
		if err != nil {
			return time.Time{}, 0, err
		}

		var year int
		if yearString, hasYear := matches[prefix+"short_Y"]; hasYear {
			year, err = strconv.Atoi(yearString)
			if err != nil {
				return time.Time{}, 0, err
			}
			// For 2-digit years, add 2000
			if year < 100 {
				year += ((basedate.Year() / 100) * 100)
			}
		} else {
			year = nextOccurenceYear(basedate, month, day)
		}

		rtn, err := tryMakeDate(year, month, day)
		return rtn, ParsedSingleDateType_Date, err

	} else if _, ok := matches[prefix+"long"]; ok {
		day, err := strconv.Atoi(matches[prefix+"long_D"])
		if err != nil {
			return time.Time{}, 0, err
		}

		month := int(monthMap[strings.ToLower(matches[prefix+"long_M"])])

		var year int
		if yearString, hasYear := matches[prefix+"long_Y"]; hasYear {
			year, err = strconv.Atoi(yearString)
			if err != nil {
				return time.Time{}, 0, err
			}
		} else {
			year = nextOccurenceYear(basedate, month, day)
		}

		rtn, err := tryMakeDate(year, month, day)
		return rtn, ParsedSingleDateType_Date, err
	}
	return time.Time{}, 0, fmt.Errorf("Unknown date match %v", matches)
}

var dateRangeRegex = regexp2.MustCompile(fmt.Sprintf(`(?ims)^\s*%s\s*$`, joinRegexOptions(slices.Values(
	[]string{
		// Upcoming (also matches blank string)
		`(?P<upcoming>(?:upcoming)?)`,
		// Upcoming week
		`(?P<upcomingweek>week)`,
		// Week of X
		`(?P<weekof>week\s+of\s+` + singleDateRegex("weekof_") + `)`,
		// Single date
		`(?P<date>` + singleDateRegex("date_") + `)`,
		// X to X
		`(?P<to>` + singleDateRegex("to1_") + `\s+to\s+` + singleDateRegex("to2_") + `)`,
	}))), regexp2.RE2)

func matchRegexGetGroups(r *regexp2.Regexp, s string) map[string]string {
	match, err := r.FindStringMatch(s)
	if err != nil || match == nil {
		return nil
	}

	result := make(map[string]string)
	for i, name := range r.GetGroupNames() {
		if name != "" {
			group := match.GroupByNumber(i)
			if len(group.Captures) != 0 {
				result[name] = group.String()
			}
		}
	}

	return result
}

type TimesCommand struct {
	dtStart       time.Time
	dtEnd         time.Time
	header        string
	sephardic     bool
	includePassed bool
}

func formatDateStringForSingle(date time.Time, dateType ParsedSingleDateType) string {
	switch dateType {
	case ParsedSingleDateType_Date:
		return "date:"
	case ParsedSingleDateType_Today:
		return "today"
	case ParsedSingleDateType_Tomorrow:
		return "tomorrow"
	case ParsedSingleDateType_Weekday:
		return date.Weekday().String()
	}
	return ""
}

func formatDateStringForMultiple(date time.Time, dateType ParsedSingleDateType) string {
	dateStr := date.Format("1/2/06")
	return dateStr
	// switch dateType {
	// case ParsedSingleDateType_Date:
	// 	return dateStr
	// case ParsedSingleDateType_Today:
	// 	return "today (" + dateStr + ")"
	// case ParsedSingleDateType_Tomorrow:
	// 	return "tomorrow (" + dateStr + ")"
	// case ParsedSingleDateType_Weekday:
	// 	return date.Weekday().String() + " (" + dateStr + ")"
	// }
	// return ""
}

func upcomingMinyanTimesCommand(isSephardic bool) *TimesCommand {
	dtStart := time.Now().In(constants.MinyanLocation())
	dtEnd := dtStart.Add(25 * time.Hour)

	return &TimesCommand{
		dtStart:       dtStart,
		dtEnd:         dtEnd,
		header:        "Upcoming minyan times",
		sephardic:     isSephardic,
		includePassed: false,
	}
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

	matches := matchRegexGetGroups(dateRangeRegex, text)
	if matches == nil {
		return nil, fmt.Errorf("Date did not match: '%s'", text)
	}

	if _, ok := matches["upcoming"]; ok {
		return upcomingMinyanTimesCommand(isSephardic), nil
	} else if _, ok := matches["upcomingweek"]; ok {
		dtStart := startOfDate(time.Now().In(constants.MinyanLocation()))
		dtEnd := plusOneWeek(dtStart)

		return &TimesCommand{
			dtStart:       dtStart,
			dtEnd:         dtEnd,
			header:        "Minyan times for the upcoming week",
			sephardic:     isSephardic,
			includePassed: false,
		}, nil
	} else if _, ok := matches["date"]; ok {
		date, dateType, err := parseSingleDateFromMatch(
			"date_",
			time.Now().In(constants.MinyanLocation()),
			matches)

		if err != nil {
			return nil, err
		}

		dtStart, dtEnd := datetimeRangeForDay(date)

		headerDateStr := formatDateStringForSingle(date, dateType)
		return &TimesCommand{
			dtStart:       dtStart,
			dtEnd:         dtEnd,
			header:        "Minyan times for " + headerDateStr,
			sephardic:     isSephardic,
			includePassed: true,
		}, nil
	} else if _, ok := matches["weekof"]; ok {
		date, dateType, err := parseSingleDateFromMatch(
			"weekof_",
			time.Now().In(constants.MinyanLocation()),
			matches)

		if err != nil {
			return nil, err
		}

		dtStart := startOfDate(date.AddDate(0, 0, -int(date.Weekday())))
		dtEnd := plusOneWeek(dtStart)

		headerDateStr := formatDateStringForMultiple(date, dateType)
		return &TimesCommand{
			dtStart:       dtStart,
			dtEnd:         dtEnd,
			header:        "Minyan times for the week of " + headerDateStr,
			sephardic:     isSephardic,
			includePassed: true,
		}, nil
	} else if _, ok := matches["to"]; ok {
		date1, dateType1, err := parseSingleDateFromMatch(
			"to1_",
			time.Now().In(constants.MinyanLocation()),
			matches)

		if err != nil {
			return nil, err
		}

		dtStart := startOfDate(date1)

		date2, dateType2, err := parseSingleDateFromMatch(
			"to2_",
			dtStart,
			matches)

		if err != nil {
			return nil, err
		}

		dtEnd := endOfDate(date2)

		headerDateStr1 := formatDateStringForMultiple(date1, dateType1)
		headerDateStr2 := formatDateStringForMultiple(date2, dateType2)

		return &TimesCommand{
			dtStart:       dtStart,
			dtEnd:         dtEnd,
			header:        "Minyan times from " + headerDateStr1 + " to " + headerDateStr2,
			sephardic:     isSephardic,
			includePassed: true,
		}, nil
	}

	return nil, fmt.Errorf("Invalid date match. Groups %v, string %q", matches, text)
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
	} else if strings.HasPrefix(inputText, "!help") {
		state.QueueSimpleStringMessage(v.Info.Chat, strings.Join([]string{
			"*Usage:*",
			"",
			"`!times` or `!times upcoming`",
			"- Displays minyan times for the next 25 hours",
			"",
			"`!times week`",
			"- Displays minyan times for the next 7 days",
			"",
			"`!times DATE`",
			"- Displays minyan times for `DATE`",
			"",
			"`!times week of DATE`",
			"- Displays minyan times for the week of `DATE`",
			"",
			"`!times DATE to DATE`",
			"- Displays minyan times between the first `DATE` and the second `DATE`",
			"",
			"The `DATE` can be in any of the following formats (capitalization doesn't matter):",
			"- `today` or `tomorrow`",
			"- A day of the week like `Mon`, `Tuesday`, `Shabbat`, etc.",
			"- A date in the format `M[M]/D[D][/[YY]YY]`, e.g. `1/21`, `08/15/25`, `11/07/2026`",
			"- A date in the format `Month DD[th][[,] YYYY]`, e.g. `Jan 21st`, `August 15 2025`, `November 7th, 2000`",
		}, "\n"))

	}
}
