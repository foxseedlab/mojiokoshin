package audio

import (
	"github.com/foxseedlab/mojiokoshin/internal/audio"
	"github.com/samber/do/v2"
)

func RegisterDI(injector do.Injector) {
	do.Provide(injector, func(i do.Injector) (audio.Mixer, error) {
		return NewOpusMixer(), nil
	})
	do.ProvideValue(injector, audio.MixerFactory(func() audio.Mixer {
		return NewOpusMixer()
	}))
}
