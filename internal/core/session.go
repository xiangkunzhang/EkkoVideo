package core

import "time"

// SessionPhase is the self-healing playback state machine phase.
type SessionPhase int

const (
	PhaseIdle SessionPhase = iota
	PhaseResolving
	PhaseLoading
	PhasePlaying
	PhaseBuffering
	PhaseRecovering
	PhaseEnded
	PhaseFailed
)

func (p SessionPhase) String() string {
	switch p {
	case PhaseIdle:
		return "idle"
	case PhaseResolving:
		return "resolving"
	case PhaseLoading:
		return "loading"
	case PhasePlaying:
		return "playing"
	case PhaseBuffering:
		return "buffering"
	case PhaseRecovering:
		return "recovering"
	case PhaseEnded:
		return "ended"
	case PhaseFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// PlaybackSession coordinates queue progression, active line fallback, and error recovery.
type PlaybackSession struct {
	Generation uint64
	Phase      SessionPhase
	Queue      *Queue
	Source     *PlaySource
	LineIndex  int
	Retries    int
	MaxRetries int
	LastError  string
	StartedAt  time.Time
}

func NewPlaybackSession() *PlaybackSession {
	return &PlaybackSession{MaxRetries: 2}
}

func (s *PlaybackSession) BeginResolve(q *Queue) uint64 {
	s.Generation++
	s.Queue = q
	s.Phase = PhaseResolving
	s.Source = nil
	s.LineIndex = 0
	s.Retries = 0
	s.LastError = ""
	s.StartedAt = time.Now()
	return s.Generation
}

func (s *PlaybackSession) SetResolved(gen uint64, src *PlaySource) bool {
	if gen != s.Generation || src == nil {
		return false
	}
	s.Source = src
	s.Phase = PhaseLoading
	s.StartedAt = time.Now()
	return true
}

func (s *PlaybackSession) SetPlaying() {
	s.Phase = PhasePlaying
	s.Retries = 0
	s.LastError = ""
}

func (s *PlaybackSession) SetBuffering(buffering bool) {
	if buffering {
		if s.Phase == PhasePlaying || s.Phase == PhaseLoading {
			s.Phase = PhaseBuffering
		}
	} else if s.Phase == PhaseBuffering {
		s.Phase = PhasePlaying
	}
}

// HandleFailure advances the recovery state machine, returning true and the next
// URL/line to try if recovery is possible, or false if all options are exhausted.
func (s *PlaybackSession) HandleFailure(reason string) (retry bool, lineIdx int) {
	s.LastError = reason
	if s.Retries < s.MaxRetries {
		s.Retries++
		s.Phase = PhaseRecovering
		return true, s.LineIndex
	}
	if s.Source != nil && s.LineIndex+1 < len(s.Source.Lines) {
		s.LineIndex++
		s.Retries = 0
		s.Phase = PhaseRecovering
		return true, s.LineIndex
	}
	s.Phase = PhaseFailed
	return false, s.LineIndex
}
