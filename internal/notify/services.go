package notify

import (
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
)

// MaskedSecret is the placeholder returned to clients in place of secret values.
const MaskedSecret = "********"

// ServiceTypes lists the supported notification service types.
var ServiceTypes = []string{"smtp", "ntfy", "discord", "telegram", "pushover", "gotify", "slack", "webhook", "advanced"}

var secretFields = map[string][]string{
	"smtp":     {"password"},
	"ntfy":     {"token", "password"},
	"discord":  {"webhook_url"},
	"telegram": {"token"},
	"pushover": {"token", "user_key"},
	"gotify":   {"token"},
	"slack":    {"webhook_url"},
	"webhook":  {"url"},
	"advanced": {"url"},
}

var requiredFields = map[string][]string{
	"smtp":     {"host", "from", "to"},
	"ntfy":     {"topic"},
	"discord":  {"webhook_url"},
	"telegram": {"token", "chats"},
	"pushover": {"token", "user_key"},
	"gotify":   {"host", "token"},
	"slack":    {"webhook_url"},
	"webhook":  {"url"},
	"advanced": {"url"},
}

var (
	discordIDRe    = regexp.MustCompile(`^[0-9]+$`)
	discordTokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// SecretFields returns the config keys holding secrets for a service type.
func SecretFields(serviceType string) []string {
	return append([]string(nil), secretFields[serviceType]...)
}

// ValidateServiceConfig checks the type is known and required fields are set.
func ValidateServiceConfig(serviceType string, cfg map[string]string) error {
	req, ok := requiredFields[serviceType]
	if !ok {
		return fmt.Errorf("unknown service type %q", serviceType)
	}
	var missing []string
	for _, k := range req {
		if strings.TrimSpace(cfg[k]) == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s: missing required fields: %s", serviceType, strings.Join(missing, ", "))
	}
	if serviceType == "smtp" {
		switch cfg["encryption"] {
		case "", "none", "starttls", "tls":
		default:
			return fmt.Errorf("smtp: invalid encryption %q (want none, starttls or tls)", cfg["encryption"])
		}
		if _, err := mail.ParseAddress(cfg["from"]); err != nil {
			return fmt.Errorf("smtp: invalid from address %q (use addr@example.com or Name <addr@example.com>)", cfg["from"])
		}
		if _, err := mail.ParseAddressList(cfg["to"]); err != nil {
			return fmt.Errorf("smtp: invalid to address list %q (comma-separated addresses, names optional)", cfg["to"])
		}
	}
	return nil
}

// MaskConfig returns a copy of cfg with non-empty secret values replaced by MaskedSecret.
func MaskConfig(serviceType string, cfg map[string]string) map[string]string {
	out := make(map[string]string, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	for _, k := range secretFields[serviceType] {
		if out[k] != "" {
			out[k] = MaskedSecret
		}
	}
	return out
}

// MergeSecrets returns incoming with any secret still equal to MaskedSecret
// replaced by the stored value.
func MergeSecrets(serviceType string, incoming, stored map[string]string) map[string]string {
	out := make(map[string]string, len(incoming))
	for k, v := range incoming {
		out[k] = v
	}
	for _, k := range secretFields[serviceType] {
		if out[k] == MaskedSecret {
			if sv, ok := stored[k]; ok {
				out[k] = sv
			} else {
				delete(out, k)
			}
		}
	}
	return out
}

// BuildURL maps a service config to a shoutrrr service URL.
func BuildURL(serviceType string, cfg map[string]string) (string, error) {
	if err := ValidateServiceConfig(serviceType, cfg); err != nil {
		return "", err
	}
	switch serviceType {
	case "smtp":
		return buildSMTP(cfg)
	case "ntfy":
		host := cfg["host"]
		if host == "" {
			host = "ntfy.sh"
		}
		u := url.URL{Scheme: "ntfy", Host: host, Path: "/" + strings.TrimPrefix(cfg["topic"], "/")}
		switch {
		case cfg["username"] != "" || cfg["password"] != "":
			u.User = url.UserPassword(cfg["username"], cfg["password"])
		case cfg["token"] != "":
			// shoutrrr has no token option; ntfy accepts an access token as
			// the Basic-auth password with an empty username.
			u.User = url.UserPassword("", cfg["token"])
		}
		return u.String(), nil
	case "discord":
		id, token, err := webhookParts(cfg["webhook_url"], "webhooks")
		if err != nil {
			return "", fmt.Errorf("discord: %w", err)
		}
		if !discordIDRe.MatchString(id) || !discordTokenRe.MatchString(token) {
			return "", fmt.Errorf("discord: webhook_url has an invalid id or token")
		}
		return (&url.URL{Scheme: "discord", User: url.User(token), Host: id}).String(), nil
	case "telegram":
		u := url.URL{Scheme: "telegram", Host: "telegram"}
		bot, tok, found := strings.Cut(cfg["token"], ":")
		if !found {
			return "", fmt.Errorf("telegram: token must look like <botID>:<token>")
		}
		u.User = url.UserPassword(bot, tok)
		u.Path = "/"
		u.RawQuery = url.Values{"chats": {cfg["chats"]}}.Encode()
		return u.String(), nil
	case "pushover":
		// shoutrrr reads the user key from the host and the API token from the password.
		u := url.URL{Scheme: "pushover", User: url.UserPassword("shoutrrr", cfg["token"]), Host: cfg["user_key"]}
		return u.String(), nil
	case "gotify":
		u := url.URL{Scheme: "gotify", Host: cfg["host"], Path: "/" + cfg["token"]}
		return u.String(), nil
	case "slack":
		_, rest, ok := strings.Cut(cfg["webhook_url"], "hooks.slack.com/services/")
		if !ok {
			return "", fmt.Errorf("slack: webhook_url must start with https://hooks.slack.com/services/")
		}
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if len(parts) != 3 {
			return "", fmt.Errorf("slack: webhook_url must look like https://hooks.slack.com/services/T.../B.../...")
		}
		u := url.URL{Scheme: "slack", User: url.UserPassword("hook", strings.Join(parts, "-")), Host: "webhook"}
		return u.String(), nil
	case "webhook":
		raw := cfg["url"]
		pu, err := url.Parse(raw)
		if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
			return "", fmt.Errorf("webhook: url must be an http(s) URL")
		}
		return "generic+" + raw, nil
	case "advanced":
		raw := cfg["url"]
		if !strings.Contains(raw, "://") {
			return "", fmt.Errorf("advanced: url must be a shoutrrr service URL (scheme://...)")
		}
		return raw, nil
	}
	return "", fmt.Errorf("unknown service type %q", serviceType)
}

func buildSMTP(cfg map[string]string) (string, error) {
	port := cfg["port"]
	if port == "" {
		port = "587"
	}
	u := url.URL{Scheme: "smtp", Host: cfg["host"] + ":" + port}
	q := url.Values{}
	if cfg["username"] != "" {
		u.User = url.UserPassword(cfg["username"], cfg["password"])
		q.Set("auth", "Plain")
	} else {
		q.Set("auth", "None")
	}
	switch cfg["encryption"] {
	case "none":
		q.Set("encryption", "None")
		q.Set("usestarttls", "no")
	case "tls":
		q.Set("encryption", "ImplicitTLS")
		q.Set("usestarttls", "no")
	default: // starttls
		q.Set("encryption", "ExplicitTLS")
		q.Set("usestarttls", "yes")
	}
	// Shoutrrr uses fromaddress/toaddresses verbatim for MAIL FROM/RCPT TO, so
	// "Name <addr>" is split into the bare address and (for the sender) fromname.
	from, err := mail.ParseAddress(cfg["from"])
	if err != nil {
		return "", fmt.Errorf("smtp: invalid from address %q: %w", cfg["from"], err)
	}
	to, err := mail.ParseAddressList(cfg["to"])
	if err != nil {
		return "", fmt.Errorf("smtp: invalid to address list %q: %w", cfg["to"], err)
	}
	recipients := make([]string, len(to))
	for i, a := range to {
		recipients[i] = a.Address
	}
	q.Set("fromaddress", from.Address)
	if from.Name != "" {
		q.Set("fromname", from.Name)
	}
	q.Set("toaddresses", strings.Join(recipients, ","))
	q.Set("usehtml", "yes")
	// Shoutrrr does not decode "+" as a space, so spaces must be %20.
	u.RawQuery = strings.ReplaceAll(q.Encode(), "+", "%20")
	return u.String(), nil
}

// webhookParts extracts ID and token from .../<marker>/<id>/<token>.
func webhookParts(raw, marker string) (id, token string, err error) {
	u, perr := url.Parse(raw)
	if perr != nil {
		return "", "", fmt.Errorf("invalid webhook_url")
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, s := range segs {
		if s == marker && i+2 < len(segs) {
			return segs[i+1], segs[i+2], nil
		}
	}
	return "", "", fmt.Errorf("webhook_url must look like https://host/api/webhooks/<id>/<token>")
}
