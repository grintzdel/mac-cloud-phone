package webrtc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"cloudphone/internal/scrcpy"
)

const (
	rtpMTU            = 1200
	keyFrameCooldown  = time.Second
	iceGatherTimeout  = 5 * time.Second
	opusFrameDuration = 20 * time.Millisecond
)

type Streamer struct {
	api    *webrtc.API
	video  *webrtc.TrackLocalStaticRTP
	audio  *webrtc.TrackLocalStaticSample
	logger *slog.Logger

	requestKeyFrame func()
	lastKeyRequest  time.Time
	keyMu           sync.Mutex

	pcMu sync.Mutex
	pc   *webrtc.PeerConnection

	assembler  frameAssembler
	packetizer *videoPacketizer
}

func NewStreamer(requestKeyFrame func(), logger *slog.Logger) (*Streamer, error) {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, registry); err != nil {
		return nil, err
	}
	se := webrtc.SettingEngine{}
	se.SetIPFilter(allowedIP)
	se.SetIncludeLoopbackCandidate(true)
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeUDP6})

	video, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{
		MimeType:    webrtc.MimeTypeH264,
		ClockRate:   videoClockRate,
		SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
	}, "video", "cloudphone")
	if err != nil {
		return nil, err
	}
	audio, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
	}, "audio", "cloudphone")
	if err != nil {
		return nil, err
	}

	return &Streamer{
		api:             webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(registry), webrtc.WithSettingEngine(se)),
		video:           video,
		audio:           audio,
		logger:          logger,
		requestKeyFrame: requestKeyFrame,
		packetizer:      newVideoPacketizer(rtpMTU),
	}, nil
}

func (s *Streamer) Answer(ctx context.Context, offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	pc, err := s.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	fail := func(err error) (webrtc.SessionDescription, error) {
		_ = pc.Close()
		return webrtc.SessionDescription{}, err
	}

	for _, track := range []webrtc.TrackLocal{s.video, s.audio} {
		sender, err := pc.AddTrack(track)
		if err != nil {
			return fail(err)
		}
		go s.readRTCP(sender)
	}
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.logger.Info("peer connection state", "state", state.String())
		if state == webrtc.PeerConnectionStateConnected {
			s.forceKeyFrame()
		}
		if state == webrtc.PeerConnectionStateFailed {
			_ = pc.Close()
		}
	})

	if err := pc.SetRemoteDescription(offer); err != nil {
		return fail(fmt.Errorf("set offer: %w", err))
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return fail(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		return fail(err)
	}
	select {
	case <-gathered:
	case <-time.After(iceGatherTimeout):
		return fail(errors.New("ICE gathering timed out"))
	case <-ctx.Done():
		return fail(ctx.Err())
	}

	s.pcMu.Lock()
	previous := s.pc
	s.pc = pc
	s.pcMu.Unlock()
	if previous != nil {
		s.logger.Info("replacing previous client")
		_ = previous.Close()
	}
	return *pc.LocalDescription(), nil
}

func (s *Streamer) readRTCP(sender *webrtc.RTPSender) {
	for {
		packets, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, p := range packets {
			switch p.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				s.forceKeyFrame()
			}
		}
	}
}

func (s *Streamer) forceKeyFrame() {
	s.keyMu.Lock()
	if time.Since(s.lastKeyRequest) < keyFrameCooldown {
		s.keyMu.Unlock()
		return
	}
	s.lastKeyRequest = time.Now()
	s.keyMu.Unlock()
	s.logger.Debug("requesting key frame")
	s.requestKeyFrame()
}

func (s *Streamer) WriteVideo(f *scrcpy.Frame) error {
	data, ok := s.assembler.push(f)
	if !ok {
		return nil
	}
	for _, pkt := range s.packetizer.packetize(data, f.PTS) {
		if err := s.video.WriteRTP(pkt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Streamer) WriteAudio(opus []byte) error {
	return s.audio.WriteSample(media.Sample{Data: opus, Duration: opusFrameDuration})
}

func (s *Streamer) Close() error {
	s.pcMu.Lock()
	defer s.pcMu.Unlock()
	if s.pc == nil {
		return nil
	}
	return s.pc.Close()
}
