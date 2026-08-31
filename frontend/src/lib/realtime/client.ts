import { getAccessToken } from '@/lib/session';

/**
 * Cliente da infraestrutura de tempo real (Story 4.4, spec-4-4, AD-3):
 * `conectarRealtime` é o único ponto de entrada — emite um ticket de curta
 * duração (`POST /api/realtime/ticket`), abre um `EventSource` contra
 * `GET /api/realtime/stream?ticket=<ticket>` e mantém a conexão viva,
 * reconectando sozinho quando ela cai.
 *
 * `aoMudarStatus('conectado')` dispara em TODA conexão bem-sucedida —
 * inclusive a primeira — nunca só nas reconexões: é o único gatilho que o
 * chamador deve usar para refazer o GET completo do estado atual (AD-3:
 * "sempre GET completo ao reconectar", nunca espera replay de eventos
 * perdidos; unificar os dois caminhos evita duas responsabilidades
 * divergentes para a mesma coisa).
 *
 * `onerror` do `EventSource` NUNCA é deixado reconectar sozinho (o retry
 * nativo reenviaria o MESMO ticket, já consumido, e falharia para sempre) —
 * este módulo sempre fecha a conexão corrente e pede um ticket novo antes de
 * tentar de novo. O retry usa backoff progressivo (1000ms, 2000ms, 4000ms,
 * ... até um teto de ~5000ms, com jitter) em vez de um intervalo fixo: como
 * este é um app single-instance (AD-3), um restart do servidor derrubaria
 * TODOS os clientes conectados ao mesmo tempo — um retry fixo de 1000ms
 * faria todo mundo martelar `POST /api/realtime/ticket` (grava no banco) em
 * lockstep, indefinidamente. O contador de tentativas é resetado a cada
 * "episódio de queda" (zera assim que uma conexão abre com sucesso). Um
 * temporizador de 3000ms, armado uma única vez por episódio de queda (não
 * reiniciado a cada nova tentativa malsucedida dentro da mesma queda),
 * dispara `aoMudarStatus('reconectando')` só se a reconexão ainda não tiver
 * sucedido até lá — uma reconexão em menos de 3s permanece silenciosa
 * (UX-DR18).
 */

export interface EventoRealtime {
  resource: string;
  id: string;
  change: string;
}

export type StatusRealtime = 'conectado' | 'reconectando';

const LIMIAR_RECONECTANDO_MS = 3000;
const INTERVALO_RETRY_BASE_MS = 1000;
const INTERVALO_RETRY_MAX_MS = 5000;
const JITTER_RETRY_MAX_MS = 250;

// calcularDelayRetry: backoff exponencial (base * 2^tentativas) com teto de
// INTERVALO_RETRY_MAX_MS, mais um jitter aleatório de até JITTER_RETRY_MAX_MS
// — evita que todos os clientes de um restart do servidor martelem o
// endpoint de ticket exatamente no mesmo instante em lockstep.
function calcularDelayRetry(tentativas: number): number {
  const exponencial = INTERVALO_RETRY_BASE_MS * 2 ** tentativas;
  const base = Math.min(INTERVALO_RETRY_MAX_MS, exponencial);
  const jitter = Math.random() * JITTER_RETRY_MAX_MS;
  return base + jitter;
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

export function conectarRealtime(
  aoReceberEvento: (evento: EventoRealtime) => void,
  aoMudarStatus: (status: StatusRealtime) => void,
): () => void {
  let cancelado = false;
  let eventSourceAtual: EventSource | null = null;
  let temporizadorReconectando: ReturnType<typeof setTimeout> | null = null;
  let temporizadorRetry: ReturnType<typeof setTimeout> | null = null;
  // avisouReconectando evita reemitir 'reconectando' repetidamente durante a
  // MESMA queda (cada tentativa malsucedida dispara onerror de novo; sem
  // este guard, o temporizador de 3s reiniciaria — ou reemitiria o status —
  // a cada nova falha, em vez de avisar uma única vez até reconectar).
  let avisouReconectando = false;
  // tentativasRetry conta as tentativas malsucedidas dentro do episódio de
  // queda atual — alimenta o backoff progressivo e é zerado assim que uma
  // conexão abre com sucesso.
  let tentativasRetry = 0;

  function fecharConexaoAtual() {
    if (eventSourceAtual) {
      eventSourceAtual.close();
      eventSourceAtual = null;
    }
  }

  function cancelarTemporizadorReconectando() {
    if (temporizadorReconectando) {
      clearTimeout(temporizadorReconectando);
      temporizadorReconectando = null;
    }
  }

  // registrarFalha é chamado em QUALQUER ponto de falha (ticket não emitido,
  // rede indisponível, ou o próprio EventSource entrando em erro): arma o
  // temporizador de 3s (se ainda não armado nem já avisado nesta queda) e
  // agenda a próxima tentativa com backoff progressivo.
  function registrarFalha() {
    if (cancelado) return;

    if (!temporizadorReconectando && !avisouReconectando) {
      temporizadorReconectando = setTimeout(() => {
        temporizadorReconectando = null;
        if (!cancelado) {
          avisouReconectando = true;
          aoMudarStatus('reconectando');
        }
      }, LIMIAR_RECONECTANDO_MS);
    }

    if (!temporizadorRetry) {
      const delay = calcularDelayRetry(tentativasRetry);
      tentativasRetry += 1;
      temporizadorRetry = setTimeout(() => {
        temporizadorRetry = null;
        void abrirConexao();
      }, delay);
    }
  }

  async function abrirConexao() {
    if (cancelado) return;

    let ticket: string;
    try {
      const res = await fetch('/api/realtime/ticket', {
        method: 'POST',
        headers: authHeaders(),
      });
      if (cancelado) return;
      if (!res.ok) {
        registrarFalha();
        return;
      }
      const data = (await res.json()) as { ticket?: string };
      if (cancelado) return;
      if (!data.ticket) {
        registrarFalha();
        return;
      }
      ticket = data.ticket;
    } catch {
      if (!cancelado) {
        registrarFalha();
      }
      return;
    }

    const es = new EventSource(`/api/realtime/stream?ticket=${encodeURIComponent(ticket)}`);
    eventSourceAtual = es;

    es.addEventListener('open', () => {
      if (cancelado) return;
      cancelarTemporizadorReconectando();
      avisouReconectando = false;
      tentativasRetry = 0;
      aoMudarStatus('conectado');
    });

    es.addEventListener('message', (evento) => {
      if (cancelado) return;
      try {
        aoReceberEvento(JSON.parse(evento.data) as EventoRealtime);
      } catch {
        // Payload inesperado — ignorado, nunca derruba a conexão por causa
        // de um único evento malformado.
      }
    });

    es.addEventListener('error', () => {
      if (cancelado) return;
      // Fecha explicitamente: nunca deixa o retry nativo do EventSource
      // reenviar o MESMO ticket já consumido pela query string original.
      fecharConexaoAtual();
      registrarFalha();
    });
  }

  void abrirConexao();

  return () => {
    cancelado = true;
    cancelarTemporizadorReconectando();
    if (temporizadorRetry) {
      clearTimeout(temporizadorRetry);
      temporizadorRetry = null;
    }
    fecharConexaoAtual();
  };
}
