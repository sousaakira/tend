package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// What goes at the right end of the tab bar: herdr's `ui.tab_bar_right`
// (`config/tab_bar.rs`). Each entry is one of
//
//	{ type = "zoom" }                       ZOOM while a pane is zoomed
//	{ type = "hostname" }                   the machine the server runs on
//	{ type = "datetime", format = "%H:%M" } the time, as strftime writes it
//	{ type = "text", text = "prod" }        a fixed word
//	{ type = "command", command = "…" }     the last line a command prints
//
// Hostname, datetime and command are worked out by the server, as in herdr:
// the clock and the commands that matter are the ones where the panes are.

// Tab bar limits, herdr's.
const (
	MaxTabBarEntries       = 16
	DefaultCommandInterval = 5
	DefaultCommandTimeout  = 2
	MaxCommandInterval     = 31_536_000 // a year
	MaxCommandTimeout      = 3_600
)

// TabBarEntry is one thing at the right of the tab bar.
type TabBarEntry struct {
	Type    string `toml:"type"`
	Format  string `toml:"format,omitempty"`
	Text    string `toml:"text,omitempty"`
	Command string `toml:"command,omitempty"`
	// IntervalSeconds is how often the command runs and TimeoutSeconds how
	// long one run may take. Zero means herdr's default, which is what an
	// entry that does not mention them gets.
	IntervalSeconds uint64 `toml:"interval_seconds,omitempty"`
	TimeoutSeconds  uint64 `toml:"timeout_seconds,omitempty"`
}

// Interval is how often a command entry runs.
func (e TabBarEntry) Interval() time.Duration {
	if e.IntervalSeconds == 0 {
		return DefaultCommandInterval * time.Second
	}
	return time.Duration(e.IntervalSeconds) * time.Second
}

// Timeout is how long one run of a command entry may take.
func (e TabBarEntry) Timeout() time.Duration {
	if e.TimeoutSeconds == 0 {
		return DefaultCommandTimeout * time.Second
	}
	return time.Duration(e.TimeoutSeconds) * time.Second
}

// DatetimeFormat is the entry's format, or herdr's default.
func (e TabBarEntry) DatetimeFormat() string {
	if e.Format == "" {
		return "%H:%M"
	}
	return e.Format
}

// checkTabBar refuses entries that could not be shown. herdr hides such an
// entry and says so in its diagnostics; tend refuses the file, as it does
// every other value that cannot be used, so the mistake is seen at load and
// not as a gap in the bar.
func checkTabBar(entries []TabBarEntry) error {
	if len(entries) > MaxTabBarEntries {
		return fmt.Errorf("ui.tab_bar_right has %d entries; the most is %d", len(entries), MaxTabBarEntries)
	}
	for i, e := range entries {
		where := fmt.Sprintf("ui.tab_bar_right[%d]", i)
		switch e.Type {
		case "zoom", "hostname":
		case "text":
			if e.Text == "" {
				return fmt.Errorf("%s is text with no text", where)
			}
		case "datetime":
			if err := CheckStrftime(e.DatetimeFormat()); err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
		case "command":
			if strings.TrimSpace(e.Command) == "" {
				return fmt.Errorf("%s: command is empty", where)
			}
			if e.IntervalSeconds > MaxCommandInterval {
				return fmt.Errorf("%s: interval_seconds may be at most %d", where, MaxCommandInterval)
			}
			if e.TimeoutSeconds > MaxCommandTimeout {
				return fmt.Errorf("%s: timeout_seconds may be at most %d", where, MaxCommandTimeout)
			}
		case "":
			return fmt.Errorf("%s has no type; use zoom, hostname, datetime, text or command", where)
		default:
			return fmt.Errorf("%s has type %q; use zoom, hostname, datetime, text or command", where, e.Type)
		}
	}
	return nil
}

// CheckStrftime reports whether a format can be written. The directives are
// the ones herdr's formatter takes for a local time; %z and %Z are refused, as
// herdr refuses them, because a format that works only sometimes is worse.
func CheckStrftime(format string) error {
	if format == "" {
		return errors.New("datetime format is empty")
	}
	_, err := strftime(format, time.Time{})
	return err
}

// Strftime writes t in a format checked by CheckStrftime.
func Strftime(format string, t time.Time) string {
	out, _ := strftime(format, t)
	return out
}

var weekdays = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

func strftime(format string, t time.Time) (string, error) {
	var b strings.Builder
	pad := func(v, width int, fill byte) {
		s := strconv.Itoa(v)
		for len(s) < width {
			s = string(fill) + s
		}
		b.WriteString(s)
	}
	hour12 := func() int {
		h := t.Hour() % 12
		if h == 0 {
			return 12
		}
		return h
	}
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' {
			b.WriteByte(c)
			continue
		}
		i++
		if i == len(format) {
			return "", errors.New("invalid datetime format: it ends in a lone %")
		}
		switch format[i] {
		case 'a':
			b.WriteString(weekdays[t.Weekday()][:3])
		case 'A':
			b.WriteString(weekdays[t.Weekday()])
		case 'b', 'h':
			b.WriteString(t.Month().String()[:3])
		case 'B':
			b.WriteString(t.Month().String())
		case 'c':
			b.WriteString(Strftime("%a %b %e %H:%M:%S %Y", t))
		case 'C':
			pad(t.Year()/100, 2, '0')
		case 'd':
			pad(t.Day(), 2, '0')
		case 'D':
			b.WriteString(Strftime("%m/%d/%y", t))
		case 'e':
			pad(t.Day(), 2, ' ')
		case 'F':
			b.WriteString(Strftime("%Y-%m-%d", t))
		case 'g':
			y, _ := t.ISOWeek()
			pad(y%100, 2, '0')
		case 'G':
			y, _ := t.ISOWeek()
			b.WriteString(strconv.Itoa(y))
		case 'H':
			pad(t.Hour(), 2, '0')
		case 'I':
			pad(hour12(), 2, '0')
		case 'j':
			pad(t.YearDay(), 3, '0')
		case 'k':
			pad(t.Hour(), 2, ' ')
		case 'l':
			pad(hour12(), 2, ' ')
		case 'm':
			pad(int(t.Month()), 2, '0')
		case 'M':
			pad(t.Minute(), 2, '0')
		case 'n':
			b.WriteByte('\n')
		case 'p':
			if t.Hour() < 12 {
				b.WriteString("AM")
			} else {
				b.WriteString("PM")
			}
		case 'P':
			if t.Hour() < 12 {
				b.WriteString("am")
			} else {
				b.WriteString("pm")
			}
		case 'r':
			b.WriteString(Strftime("%I:%M:%S %p", t))
		case 'R':
			b.WriteString(Strftime("%H:%M", t))
		case 'S':
			pad(t.Second(), 2, '0')
		case 't':
			b.WriteByte('\t')
		case 'T':
			b.WriteString(Strftime("%H:%M:%S", t))
		case 'u':
			pad((int(t.Weekday())+6)%7+1, 1, '0')
		case 'U':
			pad((t.YearDay()-1+7-int(t.Weekday()))/7, 2, '0')
		case 'V':
			_, w := t.ISOWeek()
			pad(w, 2, '0')
		case 'w':
			pad(int(t.Weekday()), 1, '0')
		case 'W':
			pad((t.YearDay()-1+7-(int(t.Weekday())+6)%7)/7, 2, '0')
		case 'y':
			pad(t.Year()%100, 2, '0')
		case 'Y':
			b.WriteString(strconv.Itoa(t.Year()))
		case '%':
			b.WriteByte('%')
		case 'z', 'Z':
			return "", fmt.Errorf("unsupported datetime format: %%%c needs a time zone offset herdr does not format either", format[i])
		default:
			return "", fmt.Errorf("invalid datetime format: %%%c is not a directive", format[i])
		}
	}
	return b.String(), nil
}
