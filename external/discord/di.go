package discord

import (
	"github.com/foxseedlab/mojiokoshin/internal/config"
	discordpkg "github.com/foxseedlab/mojiokoshin/internal/discord"
	"github.com/samber/do/v2"
)

func RegisterDI(injector do.Injector) {
	do.Provide(injector, func(i do.Injector) (discordpkg.Client, error) {
		c := do.MustInvoke[*config.Config](i)
		return NewClient(c.DiscordToken, c.DiscordGuildID, c.DiscordVCID), nil
	})
}
