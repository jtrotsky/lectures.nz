package model

import "time"

// Lecture represents a single public lecture or event.
type Lecture struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Link        string     `json:"link"`
	TimeStart   time.Time  `json:"time_start"`
	TimeEnd     *time.Time `json:"time_end,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	SummaryHTML string     `json:"summary_html,omitempty"`
	Free        bool       `json:"free"`
	Cost        string     `json:"cost,omitempty"`
	Location    string     `json:"location,omitempty"`
	Image       string     `json:"image,omitempty"`
	Speakers    []Speaker  `json:"speakers,omitempty"`
	HostSlug    string     `json:"host_slug"`
}

// Speaker represents a speaker at a lecture.
type Speaker struct {
	Name string `json:"name"`
	Bio  string `json:"bio,omitempty"`
}

// Host represents an institution that hosts lectures.
type Host struct {
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Website     string    `json:"website"`
	Description string    `json:"description,omitempty"`
	Icon        string    `json:"icon,omitempty"`
	Bluesky     string    `json:"bluesky,omitempty"`
	Lectures    []Lecture `json:"lectures,omitempty"`
}
