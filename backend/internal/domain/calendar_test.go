package domain

import (
	"errors"
	"testing"
	"time"
)

func TestCalendarWorkingDays(t *testing.T) {
	cases := map[string]int{
		"2026-W02": 0, // New Year holidays, 5..11 January
		"2026-W03": 5, // 12..18 January
		"2026-W09": 4, // 23 February
		"2026-W11": 4, // 9 March transferred
		"2026-W19": 5, // 4..8 May, 9..10 weekend
		"2026-W20": 4, // 11 May transferred
		"2026-W24": 4, // 12 June
		"2026-W45": 4, // 4 November
	}
	for week, want := range cases {
		year, number, err := ParseWeek(week)
		if err != nil {
			t.Fatal(err)
		}
		got, err := WorkingDaysInWeekChecked(year, number)
		if err != nil {
			t.Fatalf("%s: %v", week, err)
		}
		if got != want {
			t.Errorf("%s working days = %d, want %d", week, got, want)
		}
	}
}

func TestCalendarNormHoursAndShortenedDays(t *testing.T) {
	norm, err := NormHoursInWeek(2026, 18, 40) // 27..30 April + 1..3 May, 30 April shortened
	if err != nil {
		t.Fatal(err)
	}
	if want := 4*8 - 1; norm != float64(want) {
		t.Fatalf("norm hours = %v, want %d", norm, want)
	}
	minutes, err := NormMinutesInWeek(2026, 18, 40)
	if err != nil {
		t.Fatal(err)
	}
	if minutes != 31*60 {
		t.Fatalf("norm minutes = %d, want %d", minutes, 31*60)
	}
}

func TestCalendarOverlapExcludesHolidays(t *testing.T) {
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 16, 0, 0, 0, 0, time.UTC)
	days, err := WorkingDaysInOverlapChecked(start, end)
	if err != nil {
		t.Fatal(err)
	}
	if days != 5 {
		t.Fatalf("working days = %d, want 5 (12..16 January)", days)
	}
}

func TestMissingCalendarIsHardFailure(t *testing.T) {
	if _, err := WorkingDaysInWeekChecked(2030, 10); !errors.Is(err, ErrCalendarMissing) {
		t.Fatalf("error = %v, want ErrCalendarMissing", err)
	}
	if CalendarAvailable(2030) {
		t.Fatal("2030 must not be available without a calendar file")
	}
	if err := ValidateCalendarYears([]string{"2030-W10"}); !errors.Is(err, ErrCalendarMissing) {
		t.Fatalf("ValidateCalendarYears = %v, want ErrCalendarMissing", err)
	}
	if err := ValidateCalendarYears([]string{"2026-W10", "2026-W11"}); err != nil {
		t.Fatalf("valid horizon rejected: %v", err)
	}
}
