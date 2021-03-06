package server

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/heetch/regula/store"
	"github.com/rs/zerolog"
)

// Server is an HTTP server serving the Regula API.
type Server struct {
	Mux    *http.ServeMux // Can be used to add handlers to the server.
	logger zerolog.Logger
	server *http.Server
}

// New creates a Server instance.
func New(service store.RulesetService, cfg Config) *Server {
	srv := Server{
		Mux: http.NewServeMux(),
	}

	if cfg.Logger == nil {
		lg := zerolog.New(os.Stderr).With().Timestamp().Logger()
		cfg.Logger = &lg
	}

	srv.logger = *cfg.Logger

	srv.server = new(http.Server)

	ctx, cancel := context.WithCancel(context.Background())
	// cancel context on shutdown to stop long running operations like watches.
	srv.server.RegisterOnShutdown(cancel)

	srv.Mux.Handle("/", NewHandler(ctx, service, cfg))

	return &srv
}

// Run runs the server on the chosen address. The given context must be used to
// gracefully stop the server.
func (s *Server) Run(ctx context.Context, addr string) error {
	s.server.Handler = s.Mux

	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	go func() {
		s.logger.Info().Msg("Listening on " + l.Addr().String())
		err := s.server.Serve(l)
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = s.server.Shutdown(ctx)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to shutdown server gracefully")
	}

	return err
}
