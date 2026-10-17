// Package profiles — встроенные профили приложений. Роутер не видит само
// приложение, только его трафик, поэтому профиль — это набор доменов и
// IP-диапазонов сервиса.
package profiles

type Profile struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"` // суффиксы доменов
	IPs     []string `json:"ips,omitempty"`
}

var builtin = []Profile{
	{ID: "youtube", Name: "YouTube", Domains: []string{
		"youtube.com", "youtu.be", "ytimg.com", "googlevideo.com", "youtube-nocookie.com",
		"youtubei.googleapis.com", "yt.be", "ggpht.com", "youtubekids.com",
	}},
	{ID: "telegram", Name: "Telegram", Domains: []string{
		"telegram.org", "telegram.me", "t.me", "telegra.ph", "telesco.pe", "tdesktop.com", "cdn-telegram.org",
	}, IPs: []string{
		"91.108.4.0/22", "91.108.8.0/22", "91.108.12.0/22", "91.108.16.0/22", "91.108.20.0/22",
		"91.108.56.0/22", "95.161.64.0/20", "149.154.160.0/20", "185.76.151.0/24",
		"2001:67c:4e8::/48", "2001:b28:f23c::/47", "2001:b28:f23f::/48", "2a0a:f280::/32",
	}},
	{ID: "discord", Name: "Discord", Domains: []string{
		"discord.com", "discord.gg", "discordapp.com", "discordapp.net", "discord.media",
		"discordcdn.com", "discord.dev", "discord.new", "discordstatus.com", "dis.gd",
	}},
	{ID: "instagram", Name: "Instagram", Domains: []string{
		"instagram.com", "cdninstagram.com", "ig.me", "instagr.am", "fbcdn.net", "facebook.com",
		"facebook.net", "fb.com",
	}},
	// FaceTime: вызов устанавливается через службы Apple (push, идентификация),
	// а голос и видео идут по UDP через серверы Apple в сети 17.0.0.0/8.
	{ID: "facetime", Name: "FaceTime", Domains: []string{
		"facetime.apple.com", "push.apple.com", "ess.apple.com", "identity.apple.com",
		"gs.apple.com", "apple-relay.apple.com", "apple-relay.cloudflare.com",
	}},
	{ID: "whatsapp", Name: "WhatsApp", Domains: []string{
		"whatsapp.com", "whatsapp.net", "wa.me", "whatsapp.biz",
	}},
	{ID: "chatgpt", Name: "ChatGPT / OpenAI", Domains: []string{
		"openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com", "chat.com", "sora.com",
	}},
	{ID: "claude", Name: "Claude", Domains: []string{
		"claude.ai", "anthropic.com", "claude.com", "claudeusercontent.com",
	}},
	{ID: "gemini", Name: "Gemini", Domains: []string{
		"gemini.google.com", "aistudio.google.com", "generativelanguage.googleapis.com", "bard.google.com",
	}},
	{ID: "netflix", Name: "Netflix", Domains: []string{
		"netflix.com", "netflix.net", "nflxext.com", "nflximg.com", "nflximg.net", "nflxso.net", "nflxvideo.net",
	}},
	{ID: "spotify", Name: "Spotify", Domains: []string{
		"spotify.com", "scdn.co", "spotifycdn.com", "spotify.design", "spoti.fi", "audio-ak-spotify-com.akamaized.net",
	}},
	{ID: "twitch", Name: "Twitch", Domains: []string{
		"twitch.tv", "ttvnw.net", "jtvnw.net", "twitchcdn.net", "twitchsvc.net", "ext-twitch.tv",
	}},
	{ID: "tiktok", Name: "TikTok", Domains: []string{
		"tiktok.com", "tiktokv.com", "tiktokcdn.com", "tiktokcdn-us.com", "byteoversea.com", "ibytedtos.com", "musical.ly",
	}},
	{ID: "x", Name: "X (Twitter)", Domains: []string{
		"x.com", "twitter.com", "twimg.com", "t.co",
	}},
	{ID: "linkedin", Name: "LinkedIn", Domains: []string{
		"linkedin.com", "licdn.com", "lnkd.in",
	}},
	{ID: "steam", Name: "Steam", Domains: []string{
		"steampowered.com", "steamcommunity.com", "steamstatic.com", "steamcontent.com", "steamserver.net", "steam-chat.com",
	}},
	{ID: "github", Name: "GitHub", Domains: []string{
		"github.com", "githubusercontent.com", "githubassets.com", "github.io", "ghcr.io",
	}},
}

func All() []Profile { return builtin }

func Get(id string) (Profile, bool) {
	for _, p := range builtin {
		if p.ID == id {
			return p, true
		}
	}
	return Profile{}, false
}
