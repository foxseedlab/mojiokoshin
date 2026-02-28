package webhook

import (
	"github.com/foxseedlab/mojiokoshin/internal/config"
	"github.com/foxseedlab/mojiokoshin/internal/webhook"
	"github.com/samber/do/v2"
)

func RegisterDI(injector do.Injector) {
	do.Provide(injector, func(i do.Injector) (webhook.Sender, error) {
		c := do.MustInvoke[*config.Config](i)
		return NewHTTPSender(c.TranscriptWebhookURL), nil
	})
}
