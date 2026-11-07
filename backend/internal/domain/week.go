// Package domain – core domain types and pure helpers.
// Week utilities work with ISO 8601 week keys (YYYY-WNN).
// Weeks start on Monday (Russian standard).
// Russian production calendar is a future enhancement; for now 5 working days/week.
package domain

import (
	"fmt"
	"strconv"
	"time"
)

// WeekMonday returns the Monday (00:00 UTC) of the given ISO week.
func WeekMonday(year, week int) time.Time {
	// January 4 is always in ISO week 1.
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	wd := jan4.Weekday()
	if wd == time.Sunday {
		wd = 7
	}
	mondayW1 := jan4.AddDate(0, 0, -int(wd-1))
	return mondayW1.AddDate(0, 0, (week-1)*7)
}

// ParseWeek validates an ISO week key "2026-W01" and returns (year, week, error).
func ParseWeek(s string) (year, week int, err error) {
	if len(s) != 8 {
		return 0, 0, fmt.Errorf("invalid week key %q: must be YYYY-WNN (8 chars)", s)
	}
	year, err = strconv.Atoi(s[:4])
	if err != nil || year < 1 {
		return 0, 0, fmt.Errorf("invalid year in week key %q", s)
	}
	if s[4] != '-' || s[5] != 'W' {
		return 0, 0, fmt.Errorf("invalid week key %q: expected '-' and 'W' at positions 4-5", s)
	}
	week, err = strconv.Atoi(s[6:])
	if err != nil || week < 1 || week > 53 {
		return 0, 0, fmt.Errorf("invalid week number in %q: must be 01..53", s)
	}
	return year, week, nil
}

// FormatWeek builds an ISO week key from year and week number.
func FormatWeek(year, week int) string {
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// WeekRange returns all ISO week keys from start to end inclusive.
// Both start and end must be valid ISO week keys.
func WeekRange(start, end string) ([]string, error) {
	sy, sw, err := ParseWeek(start)
	if err != nil {
		return nil, fmt.Errorf("start week: %w", err)
	}
	ey, ew, err := ParseWeek(end)
	if err != nil {
		return nil, fmt.Errorf("end week: %w", err)
	}

	var weeks []string
	y, w := sy, sw
	for {
		weeks = append(weeks, FormatWeek(y, w))
		if y == ey && w == ew {
			break
		}
		w++
		// ISO year can have 52 or 53 weeks; 53 is rare. We cap at 52 for simplicity
		// unless the target end week is beyond.
		if w > 53 || (w > 52 && (y < ey || (y == ey && w > ew+1))) {
			w = 1
			y++
		}
		if y > ey+1 {
			break // safety
		}
	}
	return weeks, nil
}

// WeekDateRange returns the Monday of startWeek and the Sunday of endWeek.
func WeekDateRange(startWeek, endWeek string) (start, end time.Time, err error) {
	sy, sw, err := ParseWeek(startWeek)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	ey, ew, err := ParseWeek(endWeek)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start = WeekMonday(sy, sw)
	end = WeekMonday(ey, ew).AddDate(0, 0, 6) // Sunday
	return start, end, nil
}

// WorkingDaysInWeek returns the number of working days (Mon–Fri) in a given ISO week.
// Simplified: always 5. Russian production calendar with holidays is a future enhancement.
func WorkingDaysInWeek(_, _ int) int {
	return 5
}

// WeekContainsDate checks whether the given date falls within the ISO week (Mon–Sun).
func WeekContainsDate(year, week int, date time.Time) bool {
	monday := WeekMonday(year, week)
	sunday := monday.AddDate(0, 0, 6)
	// Normalize date to UTC date only.
	d := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	return !d.Before(monday) && !d.After(sunday)
}

// WorkingDaysInOverlap counts Mon–Fri days in [start, end] (inclusive, UTC dates).
func WorkingDaysInOverlap(start, end time.Time) int {
	count := 0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		wd := d.Weekday()
		if wd != time.Saturday && wd != time.Sunday {
			count++
		}
	}
	return count
}
