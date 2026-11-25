package scrcpy

import (
	"errors"
	"io"
)

const controlTypeResetVideo = 17

var ErrNoControl = errors.New("scrcpy: control channel not enabled")

func WriteResetVideo(w io.Writer) error {
	_, err := w.Write([]byte{controlTypeResetVideo})
	return err
}

func (s *Server) ResetVideo() error {
	if s.Control == nil {
		return ErrNoControl
	}
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	return WriteResetVideo(s.Control)
}
