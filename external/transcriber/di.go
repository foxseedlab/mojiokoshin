package transcriber

import (
	"github.com/foxseedlab/mojiokoshin/internal/config"
	"github.com/foxseedlab/mojiokoshin/internal/transcriber"
	"github.com/samber/do/v2"
)

func RegisterDI(injector do.Injector) {
	do.Provide(injector, func(i do.Injector) (transcriber.Transcriber, error) {
		c := do.MustInvoke[*config.Config](i)
		return NewCloudSpeechTranscriber(CloudSpeechConfig{
			ProjectID:       c.GoogleCloudProjectID,
			CredentialsJSON: c.GoogleCloudCredentialsJSON,
			Language:        c.DefaultTranscribeLanguage,
			Location:        c.GoogleCloudSpeechLocation,
			Model:           c.GoogleCloudSpeechModel,
		}), nil
	})
}
