package domain

// Russian federal production calendar. The versioned YAML files under data/ are mirrored
// from contracts/calendar-ru-*.yaml and embedded so capacity math works without a live
// service. A year without calendar data is a hard failure for the run pipeline (ADR-002 §3).

import (
	"embed"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed data/calendar-ru-*.yaml
var calendarFiles embed.FS

type productionCalendar struct {
	Year                int      `yaml:"year"`
	Source              string   `yaml:"source"`
	NonWorkingDates     []string `yaml:"non_working_dates"`
	WorkingWeekendDates []string `yaml:"working_weekend_dates"`
	ShortenedDates      []string `yaml:"shortened_dates"`
	nonWorking          map[string]bool
	workingWeekend      map[string]bool
	shortened           map[string]bool
}

var (
	calendarOnce   sync.Once
	calendarByYear map[int]*productionCalendar
	calendarErr    error
)

func loadCalendars() {
	entries, err := calendarFiles.ReadDir("data")
	if err != nil {
		calendarErr = fmt.Errorf("read embedded calendars: %w", err)
		return
	}
	calendarByYear = make(map[int]*productionCalendar, len(entries))
	for _, entry := range entries {
		raw, err := calendarFiles.ReadFile("data/" + entry.Name())
		if err != nil {
			calendarErr = fmt.Errorf("read %s: %w", entry.Name(), err)
			return
		}
		var c productionCalendar
		if err := yaml.Unmarshal(raw, &c); err != nil {
			calendarErr = fmt.Errorf("parse %s: %w", entry.Name(), err)
			return
		}
		c.nonWorking = dateSet(c.NonWorkingDates)
		c.workingWeekend = dateSet(c.WorkingWeekendDates)
		c.shortened = dateSet(c.ShortenedDates)
		if c.Year < 1 || len(c.nonWorking) == 0 {
			calendarErr = fmt.Errorf("calendar %s has no year or holidays", entry.Name())
			return
		}
		calendarByYear[c.Year] = &c
	}
}

func dateSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[strings.TrimSpace(value)] = true
	}
	return out
}

func dateKey(t time.Time) string {
	return fmt.Sprintf("%04d-%02d-%02d", t.Year(), int(t.Month()), t.Day())
}

func calendarFor(year int) (*productionCalendar, error) {
	calendarOnce.Do(loadCalendars)
	if calendarErr != nil {
		return nil, calendarErr
	}
	c, ok := calendarByYear[year]
	if !ok {
		years := make([]int, 0, len(calendarByYear))
		for y := range calendarByYear {
			years = append(years, y)
		}
		sort.Ints(years)
		return nil, fmt.Errorf("%w: no production calendar for %d (available: %v)",
			ErrCalendarMissing, year, years)
	}
	return c, nil
}

// ErrCalendarMissing is the G5 hard failure when a planned year has no versioned calendar.
var ErrCalendarMissing = fmt.Errorf("calendar data missing")

// CalendarAvailable reports whether capacity can be computed for the year.
func CalendarAvailable(year int) bool {
	c, err := calendarFor(year)
	return err == nil && c != nil
}

func (c *productionCalendar) isWorkingDay(day time.Time) bool {
	key := dateKey(day)
	if c.workingWeekend[key] {
		return true
	}
	weekday := day.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false
	}
	return !c.nonWorking[key]
}

func (c *productionCalendar) daysInWeek(year, week int) (working, shortened int) {
	monday := WeekMonday(year, week)
	for i := 0; i < 7; i++ {
		day := monday.AddDate(0, 0, i)
		if !c.isWorkingDay(day) {
			continue
		}
		working++
		if c.shortened[dateKey(day)] {
			shortened++
		}
	}
	return working, shortened
}

// WorkingDaysInWeekChecked returns the calendar-derived working days of an ISO week.
func WorkingDaysInWeekChecked(year, week int) (int, error) {
	c, err := calendarFor(year)
	if err != nil {
		return 0, err
	}
	working, _ := c.daysInWeek(year, week)
	return working, nil
}

// WorkingDaysInWeek is the compatibility accessor for code that cannot propagate an error.
// The run pipeline uses WorkingDaysInWeekChecked so an unknown year never silently falls back.
func WorkingDaysInWeek(year, week int) int {
	working, err := WorkingDaysInWeekChecked(year, week)
	if err != nil {
		return 5
	}
	return working
}

// NormHoursInWeek derives norm hours from the calendar and the employee's weekly norm.
// A shortened pre-holiday day loses one eighth of the daily hours.
func NormHoursInWeek(year, week int, weeklyHours float64) (float64, error) {
	c, err := calendarFor(year)
	if err != nil {
		return 0, err
	}
	working, shortened := c.daysInWeek(year, week)
	daily := weeklyHours / 5
	norm := float64(working)*daily - float64(shortened)*daily/8
	if norm < 0 {
		return 0, nil
	}
	return norm, nil
}

// NormMinutesInWeek returns the calendar-derived norm in whole minutes.
func NormMinutesInWeek(year, week int, weeklyHours float64) (int, error) {
	norm, err := NormHoursInWeek(year, week, weeklyHours)
	if err != nil {
		return 0, err
	}
	return int(norm*60 + 0.5), nil
}

// WorkingDaysInOverlapChecked counts calendar working days in an inclusive date range.
func WorkingDaysInOverlapChecked(start, end time.Time) (int, error) {
	year := start.Year()
	c, err := calendarFor(year)
	if err != nil {
		return 0, err
	}
	count := 0
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		if c.isWorkingDay(day) {
			count++
		}
	}
	return count, nil
}

// WorkingDaysInOverlap is the compatibility accessor for callers that cannot propagate an error.
func WorkingDaysInOverlap(start, end time.Time) int {
	count, err := WorkingDaysInOverlapChecked(start, end)
	if err != nil {
		return legacyWorkingDaysInOverlap(start, end)
	}
	return count
}

func legacyWorkingDaysInOverlap(start, end time.Time) int {
	count := 0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		wd := d.Weekday()
		if wd != time.Saturday && wd != time.Sunday {
			count++
		}
	}
	return count
}

// ValidateCalendarYears rejects planning horizons that include a year without calendar data.
func ValidateCalendarYears(weeks []string) error {
	seen := map[int]bool{}
	for _, week := range weeks {
		year, number, err := ParseWeek(week)
		if err != nil {
			return err
		}
		if seen[year] {
			continue
		}
		seen[year] = true
		if _, err := calendarFor(year); err != nil {
			return err
		}
		if number < 1 || number > 53 {
			return fmt.Errorf("invalid ISO week %q", week)
		}
	}
	return nil
}

// CalendarSource returns the citation of the loaded calendar, for audit and explainability.
func CalendarSource(year int) string {
	c, err := calendarFor(year)
	if err != nil {
		return ""
	}
	return c.Source
}
