import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { conectarRealtime, type EventoRealtime, type StatusRealtime } from './client';

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

/**
 * FakeEventSource sobrescreve `window.EventSource` (jsdom não implementa a
 * API) — mesmo espírito de sobrescrever `window.matchMedia` localmente já
 * usado pelos testes de tabela da Story 4.3 (ver App.tsx/CatalogoListagem
 * — nenhuma lib nova). `instancias` deixa o teste inspecionar a URL de cada
 * conexão aberta e disparar `open`/`message`/`error` manualmente via
 * `addEventListener` (o cliente real usa `addEventListener`, não os
 * `on<evento>`, por convenção de lint do projeto).
 */
class FakeEventSource {
  static instancias: FakeEventSource[] = [];
  url: string;
  closed = false;
  private ouvintes: Record<string, ((evento: MessageEvent) => void)[]> = {};

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instancias.push(this);
  }

  addEventListener(tipo: string, ouvinte: (evento: MessageEvent) => void) {
    (this.ouvintes[tipo] ??= []).push(ouvinte);
  }

  disparar(tipo: string, evento: Partial<MessageEvent> = {}) {
    for (const ouvinte of this.ouvintes[tipo] ?? []) {
      ouvinte(evento as MessageEvent);
    }
  }

  close() {
    this.closed = true;
  }
}

function ultimaInstancia(): FakeEventSource {
  const instancia = FakeEventSource.instancias.at(-1);
  if (!instancia) {
    throw new Error('nenhuma FakeEventSource foi criada ainda');
  }
  return instancia;
}

function stubFetchTicket(sequenciaDeTickets: (string | null)[]) {
  let chamada = 0;
  const fetchMock = vi.fn(() => {
    const ticket = sequenciaDeTickets[Math.min(chamada, sequenciaDeTickets.length - 1)];
    chamada += 1;
    if (ticket === null) {
      return Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
    }
    return Promise.resolve({ ok: true, json: async () => ({ ticket }) });
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

beforeEach(() => {
  FakeEventSource.instancias = [];
  vi.stubGlobal('EventSource', FakeEventSource as unknown as typeof EventSource);
  vi.useFakeTimers();
  // O backoff de retry (ver client.ts) soma um jitter aleatório ao delay —
  // zera o jitter aqui para que os tempos de retry nos testes permaneçam
  // determinísticos (1000ms, 2000ms, 4000ms, ...).
  vi.spyOn(Math, 'random').mockReturnValue(0);
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('conectarRealtime', () => {
  it('emite um ticket, abre o EventSource com o ticket na query string e chama aoMudarStatus("conectado") no onopen', async () => {
    const fetchMock = stubFetchTicket(['ticket-abc']);
    const aoReceberEvento = vi.fn();
    const aoMudarStatus = vi.fn();

    const desconectar = conectarRealtime(aoReceberEvento, aoMudarStatus);
    await vi.advanceTimersByTimeAsync(0);

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/realtime/ticket',
      expect.objectContaining({ method: 'POST' }),
    );
    const es = ultimaInstancia();
    expect(es.url).toBe('/api/realtime/stream?ticket=ticket-abc');

    es.disparar('open');
    expect(aoMudarStatus).toHaveBeenCalledWith('conectado');

    desconectar();
  });

  it('repassa o evento decodificado via JSON.parse ao onmessage', async () => {
    stubFetchTicket(['ticket-abc']);
    const aoReceberEvento = vi.fn();
    const aoMudarStatus = vi.fn();

    const desconectar = conectarRealtime(aoReceberEvento, aoMudarStatus);
    await vi.advanceTimersByTimeAsync(0);

    const es = ultimaInstancia();
    const evento: EventoRealtime = { resource: 'produtos', id: 'p1', change: 'updated' };
    es.disparar('message', { data: JSON.stringify(evento) });

    expect(aoReceberEvento).toHaveBeenCalledWith(evento);

    desconectar();
  });

  it('reconexão rápida (<3s): nenhum status "reconectando" é emitido', async () => {
    stubFetchTicket(['ticket-1', 'ticket-2']);
    const aoMudarStatus = vi.fn();

    const desconectar = conectarRealtime(vi.fn(), aoMudarStatus);
    await vi.advanceTimersByTimeAsync(0);
    ultimaInstancia().disparar('open');
    aoMudarStatus.mockClear();

    // Conexão cai — o handler fecha o EventSource atual e agenda uma nova
    // tentativa em 1000ms.
    ultimaInstancia().disparar('error');
    expect(ultimaInstancia().closed).toBe(true);

    await vi.advanceTimersByTimeAsync(1000); // dispara o retry -> novo ticket + EventSource
    // Reconecta ANTES do limiar de 3s (só ~1s desde o erro).
    ultimaInstancia().disparar('open');

    expect(aoMudarStatus).not.toHaveBeenCalledWith('reconectando');
    expect(aoMudarStatus).toHaveBeenCalledWith('conectado');

    desconectar();
  });

  it('reconexão lenta (>=3s): "reconectando" aparece antes de suceder', async () => {
    stubFetchTicket(['ticket-1', 'ticket-2', 'ticket-3', 'ticket-4', 'ticket-5']);
    const aoMudarStatus = vi.fn();

    const desconectar = conectarRealtime(vi.fn(), aoMudarStatus);
    await vi.advanceTimersByTimeAsync(0);
    ultimaInstancia().disparar('open');
    aoMudarStatus.mockClear();

    ultimaInstancia().disparar('error');

    // Avança 3s inteiros (passando por vários ciclos de retry de 1000ms) sem
    // nenhuma reconexão bem-sucedida — o limiar de 3000ms deve disparar.
    await vi.advanceTimersByTimeAsync(3000);

    expect(aoMudarStatus).toHaveBeenCalledWith('reconectando');

    // Só então a conexão volta.
    aoMudarStatus.mockClear();
    ultimaInstancia().disparar('open');
    expect(aoMudarStatus).toHaveBeenCalledWith('conectado');
    expect(aoMudarStatus).not.toHaveBeenCalledWith('reconectando');

    desconectar();
  });

  it('onerror nunca deixa o EventSource antigo aberto — fecha antes de tentar de novo', async () => {
    stubFetchTicket(['ticket-1', 'ticket-2']);
    const desconectar = conectarRealtime(vi.fn(), vi.fn());
    await vi.advanceTimersByTimeAsync(0);

    const primeira = ultimaInstancia();
    primeira.disparar('error');
    expect(primeira.closed).toBe(true);

    await vi.advanceTimersByTimeAsync(1000);
    const segunda = ultimaInstancia();
    expect(segunda).not.toBe(primeira);
    expect(segunda.url).toBe('/api/realtime/stream?ticket=ticket-2');

    desconectar();
  });

  it('falha ao emitir o ticket (resposta não-OK) também aciona o ciclo de retry', async () => {
    const fetchMock = stubFetchTicket([null, 'ticket-2']);
    const aoMudarStatus = vi.fn();

    const desconectar = conectarRealtime(vi.fn(), aoMudarStatus);
    await vi.advanceTimersByTimeAsync(0);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(FakeEventSource.instancias).toHaveLength(0);

    await vi.advanceTimersByTimeAsync(1000);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(FakeEventSource.instancias).toHaveLength(1);
    expect(ultimaInstancia().url).toBe('/api/realtime/stream?ticket=ticket-2');

    desconectar();
  });

  it('retry usa backoff progressivo (1s, 2s, 4s, ...) até um teto de 5s, resetado ao reconectar', async () => {
    stubFetchTicket([null, null, null, null, 'ticket-ok']);
    const desconectar = conectarRealtime(vi.fn(), vi.fn());
    await vi.advanceTimersByTimeAsync(0); // 1ª tentativa (falha) -> agenda retry em 1000ms

    // 2ª tentativa: só depois de 1000ms (não antes).
    await vi.advanceTimersByTimeAsync(999);
    expect(FakeEventSource.instancias).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1); // completa os 1000ms -> 2ª tentativa (falha)

    // 3ª tentativa: só depois de mais 2000ms (backoff dobrou).
    await vi.advanceTimersByTimeAsync(1999);
    expect(FakeEventSource.instancias).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1); // completa os 2000ms -> 3ª tentativa (falha)

    // 4ª tentativa: só depois de mais 4000ms (backoff dobrou de novo).
    await vi.advanceTimersByTimeAsync(3999);
    expect(FakeEventSource.instancias).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1); // completa os 4000ms -> 4ª tentativa (falha)

    // 5ª tentativa: o backoff não passa do teto de 5000ms (nunca 8000ms).
    await vi.advanceTimersByTimeAsync(4999);
    expect(FakeEventSource.instancias).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1); // completa os 5000ms -> 5ª tentativa (sucesso)

    expect(FakeEventSource.instancias).toHaveLength(1);
    expect(ultimaInstancia().url).toBe('/api/realtime/stream?ticket=ticket-ok');

    desconectar();
  });

  it('a função de cleanup fecha a conexão atual e cancela temporizadores pendentes', async () => {
    stubFetchTicket(['ticket-1', 'ticket-2']);
    const aoMudarStatus = vi.fn();

    const desconectar = conectarRealtime(vi.fn(), aoMudarStatus);
    await vi.advanceTimersByTimeAsync(0);
    const es = ultimaInstancia();
    es.disparar('error'); // agenda o temporizador de "reconectando" (3s) + retry (1s)

    desconectar();
    expect(es.closed).toBe(true);

    aoMudarStatus.mockClear();
    await vi.advanceTimersByTimeAsync(5000);
    // Nenhum novo EventSource, nenhum novo status — tudo foi cancelado.
    expect(FakeEventSource.instancias).toHaveLength(1);
    expect(aoMudarStatus).not.toHaveBeenCalled();
  });

  it('a primeira conexão já dispara aoMudarStatus("conectado") — mesmo caminho de uma reconexão', async () => {
    stubFetchTicket(['ticket-abc']);
    const status: StatusRealtime[] = [];
    const desconectar = conectarRealtime(vi.fn(), (s) => status.push(s));

    await vi.advanceTimersByTimeAsync(0);
    ultimaInstancia().disparar('open');

    expect(status).toEqual(['conectado']);
    desconectar();
  });
});
