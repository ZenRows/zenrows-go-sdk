package batch

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// utcOffsetLength is the length of a trailing "+HH:MM" / "-HH:MM" timezone offset.
const utcOffsetLength = 6

var daysOfWeek = map[string]bool{
	"mon": true, "tue": true, "wed": true, "thu": true, "fri": true, "sat": true, "sun": true,
}

func validateTimezone(tz, field string) error {
	if tz == "" {
		return fmt.Errorf("%s is required (IANA name, e.g. \"Europe/Berlin\")", field)
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Errorf("%s: %q is not a valid IANA timezone", field, tz)
	}
	return nil
}

// hasTZSuffix reports whether raw carries an RFC 3339-style tz tail (Z / +HH:MM / -HH:MM).
func hasTZSuffix(raw string) bool {
	raw = strings.TrimSpace(raw)
	sepIdx := -1
	for _, sep := range []string{"T", " "} {
		if i := strings.Index(raw, sep); i >= 0 {
			sepIdx = i
			break
		}
	}
	if sepIdx < 0 {
		return false
	}
	tail := raw[sepIdx+1:]
	if strings.HasSuffix(tail, "Z") || strings.HasSuffix(tail, "z") {
		return true
	}
	if len(tail) >= utcOffsetLength {
		c := tail[len(tail)-utcOffsetLength]
		return c == '+' || c == '-'
	}
	return false
}

func validateFullHour(s string) error {
	if len(s) != 5 || s[2] != ':' || s[3:] != "00" {
		return fmt.Errorf("times_of_day entry %q must be on the hour (HH:00); minute granularity is rejected", s)
	}
	h, err := strconv.Atoi(s[:2])
	if err != nil {
		return fmt.Errorf("times_of_day entry %q has a non-numeric hour", s)
	}
	if h < 0 || h > 23 {
		return fmt.Errorf("times_of_day entry %q is not a valid hour (00..23)", s)
	}
	return nil
}

// At is a one-shot schedule fire at a specific wall-clock time. Build with NewAt.
type At struct {
	at       string
	timezone string
}

// NewAt builds a one-shot schedule fire at a tz-naive ISO timestamp (no "Z", no offset — the
// timezone param is the single authoritative interpreter, which keeps DST transitions
// deterministic).
func NewAt(at, timezone string) (At, error) {
	if err := validateTimezone(timezone, "At.timezone"); err != nil {
		return At{}, err
	}
	if strings.TrimSpace(at) == "" {
		return At{}, fmt.Errorf("invalid At.at: must be a non-empty ISO timestamp string")
	}
	if hasTZSuffix(at) {
		return At{}, fmt.Errorf(
			"invalid At.at %q: must be tz-naive (no Z, no offset); supply timezone separately to keep DST transitions deterministic",
			at,
		)
	}
	return At{at: at, timezone: timezone}, nil
}

// NewAtTime is NewAt for a time.Time value — formatted as a naive ISO timestamp
// ("2006-01-02T15:04:05"), dropping any location info. Pass the wall-clock time you want in
// the schedule's timezone, not a UTC-converted one.
func NewAtTime(at time.Time, timezone string) (At, error) {
	return NewAt(at.Format("2006-01-02T15:04:05"), timezone)
}

func (a At) toSchedule() JobSchedule {
	return JobSchedule{At: a.at, Timezone: a.timezone}
}

// Rate is an interval-based schedule fire policy — every N units, no wall-clock alignment.
// Build with NewRate.
type Rate struct {
	every int
	unit  string
}

// NewRate builds an interval-based schedule. unit is one of "minute", "hour", "day".
func NewRate(every int, unit string) (Rate, error) {
	if every < 1 {
		return Rate{}, fmt.Errorf("invalid Rate.every: must be >= 1, got %d", every)
	}
	switch unit {
	case "minute", "hour", "day":
	default:
		return Rate{}, fmt.Errorf("invalid Rate.unit: must be one of \"minute\", \"hour\", \"day\"; got %q", unit)
	}
	return Rate{every: every, unit: unit}, nil
}

func (r Rate) toSchedule() JobSchedule {
	return JobSchedule{Rate: &ScheduleRate{Every: r.every, Unit: r.unit}}
}

// Cadence is a Calendar schedule's day-selection policy — Daily, Weekly, or Monthly.
type Cadence interface {
	cadenceDict() ScheduleCadence
}

// Daily fires every day. No knobs.
type Daily struct{}

func (Daily) cadenceDict() ScheduleCadence {
	return ScheduleCadence{Daily: map[string]any{}}
}

// Weekly fires on specific days of the week. Build with NewWeekly.
type Weekly struct {
	days []string
}

// NewWeekly builds a weekly cadence. days are lower-case 3-letter names (mon/tue/wed/thu/
// fri/sat/sun), at least one required.
func NewWeekly(days []string) (Weekly, error) {
	if len(days) == 0 {
		return Weekly{}, fmt.Errorf("invalid Weekly.days: must be non-empty")
	}
	for _, d := range days {
		if !daysOfWeek[d] {
			return Weekly{}, fmt.Errorf(
				"invalid Weekly.days entry %q: not a valid day (use one of mon, tue, wed, thu, fri, sat, sun)", d,
			)
		}
	}
	return Weekly{days: append([]string(nil), days...)}, nil
}

func (w Weekly) cadenceDict() ScheduleCadence {
	return ScheduleCadence{Weekly: &ScheduleWeekly{Days: w.days}}
}

// Monthly fires on specific days of the month. Build with NewMonthly.
type Monthly struct {
	days []int
}

// NewMonthly builds a monthly cadence. days are 1-31, at least one required. Days that don't
// exist in a given month (e.g. 31 in April) are silently skipped by the scheduler.
func NewMonthly(days []int) (Monthly, error) {
	if len(days) == 0 {
		return Monthly{}, fmt.Errorf("invalid Monthly.days: must be non-empty")
	}
	for _, d := range days {
		if d < 1 || d > 31 {
			return Monthly{}, fmt.Errorf("invalid Monthly.days entry %d: out of range (1..31)", d)
		}
	}
	return Monthly{days: append([]int(nil), days...)}, nil
}

func (m Monthly) cadenceDict() ScheduleCadence {
	return ScheduleCadence{Monthly: &ScheduleMonthly{Days: m.days}}
}

// Calendar is a calendar-style schedule fire policy: a list of times-of-day on a
// daily/weekly/monthly cadence. Build with NewCalendar.
type Calendar struct {
	timesOfDay []string
	cadence    Cadence
	timezone   string
}

// NewCalendar builds a calendar schedule. timesOfDay are full-hour 24h strings ("09:00", not
// "09:30"); cadence is exactly one of Daily{}, a Weekly, or a Monthly; timezone is mandatory
// (IANA name).
func NewCalendar(timesOfDay []string, cadence Cadence, timezone string) (Calendar, error) {
	if err := validateTimezone(timezone, "Calendar.timezone"); err != nil {
		return Calendar{}, err
	}
	if len(timesOfDay) == 0 {
		return Calendar{}, fmt.Errorf("invalid Calendar.times_of_day: must be non-empty")
	}
	for _, t := range timesOfDay {
		if err := validateFullHour(t); err != nil {
			return Calendar{}, err
		}
	}
	if cadence == nil {
		return Calendar{}, fmt.Errorf("invalid Calendar.cadence: must be Daily{}, Weekly, or Monthly")
	}
	return Calendar{
		timesOfDay: append([]string(nil), timesOfDay...),
		cadence:    cadence,
		timezone:   timezone,
	}, nil
}

func (c Calendar) toSchedule() JobSchedule {
	cadence := c.cadence.cadenceDict()
	return JobSchedule{
		Calendar: &ScheduleCalendar{TimesOfDay: c.timesOfDay, Cadence: cadence},
		Timezone: c.timezone,
	}
}

// Schedule is anything that can produce the wire JobSchedule block for SubmitScheduled /
// ScheduleControls.Update — an At, Rate, or Calendar.
type Schedule interface {
	toSchedule() JobSchedule
}

var (
	_ Schedule = At{}
	_ Schedule = Rate{}
	_ Schedule = Calendar{}
)
