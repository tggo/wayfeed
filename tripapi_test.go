package main

import (
	"strings"
	"testing"
)

func TestAStopIsReadBackFromItsTopicFilter(t *testing.T) {
	s := stopOf("Kandy", "near:7.29,80.63 25km during:2026-11-05..2026-11-08 bucket:safety,roads -source:reddit")
	if s.Name != "Kandy" || s.Lat != 7.29 || s.Lon != 80.63 || s.RadiusKm != 25 || s.From != "2026-11-05" || s.To != "2026-11-08" {
		t.Fatalf("%+v", s)
	}
	if u := stopOf("Moab", "near:38.57,-109.55 60km bucket:events"); u.From != "" || u.Lon != -109.55 || u.RadiusKm != 60 {
		t.Fatalf("a stop with no dates: %+v", u)
	}
}

func TestTheZoneOfAStopAndItsDays(t *testing.T) {
	for _, c := range []struct {
		s    Stop
		want string
	}{{Stop{Country: "LK"}, "Asia/Colombo"}, {Stop{Country: "GE"}, "Asia/Tbilisi"}, {Stop{Country: "US", Lon: -111.65}, "America/Denver"}, {Stop{Country: "US", Lon: -122.4}, "America/Los_Angeles"}, {Stop{}, "UTC"}} {
		if got := tripZone(c.s); got != c.want {
			t.Errorf("%+v: %s, want %s", c.s, got, c.want)
		}
	}
	if d := tripDays(Stop{From: "2026-11-05", To: "2026-11-08"}, "2026-10-22"); len(d) != 4 || d[0] != "2026-11-05" {
		t.Fatalf("%v", d)
	}
	if d := tripDays(Stop{}, "2026-10-22"); len(d) != 5 || d[0] != "2026-10-22" || d[4] != "2026-10-26" {
		t.Fatalf("a stop with no dates is five days from today: %v", d)
	}
	if d := tripDays(Stop{From: "2026-01-01", To: "2026-12-31"}, "2026-10-22"); len(d) != 14 {
		t.Fatalf("at most 14 days: %d", len(d))
	}
}

func TestArchiveAndDeleteUseTheChannelRoutes(t *testing.T) {
	f := newFake(t)
	app, h := newTestApp(t, f)
	c := signedIn(t, app, live())
	if rec := do(h, "POST", "/trip/archive", "", c); rec.Code != 303 {
		t.Fatalf("%d", rec.Code)
	}
	if last := f.bodies[len(f.bodies)-1]; last != `patch:{"paused":true}` {
		t.Fatalf("archive pauses the channel: %s", last)
	}
	if rec := do(h, "POST", "/trip/delete", "", c); rec.Code != 303 || f.deleted != 1 {
		t.Fatalf("%d deleted %d", rec.Code, f.deleted)
	}
	// the briefing of a stop is read from its own topic, grouped by day and bucket, with summaries
	do(h, "GET", "/trip", "", c)
	joined := strings.Join(f.queries, " ")
	for _, want := range []string{"topic%3At_kandy", "group=day%2Cbucket", "summary=1", "collapse=event", "tz=Asia%2FColombo"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the stop read lacks %q: %s", want, joined)
		}
	}
}
