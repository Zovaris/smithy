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
	discordRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{20,}$`)
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

// Kinds is the set of kinds connector name sends: connect.<name>.kinds, else a notify.kinds the
// user set, else DefaultKinds.
func Kinds(name string) map[string]bool {
	v := config.Get("connect."+name+".kinds", "")
	if strings.TrimSpace(v) == "" {
		v = strings.Join(DefaultKinds(), ",")
		if nv, src, err := config.Effective("notify.kinds"); err == nil && src != "default" {
			v = nv
		}
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
		cs = append(cs, &Telegram{Token: tok, Chat: chat, Users: idSet("connect.telegram.users"),
			OffsetFile: filepath.Join(ghx.Home(), "telegram.offset")})
	}
	sc, serr := slackFromConfig()
	if sc != nil {
		cs = append(cs, sc)
	}
	dc, derr := discordFromConfig()
	if dc != nil {
		cs = append(cs, dc)
	}
	return cs, errors.Join(err, serr, derr)
}

func discordFromConfig() (Connector, error) {
	tok, _, err := auth.Get("discord")
	ch := strings.TrimSpace(config.Get("connect.discord.channel", ""))
	switch {
	case err != nil:
		return nil, err
	case tok == "" && ch == "":
		return nil, nil
	case tok == "":
		return nil, errors.New("discord: no bot token (smithy auth set discord)")
	case !discordRe.MatchString(strings.TrimPrefix(tok, "Bot ")):
		return nil, errors.New("discord: the stored token is not a bot token")
	case !snowflakeRe.MatchString(ch):
		return nil, errors.New("discord: connect.discord.channel must be the numeric id of a server text channel")
	}
	return &Discord{Token: strings.TrimPrefix(tok, "Bot "), Channel: ch, Users: idSet("connect.discord.users")}, nil
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

// DefaultKinds are notify.Kinds plus the kinds only the daemon sends; the weekly report is
// opt-in (add report to connect.<name>.kinds).
func DefaultKinds() []string {
	return append(append([]string{}, notify.Kinds...), "pr_merged", "ci_red")
}
