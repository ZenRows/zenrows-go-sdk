package batch_test

import (
	"testing"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

func TestNewAtRejectsTZAwareTimestamp(t *testing.T) {
	// Regression test: aware datetimes/offset strings must be rejected client-side so DST
	// transitions stay deterministic — Timezone is the single authoritative interpreter.
	if _, err := batch.NewAt("2026-09-01T09:00:00Z", "Europe/Berlin"); err == nil {
		t.Fatal("expected an error for a Z-suffixed (tz-aware) timestamp")
	}
	if _, err := batch.NewAt("2026-09-01T09:00:00+02:00", "Europe/Berlin"); err == nil {
		t.Fatal("expected an error for an offset-suffixed timestamp")
	}
}

func TestNewAtAcceptsNaiveTimestamp(t *testing.T) {
	if _, err := batch.NewAt("2026-09-01T09:00:00", "Europe/Berlin"); err != nil {
		t.Fatalf("unexpected error for a naive timestamp: %v", err)
	}
}

func TestNewAtRejectsInvalidTimezone(t *testing.T) {
	if _, err := batch.NewAt("2026-09-01T09:00:00", "Not/AZone"); err == nil {
		t.Fatal("expected an error for an invalid IANA timezone")
	}
}

func TestNewAtRejectsEmptyTimestamp(t *testing.T) {
	if _, err := batch.NewAt("", "Europe/Berlin"); err == nil {
		t.Fatal("expected an error for an empty timestamp")
	}
}

func TestNewRateRejectsNonPositiveEvery(t *testing.T) {
	if _, err := batch.NewRate(0, "minute"); err == nil {
		t.Fatal("expected an error for every=0")
	}
	if _, err := batch.NewRate(-1, "hour"); err == nil {
		t.Fatal("expected an error for a negative every")
	}
}

func TestNewRateRejectsInvalidUnit(t *testing.T) {
	if _, err := batch.NewRate(5, "fortnight"); err == nil {
		t.Fatal("expected an error for an invalid unit")
	}
}

func TestNewRateAcceptsValidInput(t *testing.T) {
	if _, err := batch.NewRate(15, "minute"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewWeeklyRejectsEmptyDays(t *testing.T) {
	if _, err := batch.NewWeekly(nil); err == nil {
		t.Fatal("expected an error for empty days")
	}
}

func TestNewWeeklyRejectsInvalidDayName(t *testing.T) {
	if _, err := batch.NewWeekly([]string{"mon", "funday"}); err == nil {
		t.Fatal("expected an error for an invalid day name")
	}
}

func TestNewWeeklyAcceptsValidDays(t *testing.T) {
	if _, err := batch.NewWeekly([]string{"mon", "wed", "fri"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewMonthlyRejectsOutOfRangeDay(t *testing.T) {
	if _, err := batch.NewMonthly([]int{0}); err == nil {
		t.Fatal("expected an error for day 0")
	}
	if _, err := batch.NewMonthly([]int{32}); err == nil {
		t.Fatal("expected an error for day 32")
	}
}

func TestNewMonthlyAcceptsValidDays(t *testing.T) {
	if _, err := batch.NewMonthly([]int{1, 15}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewCalendarRejectsMinuteGranularity(t *testing.T) {
	// Regression test: only full-hour "HH:00" strings are valid; minute granularity is
	// rejected client-side the same way the server rejects it with 400.
	weekly, _ := batch.NewWeekly([]string{"mon"})
	if _, err := batch.NewCalendar([]string{"09:30"}, weekly, "Europe/Berlin"); err == nil {
		t.Fatal("expected an error for a non-full-hour time")
	}
}

func TestNewCalendarRejectsEmptyTimesOfDay(t *testing.T) {
	weekly, _ := batch.NewWeekly([]string{"mon"})
	if _, err := batch.NewCalendar(nil, weekly, "Europe/Berlin"); err == nil {
		t.Fatal("expected an error for empty times_of_day")
	}
}

func TestNewCalendarRejectsInvalidTimezone(t *testing.T) {
	weekly, _ := batch.NewWeekly([]string{"mon"})
	if _, err := batch.NewCalendar([]string{"09:00"}, weekly, ""); err == nil {
		t.Fatal("expected an error for a missing timezone")
	}
}

func TestNewCalendarAcceptsValidInput(t *testing.T) {
	weekly, err := batch.NewWeekly([]string{"mon", "wed", "fri"})
	if err != nil {
		t.Fatalf("unexpected error building Weekly: %v", err)
	}
	if _, err := batch.NewCalendar([]string{"09:00", "18:00"}, weekly, "Europe/Berlin"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
