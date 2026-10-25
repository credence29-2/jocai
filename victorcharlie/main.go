// Victor Charlie is a self-hosted chat server: text channels, DMs, voice, webcams and
// screen sharing for a group of friends, running on one machine.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/credence29-2/jocai/victorcharlie/internal/api"
	"github.com/credence29-2/jocai/victorcharlie/internal/config"
	"github.com/credence29-2/jocai/victorcharlie/internal/media"
	"github.com/credence29-2/jocai/victorcharlie/internal/store"
	"github.com/credence29-2/jocai/victorcharlie/internal/webui"
	"golang.org/x/crypto/acme/autocert"
)

var version = "dev"

// errLogged marks a startup error that has already been written to the log.
var errLogged = errors.New("already logged")

// quietTLS drops the "TLS handshake error" lines that bots probing random
// names like access.yourname.duckdns.org cause all day long.
type quietTLS struct{ w io.Writer }

func (q quietTLS) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte("TLS handshake error")) {
		return len(p), nil
	}
	return q.w.Write(p)
}

func main() {
	dataDir := flag.String("data", defaultDataDir(), "folder for the database, uploads and victorcharlie.json")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	log.SetFlags(log.Ldate | log.Ltime)
	if os.Getenv("VC_RESTARTED") != "" {
		// Give the old process a moment to let go of its ports.
		time.Sleep(3 * time.Second)
	}
	if err := run(*dataDir); err != nil {
		pauseOnWindows()
		os.Exit(1)
	}
}

func defaultDataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "victorcharlie-data"
	}
	return filepath.Join(filepath.Dir(exe), "victorcharlie-data")
}

func run(dataDir string) (err error) {
	defer func() {
		if err != nil && err != errLogged {
			log.Printf("Victor Charlie stopped: %v", err)
		}
	}()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(dataDir, "victorcharlie.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	// Registered after the Close above so it runs first: the reason for a
	// failed start lands in the log file, not only in a console nobody sees.
	defer func() {
		if err != nil {
			log.Printf("Victor Charlie stopped: %v", err)
			err = errLogged
		}
	}()
	logOut := io.MultiWriter(os.Stderr, logFile)
	log.SetOutput(logOut)
	cfg, err := config.Load(dataDir)
	if err != nil {
		return err
	}
	st, err := store.Open(dataDir)
	if err != nil {
		return err
	}
	defer st.Close()

	// LiveKit posts participant and track events here.
	internal, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	webhook := fmt.Sprintf("http://%s/internal/livekit", internal.Addr())

	m, err := media.Start(media.Options{
		Key: cfg.LiveKitKey, Secret: cfg.LiveKitSecret,
		TCPPort: cfg.RTCTCPPort, UDPPort: cfg.RTCUDPPort,
		PublicIP: cfg.PublicIP, LANOnly: cfg.LANOnly,
		WebhookURL: webhook,
	})
	// LiveKit points the standard logger at its own, filtered at warn level.
	// Take it back, even if LiveKit failed, so Victor Charlie's messages show.
	log.SetOutput(logOut)
	log.SetFlags(log.Ldate | log.Ltime)
	log.SetPrefix("")
	if err != nil {
		return err
	}
	defer m.Stop()

	srv := api.New(cfg, st, m, webui.FS())
	go http.Serve(internal, srv.InternalHandler())

	h := srv.Handler()
	quiet := log.New(quietTLS{logOut}, "", log.Ldate|log.Ltime)
	errc := make(chan error, 2)
	var servers []*http.Server
	switch cfg.HTTPS {
	case "auto":
		mgr := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.Domain),
			Cache:      autocert.DirCache(filepath.Join(dataDir, "certs")),
			Email:      cfg.ACMEEmail,
		}
		tlsSrv := &http.Server{Addr: net.JoinHostPort(cfg.ListenIP, "443"), Handler: h, TLSConfig: &tls.Config{GetCertificate: mgr.GetCertificate, NextProtos: []string{"h2", "http/1.1", "acme-tls/1"}}, ReadHeaderTimeout: 10 * time.Second, ErrorLog: quiet}
		plain := &http.Server{Addr: net.JoinHostPort(cfg.ListenIP, "80"), Handler: mgr.HTTPHandler(nil), ReadHeaderTimeout: 10 * time.Second, ErrorLog: quiet}
		servers = append(servers, tlsSrv, plain)
		go func() { errc <- tlsSrv.ListenAndServeTLS("", "") }()
		go func() { errc <- plain.ListenAndServe() }()
		log.Printf("%s %s is up at https://%s", cfg.AppName, version, cfg.Domain)
	default:
		plain := &http.Server{Addr: net.JoinHostPort(cfg.ListenIP, strconv.Itoa(cfg.HTTPPort)), Handler: h, ReadHeaderTimeout: 10 * time.Second, ErrorLog: quiet}
		servers = append(servers, plain)
		go func() { errc <- plain.ListenAndServe() }()
		log.Printf("%s %s is up at http://localhost:%d", cfg.AppName, version, cfg.HTTPPort)
		if cfg.HTTPS == "off" {
			log.Printf("HTTPS is off. Browsers only allow the mic, camera and screen share over HTTPS or on localhost, so set up a domain before inviting people. See the README.")
		}
	}
	log.Printf("Settings: %s", cfg.Path())
	log.Printf("Voice and video use TCP %d and UDP %d. Forward both on your router for people outside your home.", cfg.RTCTCPPort, cfg.RTCUDPPort)
	if st.UserCount() == 0 {
		log.Printf("No accounts yet. Open the site and the first account you make becomes the admin.")
	}
	if cfg.DuckDNSToken != "" && strings.HasSuffix(cfg.Domain, ".duckdns.org") {
		go duckDNS(strings.TrimSuffix(cfg.Domain, ".duckdns.org"), cfg.DuckDNSToken)
	}

	ipChanged := make(chan struct{})
	if m.LooksUpIP {
		go watchPublicIP(m.PublicIP, ipChanged)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	restart := false
	select {
	case err := <-errc:
		return err
	case <-sig:
		log.Printf("shutting down")
	case <-ipChanged:
		restart = true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		s.Shutdown(ctx)
	}
	if restart {
		m.Stop()
		st.Close()
		logFile.Close()
		return restartSelf()
	}
	return nil
}

// watchPublicIP closes changed when this network's public IP differs from
// the one voice is advertising, or when a lookup that failed at startup
// (say the router was still booting) starts working. Voice from outside the
// house breaks until Victor Charlie restarts with the new address, so it does.
func watchPublicIP(current string, changed chan struct{}) {
	for {
		time.Sleep(10 * time.Minute)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		ip, err := media.LookupPublicIP(ctx)
		cancel()
		if err != nil || ip == "" || ip == current {
			continue
		}
		if current == "" {
			log.Printf("The internet is reachable now (public IP %s). Restarting so voice works from outside your network.", ip)
		} else {
			log.Printf("Your public IP changed from %s to %s. Restarting so voice keeps working from outside your network.", current, ip)
		}
		close(changed)
		return
	}
}

// duckDNS keeps a duckdns.org name pointed at this network's public IP.
func duckDNS(sub, token string) {
	for {
		resp, err := http.Get("https://www.duckdns.org/update?domains=" + sub + "&token=" + token + "&ip=")
		if err != nil {
			log.Printf("DuckDNS update failed: %v", err)
		} else {
			b := make([]byte, 16)
			n, _ := resp.Body.Read(b)
			resp.Body.Close()
			if !strings.HasPrefix(string(b[:n]), "OK") {
				log.Printf("DuckDNS rejected the update. Check duckdns_token in victorcharlie.json.")
			}
		}
		time.Sleep(5 * time.Minute)
	}
}
