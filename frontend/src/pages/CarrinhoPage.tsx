import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { formatarQuantidade } from '@/components/catalogo/formatacao';
import { useCarrinho, type ItemCarrinho } from '@/lib/carrinho';
import { useAuth } from '@/lib/auth';

/**
 * Página `/carrinho` (Story 7.1, spec-7-1, e Envio de Pedido, Story 7.2,
 * spec-7-2, rota filha de `RotaProtegida` — sem gate de papel próprio,
 * `usuario`+): superfície principal das duas stories. Lista os itens do
 * carrinho (`useCarrinho().itens`), cada linha com o nome do Produto, o nome
 * do Estoque, a quantidade e um botão "Remover".
 *
 * Toda busca/limpeza (inclusive o aviso automático por item obsoleto —
 * Produto mesclado ou Estoque excluído, AC2) já é responsabilidade do
 * `CarrinhoProvider` (`refresh`, chamado aqui no mount) — esta página só
 * renderiza o estado global, nunca busca `GET /api/carrinho` por conta
 * própria.
 *
 * Carrinho vazio (Story 6.4/2.2 já limparam tudo, ou nunca teve item)
 * mostra a mensagem orientando buscar produto ou usar a câmera (EXPERIENCE.md:
 * "nunca uma tela em branco"). Falha ao buscar `GET /api/carrinho`
 * (`useCarrinho().erro`) mostra uma mensagem DISTINTA (`role="alert"`) — nunca
 * a de carrinho vazio, que enganaria o usuário sobre a causa real. Mesmo
 * convênio de três estados (carregando/erro/vazio) de
 * `CatalogoListagem.tsx`.
 *
 * Remover (AC4): "Remover" numa linha abre o `ConfirmDialog` reutilizável
 * (nunca `window.confirm`); ao confirmar, `useCarrinho().removerItem` faz o
 * `DELETE` + refresh do estado global (badge incluso). Sucesso ->
 * `toast.success`; falha -> `toast.error` com a mensagem do servidor
 * (molde de LocaisEstoqueSection para o guard de exclusão, adaptado a toast
 * porque `ConfirmDialog` não tem um slot de erro inline como os `Dialog` de
 * formulário).
 *
 * Enviar Pedido (Story 7.2): o botão "Enviar Pedido" só aparece com o
 * carrinho não-vazio e abre um `Dialog` de formulário (molde dos diálogos de
 * `ProdutoDetalhePage`, nunca `ConfirmDialog` — este não tem slot de input).
 * O campo "Solicitante" vem pré-preenchido com `useAuth().usuario.nome`, mas
 * é texto livre editável — a identidade real gravada em `pedidos.usuario_id`
 * é sempre a da sessão no servidor. "Obra / centro de custo" é obrigatório;
 * "Observação" é opcional. Sucesso -> `toast.success` + o `refresh` disparado
 * por `enviarPedido` já reflete o carrinho esvaziado. Falha (carrinho vazio
 * revalidado, item indisponível, validação) -> `toast.error` com a mensagem
 * do servidor, diálogo permanece aberto para nova tentativa.
 */

const MENSAGEM_CARRINHO_VAZIO =
  'Seu carrinho está vazio. Busque um produto ou aponte a câmera para um código.';
const MENSAGEM_CARRINHO_ERRO =
  'Não foi possível carregar o carrinho agora. Tente novamente em instantes.';

export function CarrinhoPage() {
  const { itens, carregando, erro, refresh, removerItem, enviarPedido } = useCarrinho();
  const { usuario } = useAuth();
  const [remocaoPendente, setRemocaoPendente] = useState<ItemCarrinho | null>(null);
  const [removendo, setRemovendo] = useState(false);
  const [envioAberto, setEnvioAberto] = useState(false);
  const [solicitante, setSolicitante] = useState('');
  const [obraCentroCusto, setObraCentroCusto] = useState('');
  const [observacao, setObservacao] = useState('');
  const [enviando, setEnviando] = useState(false);
  const vazio = !carregando && !erro && itens.length === 0;

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function confirmarRemocao() {
    if (!remocaoPendente || removendo) {
      return;
    }
    const item = remocaoPendente;
    setRemovendo(true);
    const resultado = await removerItem(item.produtoId, item.estoqueId);
    setRemovendo(false);
    setRemocaoPendente(null);
    if (resultado.ok) {
      toast.success('Item removido do carrinho.');
    } else {
      toast.error(resultado.mensagem);
    }
  }

  function abrirEnvio() {
    setSolicitante(usuario?.nome ?? '');
    setObraCentroCusto('');
    setObservacao('');
    setEnvioAberto(true);
  }

  async function confirmarEnvio() {
    if (enviando || solicitante.trim() === '' || obraCentroCusto.trim() === '') {
      return;
    }
    setEnviando(true);
    try {
      const resultado = await enviarPedido(
        solicitante.trim(),
        obraCentroCusto.trim(),
        observacao.trim(),
      );
      if (!resultado.ok) {
        toast.error(resultado.mensagem);
        return;
      }
      toast.success('Pedido enviado.');
      setEnvioAberto(false);
    } finally {
      setEnviando(false);
    }
  }

  return (
    <div className="flex flex-col gap-4 p-6">
      <Card>
        <CardHeader>
          <h1 className="text-heading-lg">Carrinho</h1>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {erro && (
            <p role="alert" className="text-body text-destructive">
              {MENSAGEM_CARRINHO_ERRO}
            </p>
          )}

          {!erro && carregando && itens.length === 0 && (
            <output className="text-body text-muted-foreground">Carregando carrinho...</output>
          )}

          {vazio && <p className="text-body text-muted-foreground">{MENSAGEM_CARRINHO_VAZIO}</p>}

          {!erro && itens.length > 0 && (
            <ul className="flex flex-col gap-2">
              {itens.map((item) => (
                <li
                  key={`${item.produtoId}:${item.estoqueId}`}
                  className="text-body flex items-center justify-between gap-4 border-b border-border pb-2 last:border-b-0 last:pb-0"
                >
                  <div className="flex min-w-0 flex-col">
                    <span className="min-w-0 truncate">{item.produtoNome}</span>
                    <span className="text-label text-muted-foreground">{item.estoqueNome}</span>
                  </div>
                  <span className="flex shrink-0 items-center gap-3">
                    <span className="tabular-nums">{formatarQuantidade(item.quantidade)}</span>
                    <Button
                      type="button"
                      variant="destructive"
                      size="sm"
                      aria-label={`Remover ${item.produtoNome} do carrinho`}
                      onClick={() => setRemocaoPendente(item)}
                      disabled={removendo}
                    >
                      Remover
                    </Button>
                  </span>
                </li>
              ))}
            </ul>
          )}

          {!erro && itens.length > 0 && (
            <div className="flex justify-end">
              <Button type="button" onClick={abrirEnvio}>
                Enviar Pedido
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      <ConfirmDialog
        open={remocaoPendente !== null}
        onOpenChange={(aberto) => {
          if (!aberto && !removendo) {
            setRemocaoPendente(null);
          }
        }}
        onConfirm={confirmarRemocao}
        title={`Remover "${remocaoPendente?.produtoNome ?? ''}" do carrinho?`}
        confirmLabel="Remover"
      />

      <Dialog
        open={envioAberto}
        onOpenChange={(aberto) => {
          if (!aberto && !enviando) {
            setEnvioAberto(false);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Enviar Pedido</DialogTitle>
          </DialogHeader>
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void confirmarEnvio();
            }}
          >
            <div className="flex flex-col gap-2">
              <Label htmlFor="pedido-solicitante">Solicitante</Label>
              <Input
                id="pedido-solicitante"
                value={solicitante}
                onChange={(event) => setSolicitante(event.target.value)}
                autoComplete="off"
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="pedido-obra">Obra / centro de custo</Label>
              <Input
                id="pedido-obra"
                value={obraCentroCusto}
                onChange={(event) => setObraCentroCusto(event.target.value)}
                autoComplete="off"
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="pedido-observacao">Observação (opcional)</Label>
              <textarea
                id="pedido-observacao"
                className="min-h-16 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-2 text-base shadow-xs outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm dark:bg-input/30"
                value={observacao}
                onChange={(event) => setObservacao(event.target.value)}
              />
            </div>
            <DialogFooter>
              <Button
                type="submit"
                disabled={enviando || solicitante.trim() === '' || obraCentroCusto.trim() === ''}
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

export default CarrinhoPage;
