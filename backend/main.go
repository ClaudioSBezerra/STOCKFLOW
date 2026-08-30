// Command api is the stockflow backend HTTP server. On startup it loads local
// env vars (.env, when present), opens the database connection pool, applies
// all pending SQL migrations synchronously (blocking — the process never
// accepts HTTP traffic against a schema that failed to migrate), starts the
// e-mail outbox worker (services.IniciarWorkerEmail), and then serves the
// liveness endpoint plus the public authentication routes (cadastro e
// verificação de e-mail — Story 1.3; login, refresh e /me — Story 1.4;
// esqueci-senha e redefinir-senha — Story 1.6), the first role-gated route
// (GET /api/usuarios, mínimo `gestor` — Story 1.5), a solicitação de
// promoção de papel — Story 1.7 (POST /api/promocoes e GET /api/promocoes/minha
// para qualquer conta autenticada; GET /api/promocoes e
// POST /api/promocoes/{id}/decisao com mínimo `gestor`) e a gestão de contas —
// desativação e rebaixamento — Story 1.8 (POST /api/usuarios/{id}/desativacao e
// POST /api/usuarios/{id}/rebaixamento, mínimo `gestor`), o login federado via
// Keycloak — SSO Ferreira Costa — Story 1.9 (GET /api/auth/sso/config e
// POST /api/auth/logout sempre registrados; POST /api/auth/sso/keycloak atrás do
// middleware `iam` só quando `IAM_BASE_URL` está configurado) e o log de acesso
// e auditoria — Story 1.12 (GET /api/logs-acesso, mínimo `adm`: toda tentativa
// de login por senha ou SSO é registrada append-only em `logs_acesso`).
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
	"stockflow/backend/iam"
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

	// Config do realm Keycloak (Story 1.9, AD-7): lida uma vez no startup do
	// ambiente. Sem IAM_BASE_URL, o login federado fica desligado e o servidor
	// sobe idêntico ao comportamento atual (só /api/auth/sso/config e
	// /api/auth/logout são registrados, nada mais).
	iamCfg := iam.CarregarConfig()

	// Worker de e-mail (AD-4): uma única goroutine consumindo emails_pendentes
	// por polling. Sobe depois das migrations (a tabela precisa existir) e
	// para durante o shutdown gracioso abaixo, antes do pool de conexões
	// fechar.
	pararWorkerEmail := services.IniciarWorkerEmail(db, emailCfg, services.IntervaloPollingEmail)
	defer pararWorkerEmail()

	mux := newMux(db, emailCfg, []byte(jwtSecret), iamCfg)

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
func newMux(db *sql.DB, emailCfg services.EmailConfig, jwtSecret []byte, iamCfg iam.Config) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", healthHandler(db))
	mux.HandleFunc("POST /api/auth/cadastro", handlers.CadastroHandler(db, emailCfg))
	mux.HandleFunc("GET /api/auth/verificar-email", handlers.VerificarEmailHandler(db))
	mux.HandleFunc("POST /api/auth/login", handlers.LoginHandler(db, jwtSecret))
	mux.HandleFunc("POST /api/auth/refresh", handlers.RefreshHandler(db, jwtSecret))
	mux.HandleFunc("POST /api/auth/esqueci-senha", handlers.EsqueciSenhaHandler(db, emailCfg))
	mux.HandleFunc("GET /api/auth/redefinir-senha", handlers.ValidarRedefinicaoSenhaHandler(db))
	mux.HandleFunc("POST /api/auth/redefinir-senha", handlers.RedefinirSenhaHandler(db))
	mux.HandleFunc("GET /api/auth/me", middleware.RequireAuth(db, jwtSecret)(handlers.MeHandler()))

	// MFA obrigatório para papéis administrativos — Story 1.11 (FR-37/SM-2).
	// /mfa/verificar é pública (troca o token de login pendente por sessão,
	// ainda sem sessão nenhuma nesse ponto); /mfa/iniciar e /mfa/confirmar
	// exigem sessão já autenticada (RequireAuth), mas nenhum papel mínimo —
	// qualquer conta pode configurar MFA, mesmo que só gestor/adm o exijam.
	mux.HandleFunc("POST /api/auth/mfa/verificar", handlers.MFAVerificarHandler(db, jwtSecret))
	mux.HandleFunc("POST /api/auth/mfa/iniciar", middleware.RequireAuth(db, jwtSecret)(handlers.MFAIniciarHandler(db)))
	mux.HandleFunc("POST /api/auth/mfa/confirmar", middleware.RequireAuth(db, jwtSecret)(handlers.MFAConfirmarHandler(db)))
	mux.HandleFunc("GET /api/usuarios", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.ListarUsuariosHandler(db))))
	mux.HandleFunc("POST /api/promocoes", middleware.RequireAuth(db, jwtSecret)(
		handlers.SolicitarPromocaoHandler(db)))
	mux.HandleFunc("GET /api/promocoes/minha", middleware.RequireAuth(db, jwtSecret)(
		handlers.MinhaSolicitacaoHandler(db)))
	mux.HandleFunc("GET /api/promocoes", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.ListarPromocoesHandler(db))))
	mux.HandleFunc("POST /api/promocoes/{id}/decisao", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.DecidirPromocaoHandler(db))))
	mux.HandleFunc("POST /api/usuarios/{id}/desativacao", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.DesativarUsuarioHandler(db))))
	mux.HandleFunc("POST /api/usuarios/{id}/rebaixamento", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.RebaixarUsuarioHandler(db))))

	// Log de acesso e auditoria — Story 1.12 (FR-38/NFR-3). Mesma composição de
	// GET /api/usuarios, aqui com o gate de papel + MFA (Story 1.11) resolvido
	// no mínimo `adm`: só um `adm` consulta a trilha append-only de tentativas
	// de login. Não há rota de escrita — `logs_acesso` só recebe o INSERT
	// não-fatal disparado de dentro de LoginHandler/KeycloakSSOHandler.
	mux.HandleFunc("GET /api/logs-acesso", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.ListarLogsAcessoHandler(db))))

	// Login federado via Keycloak — SSO Ferreira Costa (Story 1.9, AD-7).
	// /api/auth/sso/config e /api/auth/logout são SEMPRE registrados (o
	// primeiro responde {"enabled":false} sem config; o segundo é o único
	// caminho de logout do produto, inclusive para o login por senha). A troca
	// de token só existe quando o realm está configurado, sempre atrás do
	// middleware `iam`.
	mux.HandleFunc("GET /api/auth/sso/config", handlers.SSOConfigHandler(iamCfg))
	mux.HandleFunc("POST /api/auth/logout", handlers.LogoutHandler(db))
	if iamCfg.Habilitado() {
		jwks := iam.NewJWKSClient(iamCfg.RealmURL+"/protocol/openid-connect/certs", time.Hour)
		if len(iamCfg.AllowedClientIDs) == 0 {
			slog.Warn("SSO habilitado mas IAM_ALLOWED_CLIENT_IDS vazio — todo login SSO falhará no azp")
		}
		mux.HandleFunc("POST /api/auth/sso/keycloak",
			iam.Middleware(jwks, iamCfg)(handlers.KeycloakSSOHandler(db, jwtSecret)))
	}

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
