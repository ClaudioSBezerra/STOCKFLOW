import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ArrowLeftRight } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { FaixaIndicadores } from '@/components/lista/FaixaIndicadores';
import { PilhulaStatus } from '@/components/lista/PilhulaStatus';
import { conectarRealtime, type StatusRealtime } from '@/lib/realtime/client';
import { formatarQuantidade } from '@/components/catalogo/formatacao';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Seção "Movimentações" (Story 5.3; padrão de lista na 17.5). Lista
 * SOMENTE-LEITURA de `GET /api/movimentacoes`: filtros de período (padrão
 * Últimos 30 dias), tipo e estoque enviados ao servidor (lista e indicadores
 * com os mesmos parâmetros), "Limpar filtros", faixa Baixas/Transferências
 * (`/api/movimentacoes/indicadores`; "—" se falhar) e linhas de 60px — ícone,
 * Produto + "origem → destino", tipo em pílula, quantidade, autor e data.
 * NENHUMA ação por linha: a trilha é append-only.
 *
 * `seqRef` descarta respostas fora de ordem; aviso de teto de 500. Tempo real
 * (molde de `ProdutoDetalhePage`): a carga inicial E o refetch pós-reconexão
 * são o MESMO caminho (`aoMudarStatus('conectado')`, que dispara também na 1ª
 * conexão). Um evento SSE `resource === 'movimentacoes'` dispara
 * `toast.info('Movimentações atualizada.')` + refetch de lista e indicadores;
 * o dado antigo permanece visível até a resposta chegar. Status
 * `'reconectando'` mostra `<output aria-live="polite">Reconectando...</output>`
 * até `'conectado'`. Unmount desconecta a SSE.
 */

interface Movimentacao {
  id: string;
  produtoId: string;
  produtoNome: string;
  tipo: string;
  estoqueOrigemId: string | null;
  estoqueOrigemNome: string | null;
  estoqueDestinoId: string | null;
  estoqueDestinoNome: string | null;
  quantidade: number;
  usuarioId: string;
  usuarioNome: string;
  criadoEm: string;
  inativo?: boolean;
}

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar as movimentações. Tente novamente em instantes.';

// MAX_MOVIMENTACOES espelha services.maxMovimentacoesPorConsulta (backend):
// cada consulta devolve no máximo as 500 mais recentes. Quando a resposta
// bate nesse teto a tela avisa do limite — sem afirmar que há linhas ocultas
// (pode haver exatamente 500), só que a consulta não vai além disso.
const MAX_MOVIMENTACOES = 500;

const ROTULO_TIPO: Record<string, string> = {
  baixa: 'Baixa',
  transferencia: 'Transferência',
  ajuste: 'Ajuste',
  entrada: 'Entrada',
};

type Periodo = '7' | '30' | '90' | 'todo';
const PERIODOS: { valor: Periodo; rotulo: string }[] = [
  { valor: '7', rotulo: 'Últimos 7 dias' },
  { valor: '30', rotulo: 'Últimos 30 dias' },
  { valor: '90', rotulo: 'Últimos 90 dias' },
  { valor: 'todo', rotulo: 'Todo o período' },
];
const PERIODO_PADRAO: Periodo = '30';

interface EstoqueOpcao {
  id: string;
  nome: string;
}

interface IndicadoresMovimentacoes {
  baixas: number;
  transferencias: number;
}

function dataLocalISO(d: Date): string {
  const mm = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  return `${d.getFullYear()}-${mm}-${dd}`;
}

// Querystring dos filtros, compartilhada pela lista e pelos indicadores.
function montarQuery(periodo: Periodo, tipo: string, estoque: string): string {
  const params = new URLSearchParams();
  if (periodo !== 'todo') {
    const de = new Date();
    de.setDate(de.getDate() - Number(periodo));
    params.set('de', dataLocalISO(de));
  }
  if (tipo) params.set('tipo', tipo);
  if (estoque) params.set('estoque', estoque);
  const q = params.toString();
  return q ? `?${q}` : '';
}

export function MovimentacoesSection() {
  const [movimentacoes, setMovimentacoes] = useState<Movimentacao[]>([]);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [carregou, setCarregou] = useState(false);
  const [statusConexao, setStatusConexao] = useState<StatusRealtime | null>(null);
  const [periodo, setPeriodo] = useState<Periodo>(PERIODO_PADRAO);
  const [tipo, setTipo] = useState('');
  const [estoque, setEstoque] = useState('');
  const [estoques, setEstoques] = useState<EstoqueOpcao[]>([]);
  // null até carregar ou se a rota falhar — a faixa mostra "—".
  const [indicadores, setIndicadores] = useState<IndicadoresMovimentacoes | null>(null);

  // Filtros lidos por ref: `carregar` fica estável e a SSE não reconecta a
  // cada troca de filtro (o efeito abaixo refaz a carga quando eles mudam).
  const filtrosRef = useRef({
    periodo: PERIODO_PADRAO as Periodo,
    tipo: '',
    estoque: '',
  });

  // Contador de sequência: uma resposta de uma chamada mais antiga que chega
  // DEPOIS de uma nova é descartada (um refetch por evento SSE não pode ser
  // sobrescrito por uma carga anterior mais lenta).
  const seqRef = useRef(0);

  const seqIndRef = useRef(0);

  const carregarIndicadores = useCallback(async () => {
    const seq = ++seqIndRef.current;
    const { periodo: p, tipo: t, estoque: e } = filtrosRef.current;
    try {
      const res = await fetch(apiUrl(`/api/movimentacoes/indicadores${montarQuery(p, t, e)}`), {
        headers: authHeaders(),
      });
      if (seq !== seqIndRef.current) return;
      if (!res.ok) {
        setIndicadores(null);
        return;
      }
      const body = (await res.json()) as IndicadoresMovimentacoes;
      if (seq !== seqIndRef.current) return;
      setIndicadores(body);
    } catch {
      if (seq === seqIndRef.current) setIndicadores(null);
    }
  }, []);

  const carregar = useCallback(async () => {
    const seq = ++seqRef.current;
    const { periodo: p, tipo: t, estoque: e } = filtrosRef.current;
    try {
      const res = await fetch(apiUrl(`/api/movimentacoes${montarQuery(p, t, e)}`), {
        headers: authHeaders(),
      });
      if (seq !== seqRef.current) {
        return;
      }
      if (!res.ok) {
        // Dado anterior permanece visível; só o aviso de erro aparece.
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { movimentacoes: Movimentacao[] };
      if (seq !== seqRef.current) {
        return;
      }
      setMovimentacoes(body.movimentacoes ?? []);
      setErroCarregar(null);
      setCarregou(true);
    } catch {
      if (seq === seqRef.current) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
      }
    }
  }, []);

  useEffect(() => {
    const desconectar = conectarRealtime(
      (evento) => {
        if (evento.resource === 'movimentacoes') {
          toast.info('Movimentações atualizada.');
          void carregar();
          void carregarIndicadores();
        }
      },
      (status) => {
        setStatusConexao(status);
        if (status === 'conectado') {
          void carregar();
          void carregarIndicadores();
        }
      },
    );
    return () => {
      desconectar();
    };
  }, [carregar, carregarIndicadores]);

  // Troca de filtro: rebusca lista e indicadores com os mesmos parâmetros. A
  // carga inicial vem da SSE ('conectado'); o 1º render não dispara aqui.
  const primeiroRenderRef = useRef(true);
  useEffect(() => {
    filtrosRef.current = { periodo, tipo, estoque };
    if (primeiroRenderRef.current) {
      primeiroRenderRef.current = false;
      return;
    }
    void carregar();
    void carregarIndicadores();
  }, [periodo, tipo, estoque, carregar, carregarIndicadores]);

  // Estoques para o select de filtro (falha silenciosa: o filtro fica sem opções).
  useEffect(() => {
    void (async () => {
      try {
        const res = await fetch(apiUrl('/api/estoques'), {
          headers: authHeaders(),
        });
        if (!res.ok) return;
        const body = (await res.json()) as { estoques?: EstoqueOpcao[] };
        setEstoques(body.estoques ?? []);
      } catch {
        // sem opções de estoque
      }
    })();
  }, []);

  const filtrosAlterados = periodo !== PERIODO_PADRAO || tipo !== '' || estoque !== '';
  function limparFiltros() {
    setPeriodo(PERIODO_PADRAO);
    setTipo('');
    setEstoque('');
  }

  const noLimite = movimentacoes.length === MAX_MOVIMENTACOES;

  const selectClasse =
    'text-body min-h-touch-target-min rounded-md border border-border bg-background px-3';

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <select
          aria-label="Período"
          value={periodo}
          onChange={(event) => setPeriodo(event.target.value as Periodo)}
          className={selectClasse}
        >
          {PERIODOS.map((p) => (
            <option key={p.valor} value={p.valor}>
              {p.rotulo}
            </option>
          ))}
        </select>
        <select
          aria-label="Tipo"
          value={tipo}
          onChange={(event) => setTipo(event.target.value)}
          className={selectClasse}
        >
          <option value="">Todos os tipos</option>
          {Object.entries(ROTULO_TIPO).map(([valor, rotulo]) => (
            <option key={valor} value={valor}>
              {rotulo}
            </option>
          ))}
        </select>
        <select
          aria-label="Estoque"
          value={estoque}
          onChange={(event) => setEstoque(event.target.value)}
          className={selectClasse}
        >
          <option value="">Todos os estoques</option>
          {estoques.map((e) => (
            <option key={e.id} value={e.id}>
              {e.nome}
            </option>
          ))}
        </select>
        <Button type="button" variant="outline" size="sm" onClick={limparFiltros}>
          Limpar filtros
        </Button>
      </div>

      <FaixaIndicadores
        indicadores={[
          { rotulo: 'Baixas', valor: indicadores?.baixas ?? null },
          {
            rotulo: 'Transferências',
            valor: indicadores?.transferencias ?? null,
          },
        ]}
      />

      {statusConexao === 'reconectando' && (
        <output aria-live="polite" className="text-label text-muted-foreground">
          Reconectando...
        </output>
      )}

      {erroCarregar && (
        <p role="alert" className="text-body text-destructive">
          {erroCarregar}
        </p>
      )}

      {!carregou && !erroCarregar && (
        <output className="text-body text-muted-foreground">Carregando movimentações...</output>
      )}

      {!erroCarregar && carregou && movimentacoes.length === 0 && (
        <p className="text-body text-muted-foreground">
          {filtrosAlterados
            ? 'Nenhuma movimentação encontrada com esses filtros.'
            : 'Nenhuma movimentação registrada.'}
        </p>
      )}

      {!erroCarregar && carregou && noLimite && (
        <p className="text-body text-muted-foreground">
          Cada consulta mostra no máximo 500 movimentações (as mais recentes); a consulta não vai
          além disso.
        </p>
      )}

      {movimentacoes.length > 0 && (
        <ul className="flex flex-col">
          {movimentacoes.map((mov) => (
            <li
              key={mov.id}
              className="text-body flex min-h-[60px] flex-wrap items-center gap-3 border-b border-border"
            >
              <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted">
                <ArrowLeftRight aria-hidden="true" className="h-4 w-4 text-muted-foreground" />
              </div>
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="font-medium">{mov.produtoNome}</span>
                <span className="text-label text-muted-foreground">
                  {mov.estoqueOrigemNome ?? '—'} → {mov.estoqueDestinoNome ?? '—'}
                </span>
              </div>
              <span className="flex shrink-0 items-center gap-2">
                <PilhulaStatus status={ROTULO_TIPO[mov.tipo] ?? mov.tipo} />
                {mov.inativo && <PilhulaStatus status="Inativo" />}
              </span>
              <span className="shrink-0 tabular-nums">{formatarQuantidade(mov.quantidade)}</span>
              <span className="shrink-0 text-label text-muted-foreground">{mov.usuarioNome}</span>
              <span className="shrink-0 text-label text-muted-foreground">
                {new Date(mov.criadoEm).toLocaleString('pt-BR')}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export default MovimentacoesSection;
