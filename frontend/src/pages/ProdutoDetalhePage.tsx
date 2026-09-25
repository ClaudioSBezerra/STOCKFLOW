import { useCallback, useEffect, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { XIcon } from 'lucide-react';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { EditarProdutoDialog } from '@/components/produtos/EditarProdutoDialog';
import { useAuth } from '@/lib/auth';
import { useCarrinho } from '@/lib/carrinho';
import { rankPapel } from '@/components/shell/nav-items';
import { conectarRealtime, type StatusRealtime } from '@/lib/realtime/client';
import { apiUrl, authHeaders } from '@/lib/api';
import {
  formatarQuantidade,
  IndicadorDisponibilidade,
  resumirDimensoes,
  type Dimensoes,
} from '@/components/catalogo/formatacao';

/**
 * Página de detalhe do Produto (Story 4.4, spec-4-4, rota `/produtos/:id`,
 * filha de `RotaProtegida` — sem gate de papel próprio, `usuario`+):
 * alcançável clicando um card da grade (Story 4.3) ou um resultado de busca
 * (Story 4.1). Mostra nome, código (`font-mono` quando presente), categoria,
 * dimensões (`resumirDimensoes`), indicador de disponibilidade
 * (`IndicadorDisponibilidade`) e a quantidade discriminada por Estoque —
 * mesma formatação de `CatalogoListagem`/`BuscaCatalogo` (`@/components/
 * catalogo/formatacao`).
 *
 * A busca inicial E o refetch pós-reconexão são o MESMO caminho de código:
 * `carregarDetalhe` é chamado a partir de `aoMudarStatus('conectado')`
 * (dispara também na primeira conexão) — nunca de um `useEffect` de mount
 * separado (AD-3: "sempre GET completo ao reconectar", unificar os dois
 * evita dois caminhos divergentes para a mesma responsabilidade). Único
 * segundo gatilho: um fallback de `LIMIAR_FALLBACK_SEM_SSE_MS` (4s) que
 * chama a MESMA `carregarDetalhe` caso o EventSource nunca chegue a
 * 'conectado' (incidente real, 2026-09-04 — proxy em produção que nunca
 * completa o handshake SSE, sem erro visível; tela ficava presa em
 * "Carregando produto..." para sempre). Cancelado assim que 'conectado'
 * chega primeiro; `seqRef` descarta qualquer chamada duplicada. A tela
 * assina o canal `produtos` via `conectarRealtime`; um evento sobre o
 * MESMO Produto (`resource==='produtos' && id===<id da rota>`) dispara um
 * refetch completo (mesma `carregarDetalhe`) + `toast.info('Catálogo
 * atualizado.')` (`sonner`, `aria-live="polite"` nativo do `Toaster`
 * global). Status `'reconectando'` mostra um `<output>` persistente
 * "Reconectando..." (`aria-live="polite"`) enquanto durar. Unmount
 * desconecta a SSE.
 *
 * O React Router NÃO remonta um componente de rota quando só o `:id` da
 * rota muda (a mesma rota casa `/produtos/A` e `/produtos/B`) — sem
 * cuidado extra, uma resposta em voo do produto ANTIGO poderia chegar
 * depois da troca e sobrescrever a tela com o produto ERRADO. Por isso
 * `ProdutoDetalhePage` é só um wrapper fino: repassa `id` para
 * `ProdutoDetalheConteudo` com `key={id}` — trocar o `key` força o React a
 * desmontar a instância antiga e montar uma instância nova (estado,
 * `seqRef` e `objectUrlCacheRef` sempre partem zerados para cada produto,
 * e qualquer resposta tardia da instância antiga cai num componente já
 * desmontado, sem efeito). Dentro de `ProdutoDetalheConteudo`, um `seqRef`
 * (mesmo padrão de `CatalogoListagem`/`BuscaCatalogo`) ainda descarta
 * respostas obsoletas de chamadas sobrepostas para o MESMO id (ex.: dois
 * refetches disparados em sequência rápida).
 *
 * Fotos (Story 3.6): `GET /api/produtos/{id}/fotos` + fetch-com-auth +
 * `blob()` + `URL.createObjectURL` por foto (mesmo padrão de
 * `CadastroProdutoSection.tsx` — um `<img src>` direto na URL da API
 * falharia, a rota é `RequireAuth` e `<img>` não envia `Authorization`),
 * miniaturas em grade abrindo lightbox em tela cheia (`Dialog`, mesmo molde
 * de `CadastroProdutoSection.tsx`, reaproveitado aqui sem importar
 * diretamente — estado local incompatível). 0 fotos -> sem seção de fotos,
 * sem erro. Se `fotos` mudar (ex.: refetch por evento SSE) enquanto o
 * lightbox está aberto num índice que deixou de existir no novo array, o
 * `Dialog` é fechado — `open` é derivado de `fotos[lightboxIndex]` existir,
 * nunca guardado como um booleano solto que poderia dessincronizar.
 *
 * Registrar Baixa (Story 5.1, spec-5-1) e Transferir (Story 5.2, spec-5-2):
 * cada linha de "Quantidade por Estoque" ganha os botões "Registrar Baixa" e
 * "Transferir" (`variant="outline"`, `size="sm"`), visíveis só quando
 * `podeRegistrarMovimentacao` (`rankPapel(usuario?.papel ?? '') >=
 * rankPapel('almoxarife')`, molde de `podeCadastrar`/`podeExportar`,
 * `CatalogoPage.tsx`) — o servidor continua a autoridade real (403 para
 * `usuario` mesmo que o botão nunca apareça, `RequireRole` decide). "Registrar
 * Baixa" abre um `Dialog` (estado `baixaEstoque`) com um `Input type="number"`;
 * confirmar dispara `POST /api/produtos/{id}/estoques/{estoqueId}/baixa`.
 * "Transferir" abre um `Dialog` (estado `transferenciaEstoque`) com um
 * `Select` de Estoque destino — a lista vem de `GET /api/estoques` buscada
 * LAZY quando o diálogo abre (só `almoxarife`+ vê o botão, e nem todo
 * `almoxarife`+ abre o diálogo), com a própria linha de origem excluída das
 * opções (UX; o servidor ainda rejeita origem==destino com 400) — mais um
 * `Input type="number"` para a quantidade; confirmar dispara
 * `POST /api/produtos/{id}/estoques/{estoqueId}/transferencia` com
 * `{estoqueDestinoId, quantidade}`. Nos dois casos: sucesso -> `toast.success`,
 * fecha o diálogo e refaz `carregarDetalhe()` (mesma busca usada no
 * mount/reconexão/refetch por SSE — nenhum caminho de atualização de estado
 * paralelo); falha mostra a mensagem do servidor (que já cita a quantidade
 * disponível no 409) DENTRO do diálogo, sem fechar.
 *
 * Adicionar ao Carrinho (Story 7.1, spec-7-1): visível para QUALQUER
 * usuário autenticado (`usuario`+, sem gate de papel — ao contrário de
 * Baixa/Transferir), primeiro ponto de entrada da AC1. Desabilitado
 * (`disabled`, mesmo tratamento visual `disabled:opacity-50` de qualquer
 * outro botão deste app) quando `linha.disponivel <= 0` (Story 11.3: saldo
 * físico menos reservas de Pedidos pendentes) — sem isso o
 * Usuário abriria o diálogo só para levar um 409 depois de um round-trip ao
 * servidor por uma linha que já mostra "0" na tabela. Abre um `Dialog`
 * (estado `carrinhoEstoque`) com um `Input type="number"` — molde exato do
 * diálogo de Registrar Baixa. Confirmar chama `useCarrinho().adicionarItem`
 * (que já faz o `POST /api/carrinho/itens` e, em sucesso, o refresh do
 * estado global do carrinho — badge incluso): sucesso -> `toast.success`,
 * fecha o diálogo; falha mostra a mensagem do servidor (409 já cita quanto
 * ainda cabe, 404 se o Produto foi mesclado entre a abertura da tela e a
 * confirmação) DENTRO do diálogo, sem fechar — nunca dispara
 * `carregarDetalhe()` (adicionar ao carrinho não muda `produto_estoque`,
 * Design Notes de spec-7-1).
 */

interface CategoriaDetalhe {
  id: string;
  codigo: string;
  nome: string;
}

interface LoteSaldo {
  id: string | null;
  quantidade: number;
  dataValidade: string | null;
  vencido: boolean;
  legado: boolean;
}

interface EstoqueQuantidade {
  estoqueId: string;
  estoqueNome: string;
  quantidade: number;
  // Story 11.1: Lotes do par (só no detalhe; ausente quando o Estoque não tem
  // Lote nem saldo legado).
  lotes?: LoteSaldo[];
  // Story 11.3: saldo reservado por Pedidos pendentes e o restante
  // disponível (`max(saldo − reservada, 0)`).
  reservada: number;
  disponivel: number;
}

// Story 11.3: um Pedido pendente que reserva saldo de um par (Produto, Estoque).
interface ReservaSaldo {
  pedidoId: string;
  solicitante: string;
  quantidade: number;
  criadoEm: string;
}

interface HistoricoItem {
  id: string;
  acao: string;
  autor: string;
  detalhe: Record<string, unknown>;
  criadoEm: string;
}

function descreverHistorico(item: HistoricoItem): string {
  switch (item.acao) {
    case 'nome_alterado':
      return `«${String(item.detalhe.antes ?? '')}» → «${String(item.detalhe.depois ?? '')}»`;
    case 'inativado':
      return 'Inativado';
    case 'reativado':
      return 'Reativado';
    default:
      return item.acao;
  }
}

function formatarDataHora(iso: string): string {
  const data = new Date(iso);
  if (Number.isNaN(data.getTime())) return iso;
  return data.toLocaleString('pt-BR');
}

// formatarDataValidade converte "YYYY-MM-DD" em "DD/MM/YYYY" sem passar por
// `Date` (evita deslocamento de fuso).
function formatarDataValidade(iso: string): string {
  const [ano, mes, dia] = iso.split('-');
  return `${dia}/${mes}/${ano}`;
}

// formatarDataPedido converte o timestamp ISO do Pedido em dd/mm/aaaa (pt-BR).
function formatarDataPedido(iso: string): string {
  const data = new Date(iso);
  if (Number.isNaN(data.getTime())) return iso;
  return data.toLocaleDateString('pt-BR');
}

function ListaLotes({ lotes }: { lotes: LoteSaldo[] }) {
  return (
    <ul className="text-label text-muted-foreground flex flex-col gap-1 pl-4" aria-label="Lotes">
      {lotes.map((lote, i) => (
        <li key={lote.id ?? `legado-${i}`} className="flex flex-wrap items-center gap-2">
          <span className="tabular-nums">{formatarQuantidade(lote.quantidade)}</span>
          <span>
            {lote.dataValidade
              ? `validade ${formatarDataValidade(lote.dataValidade)}`
              : 'validade desconhecida'}
          </span>
          {lote.legado && <span>(saldo anterior)</span>}
          {lote.vencido && (
            <span className="bg-warning/10 rounded-full px-2 py-0.5 text-[color:var(--color-text-on-tint-warning)]">
              Vencido
            </span>
          )}
        </li>
      ))}
    </ul>
  );
}

interface ProdutoDetalhe {
  id: string;
  nome: string;
  codigo: string | null;
  categoria: CategoriaDetalhe;
  dimensoes: Dimensoes;
  // Story 10.3 (+ decisão do code review): unidade/embalagem e os códigos de
  // compra; null quando nunca preenchidos (Produto importado/legado).
  unidadeMedida?: string | null;
  embalagem?: string | null;
  codigoFornecedor?: string | null;
  ean13?: string | null;
  templateId?: string | null;
  observacoes?: string | null;
  quantidadeTotal: number;
  quantidadeReservada: number;
  quantidadeDisponivel: number;
  disponivel: boolean;
  porEstoque: EstoqueQuantidade[];
  // Story 16.1 (FR-55): o detalhe continua devolvendo o Produto inativo.
  inativo?: boolean;
  inativadoEm?: string | null;
}

interface FotoGaleria {
  nome: string;
  url: string;
  objectUrl: string;
}

type ErroDetalhe = 'nao-encontrado' | 'generico';

const MENSAGEM_ERRO = 'Não foi possível carregar o produto agora. Tente novamente em instantes.';
const MENSAGEM_NAO_ENCONTRADO = 'Produto não encontrado.';
const MENSAGEM_SEM_ESTOQUE_REGISTRADO = 'Sem quantidade registrada por estoque.';
const MENSAGEM_ERRO_RESERVAS =
  'Não foi possível carregar os pedidos que reservaram este saldo. Feche e tente novamente.';
const MENSAGEM_ERRO_BAIXA = 'Não foi possível registrar a baixa agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_TRANSFERENCIA =
  'Não foi possível registrar a transferência agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_INATIVACAO =
  'Não foi possível inativar o produto agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_REATIVACAO =
  'Não foi possível reativar o produto agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_LISTAR_ESTOQUES =
  'Não foi possível carregar a lista de estoques. Feche e tente novamente.';

// Ver comentário no useEffect da SSE: fallback de escape para quando o
// EventSource nunca completa o handshake (incidente real, 2026-09-04).
const LIMIAR_FALLBACK_SEM_SSE_MS = 4000;

interface EstoqueOpcao {
  id: string;
  nome: string;
}

// ProdutoDetalhePage: wrapper fino de roteamento — ver doc acima sobre por
// que `key={id}` é essencial aqui (força remontagem completa a cada troca
// de produto, em vez de reaproveitar a mesma instância entre ids
// diferentes).
export function ProdutoDetalhePage() {
  const { id } = useParams<{ id: string }>();

  if (!id) {
    return null;
  }

  return <ProdutoDetalheConteudo key={id} id={id} />;
}

function ProdutoDetalheConteudo({ id }: { id: string }) {
  const { usuario } = useAuth();
  const podeRegistrarMovimentacao = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');
  // Story 16.1: inativar/reativar é só `gestor`+ (o servidor responde 403
  // para os demais; aqui os botões nem aparecem).
  const podeInativar = rankPapel(usuario?.papel ?? '') >= rankPapel('gestor');
  const [editando, setEditando] = useState(false);

  // Diálogo "Inativar produto" (Story 16.1): motivo opcional; o 409
  // (PRODUTO_COM_SALDO etc.) é mostrado dentro do diálogo.
  const [inativando, setInativando] = useState(false);
  const [motivoInativacao, setMotivoInativacao] = useState('');
  const [enviandoInativacao, setEnviandoInativacao] = useState(false);
  const [erroInativacao, setErroInativacao] = useState<string | null>(null);
  const [enviandoReativacao, setEnviandoReativacao] = useState(false);

  const [produto, setProduto] = useState<ProdutoDetalhe | null>(null);
  const [carregando, setCarregando] = useState(true);
  const [erro, setErro] = useState<ErroDetalhe | null>(null);
  const [statusConexao, setStatusConexao] = useState<StatusRealtime | null>(null);

  const [fotos, setFotos] = useState<FotoGaleria[]>([]);
  // Histórico do produto (Story 16.3): só `almoxarife`+ (o `usuario` nem faz o fetch).
  const podeVerHistorico = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');
  const [historico, setHistorico] = useState<HistoricoItem[] | null>(null);
  const [erroHistorico, setErroHistorico] = useState(false);
  const [lightboxIndex, setLightboxIndex] = useState<number | null>(null);
  const objectUrlCacheRef = useRef<Map<string, string>>(new Map());

  // Diálogo de Registrar Baixa (Story 5.1): `baixaEstoque` guarda a linha
  // (estoqueId/estoqueNome) alvo — `null` fecha o diálogo, mesmo padrão
  // derivado de `fotoLightbox` abaixo (nenhum booleano solto separado).
  const [baixaEstoque, setBaixaEstoque] = useState<EstoqueQuantidade | null>(null);
  const [quantidadeBaixa, setQuantidadeBaixa] = useState('');
  const [enviandoBaixa, setEnviandoBaixa] = useState(false);
  const [erroBaixa, setErroBaixa] = useState<string | null>(null);

  // Diálogo de Adicionar ao Carrinho (Story 7.1): `carrinhoEstoque` guarda a
  // linha alvo — `null` fecha o diálogo, mesmo padrão de `baixaEstoque`
  // acima. `adicionarItem` (useCarrinho()) já cuida do POST + refresh do
  // estado global; este componente só guarda o estado de UI do diálogo.
  const { adicionarItem } = useCarrinho();
  const [carrinhoEstoque, setCarrinhoEstoque] = useState<EstoqueQuantidade | null>(null);
  const [quantidadeCarrinho, setQuantidadeCarrinho] = useState('');
  const [enviandoCarrinho, setEnviandoCarrinho] = useState(false);
  const [erroCarrinho, setErroCarrinho] = useState<string | null>(null);

  // Diálogo "quem reservou" (Story 11.3): `reservasEstoque` guarda a linha
  // alvo; a lista vem de GET /api/produtos/{id}/estoques/{estoqueId}/reservas
  // buscada quando o valor reservado é clicado.
  const [reservasEstoque, setReservasEstoque] = useState<EstoqueQuantidade | null>(null);
  const [reservas, setReservas] = useState<ReservaSaldo[] | null>(null);
  const [erroReservas, setErroReservas] = useState<string | null>(null);
  // `reservasSeqRef` descarta respostas obsoletas (fechar/reabrir em outro
  // Estoque, ou refetch mais novo); `reservasAbertaRef` guarda o estoqueId do
  // diálogo aberto (null = fechado) para o refetch disparado pelo recarregar
  // do detalhe sem depender de estado no efeito.
  const reservasSeqRef = useRef(0);
  const reservasAbertaRef = useRef<string | null>(null);

  // Diálogo de Transferir (Story 5.2): `transferenciaEstoque` guarda a linha
  // de ORIGEM alvo — `null` fecha o diálogo. A lista de Estoques destino
  // (`estoquesDestino`) é buscada LAZY quando o diálogo abre (ver
  // abrirTransferencia/carregarEstoquesDestino abaixo — clique, não efeito);
  // `estoqueDestinoId` é a opção escolhida no `Select`.
  const [transferenciaEstoque, setTransferenciaEstoque] = useState<EstoqueQuantidade | null>(null);
  const [estoquesDestino, setEstoquesDestino] = useState<EstoqueOpcao[] | null>(null);
  const [carregandoEstoques, setCarregandoEstoques] = useState(false);
  const [estoqueDestinoId, setEstoqueDestinoId] = useState('');
  const [quantidadeTransferencia, setQuantidadeTransferencia] = useState('');
  const [enviandoTransferencia, setEnviandoTransferencia] = useState(false);
  const [erroTransferencia, setErroTransferencia] = useState<string | null>(null);

  // seqRef descarta qualquer resposta em voo que não corresponda mais à
  // chamada mais recente (mesma guarda de CatalogoListagem/BuscaCatalogo)
  // — cobre chamadas sobrepostas para o MESMO id (ex.: dois refetches em
  // sequência). A troca de id em si é coberta pelo `key={id}` do wrapper
  // acima, que já descarta a instância inteira.
  const seqRef = useRef(0);

  // Revoga todos os Object URLs em cache quando o componente desmonta —
  // mesmo cuidado de CadastroProdutoSection. Como cada produto tem sua
  // própria instância (key={id}), este cache nunca é compartilhado entre
  // produtos diferentes.
  useEffect(() => {
    const cache = objectUrlCacheRef.current;
    return () => {
      for (const url of cache.values()) {
        URL.revokeObjectURL(url);
      }
    };
  }, []);

  // carregarFotos busca a galeria e resolve o Object URL de cada foto ainda
  // ausente do cache local — uma miniatura já resolvida nunca é rebuscada.
  // Falha aqui é silenciosa (a tela de detalhe é só consulta — Never da
  // spec — e "0 fotos" já é um estado válido sem erro): a galeria
  // simplesmente não aparece/atualiza nessa rodada. `seq` descarta o
  // resultado inteiro (ou parcial, entre fotos) se um refetch mais novo já
  // tiver começado.
  const carregarFotos = useCallback(async (produtoId: string, seq: number) => {
    try {
      const res = await fetch(apiUrl(`/api/produtos/${produtoId}/fotos`), { headers: authHeaders() });
      if (seq !== seqRef.current) return;
      if (!res.ok) return;
      const body = (await res.json()) as { fotos: { nome: string; url: string }[] };
      if (seq !== seqRef.current) return;

      const itens: FotoGaleria[] = [];
      for (const foto of body.fotos) {
        if (seq !== seqRef.current) return;
        let objectUrl = objectUrlCacheRef.current.get(foto.nome);
        if (!objectUrl) {
          const resFoto = await fetch(apiUrl(foto.url), { headers: authHeaders() });
          if (seq !== seqRef.current) return;
          if (!resFoto.ok) return;
          const blob = await resFoto.blob();
          if (seq !== seqRef.current) return;
          objectUrl = URL.createObjectURL(blob);
          objectUrlCacheRef.current.set(foto.nome, objectUrl);
        }
        itens.push({ nome: foto.nome, url: foto.url, objectUrl });
      }
      if (seq !== seqRef.current) return;
      setFotos(itens);
    } catch {
      // silencioso — ver comentário acima.
    }
  }, []);

  // carregarHistorico busca o histórico do Produto; falha mostra só uma
  // mensagem discreta, sem quebrar o detalhe.
  const carregarHistorico = useCallback(
    async (produtoId: string, seq: number) => {
      try {
        const res = await fetch(apiUrl(`/api/produtos/${produtoId}/historico`), { headers: authHeaders() });
        if (seq !== seqRef.current) return;
        if (!res.ok) {
          setErroHistorico(true);
          return;
        }
        const body = (await res.json()) as { historico: HistoricoItem[] };
        if (seq !== seqRef.current) return;
        setHistorico((body.historico ?? []).map((h) => ({ ...h, detalhe: h.detalhe ?? {} })));
        setErroHistorico(false);
      } catch {
        if (seq === seqRef.current) setErroHistorico(true);
      }
    },
    [],
  );

  // carregarReservas busca a lista de Pedidos pendentes com reserva do par
  // (Story 11.3, FR-50). Só a chamada MAIS RECENTE aplica resultado ou erro —
  // uma resposta lenta de um Estoque anterior nunca sobrescreve o estado novo.
  const carregarReservas = useCallback(
    async (estoqueId: string) => {
      const seq = ++reservasSeqRef.current;
      try {
        const res = await fetch(apiUrl(`/api/produtos/${id}/estoques/${estoqueId}/reservas`), {
          headers: authHeaders(),
        });
        if (seq !== reservasSeqRef.current) return;
        if (!res.ok) {
          setErroReservas(MENSAGEM_ERRO_RESERVAS);
          return;
        }
        const body = (await res.json()) as { reservas: ReservaSaldo[] };
        if (seq !== reservasSeqRef.current) return;
        setErroReservas(null);
        setReservas(body.reservas);
      } catch {
        if (seq === reservasSeqRef.current) {
          setErroReservas(MENSAGEM_ERRO_RESERVAS);
        }
      }
    },
    [id],
  );

  // Quando o detalhe é recarregado (evento SSE `produtos`, reconexão, ação do
  // próprio usuário) com o diálogo de reservas aberto, refaz a lista do
  // Estoque aberto; se o Estoque sumiu do detalhe, fecha o diálogo. Reservada
  // 0 mantém o diálogo aberto e a lista refeita mostra "nenhum pedido".
  const sincronizarReservas = useCallback(
    (detalhe: ProdutoDetalhe) => {
      const aberto = reservasAbertaRef.current;
      if (aberto === null) return;
      const linha = detalhe.porEstoque.find((l) => l.estoqueId === aberto);
      if (!linha) {
        reservasAbertaRef.current = null;
        reservasSeqRef.current += 1;
        setReservasEstoque(null);
        setReservas(null);
        setErroReservas(null);
        return;
      }
      setReservasEstoque(linha);
      void carregarReservas(aberto);
    },
    [carregarReservas],
  );

  // carregarDetalhe é o ÚNICO caminho de busca do Produto — chamado tanto na
  // primeira conexão SSE quanto em qualquer reconexão/refetch por evento
  // (ver doc do componente acima). Incrementa `seqRef` a cada chamada: uma
  // resposta que chega depois de uma chamada mais nova (outro refetch já
  // disparado) é descartada em vez de sobrescrever a tela. 404 mostra
  // MENSAGEM_NAO_ENCONTRADO — distinto do erro genérico (500/rede), que
  // sugere tentar de novo (um produto que não existe nunca vai aparecer).
  const carregarDetalhe = useCallback(async () => {
    const seq = ++seqRef.current;
    setErro(null);
    setCarregando(true);
    try {
      const res = await fetch(apiUrl(`/api/produtos/${id}`), { headers: authHeaders() });
      if (seq !== seqRef.current) return;
      if (!res.ok) {
        setErro(res.status === 404 ? 'nao-encontrado' : 'generico');
        return;
      }
      const data = (await res.json()) as { produto: ProdutoDetalhe };
      if (seq !== seqRef.current) return;
      setProduto(data.produto);
      sincronizarReservas(data.produto);
      await carregarFotos(id, seq);
      if (podeVerHistorico) await carregarHistorico(id, seq);
    } catch {
      if (seq === seqRef.current) {
        setErro('generico');
      }
    } finally {
      if (seq === seqRef.current) {
        setCarregando(false);
      }
    }
  }, [id, carregarFotos, carregarHistorico, podeVerHistorico, sincronizarReservas]);

  function abrirReservas(linha: EstoqueQuantidade) {
    reservasAbertaRef.current = linha.estoqueId;
    setReservasEstoque(linha);
    setReservas(null);
    setErroReservas(null);
    void carregarReservas(linha.estoqueId);
  }

  function fecharReservas() {
    reservasAbertaRef.current = null;
    reservasSeqRef.current += 1; // invalida qualquer resposta em voo
    setReservasEstoque(null);
    setReservas(null);
    setErroReservas(null);
  }

  // confirmarBaixa envia POST /api/produtos/{id}/estoques/{estoqueId}/baixa
  // para a linha guardada em `baixaEstoque` (molde exato do POST de
  // CadastroProdutoSection.tsx: headers com authHeaders(), body JSON). Defesa
  // em profundidade contra duplo-submit (`desabilitado`, mesmo padrão de
  // CadastroProdutoSection): o `disabled` do botão só reflete `enviandoBaixa`
  // após o próximo repaint. Sucesso -> toast + fecha o diálogo + refetch via
  // carregarDetalhe (MESMA função do mount/reconexão/SSE, nunca um caminho
  // de atualização de estado paralelo); falha mantém o diálogo aberto e
  // mostra a mensagem do servidor (envelope AD-14, já cita a quantidade
  // disponível no 409) — nunca uma string genérica fixa quando o servidor
  // devolveu uma.
  async function confirmarBaixa() {
    if (!baixaEstoque || enviandoBaixa || quantidadeBaixa.trim() === '') {
      return;
    }
    const quantidade = Number(quantidadeBaixa);
    if (!Number.isFinite(quantidade)) {
      setErroBaixa('Quantidade inválida.');
      return;
    }
    setEnviandoBaixa(true);
    setErroBaixa(null);
    try {
      const res = await fetch(apiUrl(`/api/produtos/${id}/estoques/${baixaEstoque.estoqueId}/baixa`), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ quantidade }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
        setErroBaixa(body.error?.message ?? MENSAGEM_ERRO_BAIXA);
        return;
      }
      toast.success('Baixa registrada.');
      setBaixaEstoque(null);
      setQuantidadeBaixa('');
      await carregarDetalhe();
    } catch {
      setErroBaixa(MENSAGEM_ERRO_BAIXA);
    } finally {
      setEnviandoBaixa(false);
    }
  }

  // confirmarInativacao envia POST /api/produtos/{id}/inativacao com o
  // motivo opcional (vazio -> sem motivo). Sucesso -> toast + fecha + refetch
  // (carregarDetalhe); falha mantém o diálogo aberto com a mensagem do
  // servidor (ex.: 409 PRODUTO_COM_SALDO citando os Estoques).
  async function confirmarInativacao() {
    if (enviandoInativacao) {
      return;
    }
    setEnviandoInativacao(true);
    setErroInativacao(null);
    try {
      const motivo = motivoInativacao.trim();
      const res = await fetch(apiUrl(`/api/produtos/${id}/inativacao`), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify(motivo === '' ? {} : { motivo }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
        setErroInativacao(body.error?.message ?? MENSAGEM_ERRO_INATIVACAO);
        return;
      }
      toast.success('Produto inativado.');
      setInativando(false);
      setMotivoInativacao('');
      await carregarDetalhe();
    } catch {
      setErroInativacao(MENSAGEM_ERRO_INATIVACAO);
    } finally {
      setEnviandoInativacao(false);
    }
  }

  // reativar envia POST /api/produtos/{id}/reativacao. Erro (ex.: 409
  // EAN_EM_USO) vira toast com a mensagem do servidor.
  async function reativar() {
    if (enviandoReativacao) {
      return;
    }
    setEnviandoReativacao(true);
    try {
      const res = await fetch(apiUrl(`/api/produtos/${id}/reativacao`), {
        method: 'POST',
        headers: authHeaders(),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
        toast.error(body.error?.message ?? MENSAGEM_ERRO_REATIVACAO);
        return;
      }
      toast.success('Produto reativado.');
      await carregarDetalhe();
    } catch {
      toast.error(MENSAGEM_ERRO_REATIVACAO);
    } finally {
      setEnviandoReativacao(false);
    }
  }

  // confirmarAdicionarCarrinho chama useCarrinho().adicionarItem para a
  // linha guardada em `carrinhoEstoque` (Story 7.1, spec-7-1) — molde de
  // confirmarBaixa, mas sem refetch de `carregarDetalhe()` no sucesso:
  // adicionar ao carrinho não escreve em `produto_estoque`, a tabela
  // "Quantidade por Estoque" não muda.
  async function confirmarAdicionarCarrinho() {
    if (!carrinhoEstoque || enviandoCarrinho || quantidadeCarrinho.trim() === '') {
      return;
    }
    const quantidade = Number(quantidadeCarrinho);
    if (!Number.isFinite(quantidade)) {
      setErroCarrinho('Quantidade inválida.');
      return;
    }
    setEnviandoCarrinho(true);
    setErroCarrinho(null);
    try {
      const resultado = await adicionarItem(id, carrinhoEstoque.estoqueId, quantidade);
      if (!resultado.ok) {
        setErroCarrinho(resultado.mensagem);
        return;
      }
      toast.success('Item adicionado ao carrinho. O saldo só é reservado ao enviar o Pedido.');
      setCarrinhoEstoque(null);
      setQuantidadeCarrinho('');
    } finally {
      setEnviandoCarrinho(false);
    }
  }

  // carregarEstoquesDestino busca a lista de Estoques UMA vez por instância,
  // disparada pelo clique em "Transferir" (evento, não efeito). `usuario`
  // nunca chega aqui (não vê o botão) e um `almoxarife`+ que nunca abre o
  // diálogo também não paga a requisição. Falha -> `erroTransferencia` no
  // diálogo; `Select`/Confirmar ficam desabilitados enquanto
  // `estoquesDestino === null`.
  const carregarEstoquesDestino = useCallback(async () => {
    setCarregandoEstoques(true);
    try {
      const res = await fetch(apiUrl('/api/estoques'), { headers: authHeaders() });
      if (!res.ok) {
        setErroTransferencia(MENSAGEM_ERRO_LISTAR_ESTOQUES);
        return;
      }
      const body = (await res.json()) as { estoques: EstoqueOpcao[] };
      setEstoquesDestino(body.estoques);
    } catch {
      setErroTransferencia(MENSAGEM_ERRO_LISTAR_ESTOQUES);
    } finally {
      setCarregandoEstoques(false);
    }
  }, []);

  function abrirTransferencia(linha: EstoqueQuantidade) {
    setTransferenciaEstoque(linha);
    setEstoqueDestinoId('');
    setQuantidadeTransferencia('');
    setErroTransferencia(null);
    if (estoquesDestino === null && !carregandoEstoques) {
      void carregarEstoquesDestino();
    }
  }

  // confirmarTransferencia envia
  // POST /api/produtos/{id}/estoques/{estoqueOrigemId}/transferencia com
  // `{estoqueDestinoId, quantidade}` (molde de `confirmarBaixa`). Sucesso ->
  // toast + fecha o diálogo + refetch via `carregarDetalhe` (MESMA função do
  // mount/reconexão/SSE); falha mantém o diálogo aberto e mostra a mensagem
  // do servidor (envelope AD-14, já cita a quantidade disponível no 409).
  async function confirmarTransferencia() {
    if (
      !transferenciaEstoque ||
      enviandoTransferencia ||
      estoqueDestinoId === '' ||
      quantidadeTransferencia.trim() === ''
    ) {
      return;
    }
    const quantidade = Number(quantidadeTransferencia);
    if (!Number.isFinite(quantidade)) {
      setErroTransferencia('Quantidade inválida.');
      return;
    }
    setEnviandoTransferencia(true);
    setErroTransferencia(null);
    try {
      const res = await fetch(
        apiUrl(`/api/produtos/${id}/estoques/${transferenciaEstoque.estoqueId}/transferencia`),
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', ...authHeaders() },
          body: JSON.stringify({ estoqueDestinoId: estoqueDestinoId, quantidade }),
        },
      );
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
        setErroTransferencia(body.error?.message ?? MENSAGEM_ERRO_TRANSFERENCIA);
        return;
      }
      toast.success('Transferência registrada.');
      setTransferenciaEstoque(null);
      setEstoqueDestinoId('');
      setQuantidadeTransferencia('');
      await carregarDetalhe();
    } catch {
      setErroTransferencia(MENSAGEM_ERRO_TRANSFERENCIA);
    } finally {
      setEnviandoTransferencia(false);
    }
  }

  useEffect(() => {
    // Fallback de escape (incidente real, 2026-09-04): em produção, atrás de
    // certas cadeias de proxy reverso, o EventSource pode nunca completar o
    // handshake (nem 'open' nem 'error' — a conexão simplesmente não
    // resolve) — sem isto, a tela ficava presa em "Carregando produto..."
    // para sempre, sem nenhum erro visível. Não é dado migrado nem bug de
    // renderização: CatalogoListagem busca por um `useEffect` de mount
    // próprio e nunca trava; só ProdutoDetalhePage dependia 100% de
    // 'conectado'. Continua sendo a MESMA `carregarDetalhe` (nenhum caminho
    // divergente) — só um segundo gatilho, cancelado assim que 'conectado'
    // chega primeiro. `seqRef` dentro de `carregarDetalhe` já descarta uma
    // chamada duplicada caso as duas disparem quase juntas.
    const timerFallback = setTimeout(() => {
      void carregarDetalhe();
    }, LIMIAR_FALLBACK_SEM_SSE_MS);

    const desconectar = conectarRealtime(
      (evento) => {
        if (evento.resource === 'produtos' && evento.id === id) {
          toast.info('Catálogo atualizado.');
          void carregarDetalhe();
        }
      },
      (status) => {
        setStatusConexao(status);
        if (status === 'conectado') {
          clearTimeout(timerFallback);
          void carregarDetalhe();
        }
      },
    );
    return () => {
      clearTimeout(timerFallback);
      desconectar();
    };
  }, [id, carregarDetalhe]);

  // fotoLightbox é derivada — nunca guardamos um booleano "lightbox aberto"
  // separado do índice: se `fotos` mudar (ex.: refetch por evento SSE) e o
  // índice aberto deixar de existir no novo array, `fotoLightbox` vira
  // `null` no mesmo render e o `Dialog` fecha, sem precisar de um efeito
  // extra para sincronizar os dois.
  const fotoLightbox = lightboxIndex !== null ? (fotos[lightboxIndex] ?? null) : null;

  // opcoesDestino é derivada: a lista de Estoques carregada menos a linha de
  // origem do diálogo aberto (origem == destino é rejeitado pelo servidor;
  // aqui só se evita oferecer a opção inválida). Vazia + lista já carregada
  // => aviso "Nenhum outro estoque disponível" no diálogo.
  const opcoesDestino = (estoquesDestino ?? []).filter(
    (estoque) => estoque.id !== transferenciaEstoque?.estoqueId,
  );

  return (
    <div className="flex flex-col gap-4 p-6">
      {statusConexao === 'reconectando' && (
        <output aria-live="polite" className="text-label text-muted-foreground">
          Reconectando...
        </output>
      )}

      {erro && (
        <p role="alert" className="text-body text-destructive">
          {erro === 'nao-encontrado' ? MENSAGEM_NAO_ENCONTRADO : MENSAGEM_ERRO}
        </p>
      )}

      {carregando && !produto && !erro && (
        <output className="text-body text-muted-foreground">Carregando produto...</output>
      )}

      {produto && (
        <Card>
          <CardHeader className="flex flex-row items-start justify-between gap-4">
            <div className="flex flex-wrap items-center gap-2">
              <h1 className="text-heading-lg">{produto.nome}</h1>
              {produto.inativo && (
                <span className="bg-warning/10 text-label rounded-full px-2 py-0.5 text-[color:var(--color-text-on-tint-warning)]">
                  Inativo
                </span>
              )}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {podeInativar &&
                (produto.inativo ? (
                  <Button
                    type="button"
                    variant="outline"
                    disabled={enviandoReativacao}
                    onClick={() => void reativar()}
                  >
                    Reativar
                  </Button>
                ) : (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => {
                      setInativando(true);
                      setMotivoInativacao('');
                      setErroInativacao(null);
                    }}
                  >
                    Inativar produto
                  </Button>
                ))}
              {podeRegistrarMovimentacao && (
                <Button type="button" variant="outline" onClick={() => setEditando(true)}>
                  Editar
                </Button>
              )}
            </div>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex flex-col gap-1">
              <span className="text-label text-muted-foreground">
                {produto.codigo && <span className="font-mono">{produto.codigo}</span>}
                {produto.codigo && ' — '}
                {produto.categoria.nome}
              </span>
              <span className="text-body text-muted-foreground">
                {resumirDimensoes(produto.dimensoes)}
              </span>
              <IndicadorDisponibilidade disponivel={produto.disponivel} />
            </div>

            <dl className="text-body grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
              <dt className="text-muted-foreground">Unidade de medida</dt>
              <dd>{produto.unidadeMedida ?? '—'}</dd>
              <dt className="text-muted-foreground">Embalagem</dt>
              <dd>{produto.embalagem ?? '—'}</dd>
              <dt className="text-muted-foreground">Código do fornecedor</dt>
              <dd className="font-mono">{produto.codigoFornecedor ?? '—'}</dd>
              <dt className="text-muted-foreground">EAN-13</dt>
              <dd className="font-mono">{produto.ean13 ?? '—'}</dd>
            </dl>

            <div className="flex flex-col gap-2">
              <h2 className="text-heading-md">Quantidade por Estoque</h2>
              {produto.porEstoque.length === 0 ? (
                <p className="text-body text-muted-foreground">
                  {MENSAGEM_SEM_ESTOQUE_REGISTRADO}
                </p>
              ) : (
                <ul className="flex flex-col gap-1">
                  {produto.porEstoque.map((linha) => (
                    <li key={linha.estoqueId} className="text-body flex flex-col gap-1">
                      <div className="flex items-center justify-between gap-4">
                        <span>{linha.estoqueNome}</span>
                        <span className="flex items-center gap-3">
                          <span className="tabular-nums">{formatarQuantidade(linha.quantidade)}</span>
                          <span className="text-label text-muted-foreground tabular-nums">
                            Disponível: {formatarQuantidade(linha.disponivel)}
                          </span>
                          {linha.reservada > 0 && (
                            <button
                              type="button"
                              className="text-label tabular-nums underline underline-offset-2"
                              aria-label={`Saldo reservado: ${formatarQuantidade(linha.reservada)} — ver pedidos em ${linha.estoqueNome}`}
                              onClick={() => abrirReservas(linha)}
                            >
                              Saldo reservado: {formatarQuantidade(linha.reservada)}
                            </button>
                          )}
                          {/* Story 16.1: Produto inativo não oferece
                              operação de estoque (o servidor recusaria). */}
                          {!produto.inativo && (
                            <Button
                              type="button"
                              variant="outline"
                              size="sm"
                              aria-label={`Adicionar ao Carrinho em ${linha.estoqueNome}`}
                              disabled={linha.disponivel <= 0}
                              onClick={() => {
                                setCarrinhoEstoque(linha);
                                setQuantidadeCarrinho('');
                                setErroCarrinho(null);
                              }}
                            >
                              Adicionar ao Carrinho
                            </Button>
                          )}
                          {podeRegistrarMovimentacao && !produto.inativo && (
                            <>
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                aria-label={`Registrar Baixa em ${linha.estoqueNome}`}
                                onClick={() => {
                                  setBaixaEstoque(linha);
                                  setQuantidadeBaixa('');
                                  setErroBaixa(null);
                                }}
                              >
                                Registrar Baixa
                              </Button>
                              <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                aria-label={`Transferir de ${linha.estoqueNome}`}
                                onClick={() => abrirTransferencia(linha)}
                              >
                                Transferir
                              </Button>
                            </>
                          )}
                        </span>
                      </div>
                      {linha.lotes && linha.lotes.length > 0 && (
                        <ListaLotes lotes={linha.lotes} />
                      )}
                    </li>
                  ))}
                </ul>
              )}
              <p className="text-label text-muted-foreground">
                Total: {formatarQuantidade(produto.quantidadeTotal)}
                {produto.quantidadeReservada > 0 &&
                  ` — Reservado: ${formatarQuantidade(produto.quantidadeReservada)} — Disponível: ${formatarQuantidade(produto.quantidadeDisponivel)}`}
              </p>
            </div>

            {fotos.length > 0 && (
              <div className="flex flex-col gap-2">
                <h2 className="text-heading-md">Fotos</h2>
                <div className="grid grid-cols-3 gap-2 sm:grid-cols-4">
                  {fotos.map((foto, index) => (
                    <button
                      key={foto.nome}
                      type="button"
                      onClick={() => setLightboxIndex(index)}
                      aria-label={`Ampliar foto ${index + 1} de ${fotos.length}`}
                      className="overflow-hidden rounded-md border border-border"
                    >
                      <img src={foto.objectUrl} alt="" className="h-24 w-24 object-cover" />
                    </button>
                  ))}
                </div>
              </div>
            )}

            {podeVerHistorico && (
              <div className="flex flex-col gap-2" data-testid="historico-produto">
                <h2 className="text-heading-md">Histórico do produto</h2>
                {erroHistorico ? (
                  <p className="text-body-sm text-text-muted">Não foi possível carregar o histórico.</p>
                ) : historico === null ? null : historico.length === 0 ? (
                  <p className="text-body-sm text-text-muted">Nenhuma alteração registrada.</p>
                ) : (
                  <ul className="flex flex-col gap-2">
                    {historico.map((item) => (
                      <li key={item.id} className="text-body-sm">
                        <p>{descreverHistorico(item)}</p>
                        {item.acao === 'inativado' && typeof item.detalhe.motivo === 'string' && item.detalhe.motivo !== '' && (
                          <p>Motivo: {item.detalhe.motivo}</p>
                        )}
                        <p className="text-text-muted">
                          {item.autor !== '' ? `${item.autor} — ` : ''}
                          {formatarDataHora(item.criadoEm)}
                        </p>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Lightbox (mesmo molde de CadastroProdutoSection.tsx): fechar (clique
          fora, Esc, ou o botão "Fechar") só muda `lightboxIndex` para `null`
          — nenhuma navegação, nenhum reload. */}
      <Dialog
        open={fotoLightbox !== null}
        onOpenChange={(open) => {
          if (!open) {
            setLightboxIndex(null);
          }
        }}
      >
        <DialogContent
          showCloseButton={false}
          className="flex w-screen h-screen max-w-none sm:!max-w-none translate-x-0 translate-y-0 top-0 left-0 items-center justify-center border-none bg-black/95 p-0"
        >
          <DialogTitle className="sr-only">Foto ampliada de {produto?.nome}</DialogTitle>
          <DialogClose className="absolute top-4 right-4 rounded-xs text-white opacity-90 ring-offset-background transition-opacity hover:opacity-100 focus:opacity-100 focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:outline-hidden">
            <XIcon className="size-6" />
            <span className="sr-only">Fechar</span>
          </DialogClose>
          {fotoLightbox && (
            <img
              src={fotoLightbox.objectUrl}
              alt=""
              className="max-h-full max-w-full object-contain"
            />
          )}
        </DialogContent>
      </Dialog>

      {produto && podeRegistrarMovimentacao && (
        <EditarProdutoDialog
          produto={produto}
          open={editando}
          onOpenChange={setEditando}
          onSalvo={carregarDetalhe}
          onFotosAlteradas={carregarDetalhe}
        />
      )}

      {/* Adicionar ao Carrinho (Story 7.1): controlado por `carrinhoEstoque`
          — `null` fecha o diálogo. Fechar enquanto o envio está em voo é
          ignorado (mesma defesa em profundidade do `enviandoCarrinho` no
          botão). Molde exato do diálogo de Registrar Baixa logo abaixo. */}
      <Dialog
        open={carrinhoEstoque !== null}
        onOpenChange={(open) => {
          if (!open && !enviandoCarrinho) {
            setCarrinhoEstoque(null);
            setErroCarrinho(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Adicionar ao Carrinho — {carrinhoEstoque?.estoqueNome}</DialogTitle>
          </DialogHeader>
          <p className="text-label text-muted-foreground">
            Adicionar ao carrinho não trava saldo: o saldo só fica reservado depois que o pedido for
            enviado.
          </p>
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void confirmarAdicionarCarrinho();
            }}
          >
            <div className="flex flex-col gap-2">
              <Label htmlFor="carrinho-quantidade">Quantidade</Label>
              <Input
                id="carrinho-quantidade"
                type="number"
                inputMode="decimal"
                value={quantidadeCarrinho}
                onChange={(event) => setQuantidadeCarrinho(event.target.value)}
              />
            </div>
            {erroCarrinho && (
              <p role="alert" className="text-body text-destructive">
                {erroCarrinho}
              </p>
            )}
            <DialogFooter>
              <Button type="submit" disabled={enviandoCarrinho || quantidadeCarrinho.trim() === ''}>
                Confirmar
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Quem reservou (Story 11.3): lista de Pedidos pendentes/solicitantes
          com reserva do par (Produto, Estoque). Só leitura. */}
      <Dialog
        open={reservasEstoque !== null}
        onOpenChange={(open) => {
          if (!open) {
            fecharReservas();
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Saldo reservado — {reservasEstoque?.estoqueNome}</DialogTitle>
          </DialogHeader>
          {erroReservas && (
            <p role="alert" className="text-body text-destructive">
              {erroReservas}
            </p>
          )}
          {!erroReservas && reservas === null && (
            <output className="text-body text-muted-foreground">Carregando reservas...</output>
          )}
          {reservas !== null && reservas.length === 0 && (
            <p className="text-body text-muted-foreground">Nenhum pedido reserva este saldo.</p>
          )}
          {reservas !== null && reservas.length > 0 && (
            <ul className="flex flex-col gap-1" aria-label="Pedidos com saldo reservado">
              {reservas.map((reserva) => (
                <li key={reserva.pedidoId} className="text-body flex justify-between gap-4">
                  <span className="flex flex-col">
                    <span>{reserva.solicitante}</span>
                    <span className="text-label text-muted-foreground">
                      Pedido <span className="font-mono">{reserva.pedidoId.slice(0, 8)}</span> —{' '}
                      {formatarDataPedido(reserva.criadoEm)}
                    </span>
                  </span>
                  <span className="tabular-nums">{formatarQuantidade(reserva.quantidade)}</span>
                </li>
              ))}
            </ul>
          )}
        </DialogContent>
      </Dialog>

      {/* Registrar Baixa (Story 5.1): controlado por `baixaEstoque` — `null`
          fecha o diálogo. Fechar enquanto o envio está em voo é ignorado
          (mesma defesa em profundidade do `enviandoBaixa` no botão). */}
      <Dialog
        open={baixaEstoque !== null}
        onOpenChange={(open) => {
          if (!open && !enviandoBaixa) {
            setBaixaEstoque(null);
            setErroBaixa(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Registrar Baixa — {baixaEstoque?.estoqueNome}</DialogTitle>
          </DialogHeader>
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void confirmarBaixa();
            }}
          >
            {baixaEstoque && (
              <div className="flex flex-col gap-1">
                <p className="text-body tabular-nums">
                  Disponível para baixa: {formatarQuantidade(baixaEstoque.disponivel)}
                </p>
                <p className="text-label text-muted-foreground">
                  O saldo reservado por Pedidos pendentes não pode ser baixado.
                </p>
              </div>
            )}
            <div className="flex flex-col gap-2">
              <Label htmlFor="baixa-quantidade">Quantidade</Label>
              <Input
                id="baixa-quantidade"
                type="number"
                inputMode="decimal"
                value={quantidadeBaixa}
                onChange={(event) => setQuantidadeBaixa(event.target.value)}
              />
            </div>
            {erroBaixa && (
              <p role="alert" className="text-body text-destructive">
                {erroBaixa}
              </p>
            )}
            <DialogFooter>
              <Button
                type="submit"
                disabled={enviandoBaixa || quantidadeBaixa.trim() === ''}
              >
                Confirmar
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Inativar produto (Story 16.1): motivo opcional + confirmar. Fechar
          enquanto o envio está em voo é ignorado. */}
      <Dialog
        open={inativando}
        onOpenChange={(open) => {
          if (!open && !enviandoInativacao) {
            setInativando(false);
            setErroInativacao(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Inativar produto</DialogTitle>
            <DialogDescription>
              O produto sai de uso, mas o histórico é mantido. Só é possível inativar um produto
              sem saldo em nenhum estoque e sem reserva de Pedido.
            </DialogDescription>
          </DialogHeader>
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void confirmarInativacao();
            }}
          >
            <div className="flex flex-col gap-2">
              <Label htmlFor="inativacao-motivo">Motivo (opcional)</Label>
              <textarea
                id="inativacao-motivo"
                className="min-h-16 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-2 text-base shadow-xs outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm dark:bg-input/30"
                maxLength={500}
                value={motivoInativacao}
                onChange={(event) => setMotivoInativacao(event.target.value)}
              />
            </div>
            {erroInativacao && (
              <p role="alert" className="text-body text-destructive">
                {erroInativacao}
              </p>
            )}
            <DialogFooter>
              <DialogClose asChild>
                <Button type="button" variant="outline" disabled={enviandoInativacao}>
                  Cancelar
                </Button>
              </DialogClose>
              <Button type="submit" disabled={enviandoInativacao}>
                Confirmar
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      {/* Transferir (Story 5.2): controlado por `transferenciaEstoque` —
          `null` fecha o diálogo. A lista de Estoques destino é buscada LAZY
          (clique em "Transferir", não efeito — ver abrirTransferencia acima)
          na abertura; a própria linha de origem é excluída das opções (o
          servidor ainda rejeita origem==destino). Fechar enquanto o envio
          está em voo é ignorado. */}
      <Dialog
        open={transferenciaEstoque !== null}
        onOpenChange={(open) => {
          if (!open && !enviandoTransferencia) {
            setTransferenciaEstoque(null);
            setErroTransferencia(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Transferir — {transferenciaEstoque?.estoqueNome}</DialogTitle>
          </DialogHeader>
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void confirmarTransferencia();
            }}
          >
            {transferenciaEstoque && (
              <div className="flex flex-col gap-1">
                <p className="text-body tabular-nums">
                  Disponível para transferência: {formatarQuantidade(transferenciaEstoque.disponivel)}
                </p>
                <p className="text-label text-muted-foreground">
                  O saldo reservado por Pedidos pendentes não pode ser transferido.
                </p>
              </div>
            )}
            <div className="flex flex-col gap-2">
              <Label htmlFor="transferencia-destino">Estoque destino</Label>
              <Select
                value={estoqueDestinoId}
                onValueChange={setEstoqueDestinoId}
                disabled={carregandoEstoques || estoquesDestino === null}
              >
                <SelectTrigger id="transferencia-destino" aria-label="Estoque destino">
                  <SelectValue
                    placeholder={carregandoEstoques ? 'Carregando estoques...' : 'Selecione o destino'}
                  />
                </SelectTrigger>
                <SelectContent>
                  {opcoesDestino.map((estoque) => (
                    <SelectItem key={estoque.id} value={estoque.id}>
                      {estoque.nome}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {estoquesDestino !== null && opcoesDestino.length === 0 && (
                <p className="text-body text-muted-foreground">
                  Nenhum outro estoque disponível para transferência.
                </p>
              )}
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="transferencia-quantidade">Quantidade</Label>
              <Input
                id="transferencia-quantidade"
                type="number"
                inputMode="decimal"
                value={quantidadeTransferencia}
                onChange={(event) => setQuantidadeTransferencia(event.target.value)}
              />
            </div>
            {erroTransferencia && (
              <p role="alert" className="text-body text-destructive">
                {erroTransferencia}
              </p>
            )}
            <DialogFooter>
              <Button
                type="submit"
                disabled={
                  enviandoTransferencia ||
                  estoquesDestino === null ||
                  estoqueDestinoId === '' ||
                  quantidadeTransferencia.trim() === ''
                }
              >
                Confirmar
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export default ProdutoDetalhePage;
