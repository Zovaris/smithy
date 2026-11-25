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
	discordRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{20,}$`)
)

func users(key string) map[string]bool {
	on := map[string]bool{}
	for _, u := range strings.Split(config.Get(key, ""), ",") {
		if u = strings.TrimSpace(u); u != "" {
			on[u] = true
		}
	}
	return on
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
		cs = append(cs, &Telegram{Token: tok, Chat: chat, Users: users("connect.telegram.users")})
	}
	dc, derr := discordFromConfig()
	if dc != nil {
		cs = append(cs, dc)
	}
	return cs, errors.Join(err, derr)
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
	return &Discord{Token: strings.TrimPrefix(tok, "Bot "), Channel: ch, Users: users("connect.discord.users")}, nil
}

// File is the hub's state file.
func File() string { return filepath.Join(ghx.Home(), "connect.json") }

// DefaultKinds are notify.Kinds plus the kinds only the daemon sends.
func DefaultKinds() []string {
	return append(append([]string{}, notify.Kinds...), "pr_merged", "ci_red")
}
