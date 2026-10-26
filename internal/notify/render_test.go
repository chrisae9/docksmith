package notify

import (
	"strings"
	"testing"
)

func TestRender_LinksOnlyWithBaseURL(t *testing.T) {
	evs := []Event{{Kind: EventNewVersion, Container: "sonarr", FromVersion: "4.0.10", ToVersion: "4.0.11", Level: LevelPatch}}

	s := Settings{ServerName: "engager", BaseURL: "https://docksmith.example.com/"}
	m := Render(s, evs, false)
	if !strings.Contains(m.HTML, `href="https://docksmith.example.com/container/sonarr"`) {
		t.Fatalf("missing container link in HTML:\n%s", m.HTML)
	}
	if !strings.Contains(m.HTML, `href="https://docksmith.example.com/"`) {
		t.Fatalf("missing review-all link in HTML:\n%s", m.HTML)
	}
	if !strings.Contains(m.Text, "https://docksmith.example.com/container/sonarr") {
		t.Fatalf("missing container URL in text:\n%s", m.Text)
	}

	s.BaseURL = ""
	m = Render(s, evs, false)
	if strings.Contains(m.HTML, "href") || strings.Contains(m.Text, "http") {
		t.Fatalf("links rendered without base URL:\nHTML: %s\nText: %s", m.HTML, m.Text)
	}
}

func TestRender_Titles(t *testing.T) {
	s := Settings{ServerName: "engager"}
	nv := func(c string) Event {
		return Event{Kind: EventNewVersion, Container: c, FromVersion: "1.0.0", ToVersion: "1.1.0"}
	}
	cases := []struct {
		name    string
		evs     []Event
		summary bool
		want    string
	}{
		{"one update", []Event{nv("a")}, false, "[engager] 1 container update available"},
		{"updates", []Event{nv("a"), nv("b"), nv("c")}, false, "[engager] 3 container updates available"},
		{"failed", []Event{{Kind: EventUpdateFailed, Container: "sonarr"}}, false, "[engager] Update failed: sonarr"},
		{"summary", []Event{nv("a"), {Kind: EventUpdateSucceeded, Container: "b"}}, true, "[engager] Docksmith summary: 2 items"},
	}
	for _, c := range cases {
		if got := Render(s, c.evs, c.summary).Title; got != c.want {
			t.Errorf("%s: title %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRender_EscapesAndHistoryLink(t *testing.T) {
	s := Settings{ServerName: "<srv>", BaseURL: "https://d.example.com"}
	m := Render(s, []Event{{Kind: EventUpdateFailed, Container: "a<b>", Message: `<script>alert(1)</script>`}}, false)
	if strings.Contains(m.HTML, "<script>") || strings.Contains(m.HTML, "a<b>") || strings.Contains(m.HTML, "<srv>") {
		t.Fatalf("unescaped content in HTML:\n%s", m.HTML)
	}
	if !strings.Contains(m.HTML, `href="https://d.example.com/history"`) {
		t.Fatalf("missing history link:\n%s", m.HTML)
	}
	if !strings.Contains(m.HTML, "/container/a%3Cb%3E") {
		t.Fatalf("container name not path-escaped:\n%s", m.HTML)
	}
}

func TestRender_RejectsNonHTTPBaseURL(t *testing.T) {
	s := Settings{BaseURL: "javascript:alert(1)"}
	m := Render(s, []Event{{Kind: EventNewVersion, Container: "a", FromVersion: "1", ToVersion: "2"}}, false)
	if strings.Contains(m.HTML, "href") {
		t.Fatalf("non-http base URL produced links:\n%s", m.HTML)
	}
}

func TestRender_RejectsBaseURLWithQueryOrFragment(t *testing.T) {
	for _, b := range []string{"https://d.example.com/?x=1", "https://d.example.com/#frag"} {
		m := Render(Settings{BaseURL: b}, []Event{{Kind: EventNewVersion, Container: "a", FromVersion: "1", ToVersion: "2"}}, false)
		if strings.Contains(m.HTML, "href") {
			t.Fatalf("base %q produced links:\n%s", b, m.HTML)
		}
	}
}

func TestRender_SummaryIncludesLinks(t *testing.T) {
	evs := []Event{
		{Kind: EventNewVersion, Container: "sonarr", FromVersion: "4.0.10", ToVersion: "4.0.11"},
		{Kind: EventNewVersion, Container: "cloudflare-ddns", FromVersion: "2.3.0", ToVersion: "2.3.1"},
	}
	m := Render(Settings{BaseURL: "https://docksmith.example.com"}, evs, true)

	for _, name := range []string{"sonarr", "cloudflare-ddns"} {
		want := "https://docksmith.example.com/container/" + name
		if !strings.Contains(m.HTML, `href="`+want+`"`) {
			t.Fatalf("summary HTML missing link for %s:\n%s", name, m.HTML)
		}
		if !strings.Contains(m.Text, want) {
			t.Fatalf("summary text missing link for %s:\n%s", name, m.Text)
		}
	}
}
