package session

import (
	"github.com/foxseedlab/mojiokoshin/internal/audio"
	"github.com/foxseedlab/mojiokoshin/internal/config"
	"github.com/foxseedlab/mojiokoshin/internal/discord"
	"github.com/foxseedlab/mojiokoshin/internal/repository"
	"github.com/foxseedlab/mojiokoshin/internal/transcriber"
	"github.com/foxseedlab/mojiokoshin/internal/webhook"
	"github.com/samber/do/v2"
)

func RegisterDI(injector do.Injector) {
	do.Provide(injector, func(i do.Injector) (*Manager, error) {
		cfg := do.MustInvoke[*config.Config](i)
		repo := do.MustInvoke[repository.Repository](i)
		dc := do.MustInvoke[discord.Client](i)
		stt := do.MustInvoke[transcriber.Transcriber](i)
		wh := do.MustInvoke[webhook.Sender](i)
		newMixer := do.MustInvoke[audio.MixerFactory](i)
		return NewManager(cfg, repo, dc, stt, wh, newMixer), nil
	})
}
