// Package realtime é o mecanismo puro de fan-out em memória da
// infraestrutura de tempo real (Story 4.4, spec-4-4, AD-2/AD-3): um
// broadcaster in-process (sem Redis/RabbitMQ/WebSocket), só correto com
// instância única da aplicação. Deliberadamente SEM import de
// `database/sql` — a gestão do ticket de conexão (linha em `tokens_acao`) é
// responsabilidade de `services/realtime.go`, nunca deste pacote (ver
// Design Notes da spec: a Structural Seed lista "tickets de conexão" no
// mesmo pacote `realtime/`, mas `epic-4-context.md` fixa a regra mais
// específica "nenhum acesso a banco fora de services/").
package realtime

import "sync"

// canaisValidos são os 4 canais fixos previstos pela AD-3 (Structural
// Seed) — só `produtos` é publicado por esta story; `estoques`/
// `movimentacoes`/`pedidos` ficam prontos para os Épicos 5-7 sem exigir um
// novo endpoint/Registry.
var canaisValidos = map[string]bool{
	"produtos":      true,
	"estoques":      true,
	"movimentacoes": true,
	"pedidos":       true,
}

// Evento é o envelope fixo publicado num canal (AD-3): payload sempre
// mínimo — o cliente sempre rebusca via GET, nunca confia neste payload
// como dado atual. `Resource` é preenchido por Publish a partir do `canal`
// informado, nunca pelo chamador.
type Evento struct {
	Resource string `json:"resource"`
	ID       string `json:"id"`
	Change   string `json:"change"`
}

// Registry é o fan-out in-process de eventos SSE, particionado POR EMPRESA
// (Story 9.1, spec-9-1): cada assinante entra numa partição identificada
// pelo `empresaID` que o middleware resolveu do slug da URL, e um evento
// publicado para uma Empresa NUNCA alcança um assinante de outra — antes
// desta story, todo assinante do processo recebia todo evento, o que
// vazaria a atividade de um cliente para outro assim que existisse mais de
// uma Empresa.
//
// Dentro de uma Empresa o comportamento é o de antes: Subscribe não filtra
// por CANAL (GET /e/{slug}/api/realtime/stream não recebe parâmetro de
// canal na URL, AD-3) — cada assinante recebe os eventos de QUALQUER canal
// da sua Empresa e filtra client-side por `resource`.
type Registry struct {
	mu sync.Mutex
	// subs é indexado por `empresaID` — a partição de fan-out. Uma partição
	// vazia é removida do mapa quando seu último assinante cancela, para que
	// o mapa não cresça indefinidamente com Empresas sem conexão aberta.
	subs map[string]map[chan Evento]struct{}
}

// NewRegistry devolve um Registry vazio, pronto para uso — `newMux` cria
// uma única instância por processo e a compartilha entre os handlers que
// publicam (produtos.go) e o que assina (realtime.go, StreamRealtimeHandler).
func NewRegistry() *Registry {
	return &Registry{subs: make(map[string]map[chan Evento]struct{})}
}

// eventoBufferSize é a capacidade do canal de cada assinante: uma folga
// pequena (não elimina, só reduz) contra a corrida entre "publicar" e "o
// select do assinante estar bloqueado esperando" — não é replay nem
// garantia de entrega (AD-3 não exige nenhuma das duas), só absorve o caso
// comum de um evento chegar entre duas iterações do loop de leitura.
const eventoBufferSize = 4

// Publish envia `evento` (com `Resource` sobrescrito para `canal`) a todos
// os assinantes DA EMPRESA `empresaID` (Story 9.1) — nenhum outro. Um
// `empresaID` sem nenhum assinante é um no-op silencioso.
//
// Não-bloqueante: um assinante lento (canal cheio) perde o evento — nunca
// trava o produtor, para que uma aba lenta/travada nunca atrase uma escrita
// de Produto no resto do sistema. `canal` fora do conjunto fixo
// {produtos,estoques,movimentacoes,pedidos} -> panic, fail-fast (mesmo
// padrão de middleware.RequireRole em papel desconhecido).
func (r *Registry) Publish(empresaID string, canal string, evento Evento) {
	if !canaisValidos[canal] {
		panic("realtime: canal inválido: " + canal)
	}
	evento.Resource = canal

	r.mu.Lock()
	defer r.mu.Unlock()
	for ch := range r.subs[empresaID] {
		select {
		case ch <- evento:
		default:
			// assinante lento — evento perdido, nunca trava o produtor.
		}
	}
}

// Subscribe registra um novo assinante NA PARTIÇÃO da Empresa `empresaID`
// (Story 9.1) e devolve o canal de leitura dos eventos daquela Empresa
// (todos os canais dela, sem filtro de canal) mais uma função de
// cancelamento — o chamador (StreamRealtimeHandler) invoca o cancelamento
// quando o cliente desconecta (r.Context().Done()), sempre via `defer`.
// Chamar o cancelamento mais de uma vez é seguro (idempotente).
func (r *Registry) Subscribe(empresaID string) (<-chan Evento, func()) {
	ch := make(chan Evento, eventoBufferSize)

	r.mu.Lock()
	if r.subs[empresaID] == nil {
		r.subs[empresaID] = make(map[chan Evento]struct{})
	}
	r.subs[empresaID][ch] = struct{}{}
	r.mu.Unlock()

	var uma sync.Once
	cancelar := func() {
		uma.Do(func() {
			// delete+close sob o MESMO mutex que Publish segura durante todo
			// o loop de fan-out: garante que nenhum Publish em andamento
			// ainda está no meio de um `select` sobre `ch` quando ele é
			// fechado (Publish só solta o lock depois de terminar o loop
			// inteiro) — sem isto, close(ch) concorrente com `ch <- evento`
			// faria Publish entrar em panic.
			r.mu.Lock()
			delete(r.subs[empresaID], ch)
			if len(r.subs[empresaID]) == 0 {
				delete(r.subs, empresaID)
			}
			close(ch)
			r.mu.Unlock()
		})
	}
	return ch, cancelar
}
