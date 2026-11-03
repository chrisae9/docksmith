package notify

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"strings"
)

// kindTitles is the per-kind title used for a single event and as the section
// heading in multi-event messages.
var kindTitles = map[EventKind]string{
	EventNewVersion:      "Update available",
	EventDigestChanged:   "Image digest changed",
	EventUpdateSucceeded: "Updated",
	EventUpdateFailed:    "Update failed",
	EventRollback:        "Rolled back",
	EventUpdateBlocked:   "Update blocked",
	EventCheckFailing:    "Update check failing",
	EventComposeMismatch: "Compose file mismatch",
}

var kindHeadings = map[EventKind]string{
	EventNewVersion:      "Updates available",
	EventDigestChanged:   "Image digests changed",
	EventUpdateSucceeded: "Updated",
	EventUpdateFailed:    "Updates failed",
	EventRollback:        "Rolled back",
	EventUpdateBlocked:   "Updates blocked",
	EventCheckFailing:    "Update checks failing",
	EventComposeMismatch: "Compose file mismatches",
}

type renderLink struct{ Label, URL string }

type renderRow struct {
	Container, Change, Level, Message, URL string
}

type renderSection struct {
	Heading string
	Rows    []renderRow
}

type renderData struct {
	Title    string
	Sections []renderSection
	Buttons  []renderLink
}

// html/template escapes all dynamic text and sanitises the href values.
var htmlTmpl = template.Must(template.New("msg").Parse(`<!DOCTYPE html>
<html><body style="font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#1f2328;">
<h2 style="font-size:18px;margin:0 0 12px;">{{.Title}}</h2>
{{range .Sections}}<h3 style="font-size:15px;margin:16px 0 6px;">{{.Heading}}</h3>
<table cellpadding="6" cellspacing="0" style="border-collapse:collapse;font-size:14px;">
<tr style="text-align:left;border-bottom:1px solid #d0d7de;"><th>Container</th><th>Change</th><th>Level</th><th></th></tr>
{{range .Rows}}<tr style="border-bottom:1px solid #eaeef2;"><td>{{.Container}}</td><td>{{.Change}}{{if .Message}}<div style="color:#59636e;">{{.Message}}</div>{{end}}</td><td>{{.Level}}</td><td>{{if .URL}}<a href="{{.URL}}">Open in Docksmith</a>{{end}}</td></tr>
{{end}}</table>
{{end}}{{if .Buttons}}<p style="margin-top:20px;">{{range .Buttons}}<a href="{{.URL}}" style="display:inline-block;padding:8px 14px;margin-right:8px;background:#0969da;color:#ffffff;text-decoration:none;border-radius:6px;">{{.Label}}</a>{{end}}</p>
{{end}}</body></html>
`))

// Render builds the message for events. summary selects the scheduled-summary
// title. Links appear only when s.BaseURL is a valid http(s) URL.
func Render(s Settings, events []Event, summary bool) Message {
	title := renderTitle(events, summary)
	if s.ServerName != "" {
		title = "[" + s.ServerName + "] " + title
	}
	base := linkBase(s.BaseURL)

	data := renderData{Title: title}
	byKind := map[EventKind][]Event{}
	var order []EventKind
	for _, e := range events {
		if _, seen := byKind[e.Kind]; !seen {
			order = append(order, e.Kind)
		}
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	ordered := make([]EventKind, 0, len(order))
	for _, k := range AllEventKinds {
		if _, ok := byKind[k]; ok {
			ordered = append(ordered, k)
		}
	}
	for _, k := range order {
		if _, known := kindHeadings[k]; !known {
			ordered = append(ordered, k)
		}
	}
	for _, k := range ordered {
		heading := kindHeadings[k]
		if heading == "" {
			heading = string(k)
		}
		sec := renderSection{Heading: fmt.Sprintf("%s (%d)", heading, len(byKind[k]))}
		for _, e := range byKind[k] {
			row := renderRow{Container: e.Container, Change: change(e), Message: e.Message}
			if e.Level > 0 {
				row.Level = e.Level.String()
			}
			if base != "" && e.Container != "" {
				row.URL = base + "/container/" + url.PathEscape(e.Container)
			}
			sec.Rows = append(sec.Rows, row)
		}
		data.Sections = append(data.Sections, sec)
	}
	if base != "" {
		if _, ok := byKind[EventNewVersion]; ok {
			data.Buttons = append(data.Buttons, renderLink{"Review all updates", base + "/"})
		}
		_, failed := byKind[EventUpdateFailed]
		_, rolled := byKind[EventRollback]
		if failed || rolled {
			data.Buttons = append(data.Buttons, renderLink{"View history", base + "/history"})
		}
	}

	var html bytes.Buffer
	if err := htmlTmpl.Execute(&html, data); err != nil {
		html.Reset() // cannot happen with these types; fall back to text only
	}
	return Message{Title: title, Text: renderText(data), HTML: html.String()}
}

func renderTitle(events []Event, summary bool) string {
	if summary {
		return fmt.Sprintf("Docksmith summary: %d %s", len(events), plural(len(events), "item", "items"))
	}
	allNew := len(events) > 0
	for _, e := range events {
		if e.Kind != EventNewVersion {
			allNew = false
			break
		}
	}
	switch {
	case allNew:
		return fmt.Sprintf("%d container %s available", len(events), plural(len(events), "update", "updates"))
	case len(events) == 1:
		t := kindTitles[events[0].Kind]
		if t == "" {
			t = string(events[0].Kind)
		}
		if events[0].Container == "" {
			return t
		}
		return t + ": " + events[0].Container
	}
	return fmt.Sprintf("%d Docksmith notifications", len(events))
}

func renderText(d renderData) string {
	var b strings.Builder
	for i, sec := range d.Sections {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(sec.Heading + "\n")
		for _, r := range sec.Rows {
			line := "- " + r.Container
			if r.Change != "" {
				line += ": " + r.Change
			}
			if r.Level != "" {
				line += " (" + r.Level + ")"
			}
			if r.Message != "" {
				line += " - " + r.Message
			}
			b.WriteString(line + "\n")
			if r.URL != "" {
				b.WriteString("  " + r.URL + "\n")
			}
		}
	}
	if len(d.Buttons) > 0 {
		b.WriteString("\n")
		for _, l := range d.Buttons {
			b.WriteString(l.Label + ": " + l.URL + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func change(e Event) string {
	switch {
	case e.FromVersion != "" && e.ToVersion != "":
		return e.FromVersion + " → " + e.ToVersion
	case e.ToVersion != "":
		return e.ToVersion
	}
	return e.FromVersion
}

// linkBase returns BaseURL without a trailing slash, or "" when it is unset or
// not an absolute http(s) URL without query or fragment (links are appended
// to it as path segments).
func linkBase(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return ""
	}
	return strings.TrimRight(raw, "/")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
