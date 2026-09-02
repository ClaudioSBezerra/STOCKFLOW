import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { getAccessToken } from '@/lib/session';
import { resumirDimensoes, type Dimensoes } from '@/components/catalogo/formatacao';

/**
 * Seção "Duplicatas" da página `/normalizacao` (Story 6.3, spec-6-3; ação de
 * mesclagem da Story 6.4, spec-6-4). Molde de `InconsistenciasSection.tsx`
 * (Story 6.1): botão dispara `GET /api/normalizacao/duplicatas` SÓ AO CLIQUE
 * por padrão — a análise varre o catálogo inteiro sob demanda, sem canal de
 * tempo real (nenhum estado persistido para notificar, mesmo padrão de
 * Inconsistências).
 *
 * `autoAnalisar` (Intent Contract, spec-6-3): quando `true`, dispara a
 * análise UMA VEZ ao montar — usado quando a página chega via CTA "Verificar
 * duplicatas agora" (Story 3.4, `ImportacaoProdutosSection.tsx`) com
 * `?verificarDuplicatas=1`, para que o Almoxarife caia direto na aba
 * Duplicatas com a análise já em andamento, sem precisar de um segundo
 * clique. Idioma de `VerificarEmailPage.tsx` (`useRef` para nunca disparar
 * duas vezes DENTRO da mesma montagem), mas aqui disparando uma AÇÃO em vez
 * de decidir o estado inicial a partir de um token da URL.
 *
 * O guard `useRef` local só protege contra disparo duplo dentro de UMA
 * montagem (ex. Strict Mode). Ele NÃO sobrevive a uma remontagem — e
 * `TabsContent` (Radix, `NormalizacaoPage.tsx`) desmonta o conteúdo da aba
 * inativa por padrão, então trocar de aba e voltar para "Duplicatas" remonta
 * este componente do zero. Por isso `onAutoAnalisado` (chamado uma única vez,
 * junto do disparo automático) devolve o controle para `NormalizacaoPage`,
 * que é quem NÃO desmonta ao trocar de aba e por isso é o dono correto do
 * guard "já disparei a análise automática nesta visita à página" — ver
 * comentário de `NormalizacaoPage.tsx`.
 *
 * Mesclagem (Story 6.4, spec-6-4): cada linha de Produto ganha um rádio
 * "manter este" (agrupado por `name={chaveDoGrupo}` — no máximo um
 * selecionado por grupo); o botão "Mesclar" do grupo fica desabilitado até
 * haver seleção. Clicar abre o `ConfirmDialog` (destrutivo/irreversível, ver
 * epic-6-context.md — nunca `window.confirm()`) citando o Produto mantido e
 * a lista dos removidos; confirmar dispara `POST /api/normalizacao/mesclar`
 * — sucesso remove o grupo inteiro da lista local e mostra um toast citando
 * `quantidadeConsolidada`; um `409` (grupo mudou entre a listagem e a
 * confirmação, ou um membro já foi mesclado por outra execução) mostra um
 * alerta e MANTÉM o grupo na lista — o Almoxarife pode reabrir a análise.
 */

interface ProdutoDuplicata {
  id: string;
  nome: string;
  dimensoes: Dimensoes;
}

interface GrupoDuplicata {
  produtos: ProdutoDuplicata[];
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const MENSAGEM_ERRO_ANALISAR =
  'Não foi possível analisar os produtos. Tente novamente em instantes.';
const MENSAGEM_ERRO_MESCLAR_CONFLITO =
  'Este grupo mudou desde a última análise (um produto já foi mesclado, ou uma dimensão foi corrigida). Reabra a análise de duplicatas.';
const MENSAGEM_ERRO_MESCLAR_GENERICO =
  'Não foi possível mesclar os produtos selecionados. Tente novamente em instantes.';

/** chaveDoGrupo é o identificador estável de um grupo (ids dos membros,
 * ordem estável vinda do servidor) — usado tanto como `name` do grupo de
 * rádios quanto para localizar/remover o grupo certo depois de uma
 * mesclagem bem-sucedida. */
function chaveDoGrupo(grupo: GrupoDuplicata): string {
  return grupo.produtos.map((p) => p.id).join('|');
}

/** ConfirmacaoMesclagem é o estado do grupo com mesclagem pendente de
 * confirmação no `ConfirmDialog` — já resolvido a partir da seleção de
 * rádio no momento do clique em "Mesclar", para que o diálogo não precise
 * reconsultar `grupos`/`selecaoPorGrupo`. */
interface ConfirmacaoMesclagem {
  chaveGrupo: string;
  mantidoId: string;
  mantidoNome: string;
  removidoIds: string[];
  removidoNomes: string[];
}

interface DuplicatasSectionProps {
  autoAnalisar?: boolean;
  /**
   * Chamado uma única vez, no exato momento em que `autoAnalisar` dispara a
   * análise automática — permite que `NormalizacaoPage` registre "já
   * disparei" no seu próprio estado (que sobrevive à remontagem deste
   * componente ao trocar de aba), para nunca mais passar `autoAnalisar=true`
   * de novo nesta visita à página.
   */
  onAutoAnalisado?: () => void;
}

export function DuplicatasSection({ autoAnalisar = false, onAutoAnalisado }: DuplicatasSectionProps) {
  const [grupos, setGrupos] = useState<GrupoDuplicata[] | null>(null);
  const [carregando, setCarregando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);

  // Story 6.4: seleção "manter este" por grupo (no máximo 1 produtoId por
  // chaveGrupo), o diálogo de confirmação pendente (null = fechado) e o
  // estado da chamada de mesclagem em si.
  const [selecaoPorGrupo, setSelecaoPorGrupo] = useState<Record<string, string>>({});
  const [confirmacaoPendente, setConfirmacaoPendente] = useState<ConfirmacaoMesclagem | null>(null);
  const [mesclando, setMesclando] = useState(false);
  const [erroMesclagem, setErroMesclagem] = useState<string | null>(null);

  const analisar = useCallback(async () => {
    setCarregando(true);
    setErro(null);
    setErroMesclagem(null);
    setSelecaoPorGrupo({});
    // Limpa os grupos da corrida anterior ANTES de tentar de novo — mesmo
    // cuidado de InconsistenciasSection: sem isso, um segundo clique que
    // falha deixaria os grupos de uma corrida bem-sucedida anterior visíveis
    // ao mesmo tempo que o alerta de erro novo.
    setGrupos(null);
    try {
      const res = await fetch('/api/normalizacao/duplicatas', { headers: authHeaders() });
      if (!res.ok) {
        setErro(MENSAGEM_ERRO_ANALISAR);
        return;
      }
      const body = (await res.json()) as { grupos: GrupoDuplicata[] };
      setGrupos(body.grupos ?? []);
    } catch {
      setErro(MENSAGEM_ERRO_ANALISAR);
    } finally {
      setCarregando(false);
    }
  }, []);

  const jaDisparouAuto = useRef(false);
  useEffect(() => {
    if (!autoAnalisar || jaDisparouAuto.current) return;
    jaDisparouAuto.current = true;
    onAutoAnalisado?.();
    void analisar();
  }, [autoAnalisar, analisar, onAutoAnalisado]);

  const selecionar = useCallback((chaveGrupo: string, produtoId: string) => {
    setSelecaoPorGrupo((atual) => ({ ...atual, [chaveGrupo]: produtoId }));
  }, []);

  // abrirConfirmacao resolve a seleção atual do grupo (o produto a manter)
  // contra a lista de membros e monta o estado do ConfirmDialog — nada é
  // enviado ao servidor ainda, só a confirmação explícita (onConfirm)
  // dispara a mesclagem.
  const abrirConfirmacao = useCallback(
    (grupo: GrupoDuplicata) => {
      const chaveGrupo = chaveDoGrupo(grupo);
      const mantidoId = selecaoPorGrupo[chaveGrupo];
      const mantido = grupo.produtos.find((p) => p.id === mantidoId);
      if (!mantido) return;
      const removidos = grupo.produtos.filter((p) => p.id !== mantido.id);
      setErroMesclagem(null);
      setConfirmacaoPendente({
        chaveGrupo,
        mantidoId: mantido.id,
        mantidoNome: mantido.nome,
        removidoIds: removidos.map((p) => p.id),
        removidoNomes: removidos.map((p) => p.nome),
      });
    },
    [selecaoPorGrupo],
  );

  // confirmarMesclagem chama POST /api/normalizacao/mesclar para o grupo
  // pendente. Sucesso: remove o grupo inteiro de `grupos` e mostra um toast
  // citando `quantidadeConsolidada` (Code Map, spec-6-4). `409` (grupo mudou
  // entre a listagem e a confirmação, ou um membro já foi mesclado por
  // execução concorrente — I/O Matrix de spec-6-4): mostra um alerta e
  // MANTÉM o grupo na lista, para que o Almoxarife possa reabrir a análise.
  const confirmarMesclagem = useCallback(async () => {
    if (!confirmacaoPendente) return;
    const { chaveGrupo, mantidoId, removidoIds } = confirmacaoPendente;
    setMesclando(true);
    setErroMesclagem(null);
    try {
      const res = await fetch('/api/normalizacao/mesclar', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ produtoMantidoId: mantidoId, produtoRemovidoIds: removidoIds }),
      });
      if (!res.ok) {
        setErroMesclagem(
          res.status === 409 ? MENSAGEM_ERRO_MESCLAR_CONFLITO : MENSAGEM_ERRO_MESCLAR_GENERICO,
        );
        return;
      }
      const body = (await res.json()) as { quantidadeConsolidada: number };
      setGrupos((atual) => (atual ?? []).filter((g) => chaveDoGrupo(g) !== chaveGrupo));
      setSelecaoPorGrupo((atual) => {
        const novo = { ...atual };
        delete novo[chaveGrupo];
        return novo;
      });
      toast.success(`Produtos mesclados — quantidade consolidada: ${body.quantidadeConsolidada}.`);
    } catch {
      setErroMesclagem(MENSAGEM_ERRO_MESCLAR_GENERICO);
    } finally {
      setMesclando(false);
      setConfirmacaoPendente(null);
    }
  }, [confirmacaoPendente]);

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Duplicatas</h2>
        <CardDescription>
          Grupos de Produtos candidatos a duplicata: mesmo nome, dimensões equivalentes (considerando
          conversão de unidade) e ao menos um local em comum. Escolha qual Produto cada grupo deve manter e
          mescle — a quantidade dos demais é somada no mantido e eles são removidos permanentemente.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Button onClick={() => void analisar()} disabled={carregando} className="self-start">
          {carregando ? 'Analisando...' : 'Analisar duplicatas'}
        </Button>

        {erro && (
          <p role="alert" className="text-body text-destructive">
            {erro}
          </p>
        )}

        {erroMesclagem && (
          <p role="alert" className="text-body text-destructive">
            {erroMesclagem}
          </p>
        )}

        {!erro && grupos !== null && grupos.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhuma duplicata encontrada.</p>
        )}

        {grupos !== null && grupos.length > 0 && (
          <div className="flex flex-col gap-4">
            {grupos.map((grupo) => {
              const chaveGrupo = chaveDoGrupo(grupo);
              const selecionado = selecaoPorGrupo[chaveGrupo];
              return (
                <div key={chaveGrupo} className="overflow-x-auto rounded-md border border-border">
                  <table className="w-full text-body">
                    <thead>
                      <tr className="border-b border-border text-label text-muted-foreground">
                        <th className="py-2 pr-4 pl-4 text-left font-medium">Manter</th>
                        <th className="py-2 pr-4 text-left font-medium">Produto</th>
                        <th className="py-2 pr-4 text-left font-medium">Dimensões</th>
                      </tr>
                    </thead>
                    <tbody>
                      {grupo.produtos.map((produto) => (
                        <tr key={produto.id} className="border-t border-border first:border-t-0">
                          <td className="py-2 pr-4 pl-4">
                            <input
                              type="radio"
                              name={chaveGrupo}
                              value={produto.id}
                              checked={selecionado === produto.id}
                              onChange={() => selecionar(chaveGrupo, produto.id)}
                              disabled={mesclando}
                              aria-label={`Manter ${produto.nome}`}
                              className="h-4 w-4"
                            />
                          </td>
                          <td className="py-2 pr-4">{produto.nome}</td>
                          <td className="py-2 pr-4">{resumirDimensoes(produto.dimensoes)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                  <div className="flex justify-end border-t border-border p-2">
                    <Button
                      size="sm"
                      disabled={!selecionado || mesclando}
                      onClick={() => abrirConfirmacao(grupo)}
                    >
                      Mesclar
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </CardContent>

      <ConfirmDialog
        open={confirmacaoPendente !== null}
        onOpenChange={(aberto) => {
          if (!aberto) setConfirmacaoPendente(null);
        }}
        onConfirm={() => void confirmarMesclagem()}
        title={`Mesclar em "${confirmacaoPendente?.mantidoNome ?? ''}"?`}
        description={
          confirmacaoPendente
            ? `${confirmacaoPendente.removidoNomes.join(', ')} ${
                confirmacaoPendente.removidoNomes.length > 1 ? 'serão removidos' : 'será removido'
              } e a quantidade será somada em "${confirmacaoPendente.mantidoNome}". Esta ação não pode ser desfeita.`
            : undefined
        }
        confirmLabel="Mesclar"
      />
    </Card>
  );
}

export default DuplicatasSection;
