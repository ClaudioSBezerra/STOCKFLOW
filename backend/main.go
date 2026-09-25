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
// de login por senha ou SSO é registrada append-only em `logs_acesso`) e a
// abertura do domínio de Estoques — Story 2.1 (POST /api/estoques, mínimo
// `almoxarife`, e GET /api/estoques para qualquer conta autenticada: criar e
// listar locais de estoque, com nome único — insensível a maiúsculas/minúsculas
// e a espaçamento — imposto pelo índice único sobre a coluna gerada
// `nome_normalizado`) e a exclusão de Estoque — Story 2.2
// (DELETE /api/estoques/{id}, mínimo `almoxarife`: 204 no sucesso, 404 para id
// inexistente ou malformado; o guard de estoque residual entra na Story 3.1 e
// o de Pedido pendente na Story 7.2, sem reabrir a Story 2.2) e o cadastro
// manual de Produto com dimensões estruturadas — Story 3.1 (POST /api/produtos,
// mínimo `almoxarife`: cria o Produto e a linha inicial de `produto_estoque`
// numa única transação; GET /api/categorias para qualquer conta autenticada
// lista as 25 categorias fixas de seed; o guard de quantidade residual da
// Story 2.2 passa a ser exercitado de verdade, agora que `produto_estoque`
// existe) e a Nomenclatura Guiada por subtipo — Story 3.2 (GET
// /api/nomenclatura-templates para qualquer conta autenticada lista os 28
// templates fixos de seed; POST /api/produtos ganha `template_id` opcional,
// validando `nome` contra o formato do template quando informado; POST
// /api/produtos/{id}/renomear, mínimo `almoxarife`, é o único endpoint de
// edição de Produto — escopo restrito a `nome`, revalidado contra o template
// aplicado quando existir) e a importação em massa via planilha padronizada —
// Story 3.3 (POST /api/importacoes, mínimo `almoxarife`: recebe um `.xlsx`
// multipart no campo `planilha`, valida o cabeçalho fixo de 16 colunas e
// processa cada linha sequencialmente, criando Produtos e Estoques ausentes
// automaticamente; GET /api/importacoes/ultima, mínimo `almoxarife`, indica
// até onde uma importação interrompida chegou; POST
// /api/importacoes/{id}/continuar, mínimo `almoxarife`, retoma só as linhas
// ainda pendentes) e o upload e armazenamento de foto do Produto — Story 3.5
// (POST /api/produtos/{id}/fotos, mínimo `almoxarife`: multipart, campo
// `foto`, decodifica JPG/PNG/WEBP pelo conteúdo real, redimensiona a 500px no
// maior lado — só reduz, nunca amplia — e recomprime em JPEG q=82,
// gravado em `FOTOS_DIR` sem overwrite; GET
// /api/produtos/{id}/fotos/{arquivo}, qualquer conta autenticada, serve o
// arquivo salvo) e a galeria/lightbox — Story 3.6 (GET
// /api/produtos/{id}/fotos, qualquer conta autenticada, lista todas as fotos
// do Produto ordenadas por ordem de envio, `{"fotos":[...]}` — vazio, nunca
// erro, quando não há foto). `FOTOS_DIR` segue o mesmo fail-fast de
// DATABASE_URL/JWT_SECRET: sem valor usa `./fotos`, e o diretório é criado
// no startup) e a busca por nome/código/categoria com sugestões — Story 4.1
// (GET /api/produtos/busca?q=<termo>, qualquer conta autenticada: até 7
// Produtos ranqueados por relevância, sem índice novo) e a visualização em
// grade e tabela agrupada — Story 4.3 (GET /api/produtos/catalogo, qualquer
// conta autenticada: listagem paginada em dois modos — `agrupar=false` grade,
// um Produto por linha; `agrupar=true` tabela com Produtos de mesmo nome +
// dimensões colapsados e quantidade discriminada por Estoque —, sem índice
// novo) e o detalhe do Produto por Estoque com atualização em tempo real —
// Story 4.4 (GET /api/produtos/{id}, qualquer conta autenticada: detalhe com
// quantidade discriminada por Estoque; POST /api/realtime/ticket, qualquer
// conta autenticada, emite um ticket de conexão de uso único/30s; GET
// /api/realtime/stream, SEM RequireAuth — autentica pelo próprio ticket na
// query string, já que `EventSource` não envia `Authorization` —, promove a
// conexão a SSE assinando o `*realtime.Registry` do processo, único fan-out
// in-process compartilhado com POST /api/produtos e POST
// /api/produtos/{id}/renomear, que passam a publicar no canal `produtos` a
// cada escrita bem-sucedida) e a identificação de Produto via QR Code /
// código de barras — Story 4.5 (GET /api/produtos/por-codigo?codigo=<valor>,
// qualquer conta autenticada: resolve o Código de Identificação EXATO lido de
// um QR Code / código de barras físico para o Produto correspondente —
// segmento literal registrado antes de GET /api/produtos/{id}; `codigo`
// vazio -> 400, `codigo` sem Produto -> 404) e a exportação da tabela do
// Catálogo para Excel — Story 4.6 (GET /api/produtos/catalogo/exportar,
// mínimo `almoxarife`: gera um `.xlsx` real com os mesmos filtros de GET
// /api/produtos/catalogo aplicados à tabela agrupada COMPLETA, sem
// `pagina`/`agrupar` — subtotal por grupo e total geral via fórmula
// `SUBTOTAL`, nunca soma estática) e a abertura do domínio de Movimentação de
// Estoque (Epic 5) com Registrar Baixa (consumo) — Story 5.1
// (POST /api/produtos/{id}/estoques/{estoqueId}/baixa, mínimo `almoxarife`:
// debita `produto_estoque.quantidade` e insere a Movimentação `tipo='baixa'`
// correspondente numa única transação, com `SELECT ... FOR UPDATE` travando
// a linha alvo; publica no canal SSE `movimentacoes` a cada sucesso) e
// Registrar Transferência entre Estoques — Story 5.2
// (POST /api/produtos/{id}/estoques/{estoqueId}/transferencia, mínimo
// `almoxarife`: debita a origem, credita o destino e insere a Movimentação
// `tipo='transferencia'` correspondente numa única transação, travando as
// duas linhas de `produto_estoque` — origem e destino — na ordem canônica
// ascendente de `estoque_id` via `INSERT ... ON CONFLICT DO UPDATE` (AD-10);
// publica no canal SSE `movimentacoes` a cada sucesso) e o Histórico de
// Movimentações consultável — Story 5.3 (GET /api/movimentacoes, mínimo
// `almoxarife`: lista só-leitura das Movimentações com nome de Produto,
// Estoques e autor resolvidos por JOIN, mais recente primeiro, teto de 500 —
// sem rota de escrita, a trilha é append-only). A recuperação de MFA — Story
// 14.4 — acrescenta POST /api/usuarios/{id}/mfa-reset (mínimo `adm`, alvo de
// rank menor) e POST /api/auth/mfa/desligar (só RequireAuth, a própria conta
// com senha atual + código TOTP), ambas auditadas em `auditoria_seguranca`.
package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	"stockflow/backend/realtime"
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

	// FOTOS_DIR (Story 3.5): diretório onde as fotos de Produto são gravadas
	// em disco — nunca base64 no banco. Sem valor, default `./fotos` (dev
	// local); em docker-compose, `/data/fotos` num volume nomeado persistente.
	// Mesmo tratamento fail-fast de DATABASE_URL/JWT_SECRET acima: o processo
	// nunca sobe incapaz de gravar fotos.
	fotosDir := os.Getenv("FOTOS_DIR")
	if fotosDir == "" {
		fotosDir = "./fotos"
	}
	if err := os.MkdirAll(fotosDir, 0o755); err != nil {
		slog.Error("falha ao criar FOTOS_DIR", "fotos_dir", fotosDir, "error", err)
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

	// EMPRESA_PADRAO (Story 15.4) é opcional. Definida mas sem resolver para
	// uma Empresa ativa, a raiz cai calada no login pela conta; o aviso aqui
	// é o único sinal que o operador tem de que o valor está errado.
	if v := strings.TrimSpace(os.Getenv("EMPRESA_PADRAO")); v != "" {
		if slug, err := services.EmpresaPadrao(db, v); err != nil {
			slog.Warn("falha ao conferir EMPRESA_PADRAO", "empresa_padrao", v, "error", err)
		} else if slug == "" {
			slog.Warn("EMPRESA_PADRAO não corresponde a uma Empresa ativa; a raiz mostra o login pela conta", "empresa_padrao", v)
		}
	}

	mux := newMux(db, emailCfg, []byte(jwtSecret), iamCfg, fotosDir)

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

	if err := recuperarDirty49(db, m); err != nil {
		return err
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

// recuperarDirty49 desfaz a marca `dirty` deixada pela PRIMEIRA versão da
// migration 000049, que abortava num banco com a conta sintética sem Empresa
// da 000022 (instalação nova, CI) e deixava `schema_migrations` em 49 dirty —
// a API não subia mais (stockflow.fbtechia.com, 2026-09-24). O arquivo da
// 000049 roda numa única transação implícita, então um aborto não deixa nada
// aplicado; mesmo assim só força a 48 se a coluna `usuarios.empresa_raiz_id`
// NÃO existir (prova de que o schema está mesmo na 48). Qualquer outro estado
// dirty continua exigindo intervenção manual, como sempre.
func recuperarDirty49(db *sql.DB, m *migrate.Migrate) error {
	versao, dirty, err := m.Version()
	if err != nil || !dirty || versao != 49 {
		return nil // ErrNilVersion (banco novo) e demais casos: segue o fluxo normal
	}
	var existe bool
	if err := db.QueryRow(`
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'usuarios' AND column_name = 'empresa_raiz_id')`,
	).Scan(&existe); err != nil {
		return fmt.Errorf("falha ao conferir o estado da migration 000049: %w", err)
	}
	if existe {
		return nil // estado inesperado: não mexe; m.Up() devolve o erro de dirty
	}
	slog.Warn("migration 000049 marcada dirty sem nada aplicado; voltando para a versão 48 e reaplicando")
	return m.Force(48)
}

// newMux monta o roteador HTTP do servidor — extraído de main() para que os
// testes possam despachar requisições através do mux real (mesmos padrões de
// método+rota registrados em produção), em vez de chamar cada handler
// diretamente e nunca exercitar o registro em si.
func newMux(db *sql.DB, emailCfg services.EmailConfig, jwtSecret []byte, iamCfg iam.Config, fotosDir string) *http.ServeMux {
	mux := http.NewServeMux()

	// registro (Story 4.4, spec-4-4, AD-3): única instância de
	// *realtime.Registry do processo — criada localmente aqui, NUNCA um
	// parâmetro novo de newMux (evitaria reescrever todas as chamadas de
	// teste existentes que já montam newMux com 5 argumentos). Compartilhada
	// entre os handlers que publicam (CriarProdutoHandler/
	// AtualizarNomeProdutoHandler, abaixo) e o que assina
	// (StreamRealtimeHandler, registrado mais adiante).
	registro := realtime.NewRegistry()

	// Fronteira de Empresa (Story 9.1, spec-9-1, AD-19). TODA rota de
	// negócio vive sob o prefixo `/e/{slug}/api/...` e é registrada por
	// `registrar`, que a envolve em middleware.RequireEmpresa — POR FORA de
	// RequireAuth/RequireRole: um slug que não resolve devolve 404 antes de
	// qualquer validação de token, e a Empresa chega ao handler pelo
	// contexto, uma única vez por requisição.
	//
	// Exceções (SEM prefixo e SEM RequireEmpresa): `GET /api/health` — o
	// liveness do compose/CI (AD-16), que não conhece nenhum slug e precisa
	// responder mesmo com a tabela `empresas` vazia —, a área do Dono da
	// Plataforma (`/api/plataforma/*`), o login pela conta na raiz
	// (`/api/auth/entrar*`, Story 15.2), o "Esqueci a senha" na raiz
	// (`POST /api/auth/esqueci-senha`, Story 15.3) e a Empresa padrão de um
	// servidor de um cliente só (`GET /api/entrada`, Story 15.4), registrados
	// abaixo.
	requireEmpresa := middleware.RequireEmpresa(db)
	registrar := func(padrao string, h http.HandlerFunc) {
		mux.HandleFunc(padrao, requireEmpresa(h))
	}

	mux.HandleFunc("GET /api/health", healthHandler(db))

	// Área do Dono da Plataforma — Story 9.2 (Epic 9, AD-21), spec-9-2. A
	// segunda exceção ao prefixo de Empresa: o Dono não pertence a
	// Empresa nenhuma, então estas rotas vão direto no mux, SEM o wrapper de
	// Empresa. Login/refresh/logout são públicos (o login exige e-mail + senha
	// + código TOTP numa única chamada); o resto fica atrás de
	// RequireDonoPlataforma, que só aceita o access token com
	// `aud = "plataforma"`. Nenhuma rota aqui cria um Dono — isso é só o CLI
	// cmd/seed-dono-plataforma.
	requireDono := middleware.RequireDonoPlataforma(db, jwtSecret)
	mux.HandleFunc("POST /api/plataforma/auth/login", handlers.PlataformaLoginHandler(db, jwtSecret))
	mux.HandleFunc("POST /api/plataforma/auth/refresh", handlers.PlataformaRefreshHandler(db, jwtSecret))
	mux.HandleFunc("POST /api/plataforma/auth/logout", handlers.PlataformaLogoutHandler(db))
	mux.HandleFunc("GET /api/plataforma/auth/me", requireDono(handlers.PlataformaMeHandler()))
	mux.HandleFunc("GET /api/plataforma/empresas", requireDono(handlers.ListarEmpresasHandler(db)))
	mux.HandleFunc("POST /api/plataforma/empresas", requireDono(handlers.CriarEmpresaHandler(db, emailCfg, fotosDir)))
	mux.HandleFunc("POST /api/plataforma/empresas/{id}/desativacao", requireDono(handlers.DesativarEmpresaHandler(db)))
	mux.HandleFunc("POST /api/plataforma/empresas/{id}/reativacao", requireDono(handlers.ReativarEmpresaHandler(db)))

	// Login na raiz do domínio pela conta — Story 15.2 (AD-36). Terceira
	// exceção ao prefixo de Empresa: sem `/e/{slug}` e SEM RequireEmpresa,
	// porque a Empresa é descoberta pela conta do e-mail (nunca de body/query).
	// A sessão emitida continua por Empresa (cookie `Path=/e/{slug}/api/auth`).
	mux.HandleFunc("POST /api/auth/entrar", handlers.EntrarHandler(db, jwtSecret))
	mux.HandleFunc("POST /api/auth/entrar/escolha", handlers.EntrarEscolhaHandler(db, jwtSecret))
	// "Esqueci a senha" na raiz — Story 15.3 (AD-36), mesma exceção: um
	// e-mail de redefinição por conta ativa do e-mail, cada um com o link
	// `/e/{slug}/redefinir-senha` da sua Empresa. Sempre 202.
	mux.HandleFunc("POST /api/auth/esqueci-senha", handlers.EsqueciSenhaPelaContaHandler(db, emailCfg))
	// Empresa padrão — Story 15.4 (AD-36), mesma exceção: num servidor de um
	// cliente só (`EMPRESA_PADRAO`, opcional, nunca definida em
	// stockflow.fbtechia.com) a raiz redireciona para `/e/{slug}/`. Lida uma
	// vez aqui; a Empresa é consultada a cada requisição. Público: devolve só
	// `{empresaPadrao: slug | null}`.
	empresaPadrao := os.Getenv("EMPRESA_PADRAO")
	mux.HandleFunc("GET /api/entrada", handlers.EntradaHandler(db, empresaPadrao))

	registrar("POST /e/{slug}/api/auth/cadastro", handlers.CadastroHandler(db, emailCfg))
	registrar("GET /e/{slug}/api/auth/verificar-email", handlers.VerificarEmailHandler(db))
	registrar("POST /e/{slug}/api/auth/login", handlers.LoginHandler(db, jwtSecret))
	registrar("POST /e/{slug}/api/auth/refresh", handlers.RefreshHandler(db, jwtSecret))
	registrar("POST /e/{slug}/api/auth/esqueci-senha", handlers.EsqueciSenhaHandler(db, emailCfg))
	registrar("GET /e/{slug}/api/auth/redefinir-senha", handlers.ValidarRedefinicaoSenhaHandler(db))
	registrar("POST /e/{slug}/api/auth/redefinir-senha", handlers.RedefinirSenhaHandler(db))
	registrar("GET /e/{slug}/api/auth/me", middleware.RequireAuth(db, jwtSecret)(handlers.MeHandler()))

	// Convite nominal de acesso — Story 9.3 (FR-42, AD-22). O `GET` de
	// validação é PÚBLICO (quem abre o link ainda não tem conta), ao lado de
	// verificar-email/redefinir-senha; a emissão, a listagem e a revogação
	// ficam atrás de RequireAuth + RequireRole(gestor) — o gate de papel é do
	// middleware, nunca do handler. Todas passam por `registrar`, então
	// RequireEmpresa continua por fora: o slug decide a Empresa do convite.
	registrar("GET /e/{slug}/api/auth/convite", handlers.ValidarConviteHandler(db))
	registrar("POST /e/{slug}/api/convites", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.EmitirConviteHandler(db, emailCfg))))
	registrar("GET /e/{slug}/api/convites", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.ListarConvitesHandler(db, emailCfg))))
	registrar("POST /e/{slug}/api/convites/{id}/revogacao", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.RevogarConviteHandler(db))))

	// MFA obrigatório para papéis administrativos — Story 1.11 (FR-37/SM-2).
	// /mfa/verificar é pública (troca o token de login pendente por sessão,
	// ainda sem sessão nenhuma nesse ponto); /mfa/iniciar e /mfa/confirmar
	// exigem sessão já autenticada (RequireAuth), mas nenhum papel mínimo —
	// qualquer conta pode configurar MFA, mesmo que só gestor/adm o exijam.
	registrar("POST /e/{slug}/api/auth/mfa/verificar", handlers.MFAVerificarHandler(db, jwtSecret))
	registrar("POST /e/{slug}/api/auth/mfa/iniciar", middleware.RequireAuth(db, jwtSecret)(handlers.MFAIniciarHandler(db)))
	registrar("POST /e/{slug}/api/auth/mfa/confirmar", middleware.RequireAuth(db, jwtSecret)(handlers.MFAConfirmarHandler(db)))
	// Story 14.4: a própria conta desliga o seu MFA (senha atual + código
	// TOTP). Só RequireAuth — qualquer papel; a recusa para gestor/adm numa
	// Empresa que exige é regra do service, não gate de papel.
	registrar("POST /e/{slug}/api/auth/mfa/desligar", middleware.RequireAuth(db, jwtSecret)(handlers.MFADesligarHandler(db)))
	registrar("GET /e/{slug}/api/usuarios", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.ListarUsuariosHandler(db))))
	registrar("POST /e/{slug}/api/promocoes", middleware.RequireAuth(db, jwtSecret)(
		handlers.SolicitarPromocaoHandler(db)))
	registrar("GET /e/{slug}/api/promocoes/minha", middleware.RequireAuth(db, jwtSecret)(
		handlers.MinhaSolicitacaoHandler(db)))
	registrar("GET /e/{slug}/api/promocoes", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.ListarPromocoesHandler(db))))
	registrar("POST /e/{slug}/api/promocoes/{id}/decisao", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.DecidirPromocaoHandler(db))))
	registrar("POST /e/{slug}/api/usuarios/{id}/desativacao", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.DesativarUsuarioHandler(db))))
	registrar("POST /e/{slug}/api/usuarios/{id}/rebaixamento", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.RebaixarUsuarioHandler(db))))
	// Story 14.4: o `adm` reseta o MFA de uma conta de rank menor da própria
	// Empresa (revoga as sessões dela e audita em `auditoria_seguranca`).
	registrar("POST /e/{slug}/api/usuarios/{id}/mfa-reset", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.ResetarMFAUsuarioHandler(db))))

	// Log de acesso e auditoria — Story 1.12 (FR-38/NFR-3). Mesma composição de
	// GET /api/usuarios, aqui com o gate de papel + MFA (Story 1.11) resolvido
	// no mínimo `adm`: só um `adm` consulta a trilha append-only de tentativas
	// de login. Não há rota de escrita — `logs_acesso` só recebe o INSERT
	// não-fatal disparado de dentro de LoginHandler/KeycloakSSOHandler.
	registrar("GET /e/{slug}/api/logs-acesso", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.ListarLogsAcessoHandler(db))))

	// Exigência de MFA da Empresa e auditoria de segurança — Story 14.3
	// (FR-53, AD-35). Só o `adm` lê/altera o flag e consulta a trilha
	// append-only `auditoria_seguranca`; RequireRole(adm) já sujeita o próprio
	// adm ao gate de MFA. Não há PUT/PATCH/DELETE sobre a auditoria.
	registrar("GET /e/{slug}/api/seguranca/mfa-empresa", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.ObterExigenciaMFAHandler(db))))
	registrar("PUT /e/{slug}/api/seguranca/mfa-empresa", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.AlterarExigenciaMFAHandler(db))))
	registrar("GET /e/{slug}/api/seguranca/auditoria", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.ListarAuditoriaSegurancaHandler(db))))

	// Gestão de Estoques — criar e listar locais — Story 2.1 (FR12);
	// excluir — Story 2.2 (FR12). POST e DELETE ficam atrás de
	// RequireRole(almoxarife): criar e excluir Estoque são restritos a
	// `almoxarife`+ e a decisão é do middleware (403 para papéis abaixo, mesmo
	// em chamada direta à API). GET leva só RequireAuth — a lista de Estoques é
	// liberada a qualquer conta autenticada (as telas de catálogo do Epic 4
	// precisam listar nomes de Estoque para contas `usuario`); por isso NÃO
	// leva RequireRole. Unicidade de nome — insensível a caixa e a espaçamento
	// — é imposta pelo índice único sobre a coluna gerada `nome_normalizado`; a
	// colisão vira 409 CONFLICT. O DELETE responde 204 sem corpo no sucesso e
	// 404 para id inexistente ou malformado; os guards de estoque residual
	// (Epic 3) e Pedido pendente (Epic 7) entram nas Stories 3.1 e 7.2.
	registrar("POST /e/{slug}/api/estoques", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.CriarEstoqueHandler(db))))
	registrar("GET /e/{slug}/api/estoques", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarEstoquesHandler(db)))
	registrar("DELETE /e/{slug}/api/estoques/{id}", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.ExcluirEstoqueHandler(db))))

	// Filiais — Story 12.1 (FR-51, AD-27). Escrita só `adm`+ (403 abaixo,
	// decidido por RequireRole); a listagem leva só RequireAuth (o almoxarife
	// precisa listar para escolher a Filial do Estoque).
	registrar("POST /e/{slug}/api/filiais", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.CriarFilialHandler(db))))
	registrar("GET /e/{slug}/api/filiais", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarFiliaisHandler(db)))

	// Centros de Custo — Story 12.3 (FR-51, AD-28). Mesmo desenho de Filiais:
	// escrita só `adm`+ (403 abaixo); a listagem leva só RequireAuth (quem
	// envia Pedido precisa listar para escolher o Centro de Custo).
	registrar("POST /e/{slug}/api/centros-custo", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.CriarCentroCustoHandler(db))))
	registrar("GET /e/{slug}/api/centros-custo", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarCentrosCustoHandler(db)))

	// Cadastro manual de Produto com dimensões estruturadas — Story 3.1
	// (FR-8). POST /api/produtos fica atrás de RequireRole(almoxarife): criar
	// Produto é restrito a `almoxarife`+, decisão do middleware (403 para
	// papéis abaixo, mesmo em chamada direta à API). GET /api/categorias leva
	// só RequireAuth — a lista fixa de categorias é liberada a qualquer conta
	// autenticada, mesmo padrão de GET /api/estoques (o formulário de cadastro
	// e as telas de catálogo do Epic 4 precisam dela).
	registrar("POST /e/{slug}/api/produtos", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.CriarProdutoHandler(db, registro))))
	registrar("GET /e/{slug}/api/categorias", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarCategoriasHandler(db)))

	// CRUD de Categorias — Story 10.5 (AD-33). Escrita só `adm`+ (403 abaixo,
	// decidido por RequireRole); o GET acima segue só RequireAuth.
	registrar("POST /e/{slug}/api/categorias", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.CriarCategoriaHandler(db))))
	registrar("PUT /e/{slug}/api/categorias/{id}", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.AtualizarCategoriaHandler(db))))
	registrar("DELETE /e/{slug}/api/categorias/{id}", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.ExcluirCategoriaHandler(db))))

	// Nomenclatura Guiada por subtipo — Story 3.2 (FR-9). GET
	// /api/nomenclatura-templates leva só RequireAuth, mesmo padrão de GET
	// /api/categorias — a lista fixa dos 28 templates é liberada a qualquer
	// conta autenticada (o formulário de cadastro precisa dela). POST
	// /api/produtos/{id}/renomear fica atrás de RequireRole(almoxarife): é o
	// único endpoint de edição de Produto que existe hoje, restrito a `nome`,
	// mesmo mínimo de papel do cadastro.
	registrar("GET /e/{slug}/api/nomenclatura-templates", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarNomenclaturaTemplatesHandler(db)))

	// CRUD de Templates de Nomenclatura — Story 10.6 (AD-33). Escrita só
	// `adm`+ (403 abaixo, decidido por RequireRole); o GET acima segue só
	// RequireAuth.
	registrar("POST /e/{slug}/api/nomenclatura-templates", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.CriarTemplateNomenclaturaHandler(db))))
	registrar("PUT /e/{slug}/api/nomenclatura-templates/{id}", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.AtualizarTemplateNomenclaturaHandler(db))))
	registrar("DELETE /e/{slug}/api/nomenclatura-templates/{id}", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.ExcluirTemplateNomenclaturaHandler(db))))
	registrar("POST /e/{slug}/api/produtos/{id}/renomear", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.AtualizarNomeProdutoHandler(db, registro))))
	// PUT /api/produtos/{id} (spec-13-1): edição completa do Produto, mesmo
	// mínimo de papel do cadastro.
	registrar("PUT /e/{slug}/api/produtos/{id}", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.AtualizarProdutoHandler(db, registro))))
	// Histórico do Produto — Story 16.3: `almoxarife`+.
	registrar("GET /e/{slug}/api/produtos/{id}/historico", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.ListarHistoricoProdutoHandler(db))))
	// Inativar e reativar um Produto — Story 16.1 (FR-55, AD-37): só
	// `gestor`+ (o 403 é decidido por RequireRole).
	registrar("POST /e/{slug}/api/produtos/{id}/inativacao", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.InativarProdutoHandler(db, registro))))
	registrar("POST /e/{slug}/api/produtos/{id}/reativacao", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelGestor)(
			handlers.ReativarProdutoHandler(db, registro))))

	// Importação em massa via planilha padronizada — Story 3.3 (FR-10). Os 3
	// endpoints ficam atrás de RequireRole(almoxarife), mesmo mínimo de papel
	// do cadastro manual (Story 3.1) — importar em massa é tão restrito quanto
	// cadastrar item a item. POST /api/importacoes recebe o `.xlsx` multipart,
	// valida o cabeçalho fixo e processa tudo sequencialmente na própria
	// requisição (sem SSE, sem worker dedicado). GET /api/importacoes/ultima e
	// POST /api/importacoes/{id}/continuar sustentam a retomada após uma
	// interrupção (rede, navegador fechado, processo derrubado).
	registrar("POST /e/{slug}/api/importacoes", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.CriarImportacaoHandler(db))))
	registrar("GET /e/{slug}/api/importacoes/ultima", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.UltimaImportacaoHandler(db))))
	registrar("POST /e/{slug}/api/importacoes/{id}/continuar", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.ContinuarImportacaoHandler(db))))

	// Upload, armazenamento, listagem e lightbox de foto do Produto —
	// Story 3.5 (FR-27/FR-28) e Story 3.6 (FR-29, listagem/galeria).
	// POST /api/produtos/{id}/fotos fica atrás de RequireRole(almoxarife),
	// mesmo mínimo de papel do cadastro/importação: enviar foto é restrito a
	// `almoxarife`+. GET /api/produtos/{id}/fotos/{arquivo} e
	// GET /api/produtos/{id}/fotos (listagem) levam só RequireAuth —
	// visualização de foto é liberada a qualquer conta autenticada, mesmo
	// padrão de GET /api/categorias/GET /api/estoques. Nenhuma tabela nova: o
	// nome do arquivo é o único vínculo com o Produto.
	registrar("POST /e/{slug}/api/produtos/{id}/fotos", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.EnviarFotoProdutoHandler(db, fotosDir))))
	registrar("GET /e/{slug}/api/produtos/{id}/fotos/{arquivo}", middleware.RequireAuth(db, jwtSecret)(
		handlers.ServirFotoProdutoHandler(db, fotosDir)))
	registrar("GET /e/{slug}/api/produtos/{id}/fotos", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarFotosProdutoHandler(db, fotosDir)))
	// Remover uma foto (trocar = remover + enviar) — mesmo papel mínimo do envio.
	registrar("DELETE /e/{slug}/api/produtos/{id}/fotos/{arquivo}", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.RemoverFotoProdutoHandler(db, fotosDir))))

	// Busca por nome/código/categoria com sugestões — Story 4.1 (FR-4). GET
	// /api/produtos/busca leva só RequireAuth, mesmo padrão de GET
	// /api/categorias/GET /api/estoques: qualquer conta autenticada
	// (`usuario`+) busca, sem RequireRole. Até 7 Produtos ranqueados por
	// relevância; `q` vazio/só espaços -> 400 VALIDATION_ERROR.
	registrar("GET /e/{slug}/api/produtos/busca", middleware.RequireAuth(db, jwtSecret)(
		handlers.BuscarProdutosHandler(db)))

	// Visualização em grade e tabela agrupada do Catálogo — Story 4.3
	// (FR-6), com filtros combináveis por categoria/Estoque/disponibilidade
	// e busca — Story 4.2 (FR-6). GET /api/produtos/catalogo leva só
	// RequireAuth, mesmo padrão de GET /api/produtos/busca: qualquer conta
	// autenticada (`usuario`+), sem RequireRole. Listagem paginada
	// (`TamanhoPaginaCatalogo` fixo) em dois modos: `agrupar=false` (grade,
	// um Produto por linha) e `agrupar=true` (tabela, Produtos de mesmo nome
	// + dimensões colapsados, com a quantidade discriminada por Estoque para
	// a expansão). 4 query params opcionais, combináveis por E lógico entre
	// si e com `agrupar`/`pagina`: `q` (substring em nome/código/categoria,
	// mesmo teto de 255 runes de GET /api/produtos/busca), `categoriaId`,
	// `estoqueId` (id malformado -> resultado vazio, nunca erro) e
	// `comEstoque` (`true`/`false`, sempre soma GLOBAL do Produto — nunca
	// escopado a `estoqueId` na mesma chamada). `pagina` inválida / `agrupar`
	// inválido / `comEstoque` inválido / `q` muito longo -> 400
	// VALIDATION_ERROR.
	registrar("GET /e/{slug}/api/produtos/catalogo", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarCatalogoHandler(db)))

	// Indicadores do Catálogo — Story 17.3 (AD-38). GET
	// /api/produtos/catalogo/indicadores só leva RequireAuth (usuario+),
	// mesmo mínimo de ListarCatalogoHandler — os indicadores refletem o
	// mesmo conjunto filtrado que o usuário já vê na listagem. Segmento de
	// 2 níveis — registrado ANTES de `catalogo/exportar` (mesmo critério de
	// especificidade de `busca`/`catalogo`/`por-codigo`).
	registrar("GET /e/{slug}/api/produtos/catalogo/indicadores", middleware.RequireAuth(db, jwtSecret)(
		handlers.IndicadoresCatalogoHandler(db, fotosDir)))

	// Exportação da tabela do Catálogo para Excel — Story 4.6 (FR-30).
	// GET /api/produtos/catalogo/exportar fica atrás de
	// RequireRole(almoxarife), mesmo mínimo de papel do cadastro/importação:
	// exportar é restrito a `almoxarife`+, decisão do middleware (403 para
	// `usuario`, mesmo em chamada direta à API). Segmento de 2 níveis
	// (`catalogo/exportar`) — não colide com `GET /api/produtos/{id}`
	// abaixo independente de ordem de registro (mesmo caso já provado por
	// `busca`/`catalogo`/`por-codigo`). Mesmos 4 filtros de
	// GET /api/produtos/catalogo (Story 4.2), SEM `pagina`/`agrupar`: sempre
	// exporta a tabela agrupada COMPLETA que casa o filtro, nunca uma
	// página — o `.xlsx` gerado (services.GerarCatalogoXLSX) tem subtotal
	// por grupo e total geral via fórmula `SUBTOTAL`, nunca soma estática,
	// para permanecer correto quando o próprio arquivo já exportado é
	// filtrado no Excel.
	registrar("GET /e/{slug}/api/produtos/catalogo/exportar", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.ExportarCatalogoHandler(db))))

	// Identificação de Produto via QR Code / código de barras — Story 4.5
	// (FR-35). GET /api/produtos/por-codigo?codigo=<valor> leva só
	// RequireAuth, mesmo padrão de GET /api/produtos/busca: qualquer conta
	// autenticada (`usuario`+), sem RequireRole. Resolve o Código de
	// Identificação EXATO lido de um QR Code / código de barras físico para o
	// Produto correspondente. Segmento literal — registrado ANTES de
	// `GET /api/produtos/{id}` abaixo: no mux do Go 1.22 o literal vence o
	// wildcard `{id}` na mesma posição, sem panic de conflito (mesmo caso já
	// provado por `busca`/`catalogo`). `codigo` vazio -> 400 VALIDATION_ERROR;
	// `codigo` sem Produto correspondente -> 404 NOT_FOUND.
	registrar("GET /e/{slug}/api/produtos/por-codigo", middleware.RequireAuth(db, jwtSecret)(
		handlers.BuscarProdutoPorCodigoHandler(db)))

	// Detalhe do Produto por Estoque com atualização em tempo real —
	// Story 4.4 (FR-7). GET /api/produtos/{id} leva só RequireAuth, mesmo
	// padrão de GET /api/produtos/catalogo: qualquer conta autenticada
	// (`usuario`+), sem RequireRole. `id` inexistente/malformado ->
	// 404 NOT_FOUND.
	registrar("GET /e/{slug}/api/produtos/{id}", middleware.RequireAuth(db, jwtSecret)(
		handlers.ObterProdutoHandler(db)))

	// Quem reservou o saldo — Story 11.3 (FR-50). Lista os Pedidos pendentes
	// com reserva ativa do par (Produto, Estoque); só RequireAuth, como o
	// detalhe do Produto. Produto/Estoque alheio/inexistente -> 404.
	registrar("GET /e/{slug}/api/produtos/{id}/estoques/{estoqueId}/reservas", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarReservasSaldoHandler(db)))

	// Registrar Baixa (consumo) — Story 5.1 (Epic 5, Movimentação de
	// Estoque). POST /api/produtos/{id}/estoques/{estoqueId}/baixa fica
	// atrás de RequireRole(almoxarife), mesmo mínimo de papel do
	// cadastro/importação de Produto: registrar consumo é restrito a
	// `almoxarife`+, decisão do middleware (403 para `usuario`, mesmo em
	// chamada direta à API). Debita `produto_estoque.quantidade` e insere a
	// Movimentação `tipo='baixa'` correspondente numa única transação
	// (services.RegistrarBaixa), publicando no canal `movimentacoes` (AD-3
	// do epic-5-context.md) a cada sucesso.
	registrar("POST /e/{slug}/api/produtos/{id}/estoques/{estoqueId}/baixa", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.RegistrarBaixaHandler(db, registro))))

	// Lançamento de saldo com Lote e Data de Validade — Story 11.1 (Epic 11,
	// AD-24, FR-47). POST /api/lotes atrás do MESMO gate de papel de
	// Baixa/Transferência, RequireRole(almoxarife): 403 para `usuario`,
	// decidido pelo middleware. Cria SEMPRE um Lote novo e a Movimentação
	// `entrada` numa única transação (services.LancarSaldo), publicando nos
	// canais `movimentacoes` e `produtos`.
	registrar("POST /e/{slug}/api/lotes", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.LancarSaldoHandler(db, registro))))

	// Registrar Transferência entre Estoques — Story 5.2 (Epic 5,
	// Movimentação de Estoque). POST
	// /api/produtos/{id}/estoques/{estoqueId}/transferencia fica atrás do
	// MESMO gate de papel de Baixa, RequireRole(almoxarife). Debita a linha
	// de origem, credita a de destino e insere a Movimentação
	// `tipo='transferencia'` correspondente numa única transação
	// (services.RegistrarTransferencia), travando as duas linhas de
	// produto_estoque na ordem canônica ascendente de estoque_id (AD-10) —
	// nunca na ordem origem/destino declarada pelo chamador; publicando no
	// canal `movimentacoes` (AD-3 do epic-5-context.md) a cada sucesso.
	registrar("POST /e/{slug}/api/produtos/{id}/estoques/{estoqueId}/transferencia", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.RegistrarTransferenciaHandler(db, registro))))

	// Histórico de Movimentações consultável — Story 5.3 (Epic 5,
	// Movimentação de Estoque). GET /api/movimentacoes fica atrás do MESMO
	// gate de papel de Baixa/Transferência, RequireRole(almoxarife):
	// consultar a trilha é restrito a `almoxarife`+ (403 para `usuario`,
	// mesmo em chamada direta à API), decisão do middleware. Rota só-leitura,
	// sem query params — a regra de consulta (JOIN de Produto/Estoques/autor,
	// ordenação mais-recente-primeiro, teto de 500) vive em
	// services.ListarMovimentacoes. Não há rota de escrita: a trilha é
	// append-only (as Movimentações nascem só de Baixa/Transferência).
	registrar("GET /e/{slug}/api/movimentacoes", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.ListarMovimentacoesHandler(db))))

	// Detecção de inconsistências dimensionais — Story 6.1 (Epic 6,
	// Normalização de Dados). GET /api/normalizacao/inconsistencias fica
	// atrás do MESMO gate de papel de GET /api/movimentacoes,
	// RequireRole(almoxarife): 403 para `usuario`, decisão do middleware.
	// Rota só-leitura, sem query params — a análise (parser tolerante da
	// origem "migracao", heurística de campo único vazio da origem "nome")
	// vive em services.AnalisarInconsistencias. Nenhuma escrita: aplicar/
	// ignorar sugestão é Story 6.2.
	registrar("GET /e/{slug}/api/normalizacao/inconsistencias", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.AnalisarInconsistenciasHandler(db))))

	// Aplicação seletiva de correções — Story 6.2 (Epic 6, Normalização de
	// Dados). MESMO gate de papel de GET /api/normalizacao/inconsistencias,
	// RequireRole(almoxarife). POST /api/normalizacao/correcoes aplica um
	// lote de correções (individual, por produto ou geral — o front-end
	// decide o agrupamento via seleção de checkboxes) e publica no canal
	// `produtos`; POST /api/normalizacao/ignoradas grava a tupla exata
	// (produto,campo,valor) que o Almoxarife decidiu não aplicar, para que
	// AnalisarInconsistencias pare de sugeri-la.
	registrar("POST /e/{slug}/api/normalizacao/correcoes", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.AplicarCorrecoesHandler(db, registro))))
	registrar("POST /e/{slug}/api/normalizacao/ignoradas", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.IgnorarSugestaoHandler(db))))

	// Detecção de duplicatas — Story 6.3 (Epic 6, Normalização de Dados).
	// MESMO gate de papel das outras rotas de Normalização,
	// RequireRole(almoxarife). Rota só-leitura, sem query params — o
	// agrupamento (nome normalizado + dimensões equivalentes + local em
	// comum) vive em services.DetectarDuplicatas. Sem publicação em tempo
	// real: análise sob demanda, nenhum estado persistido para notificar.
	registrar("GET /e/{slug}/api/normalizacao/duplicatas", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.DetectarDuplicatasHandler(db))))

	// Mesclagem de duplicatas com trilha de auditoria — Story 6.4 (Epic 6,
	// Normalização de Dados). MESMO gate de papel das outras rotas de
	// Normalização, RequireRole(almoxarife). services.MesclarDuplicatas
	// revalida o grupo inteiro dentro da transação (nunca confia na lista de
	// ids do cliente) e grava a auditoria permanente; o handler publica os 3
	// eventos em tempo real (produtos updated/deleted + movimentacoes
	// updated) só depois do commit.
	registrar("POST /e/{slug}/api/normalizacao/mesclar", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.MesclarDuplicatasHandler(db, registro))))

	// Carrinho de reserva — Story 7.1 (Epic 7, Pedidos de Retirada). As três
	// rotas ficam atrás só de RequireAuth, SEM RequireRole: qualquer conta
	// autenticada (`usuario`+) monta seu próprio carrinho — mesmo mínimo de
	// papel de GET /api/produtos/{id}. `usuarioID` vem sempre de
	// middleware.UsuarioDaSessao dentro do handler, nunca de um campo do
	// corpo/rota (Always, spec-7-1). Sem publicação em canal SSE (Never,
	// spec-7-1): o carrinho sincroniza só por refetch da própria aba após a
	// própria ação do usuário.
	registrar("POST /e/{slug}/api/carrinho/itens", middleware.RequireAuth(db, jwtSecret)(
		handlers.AdicionarItemCarrinhoHandler(db)))
	registrar("GET /e/{slug}/api/carrinho", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarCarrinhoHandler(db)))
	registrar("DELETE /e/{slug}/api/carrinho/itens/{produtoId}/{estoqueId}", middleware.RequireAuth(db, jwtSecret)(
		handlers.RemoverItemCarrinhoHandler(db)))

	// Envio de Pedido — Story 7.2 (Epic 7, Pedidos de Retirada). Atrás só de
	// RequireAuth, SEM RequireRole: mesmo mínimo de papel do Carrinho — qualquer
	// conta autenticada (`usuario`+) envia seu próprio Pedido. `usuarioID` vem
	// sempre de middleware.UsuarioDaSessao dentro do handler, nunca de um campo
	// do corpo (Always, spec-7-2). Publica no canal `pedidos` (AD-3) no sucesso.
	registrar("POST /e/{slug}/api/pedidos", middleware.RequireAuth(db, jwtSecret)(
		handlers.SubmeterPedidoHandler(db, registro)))

	// Consulta de Pedidos próprios — Story 7.3 (Epic 7, Pedidos de Retirada).
	// Mesma composição só com RequireAuth do POST acima (mux Go 1.22: os três
	// padrões coexistem). GET /api/pedidos lista os Pedidos do próprio usuário
	// da sessão (filtrável por ?status=); GET /api/pedidos/{id} devolve
	// cabeçalho + itens em snapshot, liberado ao dono ou a `almoxarife`+ pelo
	// padrão de escopo AD-8. Esta story só CONSOME o canal SSE `pedidos` — não
	// publica nada.
	registrar("GET /e/{slug}/api/pedidos", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarPedidosHandler(db)))
	// Indicadores de Pedidos — Story 17.4 (Epic 17, Novo visual). Registrado
	// ANTES de GET /api/pedidos/{id} para evitar conflito de path com o
	// padrão /{id}. Mesmo RequireAuth do GET acima; escopo por papel resolvido
	// no service, nunca 403.
	registrar("GET /e/{slug}/api/pedidos/indicadores", middleware.RequireAuth(db, jwtSecret)(
		handlers.IndicadoresPedidosHandler(db)))
	registrar("GET /e/{slug}/api/pedidos/{id}", middleware.RequireAuth(db, jwtSecret)(
		handlers.BuscarPedidoHandler(db)))

	// Recibo do Pedido em PDF gerado pelo servidor — Story 7.6 (Epic 7,
	// Pedidos de Retirada), spec-7-6. Molde EXATO de GET /api/pedidos/{id}
	// acima: atrás SÓ de RequireAuth, SEM RequireRole — mesmo padrão de
	// escopo AD-8 (dono OU `almoxarife`+). Pedido ainda não decidido
	// (`pendente`/`rejeitado`) devolve 409 CONFLICT, sem gerar PDF nenhum.
	registrar("GET /e/{slug}/api/pedidos/{id}/recibo", middleware.RequireAuth(db, jwtSecret)(
		handlers.BaixarReciboPedidoHandler(db)))

	// Aprovação/rejeição com revalidação de estoque item a item — Story 7.5
	// (Epic 7, Pedidos de Retirada), spec-7-5. Molde EXATO de POST
	// /api/estoques (linha ~369): RequireAuth + RequireRole(almoxarife) —
	// RequireRole roda a CADA requisição, nunca cacheado, o que já satisfaz
	// "papel do aprovador revalidado na submissão da decisão" só por
	// composição. Publica no canal `pedidos` (novo status) no sucesso.
	registrar("POST /e/{slug}/api/pedidos/{id}/decisao", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.DecidirPedidoHandler(db, registro))))

	// Exportação dos próprios dados pessoais — Story 8.1 (Epic 8,
	// Privacidade/LGPD), spec-8-1. Atrás SÓ de RequireAuth — SEM
	// RequireRole: qualquer papel autenticado exporta os PRÓPRIOS dados,
	// nunca os de terceiros (isso é a Story 8.2, anonimização, fora de
	// escopo aqui).
	registrar("GET /e/{slug}/api/usuarios/me/exportar-dados", middleware.RequireAuth(db, jwtSecret)(
		handlers.ExportarDadosUsuarioHandler(db)))

	// Exclusão e anonimização de dados pessoais por Adm — Story 8.2 (Epic 8,
	// Privacidade/LGPD), spec-8-2. Exclusão baseada em solicitação, nunca
	// self-service: POST /api/usuarios/me/solicitacao-exclusao fica atrás SÓ
	// de RequireAuth — qualquer papel autenticado REGISTRA a solicitação da
	// PRÓPRIA conta (a rota `me` só registra, nunca anonimiza). GET
	// /api/solicitacoes-exclusao e POST
	// /api/solicitacoes-exclusao/{id}/processamento ficam atrás de
	// RequireAuth + RequireRole(adm) — mesmo gate de GET /api/logs-acesso: só
	// um `adm` lista a fila e anonimiza. A anonimização reescreve apenas
	// nome/email/credenciais na linha de `usuarios`; nenhuma linha de
	// `movimentacoes`/`pedidos`/`logs_acesso` é tocada.
	registrar("POST /e/{slug}/api/usuarios/me/solicitacao-exclusao", middleware.RequireAuth(db, jwtSecret)(
		handlers.SolicitarExclusaoContaHandler(db)))
	registrar("GET /e/{slug}/api/solicitacoes-exclusao", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.ListarSolicitacoesExclusaoHandler(db))))
	registrar("POST /e/{slug}/api/solicitacoes-exclusao/{id}/processamento", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAdm)(
			handlers.ProcessarExclusaoContaHandler(db))))

	// Infraestrutura de tempo real (AD-2/AD-3) — Story 4.4. POST
	// /api/realtime/ticket leva só RequireAuth: qualquer conta autenticada
	// pode abrir sua própria conexão SSE. GET /api/realtime/stream é o único
	// endpoint autenticado do produto que NÃO leva RequireAuth — um
	// `EventSource` do navegador nunca envia o header `Authorization`, então
	// a autenticação acontece pelo próprio ticket (uso único, TTL 30s) na
	// query string; StreamRealtimeHandler revalida o usuário por trás do
	// ticket (services.BuscarUsuarioSessao) antes de promover a resposta a
	// `text/event-stream` — mesma defesa em profundidade de RequireAuth.
	registrar("POST /e/{slug}/api/realtime/ticket", middleware.RequireAuth(db, jwtSecret)(
		handlers.EmitirTicketRealtimeHandler(db)))
	registrar("GET /e/{slug}/api/realtime/stream", handlers.StreamRealtimeHandler(db, registro))

	// Login federado via Keycloak — SSO Ferreira Costa (Story 1.9, AD-7).
	// /api/auth/sso/config e /api/auth/logout são SEMPRE registrados (o
	// primeiro responde {"enabled":false} sem config; o segundo é o único
	// caminho de logout do produto, inclusive para o login por senha). A troca
	// de token só existe quando o realm está configurado, sempre atrás do
	// middleware `iam`.
	registrar("GET /e/{slug}/api/auth/sso/config", handlers.SSOConfigHandler(iamCfg))
	registrar("POST /e/{slug}/api/auth/logout", handlers.LogoutHandler(db))
	if iamCfg.Habilitado() {
		jwks := iam.NewJWKSClient(iamCfg.RealmURL+"/protocol/openid-connect/certs", time.Hour)
		if len(iamCfg.AllowedClientIDs) == 0 {
			slog.Warn("SSO habilitado mas IAM_ALLOWED_CLIENT_IDS vazio — todo login SSO falhará no azp")
		}
		registrar("POST /e/{slug}/api/auth/sso/keycloak",
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
