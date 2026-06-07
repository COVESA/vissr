//go:build cyclone

package ddsMgr

import (
	dds "github.com/SoundMatt/go-DDS"
	"github.com/SoundMatt/go-DDS/cyclone"
)

func init() {
	newParticipant = func() (dds.Participant, error) {
		return cyclone.New(ddsDomain)
	}
}
