package api

import (
	"net/url"
	"time"
)

// Info is daemon.json: who runs and where.
type Info struct {
	PID      int       `json:"pid"`
	PIDStart string    `json:"pid_start,omitempty"`
	Addr     string    `json:"addr"`
	Started  time.Time `json:"started"`
	Log      string    `json:"log,omitempty"`
}

// URL is the daemon's base URL.
func (i Info) URL() string { return "http://" + i.Addr }

// WebURL is the web shell's URL with the token in the fragment, which browsers never send.
func (i Info) WebURL(token string) string { return i.URL() + "/#token=" + url.QueryEscape(token) }
