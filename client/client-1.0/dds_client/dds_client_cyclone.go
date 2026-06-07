//go:build cyclone

package main

import (
	dds "github.com/SoundMatt/go-DDS"
	"github.com/SoundMatt/go-DDS/cyclone"
)

func init() {
	newParticipant = func() (dds.Participant, error) {
		return cyclone.New(clientDomain)
	}
}
