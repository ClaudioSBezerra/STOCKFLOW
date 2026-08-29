// Command api is the stockflow backend HTTP server. On startup it loads local
// env vars (.env, when present), opens the database connection pool, applies
// all pending SQL migrations synchronously (blocking — the process never
// accepts HTTP traffic against a schema that failed to migrate), starts the
// e-mail outbox worker (services.IniciarWorkerEmail), and then serves the
// liveness endpoint plus the public authentication routes (cadastro e
// verificação de e-mail — Story 1.3; login, refresh e /me — Story 1.4).
package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"stockflow/backend/handlers"
	"stockflow/backend/middleware"
	"stockflow/backend/services"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.Warn("falha ao carregar .env", "error", err)
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		slog.Error("DATABASE_URL não definido")
		os.Exit(1)
	}

	// JWT_SECRET assina os access tokens de sessão (Story 1.4, AD-6) — mesmo
	// tratamento fail-fast já aplicado acima a DATABASE_URL: o processo nunca
	// sobe capaz de emitir/validar sessão sem um segredo configurado.
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		slog.Error("JWT_SECRET não definido")
		os.Exit(1)
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		slog.Error("falha ao abrir conexão com o banco", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(15)
	db.SetConnMaxLifetime(15 * time.Minute)

	if err := pingWithRetry(db, 30, time.Second); err != nil {
		slog.Error("banco indisponível após retries", "error", err)
		os.Exit(1)
	}

	// Migrations rodam de forma síncrona e bloqueante, antes do servidor HTTP
	// aceitar qualquer conexão: nunca subimos com schema divergente.
	if err := runMigrations(db); err != nil {
		slog.Error("falha ao aplicar migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations aplicadas com sucesso")

	emailCfg := services.CarregarEmailConfig()

	// Worker de e-mail (AD-4): uma única goroutine consumindo emails_pendentes
	// por polling. Sobe depois das migrations (a tabela precisa existir) e
	// para durante o shutdown gracioso abaixo, antes do pool de conexões
	// fechar.
	pararWorkerEmail := services.IniciarWorkerEmail(db, emailCfg, services.IntervaloPollingEmail)
	defer pararWorkerEmail()

	mux := newMux(db, emailCfg, []byte(jwtSecret))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
		<-sigChan

		slog.Info("sinal de encerramento recebido, desligando graciosamente")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			slog.Error("erro ao desligar servidor HTTP", "error", err)
		}
	}()

	slog.Info("servidor iniciado", "port", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("erro fatal no servidor HTTP", "error", err)
		os.Exit(1)
	}

	// ListenAndServe retorna assim que o listener é fechado, no início de
	// Shutdown() — antes dele terminar de drenar conexões em andamento. Sem
	// esperar aqui, o defer db.Close() acima rodaria cedo demais, derrubando o
	// pool de conexões enquanto requisições ainda em voo tentam usá-lo durante
	// a janela de até 10s do graceful shutdown.
	<-shutdownDone
}

// pingWithRetry aguarda o Postgres ficar disponível — cobre o caso comum de
// `api` subir antes do `db` estar pronto para aceitar conexões, mesmo com o
// `depends_on: db healthy` do compose.
func pingWithRetry(db *sql.DB, attempts int, delay time.Duration) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = db.Ping(); err == nil {
			return nil
		}
		slog.Warn("aguardando banco de dados", "tentativa", i+1, "error", err)
		time.Sleep(delay)
	}
	return err
}

// runMigrations aplica, de forma síncrona e sequencial, todas as migrations
// pendentes embutidas no binário. Retorna erro se qualquer migration falhar;
// o chamador é responsável por encerrar o processo (log.Fatal-equivalente) —
// a aplicação nunca sobe com schema divergente.
func runMigrations(db *sql.DB) error {
	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return err
	}

	dbDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", dbDriver)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

// newMux monta o roteador HTTP do servidor — extraído de main() para que os
// testes possam despachar requisições através do mux real (mesmos padrões de
// método+rota registrados em produção), em vez de chamar cada handler
// diretamente e nunca exercitar o registro em si.
func newMux(db *sql.DB, emailCfg services.EmailConfig, jwtSecret []byte) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", healthHandler(db))
	mux.HandleFunc("POST /api/auth/cadastro", handlers.CadastroHandler(db, emailCfg))
	mux.HandleFunc("GET /api/auth/verificar-email", handlers.VerificarEmailHandler(db))
	mux.HandleFunc("POST /api/auth/login", handlers.LoginHandler(db, jwtSecret))
	mux.HandleFunc("POST /api/auth/refresh", handlers.RefreshHandler(db, jwtSecret))
	mux.HandleFunc("GET /api/auth/me", middleware.RequireAuth(db, jwtSecret)(handlers.MeHandler()))
	return mux
}

type healthResponse struct {
	Status string `json:"status"`
}

// healthHandler é o único endpoint desta story: liveness usado pelo
// healthcheck do compose/CI (AD-16).
func healthHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(healthResponse{Status: "unhealthy"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
	}
}
