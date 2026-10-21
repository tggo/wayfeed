package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// API is the public feedql API and nothing else: https://api.feedql.com/api/v1 with a Bearer token of the person.
type API struct {
	Base string // https://api.feedql.com
	HTTP *http.Client
}

// APIError is feedql's standard error body.
type APIError struct {
	Status  int
	Code    string
	Message string
	Field   string
	TripID  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("feedql %d %s: %s", e.Status, e.Code, e.Message)
}

func (a *API) client() *http.Client {
	if a.HTTP != nil {
		return a.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// Do calls the REST API as the person; body and out are JSON (nil for none).
func (a *API) Do(ctx context.Context, token, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.Base, "/")+"/api/v1"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "wayfeed/1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Field   string `json:"field"`
				TripID  string `json:"trip_id"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return &APIError{Status: res.StatusCode, Code: e.Error.Code, Message: e.Error.Message, Field: e.Error.Field, TripID: e.Error.TripID}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Token is feedql's answer to the token endpoint.
type Token struct {
	Access    string `json:"access_token"`
	Refresh   string `json:"refresh_token"`
	ExpiresIn int    `json:"expires_in"`
}

// ErrSignedOut means the tokens are no good any more: the person has to sign in again.
var ErrSignedOut = errors.New("signed out")

// Exchange posts to the OAuth token endpoint as a public client (no secret).
func (a *API) Exchange(ctx context.Context, form url.Values) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.Base, "/")+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "wayfeed/1")
	res, err := a.client().Do(req)
	if err != nil {
		return Token{}, err
	}
	defer res.Body.Close()
	var t Token
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusUnauthorized {
		return Token{}, ErrSignedOut
	}
	if res.StatusCode >= 300 || json.Unmarshal(raw, &t) != nil || t.Access == "" {
		return Token{}, fmt.Errorf("token endpoint answered %d", res.StatusCode)
	}
	return t, nil
}

// Stop, Trip and the rest mirror the public trip routes (backend contracts, Story 39.6).
type Stop struct {
	Name     string  `json:"name"`
	Country  string  `json:"country,omitempty"`
	Admin    string  `json:"admin,omitempty"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	RadiusKm int     `json:"radius_km,omitempty"`
	From     string  `json:"from"`
	To       string  `json:"to"`
}

type TripPart struct {
	State       string `json:"state"`
	Stops       []Stop `json:"stops"`
	LeadDays    int    `json:"lead_days"`
	ArchiveDays int    `json:"archive_days"`
	DeleteDays  int    `json:"delete_days"`
	AlertSev    string `json:"alert_sev"`
	CollectFrom string `json:"collect_from"`
	EndsOn      string `json:"ends_on"`
	DeleteOn    string `json:"delete_on"`
	Note        string `json:"note"`
}

// TripView is the channel view the API gives for a trip.
type TripView struct {
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Paused bool      `json:"paused"`
	Trip   *TripPart `json:"trip"`
}

type Preview struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Stops       int    `json:"stops"`
	CollectFrom string `json:"collect_from"`
	EndsOn      string `json:"ends_on"`
	DeleteOn    string `json:"delete_on"`
	Collected   string `json:"what_is_collected"`
	Refresh     string `json:"refresh"`
	WhoSees     string `json:"who_sees_it"`
	WhenDeleted string `json:"when_it_is_deleted"`
	Undo        string `json:"can_it_be_undone"`
}

type Item struct {
	ID            string    `json:"id"`
	URL           string    `json:"url"`
	Title         string    `json:"title"`
	TitleOriginal string    `json:"title_original"`
	LangOriginal  string    `json:"lang_original"`
	Source        string    `json:"source"`
	Published     time.Time `json:"published"`
	Translated    *struct {
		From string `json:"from"`
		Low  bool   `json:"low"`
	} `json:"translated"`
	Bucket   string `json:"bucket"`
	Severity int    `json:"severity"`
	Official bool   `json:"official"`
	When     *When  `json:"when"`
	// Summary is feedql's one or two sentence summary in the reader's language, when it is written.
	Summary string `json:"summary"`
}

// When is the day or days of the event a story reports (not when it was published).
type When struct {
	Start  string `json:"start"`
	End    string `json:"end"`
	Status string `json:"status"`
}

// Group is the stories of a stop, or of one day of it, in the four buckets.
type Group struct {
	Safety     []Item `json:"safety"`
	Roads      []Item `json:"roads"`
	Events     []Item `json:"events"`
	Background []Item `json:"background"`
}

// Empty says nothing is in any bucket.
func (g Group) Empty() bool {
	return len(g.Safety)+len(g.Roads)+len(g.Events)+len(g.Background) == 0
}

// DayGroup is one day of a stop.
type DayGroup struct {
	Date string `json:"date"`
	Group
}

type StopBrief struct {
	Index   int        `json:"index"`
	Stop    Stop       `json:"stop"`
	Query   string     `json:"query"`
	Items   []Item     `json:"items"`
	Current Group      `json:"current"`
	Days    []DayGroup `json:"days"`
	Scope   string     `json:"scope"`
	Widened string     `json:"widened"`
	Note    string     `json:"note"`
}

type Briefing struct {
	TripID     string      `json:"trip_id"`
	State      string      `json:"state"`
	Stops      []StopBrief `json:"stops"`
	Partial    bool        `json:"partial"`
	Disclaimer string      `json:"disclaimer"`
}

type Alert struct {
	ID        string    `json:"id"`
	StopIndex int       `json:"stop_index"`
	Bucket    string    `json:"bucket"`
	Severity  int       `json:"severity"`
	Title     string    `json:"title"`
	TitleOrig string    `json:"title_orig"`
	Place     string    `json:"place"`
	URL       string    `json:"source_url"`
	Official  bool      `json:"official"`
	SentAt    time.Time `json:"sent_at"`
}

type Alerts struct {
	Alerts   []Alert `json:"alerts"`
	Overflow []struct {
		Line string `json:"line"`
	} `json:"overflow"`
	Disclaimer string `json:"disclaimer"`
}

// PublicGet reads a public route of the API (no account): the demo trips.
func (a *API) PublicGet(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.Base, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "wayfeed/1")
	res, err := a.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode >= 300 {
		return &APIError{Status: res.StatusCode, Message: "feedql answered " + res.Status}
	}
	return json.Unmarshal(raw, out)
}

// PublicGetURL reads an absolute public address that must be on the API's own host (a feed address the list gave).
func (a *API) PublicGetURL(ctx context.Context, raw string, out any) error {
	u, err := url.Parse(raw)
	b, err2 := url.Parse(a.Base)
	if err != nil || err2 != nil || u.Host != b.Host || u.Scheme != "https" && u.Scheme != b.Scheme {
		return errors.New("feed address is not on the API host")
	}
	return a.PublicGet(ctx, u.RequestURI(), out)
}

// DemoListItem is a demo trip in the list. FeedURL is the public feed of its channel (feedql's generic fqp_ feed), which the demo pages read.
type DemoListItem struct {
	Slug    string   `json:"slug"`
	Name    string   `json:"name"`
	Stops   []string `json:"stops"`
	FeedURL string   `json:"feed_url"`
}

// DemoStop is a stop of a demo trip with the stories near it.
type DemoStop struct {
	Stop
	Items   []Item     `json:"items"`
	Current Group      `json:"current"`
	Days    []DayGroup `json:"days"`
	Scope   string     `json:"scope"`
	Widened string     `json:"widened"`
	Pending int        `json:"pending_translation"`
	Note    string     `json:"note"`
}

// DemoAlert is an alert of a demo trip.
type DemoAlert struct {
	StopIndex int       `json:"stop_index"`
	Bucket    string    `json:"bucket"`
	Severity  int       `json:"severity"`
	Title     string    `json:"title"`
	Place     string    `json:"place"`
	URL       string    `json:"source_url"`
	Official  bool      `json:"official"`
	SentAt    time.Time `json:"sent_at"`
}

// DemoTrip is a demo trip as the public API gives it.
type DemoTrip struct {
	Slug        string      `json:"slug"`
	Name        string      `json:"name"`
	Stops       []DemoStop  `json:"stops"`
	Alerts      []DemoAlert `json:"alerts"`
	Disclaimer  string      `json:"disclaimer"`
	GeneratedAt time.Time   `json:"generated_at"`
}
