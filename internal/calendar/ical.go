// Package calendar generates iCalendar (.ics) files from lecture data.
package calendar

import (
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jtrotsky/lectures.nz/internal/model"
)

const (
	prodID  = "-//lectures.nz//lectures.nz//EN"
	calName = "lectures.nz — New Zealand Public Lectures"
)

// Write writes a VCALENDAR block for the given lectures to w.
func Write(w io.Writer, lectures []model.Lecture) error {
	fmt.Fprintf(w, "BEGIN:VCALENDAR\r\n")
	fmt.Fprintf(w, "VERSION:2.0\r\n")
	fmt.Fprintf(w, "PRODID:%s\r\n", prodID)
	fmt.Fprintf(w, "CALSCALE:GREGORIAN\r\n")
	fmt.Fprintf(w, "METHOD:PUBLISH\r\n")
	fmt.Fprintf(w, "X-WR-CALNAME:%s\r\n", calName)
	fmt.Fprintf(w, "X-WR-TIMEZONE:Pacific/Auckland\r\n")

	for _, l := range lectures {
		if err := writeEvent(w, l); err != nil {
			return err
		}
	}

	fmt.Fprintf(w, "END:VCALENDAR\r\n")
	return nil
}

func writeEvent(w io.Writer, l model.Lecture) error {
	fmt.Fprintf(w, "BEGIN:VEVENT\r\n")
	fmt.Fprintf(w, "UID:%s@lectures.nz\r\n", l.ID)
	fmt.Fprintf(w, "DTSTAMP:%s\r\n", formatTime(time.Now().UTC()))
	fmt.Fprintf(w, "DTSTART:%s\r\n", formatTime(l.TimeStart))
	if l.TimeEnd != nil {
		fmt.Fprintf(w, "DTEND:%s\r\n", formatTime(*l.TimeEnd))
	} else {
		end := l.TimeStart.Add(1 * time.Hour)
		fmt.Fprintf(w, "DTEND:%s\r\n", formatTime(end))
	}
	writeFolded(w, "SUMMARY", l.Title)
	if l.Summary != "" {
		writeFolded(w, "DESCRIPTION", l.Summary)
	}
	if l.Location != "" {
		writeFolded(w, "LOCATION", l.Location)
	}
	if l.Link != "" {
		fmt.Fprintf(w, "URL:%s\r\n", l.Link)
	}
	fmt.Fprintf(w, "END:VEVENT\r\n")
	return nil
}

// formatTime formats a time.Time as iCalendar UTC timestamp (YYYYMMDDTHHMMSSZ).
func formatTime(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// writeFolded writes an iCalendar property with line folding at 75 octets.
func writeFolded(w io.Writer, name, value string) {
	// Escape special characters per RFC 5545.
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, ";", `\;`)
	value = strings.ReplaceAll(value, ",", `\,`)
	value = strings.ReplaceAll(value, "\n", `\n`)

	line := name + ":" + value
	const maxLen = 75

	if utf8.RuneCountInString(line) <= maxLen {
		fmt.Fprintf(w, "%s\r\n", line)
		return
	}

	runes := []rune(line)
	fmt.Fprintf(w, "%s\r\n", string(runes[:maxLen]))
	runes = runes[maxLen:]
	for len(runes) > 0 {
		take := maxLen - 1 // -1 for the leading space
		if take > len(runes) {
			take = len(runes)
		}
		fmt.Fprintf(w, " %s\r\n", string(runes[:take]))
		runes = runes[take:]
	}
}
