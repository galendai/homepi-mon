package nodeapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/galendai/homepi-mon/internal/protocol"
)

// maxClientMessage bounds an inbound stream message. Clients only send hello,
// ack and device_health, all of which are tiny.
const maxClientMessage = 8 << 10

// handleStream upgrades to WebSocket and pushes snapshots to one device.
//
// The server owns the cadence: it sends the current snapshot when it changes
// and a heartbeat when it does not, so a slow display can never make the daemon
// buffer without bound (MOD-001 9.2).
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	device, ok := s.authorize(w, r)
	if !ok {
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The display is a native client on the LAN, not a browser page, so
		// there is no origin to check and no cross-origin risk to accept.
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.log.Warn("stream upgrade failed", "device", device, "error", err.Error())
		return
	}
	conn.SetReadLimit(maxClientMessage)
	defer conn.CloseNow()

	ctx := r.Context()
	hello, err := s.readHello(ctx, conn)
	if err != nil {
		s.log.Warn("stream hello rejected", "device", device, "error", err.Error())
		_ = conn.Close(websocket.StatusPolicyViolation, "invalid hello")
		return
	}
	if hello.SchemaMajor != 0 && hello.SchemaMajor != protocol.SchemaMajor {
		s.sendError(ctx, conn, "schema_unsupported",
			"client schema major version is not supported by this daemon")
		_ = conn.Close(websocket.StatusPolicyViolation, "schema mismatch")
		return
	}

	// Drain client acks and health reports so the read side stays alive and a
	// closed connection is noticed promptly.
	go s.drain(ctx, conn)

	s.pump(ctx, conn, device, token, hello)
}

func (s *Server) readHello(ctx context.Context, conn *websocket.Conn) (protocol.Hello, error) {
	readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, raw, err := conn.Read(readCtx)
	if err != nil {
		return protocol.Hello{}, err
	}
	env, err := protocol.DecodeEnvelope(raw)
	if err != nil {
		return protocol.Hello{}, err
	}
	if env.Type != protocol.MsgHello {
		return protocol.Hello{}, errors.New("first message must be hello")
	}
	var hello protocol.Hello
	if err := protocol.DecodePayload(env, &hello); err != nil {
		return protocol.Hello{}, err
	}
	return hello, nil
}

// pump sends the current snapshot whenever its version advances, and a
// heartbeat when it does not.
func (s *Server) pump(ctx context.Context, conn *websocket.Conn, device, token string,
	hello protocol.Hello) {
	ticker := time.NewTicker(s.poll)
	defer ticker.Stop()

	lastSent := uint64(0)
	// If the client is resuming inside the same epoch, do not resend what it
	// already has; a different epoch always forces a full snapshot.
	if hello.LastEpoch == s.store.Epoch() {
		lastSent = hello.LastSnapshotVersion
	}
	sentAny := false
	lastBeat := s.now()

	for {
		revoked, err := s.tokenRevoked(token)
		if err != nil || revoked {
			if err != nil {
				s.log.Error("stream revocation check failed", "device", device,
					"error", err.Error())
			}
			_ = conn.Close(websocket.StatusPolicyViolation, "device token revoked")
			return
		}
		snap := s.store.Snapshot()
		if s.store.HasData() && (!sentAny || snap.SnapshotVersion > lastSent) {
			if err := s.send(ctx, conn, protocol.MsgSnapshotFull, snap); err != nil {
				s.log.Debug("stream closed", "device", device, "error", err.Error())
				return
			}
			lastSent = snap.SnapshotVersion
			sentAny = true
			lastBeat = s.now()
		} else if s.now().Sub(lastBeat) >= s.heartbeat {
			hb := protocol.Heartbeat{Interval: protocol.Duration(s.heartbeat)}
			if err := s.send(ctx, conn, protocol.MsgHeartbeat, hb); err != nil {
				s.log.Debug("stream closed", "device", device, "error", err.Error())
				return
			}
			lastBeat = s.now()
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) drain(ctx context.Context, conn *websocket.Conn) {
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
}

func (s *Server) send(ctx context.Context, conn *websocket.Conn, t protocol.MessageType, payload any) error {
	raw, err := protocol.Encode(t, s.now().UTC(), payload)
	if err != nil {
		return err
	}
	// A write deadline stops one stuck display from pinning a goroutine.
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, raw)
}

func (s *Server) sendError(ctx context.Context, conn *websocket.Conn, code, message string) {
	_ = s.send(ctx, conn, protocol.MsgError, protocol.StreamError{Code: code, Message: message})
}
