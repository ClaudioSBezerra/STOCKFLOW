import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { formatarQuantidade } from '@/components/catalogo/formatacao';
import { useCarrinho, type ItemCarrinho } from '@/lib/carrinho';

/**
 * Página `/carrinho` (Story 7.1, spec-7-1, rota filha de `RotaProtegida` —
 * sem gate de papel próprio, `usuario`+): superfície principal da story.
 * Lista os itens do carrinho (`useCarrinho().itens`), cada linha com o nome
 * do Produto, o nome do Estoque, a quantidade e um botão "Remover".
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
 */

const MENSAGEM_CARRINHO_VAZIO =
  'Seu carrinho está vazio. Busque um produto ou aponte a câmera para um código.';
const MENSAGEM_CARRINHO_ERRO =
  'Não foi possível carregar o carrinho agora. Tente novamente em instantes.';

export function CarrinhoPage() {
  const { itens, carregando, erro, refresh, removerItem } = useCarrinho();
  const [remocaoPendente, setRemocaoPendente] = useState<ItemCarrinho | null>(null);
  const [removendo, setRemovendo] = useState(false);
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
    </div>
  );
}

export default CarrinhoPage;
