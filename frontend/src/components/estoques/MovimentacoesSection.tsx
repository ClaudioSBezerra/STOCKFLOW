import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { getAccessToken } from '@/lib/session';
import { conectarRealtime, type StatusRealtime } from '@/lib/realtime/client';
import { formatarQuantidade } from '@/components/catalogo/formatacao';

/**
 * Aba "Movimentações" da página `/estoques` (Story 5.3, spec-5-3). Tabela
 * SOMENTE-LEITURA de `GET /api/movimentacoes` (Produto · Tipo · Origem ·
 * Destino · Quantidade · Autor · Data): as Baixas (Story 5.1) e
 * Transferências (Story 5.2) já registradas, mais recente primeiro. NENHUMA
 * ação de edição/exclusão em nenhuma linha — a trilha é append-only e o
 * servidor não expõe rota de escrita.
 *
 * Estrutura/tabela: molde de `LogAcessoSection` (`Card`/`CardHeader`/
 * `CardContent`, `<div className="overflow-x-auto"><table>`, `seqRef`
 * anti-corrida, aviso de teto de 500). Tempo real: molde de
 * `ProdutoDetalhePage` — a carga inicial E o refetch pós-reconexão são o
 * MESMO caminho (`carregar` só é chamado a partir de
 * `aoMudarStatus('conectado')`, que dispara também na 1ª conexão), nunca de
 * um `useEffect` de mount separado (AD-3). Um evento SSE com
 * `resource === 'movimentacoes'` dispara `toast.info('Movimentações
 * atualizada.')` + o mesmo refetch — a tela NUNCA se auto-recarrega, e o
 * dado antigo permanece visível até a resposta chegar. Status
 * `'reconectando'` mostra um `<output aria-live="polite">Reconectando...</output>`
 * persistente até `'conectado'`. Unmount desconecta a SSE.
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
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
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
};

export function MovimentacoesSection() {
  const [movimentacoes, setMovimentacoes] = useState<Movimentacao[]>([]);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [carregou, setCarregou] = useState(false);
  const [statusConexao, setStatusConexao] = useState<StatusRealtime | null>(null);

  // Contador de sequência: uma resposta de uma chamada mais antiga que chega
  // DEPOIS de uma nova é descartada (um refetch por evento SSE não pode ser
  // sobrescrito por uma carga anterior mais lenta).
  const seqRef = useRef(0);

  const carregar = useCallback(async () => {
    const seq = ++seqRef.current;
    try {
      const res = await fetch('/api/movimentacoes', { headers: authHeaders() });
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
        }
      },
      (status) => {
        setStatusConexao(status);
        if (status === 'conectado') {
          void carregar();
        }
      },
    );
    return () => {
      desconectar();
    };
  }, [carregar]);

  const noLimite = movimentacoes.length === MAX_MOVIMENTACOES;

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Movimentações</h2>
        <CardDescription>
          Baixas e transferências registradas, mais recente primeiro. Somente leitura.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
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
          <p className="text-body text-muted-foreground">Nenhuma movimentação registrada.</p>
        )}

        {!erroCarregar && carregou && noLimite && (
          <p className="text-body text-muted-foreground">
            Cada consulta mostra no máximo 500 movimentações (as mais recentes); a consulta não vai
            além disso.
          </p>
        )}

        {movimentacoes.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-body">
              <thead>
                <tr className="text-label text-muted-foreground">
                  <th className="py-2 pr-4 text-left font-medium">Produto</th>
                  <th className="py-2 pr-4 text-left font-medium">Tipo</th>
                  <th className="py-2 pr-4 text-left font-medium">Origem</th>
                  <th className="py-2 pr-4 text-left font-medium">Destino</th>
                  <th className="py-2 pr-4 text-left font-medium">Quantidade</th>
                  <th className="py-2 pr-4 text-left font-medium">Autor</th>
                  <th className="py-2 text-left font-medium">Data</th>
                </tr>
              </thead>
              <tbody>
                {movimentacoes.map((mov) => (
                  <tr key={mov.id} className="border-t border-border">
                    <td className="py-2 pr-4">{mov.produtoNome}</td>
                    <td className="py-2 pr-4">{ROTULO_TIPO[mov.tipo] ?? mov.tipo}</td>
                    <td className="py-2 pr-4">{mov.estoqueOrigemNome ?? '—'}</td>
                    <td className="py-2 pr-4">{mov.estoqueDestinoNome ?? '—'}</td>
                    <td className="py-2 pr-4">{formatarQuantidade(mov.quantidade)}</td>
                    <td className="py-2 pr-4">{mov.usuarioNome}</td>
                    <td className="py-2">{new Date(mov.criadoEm).toLocaleString('pt-BR')}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export default MovimentacoesSection;
