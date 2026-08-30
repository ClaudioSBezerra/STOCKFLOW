import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { getAccessToken } from '@/lib/session';

/**
 * Seção "Locais" da página `/estoques` (Stories 2.1 e 2.2, spec-2-1 /
 * spec-2-2). Um `Card` com (a) um formulário `<Input>` + `<Button>` "Adicionar
 * estoque" que faz `POST /api/estoques` com `{ nome }`, e (b) a lista de
 * Estoques de `GET /api/estoques`, carregada no `useEffect` de mount, cada
 * linha com um botão "Excluir".
 *
 * Sucesso do cadastro: `toast.success('Estoque criado.')`, limpa o input e
 * refaz o `GET`. `409` -> `<p role="alert">` específico ("Já existe um estoque
 * com esse nome."). Qualquer outro erro de cadastro, ou falha de carga da
 * lista -> `<p role="alert" className="text-body text-destructive">` genérico
 * (molde de `GestaoUsuariosSection`). Lista vazia -> "Nenhum estoque
 * cadastrado ainda." Botão desabilitado enquanto `enviando` ou o nome está em
 * branco (defesa contra duplo-submit).
 *
 * Exclusão (Story 2.2): "Excluir" numa linha abre o `ConfirmDialog`
 * reutilizável (nunca `window.confirm`); ao confirmar, `DELETE
 * /api/estoques/{id}` com `authHeaders()`. `204`/`res.ok` ->
 * `toast.success('Estoque excluído.')`; qualquer `!res.ok` ->
 * `MENSAGEM_ERRO_EXCLUIR` no `<p role="alert">`. A lista é sempre recarregada
 * (sucesso E falha, molde de `GestaoUsuariosSection` — a linha obsoleta cai
 * sozinha após um 404 de corrida). Os guards de estoque residual (Epic 3) e
 * Pedido pendente (Epic 7) entram nas Stories 3.1 e 7.2, no backend.
 *
 * Sem eventos SSE nesta story (o registry `realtime/` ainda não existe): a
 * tela só busca no mount.
 */

interface Estoque {
  id: string;
  nome: string;
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar a lista de estoques. Recarregue a página.';
const MENSAGEM_ERRO_CADASTRO =
  'Não foi possível cadastrar o estoque agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_DUPLICADO = 'Já existe um estoque com esse nome.';
const MENSAGEM_ERRO_EXCLUIR =
  'Não foi possível excluir o estoque agora. Tente novamente em instantes.';

export function LocaisEstoqueSection() {
  const [nome, setNome] = useState('');
  const [estoques, setEstoques] = useState<Estoque[]>([]);
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [exclusaoPendente, setExclusaoPendente] = useState<{ id: string; nome: string } | null>(
    null,
  );
  const [excluindo, setExcluindo] = useState(false);

  const carregar = useCallback(async () => {
    try {
      const res = await fetch('/api/estoques', { headers: authHeaders() });
      if (!res.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { estoques: Estoque[] };
      setEstoques(body.estoques ?? []);
      setErroCarregar(null);
    } catch {
      setErroCarregar(MENSAGEM_ERRO_CARREGAR);
    }
  }, []);

  useEffect(() => {
    void (async () => {
      await carregar();
    })();
  }, [carregar]);

  async function enviar(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    // Defesa em profundidade contra duplo-submit (molde de ConfiguracoesPage):
    // o `disabled` do botão só reflete `enviando` após o próximo repaint.
    if (enviando || nome.trim() === '') {
      return;
    }
    setErro(null);
    setEnviando(true);
    try {
      const res = await fetch('/api/estoques', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ nome }),
      });
      if (res.status === 409) {
        setErro(MENSAGEM_ERRO_DUPLICADO);
        return;
      }
      if (!res.ok) {
        setErro(MENSAGEM_ERRO_CADASTRO);
        return;
      }
      toast.success('Estoque criado.');
      setNome('');
      await carregar();
    } catch {
      setErro(MENSAGEM_ERRO_CADASTRO);
    } finally {
      setEnviando(false);
    }
  }

  async function excluir(id: string) {
    // Guard contra duplo disparo (molde de `executar` em GestaoUsuariosSection).
    if (excluindo) {
      return;
    }
    setErro(null);
    setExcluindo(true);
    try {
      const res = await fetch(`/api/estoques/${id}`, {
        method: 'DELETE',
        headers: authHeaders(),
      });
      if (res.status === 204 || res.ok) {
        toast.success('Estoque excluído.');
      } else {
        setErro(MENSAGEM_ERRO_EXCLUIR);
      }
    } catch {
      setErro(MENSAGEM_ERRO_EXCLUIR);
    } finally {
      // Sucesso OU falha: recarrega para a linha refletir o estado real (ou a
      // linha obsoleta cair após um 404 de corrida).
      setExcluindo(false);
      setExclusaoPendente(null);
      await carregar();
    }
  }

  function confirmarExclusao() {
    if (!exclusaoPendente) {
      return;
    }
    const { id } = exclusaoPendente;
    setExclusaoPendente(null);
    void excluir(id);
  }

  return (
    <Card>
      <CardHeader>
        <h1 className="text-heading-lg">Locais</h1>
        <CardDescription>Cadastre e consulte os locais de estoque.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form onSubmit={enviar} className="flex flex-col gap-2" noValidate>
          <Label htmlFor="estoque-nome">Nome do estoque</Label>
          <div className="flex gap-2">
            <Input
              id="estoque-nome"
              value={nome}
              onChange={(event) => setNome(event.target.value)}
            />
            <Button type="submit" disabled={enviando || nome.trim() === ''}>
              {enviando ? 'Adicionando...' : 'Adicionar estoque'}
            </Button>
          </div>
          {erro && (
            <p role="alert" className="text-body text-destructive">
              {erro}
            </p>
          )}
        </form>

        {erroCarregar && (
          <p role="alert" className="text-body text-destructive">
            {erroCarregar}
          </p>
        )}
        {!erroCarregar && estoques.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhum estoque cadastrado ainda.</p>
        )}
        {!erroCarregar && estoques.length > 0 && (
          <ul className="flex flex-col gap-2">
            {estoques.map((e) => (
              <li
                key={e.id}
                className="text-body flex items-center justify-between gap-2 border-b border-border pb-2 last:border-b-0 last:pb-0"
              >
                <span className="min-w-0 break-words">{e.nome}</span>
                <Button
                  type="button"
                  variant="destructive"
                  size="sm"
                  className="shrink-0"
                  aria-label={`Excluir estoque ${e.nome}`}
                  onClick={() => setExclusaoPendente({ id: e.id, nome: e.nome })}
                  disabled={excluindo}
                >
                  Excluir
                </Button>
              </li>
            ))}
          </ul>
        )}
      </CardContent>

      <ConfirmDialog
        open={exclusaoPendente !== null}
        onOpenChange={(aberto) => {
          if (!aberto) {
            setExclusaoPendente(null);
          }
        }}
        onConfirm={confirmarExclusao}
        title={`Excluir o estoque "${exclusaoPendente?.nome ?? ''}"?`}
        description="O local é removido da lista. Esta ação não pode ser desfeita."
        confirmLabel="Excluir"
      />
    </Card>
  );
}

export default LocaisEstoqueSection;
