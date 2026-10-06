package feeds

import (
	"fmt"
	"testing"
	"time"

	"github.com/Desvelao/dsipy/internal/testutil"
)

func isoFormat(t time.Time) string {
	s := t.Format("2006-01-02T15:04:05")
	if us := t.Nanosecond() / 1000; us != 0 {
		s += fmt.Sprintf(".%06d", us)
	}
	return s
}

func TestParseISODateGolden(t *testing.T) {
	var cases []struct {
		In    string
		Out   string
		Error bool
	}
	testutil.GoldenJSON(t, "feeds/iso_dates.json", &cases)
	if len(cases) < 1500 {
		t.Fatalf("few cases: %d", len(cases))
	}
	bad := 0
	for _, c := range cases {
		got, err := ParseISODate(c.In)
		if c.Error {
			if err == nil {
				t.Errorf("%q: expected error, got %s", c.In, isoFormat(got))
				bad++
			}
		} else if err != nil || isoFormat(got) != c.Out {
			t.Errorf("%q: got %v %v, want %s", c.In, got, err, c.Out)
			bad++
		}
		if bad > 25 {
			t.Fatal("too many mismatches")
		}
	}
}
