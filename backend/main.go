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
// `SUBTOTAL`, nunca soma estática).
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

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
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
	mux.HandleFunc("POST /api/estoques", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.CriarEstoqueHandler(db))))
	mux.HandleFunc("GET /api/estoques", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarEstoquesHandler(db)))
	mux.HandleFunc("DELETE /api/estoques/{id}", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.ExcluirEstoqueHandler(db))))

	// Cadastro manual de Produto com dimensões estruturadas — Story 3.1
	// (FR-8). POST /api/produtos fica atrás de RequireRole(almoxarife): criar
	// Produto é restrito a `almoxarife`+, decisão do middleware (403 para
	// papéis abaixo, mesmo em chamada direta à API). GET /api/categorias leva
	// só RequireAuth — a lista fixa de categorias é liberada a qualquer conta
	// autenticada, mesmo padrão de GET /api/estoques (o formulário de cadastro
	// e as telas de catálogo do Epic 4 precisam dela).
	mux.HandleFunc("POST /api/produtos", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.CriarProdutoHandler(db, registro))))
	mux.HandleFunc("GET /api/categorias", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarCategoriasHandler(db)))

	// Nomenclatura Guiada por subtipo — Story 3.2 (FR-9). GET
	// /api/nomenclatura-templates leva só RequireAuth, mesmo padrão de GET
	// /api/categorias — a lista fixa dos 28 templates é liberada a qualquer
	// conta autenticada (o formulário de cadastro precisa dela). POST
	// /api/produtos/{id}/renomear fica atrás de RequireRole(almoxarife): é o
	// único endpoint de edição de Produto que existe hoje, restrito a `nome`,
	// mesmo mínimo de papel do cadastro.
	mux.HandleFunc("GET /api/nomenclatura-templates", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarNomenclaturaTemplatesHandler(db)))
	mux.HandleFunc("POST /api/produtos/{id}/renomear", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.AtualizarNomeProdutoHandler(db, registro))))

	// Importação em massa via planilha padronizada — Story 3.3 (FR-10). Os 3
	// endpoints ficam atrás de RequireRole(almoxarife), mesmo mínimo de papel
	// do cadastro manual (Story 3.1) — importar em massa é tão restrito quanto
	// cadastrar item a item. POST /api/importacoes recebe o `.xlsx` multipart,
	// valida o cabeçalho fixo e processa tudo sequencialmente na própria
	// requisição (sem SSE, sem worker dedicado). GET /api/importacoes/ultima e
	// POST /api/importacoes/{id}/continuar sustentam a retomada após uma
	// interrupção (rede, navegador fechado, processo derrubado).
	mux.HandleFunc("POST /api/importacoes", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.CriarImportacaoHandler(db))))
	mux.HandleFunc("GET /api/importacoes/ultima", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.UltimaImportacaoHandler(db))))
	mux.HandleFunc("POST /api/importacoes/{id}/continuar", middleware.RequireAuth(db, jwtSecret)(
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
	mux.HandleFunc("POST /api/produtos/{id}/fotos", middleware.RequireAuth(db, jwtSecret)(
		middleware.RequireRole(services.PapelAlmoxarife)(
			handlers.EnviarFotoProdutoHandler(db, fotosDir))))
	mux.HandleFunc("GET /api/produtos/{id}/fotos/{arquivo}", middleware.RequireAuth(db, jwtSecret)(
		handlers.ServirFotoProdutoHandler(fotosDir)))
	mux.HandleFunc("GET /api/produtos/{id}/fotos", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarFotosProdutoHandler(db, fotosDir)))

	// Busca por nome/código/categoria com sugestões — Story 4.1 (FR-4). GET
	// /api/produtos/busca leva só RequireAuth, mesmo padrão de GET
	// /api/categorias/GET /api/estoques: qualquer conta autenticada
	// (`usuario`+) busca, sem RequireRole. Até 7 Produtos ranqueados por
	// relevância; `q` vazio/só espaços -> 400 VALIDATION_ERROR.
	mux.HandleFunc("GET /api/produtos/busca", middleware.RequireAuth(db, jwtSecret)(
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
	mux.HandleFunc("GET /api/produtos/catalogo", middleware.RequireAuth(db, jwtSecret)(
		handlers.ListarCatalogoHandler(db)))

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
	mux.HandleFunc("GET /api/produtos/catalogo/exportar", middleware.RequireAuth(db, jwtSecret)(
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
	mux.HandleFunc("GET /api/produtos/por-codigo", middleware.RequireAuth(db, jwtSecret)(
		handlers.BuscarProdutoPorCodigoHandler(db)))

	// Detalhe do Produto por Estoque com atualização em tempo real —
	// Story 4.4 (FR-7). GET /api/produtos/{id} leva só RequireAuth, mesmo
	// padrão de GET /api/produtos/catalogo: qualquer conta autenticada
	// (`usuario`+), sem RequireRole. `id` inexistente/malformado ->
	// 404 NOT_FOUND.
	mux.HandleFunc("GET /api/produtos/{id}", middleware.RequireAuth(db, jwtSecret)(
		handlers.ObterProdutoHandler(db)))

	// Infraestrutura de tempo real (AD-2/AD-3) — Story 4.4. POST
	// /api/realtime/ticket leva só RequireAuth: qualquer conta autenticada
	// pode abrir sua própria conexão SSE. GET /api/realtime/stream é o único
	// endpoint autenticado do produto que NÃO leva RequireAuth — um
	// `EventSource` do navegador nunca envia o header `Authorization`, então
	// a autenticação acontece pelo próprio ticket (uso único, TTL 30s) na
	// query string; StreamRealtimeHandler revalida o usuário por trás do
	// ticket (services.BuscarUsuarioSessao) antes de promover a resposta a
	// `text/event-stream` — mesma defesa em profundidade de RequireAuth.
	mux.HandleFunc("POST /api/realtime/ticket", middleware.RequireAuth(db, jwtSecret)(
		handlers.EmitirTicketRealtimeHandler(db)))
	mux.HandleFunc("GET /api/realtime/stream", handlers.StreamRealtimeHandler(db, registro))

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
