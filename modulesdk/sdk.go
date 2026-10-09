// Package modulesdk is the minimal Go SDK a hosted Terra module uses to be
// supervised by the common Module Runtime (Scene·Player redesign WS2, invocation
// plan 안 A). A module reads its Runtime-issued identity from the environment,
// binds the assigned loopback endpoint, and serves the Terra identity handshake
// and readiness probe — all guarded by the workload credential — alongside its
// own operation handler. The Runtime then verifies the handshake and probes
// readiness (modulert.Activate) before publishing the module's routes.
//
// The SDK shares its wire contract (env keys, control paths, DTOs) with the
// Runtime by importing terra-module-runtime, which is dependency-free stdlib.
// Splitting that contract into a standalone terra-module-contracts module is a
// follow-up (design §23); for the 1차 Go-only scope this single source suffices.
package modulesdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"

	modulert "github.com/StellaxiaLab/terra-sdk/modulert"
	coresvi "github.com/StellaxiaLab/terra-sdk/svi"
)

// ReadinessFunc reports whether the module is ready to serve, with an optional
// human-readable detail. A nil ReadinessFunc means "ready as soon as bound".
type ReadinessFunc func(context.Context) (ready bool, detail string)

// SVIResourcesFunc reports the module's current SVI resource descriptors. The
// daemon's remote provider adapter polls it on the same cadence as the node's
// own adapters, so the returned set IS the module's contribution to the node
// catalog: a resource missing from one poll is tombstoned, not remembered.
//
// The module describes resources in module-local terms — node identity belongs
// to the daemon, which stamps NodeID and namespaces ResourceID on ingestion.
// A module therefore leaves ResourceID/NodeID empty and identifies each
// resource by CanonicalName (e.g. "io/camera/camera-default").
type SVIResourcesFunc func(context.Context) ([]coresvi.ResourceDescriptor, error)

// Config configures a hosted module server.
type Config struct {
	// Identity is the Runtime-issued workload identity (see FromEnv).
	Identity modulert.WorkloadIdentity
	// ContributionHash is the module's contribution snapshot digest, reported in
	// the handshake and recorded by the Runtime for route publication (Phase C).
	ContributionHash string
	// Readiness reports readiness for the readiness probe (optional).
	Readiness ReadinessFunc
	// Operations serves the module's actual operation routes, mounted at the
	// endpoint root. Like the control paths it requires the workload credential
	// (the Gateway attaches it when forwarding under 안 A). Optional.
	Operations http.Handler
	// SVIResources, when set, serves the module's SVI resource descriptors at
	// modulert.SVIResourcesPath for the daemon's remote provider adapter.
	// Optional; a module with no resources leaves it nil.
	SVIResources SVIResourcesFunc
	// SVISink, when set, makes this module a possible DESTINATION for an SVI
	// binding: the daemon opens a sink at modulert.SVISinkPath and writes the
	// binding's frames to it. Optional, and separate from SVIResources —
	// describing a resource and absorbing bytes for it are different powers,
	// and the manifest declares them separately (contributions.svi.sink).
	SVISink SVISinkFunc
	// WindowOrigin is the UI origin of this module's window-mode GUI app
	// (창 모드 앱 런처 설계 §5.1), e.g. "http://127.0.0.1:<port>" of a listener
	// the module opened itself BEFORE calling Listen. Reported in the identity
	// handshake so the host publishes it alongside the module's routes; the
	// Gateway enforces the loopback origin policy on it. Optional. This origin
	// must serve the app's UI only — module APIs stay on the Runtime-assigned
	// endpoint (§5.2).
	WindowOrigin string
}

// FromEnv reads the Runtime-issued workload identity from the process
// environment. A module main typically starts with:
//
//	identity, err := modulesdk.FromEnv()
func FromEnv() (modulert.WorkloadIdentity, error) {
	return modulert.IdentityFromEnv(os.Getenv)
}

// Server is a bound hosted-module server.
type Server struct {
	listener net.Listener
	http     *http.Server
}

// Listen binds the module's Runtime-assigned loopback endpoint and prepares the
// credential-guarded control routes (handshake, readiness) plus the module's
// operations. Call Serve to run it, or Close to release the listener without
// serving.
func Listen(cfg Config) (*Server, error) {
	if cfg.Identity.Endpoint == "" {
		return nil, errors.New("modulesdk: identity endpoint is required")
	}
	if cfg.Identity.Credential == "" {
		return nil, errors.New("modulesdk: identity credential is required")
	}
	listener, err := net.Listen("tcp", cfg.Identity.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("modulesdk: bind %s: %w", cfg.Identity.Endpoint, err)
	}
	return &Server{listener: listener, http: &http.Server{Handler: routes(cfg)}}, nil
}

// Addr is the actual bound address (host:port).
func (s *Server) Addr() string { return s.listener.Addr().String() }

// Serve runs until ctx is cancelled or the listener is closed. It returns nil on
// a clean shutdown.
func (s *Server) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = s.http.Close()
	}()
	if err := s.http.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Close releases the listener without serving.
func (s *Server) Close() error { return s.listener.Close() }

func routes(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(modulert.HandshakePath, guard(cfg.Identity.Credential, handshakeHandler(cfg)))
	mux.Handle(modulert.ReadinessPath, guard(cfg.Identity.Credential, readinessHandler(cfg)))
	if cfg.SVIResources != nil {
		mux.Handle(modulert.SVIResourcesPath, guard(cfg.Identity.Credential, sviResourcesHandler(cfg)))
	}
	if cfg.SVISink != nil {
		mux.Handle(modulert.SVISinkPath, guard(cfg.Identity.Credential, sviSinkHandler(cfg, newSinkRegistry())))
	}
	if cfg.Operations != nil {
		mux.Handle("/", guard(cfg.Identity.Credential, cfg.Operations))
	}
	return mux
}

// guard rejects any request that does not present the workload credential.
func guard(credential string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(modulert.CredentialHeader) != credential {
			http.Error(w, "forbidden", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handshakeHandler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, modulert.HandshakeResponse{
			InstanceID:       cfg.Identity.InstanceID,
			Nonce:            cfg.Identity.Nonce,
			ContributionHash: cfg.ContributionHash,
			WindowOrigin:     cfg.WindowOrigin,
		})
	})
}

func readinessHandler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ready, detail := true, ""
		if cfg.Readiness != nil {
			ready, detail = cfg.Readiness(r.Context())
		}
		writeJSON(w, modulert.ReadinessResponse{Ready: ready, Detail: detail})
	})
}

func sviResourcesHandler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resources, err := cfg.SVIResources(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if resources == nil {
			resources = []coresvi.ResourceDescriptor{}
		}
		writeJSON(w, resources)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
