//go:build !cyclone

package ddsMgr

import (
	dds "github.com/SoundMatt/go-DDS"
	"github.com/SoundMatt/go-DDS/mock"
)

func init() {
	newParticipant = func() (dds.Participant, error) {
		return mock.New(ddsDomain)
	}
}
