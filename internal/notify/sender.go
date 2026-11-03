package notify

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// Message is a notification to deliver. Chat-style services receive Text (and
// Title where supported); SMTP receives HTML (falling back to Text).
type Message struct{ Title, Text, HTML string }

// Sender delivers a message through a configured service.
type Sender interface {
	Send(ctx context.Context, serviceType string, cfg map[string]string, msg Message) error
}

// ShoutrrrSender implements Sender using shoutrrr. Errors it returns never
// contain secret config values or the built service URL.
type ShoutrrrSender struct{}

var _ Sender = ShoutrrrSender{}

// Send builds the service URL and delivers msg, honouring ctx.
func (ShoutrrrSender) Send(ctx context.Context, serviceType string, cfg map[string]string, msg Message) error {
	svcURL, err := BuildURL(serviceType, cfg)
	if err != nil {
		return redactError(err, serviceType, cfg, "")
	}
	router, err := shoutrrr.CreateSender(svcURL)
	if err != nil {
		return redactError(err, serviceType, cfg, svcURL)
	}
	body := msg.Text
	if serviceType == "smtp" && msg.HTML != "" {
		body = msg.HTML
	}
	params := types.Params{}
	if msg.Title != "" {
		params["title"] = msg.Title
	}
	// This shoutrrr version has no context-aware send, so run it in a
	// goroutine and abandon it (its own timeouts bound it) when ctx ends.
	done := make(chan []error, 1)
	go func() { done <- router.Send(body, &params) }()
	select {
	case <-ctx.Done():
		return fmt.Errorf("send aborted: %w", ctx.Err())
	case errs := <-done:
		for _, e := range errs {
			if e != nil {
				return redactError(e, serviceType, cfg, svcURL)
			}
		}
		return nil
	}
}

// redactError returns a plain error (not wrapping the original, so nothing can
// be unwrapped) with all secret values and the service URL removed.
func redactError(err error, serviceType string, cfg map[string]string, svcURL string) error {
	msg := err.Error()
	for _, s := range secretNeedles(serviceType, cfg, svcURL) {
		msg = strings.ReplaceAll(msg, s, MaskedSecret)
	}
	return errors.New(msg)
}

// secretNeedles lists every string form under which a secret may appear in an
// error message: raw, escaped, and the components of URL-shaped values.
func secretNeedles(serviceType string, cfg map[string]string, svcURL string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if len(s) < 3 || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	addAll := func(s string) {
		add(s)
		add(url.QueryEscape(s))
		add(url.PathEscape(s))
	}
	addURL := func(raw string) {
		addAll(raw)
		if u, err := url.Parse(raw); err == nil {
			if u.User != nil {
				addAll(u.User.Username())
				if p, ok := u.User.Password(); ok {
					addAll(p)
				}
			}
			for _, seg := range strings.Split(u.Path, "/") {
				addAll(seg)
			}
			// Query values may be secrets (token, key, ...) and senders such as
			// shoutrrr's generic service re-encode and re-order the query, so
			// cover every value plus the raw and re-encoded query strings.
			q := u.Query()
			for _, vs := range q {
				for _, v := range vs {
					addAll(v)
				}
			}
			for _, raw := range strings.Split(u.RawQuery, "&") {
				if _, v, ok := strings.Cut(raw, "="); ok {
					addAll(v)
				}
			}
			addAll(u.RawQuery)
			addAll(q.Encode())
			bare := *u
			bare.RawQuery, bare.ForceQuery, bare.Fragment, bare.RawFragment = "", false, "", ""
			addAll(bare.String())
		}
	}
	for _, k := range secretFields[serviceType] {
		if v := cfg[k]; v != "" {
			addAll(v)
			if strings.Contains(v, "://") {
				addURL(v)
			}
		}
	}
	if svcURL != "" {
		addURL(svcURL)
	}
	// Longest first so a full URL is replaced before its parts.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && len(out[j]) > len(out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
