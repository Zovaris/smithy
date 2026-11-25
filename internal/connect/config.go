package connect

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Zovaris/smithy/internal/auth"
	"github.com/Zovaris/smithy/internal/config"
	"github.com/Zovaris/smithy/internal/ghx"
	"github.com/Zovaris/smithy/internal/notify"
)

var (
	botTokenRe = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`)
	chatIDRe   = regexp.MustCompile(`^-?[0-9]+$`)
	slackBotRe = regexp.MustCompile(`^xoxb-[A-Za-z0-9-]+$`)
	slackAppRe = regexp.MustCompile(`^xapp-[A-Za-z0-9-]+$`)
	slackChRe  = regexp.MustCompile(`^[CG][A-Z0-9]{6,}$`)
)

func idSet(key string) map[string]bool {
	users := map[string]bool{}
	for _, u := range strings.Split(config.Get(key, ""), ",") {
		if u = strings.TrimSpace(u); u != "" {
			users[u] = true
		}
	}
	return users
}

// Kinds is the set of kinds connector name sends: connect.<name>.kinds, else notify.kinds.
func Kinds(name string) map[string]bool {
	v := config.Get("connect."+name+".kinds", "")
	if strings.TrimSpace(v) == "" {
		v = config.Get("notify.kinds", strings.Join(DefaultKinds(), ","))
	}
	on := map[string]bool{}
	for _, k := range strings.Split(v, ",") {
		if k = strings.TrimSpace(k); k != "" {
			on[k] = true
		}
	}
	return on
}

// FromConfig builds every configured connector; the error says why one is off.
// Credentials come from smithy auth only, never from config.
func FromConfig() ([]Connector, error) {
	var cs []Connector
	tok, _, err := auth.Get("telegram")
	chat := strings.TrimSpace(config.Get("notify.telegram.chat", ""))
	switch {
	case err != nil:
	case tok == "" && chat == "":
	case tok == "":
		err = errors.New("telegram: no bot token (smithy auth set telegram)")
	case !botTokenRe.MatchString(tok):
		err = errors.New("telegram: the stored token is not a bot token")
	case !chatIDRe.MatchString(chat):
		err = errors.New("telegram: notify.telegram.chat must be the numeric id of a supergroup with topics")
	default:
		cs = append(cs, &Telegram{Token: tok, Chat: chat, Users: idSet("connect.telegram.users")})
	}
	sc, serr := slackFromConfig()
	if sc != nil {
		cs = append(cs, sc)
	}
	return cs, errors.Join(err, serr)
}

func slackFromConfig() (*Slack, error) {
	ch := strings.TrimSpace(config.Get("connect.slack.channel", ""))
	if ch == "" {
		return nil, nil
	}
	bot, _, err := auth.Get("slack")
	if err != nil {
		return nil, err
	}
	app, _, err := auth.Get("slack_app")
	if err != nil {
		return nil, err
	}
	switch {
	case !slackChRe.MatchString(ch):
		return nil, errors.New("slack: connect.slack.channel must be a channel id (C… or G…), not a name")
	case bot == "":
		return nil, errors.New("slack: no bot token (smithy auth set slack)")
	case !slackBotRe.MatchString(bot):
		return nil, errors.New("slack: the stored slack token is not a bot token (xoxb-)")
	case app == "":
		return nil, errors.New("slack: no app-level token for Socket Mode (smithy auth set slack_app)")
	case !slackAppRe.MatchString(app):
		return nil, errors.New("slack: the stored slack_app token is not an app-level token (xapp-)")
	}
	return &Slack{Token: bot, AppToken: app, Channel: ch, Users: idSet("connect.slack.users")}, nil
}

// File is the hub's state file.
func File() string { return filepath.Join(ghx.Home(), "connect.json") }

// DefaultKinds are notify.Kinds plus the kinds only the daemon sends.
func DefaultKinds() []string {
	return append(append([]string{}, notify.Kinds...), "pr_merged", "ci_red", "report")
}
