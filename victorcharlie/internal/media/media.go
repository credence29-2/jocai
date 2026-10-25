// Package media runs the LiveKit SFU inside the Victor Charlie process. Voice, webcams
// and screen shares all flow through it; Victor Charlie only hands out tokens and
// enforces roles through its API.
package media

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/livekit/livekit-server/pkg/config"
	"github.com/livekit/livekit-server/pkg/routing"
	"github.com/livekit/livekit-server/pkg/service"
	"github.com/livekit/livekit-server/pkg/telemetry/prometheus"
	"github.com/livekit/mediatransportutil/pkg/rtcconfig"
	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/logger"
	"github.com/livekit/protocol/webhook"
	"github.com/twitchtv/twirp"
)

// SignalPort is LiveKit's websocket port. It only listens on loopback; Victor Charlie
// proxies /rtc to it so browsers reach it through the same HTTPS port as the
// rest of the app.
const SignalPort = 7880

type Options struct {
	Key, Secret string
	TCPPort     int
	UDPPort     int
	PublicIP    string
	LANOnly     bool
	// WebhookURL receives participant and track events.
	WebhookURL string
}

type Server struct {
	lk    *service.LivekitServer
	Rooms livekit.RoomService
	keys  auth.KeyProvider

	// LooksUpIP is true when the public IP comes from STUN rather than the
	// config. PublicIP is what was found, empty if the lookup failed.
	LooksUpIP bool
	PublicIP  string
}

func Start(o Options) (*Server, error) {
	external := !o.LANOnly && o.PublicIP == ""
	conf, err := newConfig(o, external)
	if err != nil && external {
		// No internet or STUN blocked. Run for the local network rather than
		// not at all.
		log.Printf("Couldn't look up this network's public IP (%v). Voice will only work on your local network until Victor Charlie restarts.", err)
		conf, err = newConfig(o, false)
	}
	if err != nil {
		return nil, fmt.Errorf("media config: %w", err)
	}
	config.InitLoggerFromConfig(&conf.Logging)
	if err := conf.ValidateKeys(); err != nil {
		return nil, err
	}
	node, err := routing.NewLocalNode(conf)
	if err != nil {
		return nil, err
	}
	if err := prometheus.Init(string(node.NodeID()), node.NodeType()); err != nil {
		return nil, err
	}
	lk, err := service.InitializeServer(conf, node)
	if err != nil {
		return nil, err
	}
	errc := make(chan error, 1)
	go func() { errc <- lk.Start() }()
	select {
	case err := <-errc:
		return nil, fmt.Errorf("media server: %w", err)
	case <-time.After(1500 * time.Millisecond):
	}
	logger.Infow("media server running", "nodeIP", conf.RTC.NodeIP)
	base := fmt.Sprintf("http://127.0.0.1:%d", SignalPort)
	s := &Server{
		lk:        lk,
		Rooms:     livekit.NewRoomServiceProtobufClient(base, http.DefaultClient),
		keys:      auth.NewSimpleKeyProvider(o.Key, o.Secret),
		LooksUpIP: external,
	}
	if external && conf.RTC.UseExternalIP {
		s.PublicIP = conf.RTC.NodeIP.V4
	}
	return s, nil
}

func newConfig(o Options, external bool) (*config.Config, error) {
	nodeIP := ""
	if o.PublicIP != "" {
		nodeIP = "  node_ip: " + o.PublicIP
	}
	yaml := fmt.Sprintf(`
port: %d
bind_addresses: ["127.0.0.1"]
keys:
  %s: %s
logging:
  level: warn
rtc:
  tcp_port: %d
  udp_port: %d
  use_external_ip: %t
  skip_external_ip_validation: true
  advertise_internal_ip: true
%s
room:
  empty_timeout: 30
  departure_timeout: 10
webhook:
  api_key: %s
  urls: [%q]
`, SignalPort, o.Key, o.Secret, o.TCPPort, o.UDPPort, external, nodeIP, o.Key, o.WebhookURL)
	return config.NewConfig(yaml, true, nil, nil)
}

func (s *Server) Stop() { s.lk.Stop(false) }

// LookupPublicIP asks a STUN server what this network's public IPv4 is.
func LookupPublicIP(ctx context.Context) (string, error) {
	return rtcconfig.GetExternalIPv4(ctx, rtcconfig.DefaultStunServers, nil)
}

// ReceiveWebhook checks the signature on a LiveKit webhook and decodes it.
func (s *Server) ReceiveWebhook(r *http.Request) (*livekit.WebhookEvent, error) {
	return webhook.ReceiveWebhookEvent(r, s.keys)
}

// AdminContext returns a context carrying a token that may administer one
// room. LiveKit checks the room name on admin calls.
func AdminContext(ctx context.Context, key, secret, room string) (context.Context, error) {
	at := auth.NewAccessToken(key, secret).SetVideoGrant(&auth.VideoGrant{RoomAdmin: true, Room: room}).SetValidFor(time.Minute)
	tok, err := at.ToJWT()
	if err != nil {
		return nil, err
	}
	h := http.Header{}
	h.Set("Authorization", "Bearer "+tok)
	return twirpHeaders(ctx, h)
}

func twirpHeaders(ctx context.Context, h http.Header) (context.Context, error) {
	return twirp.WithHTTPRequestHeaders(ctx, h)
}

// Token mints a join token for one person in one voice channel. It is only
// good for a few minutes; LiveKit refreshes it for clients already in, so
// an old copy can't be replayed after a mute or ban.
func Token(key, secret, identity, name, room string, canPublish, canSubscribe bool, sources []string) (string, error) {
	t, f := true, false
	g := &auth.VideoGrant{RoomJoin: true, Room: room, CanSubscribe: &t, CanPublishData: &t, CanUpdateOwnMetadata: &f}
	g.CanPublish = &canPublish
	g.CanSubscribe = &canSubscribe
	g.CanPublishSources = sources
	return auth.NewAccessToken(key, secret).SetIdentity(identity).SetName(name).SetVideoGrant(g).SetValidFor(5 * time.Minute).ToJWT()
}
