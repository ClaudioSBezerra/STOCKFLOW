import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Seção "Categorias" de `/configuracoes` (Story 10.5, spec-10-5, AD-33). Só
 * é montada para `adm`+ (gate em `ConfiguracoesPage`; o servidor também
 * recusa com 403 abaixo de `adm`).
 *
 * Lista `GET /api/categorias`, cadastra (`POST`), edita inline (`PUT
 * /api/categorias/{id}`) e exclui (`DELETE`, via `ConfirmDialog` — nunca
 * `window.confirm`). `maxLength` 8 (código) e 50 (nome) espelham o banco.
 * Erros inline em `<p role="alert">`; `400`/`409` mostram a mensagem do
 * PRÓPRIO servidor (cita o campo duplicado ou a contagem de Produtos que
 * bloqueia a exclusão). A lista é recarregada após cada mutação.
 */

interface Categoria {
  id: string;
  codigo: string;
  nome: string;
}

const CODIGO_MAX = 8;
const NOME_MAX = 50;

const MENSAGEM_ERRO_CARREGAR = 'Não foi possível carregar as categorias. Recarregue a página.';
const MENSAGEM_ERRO_CADASTRO =
  'Não foi possível cadastrar a categoria agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_EDITAR =
  'Não foi possível salvar a categoria agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_EXCLUIR =
  'Não foi possível excluir a categoria agora. Tente novamente em instantes.';

async function mensagemDoServidor(res: Response, padrao: string): Promise<string> {
  const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
  return body.error?.message ?? padrao;
}

export function CategoriasSection() {
  const [categorias, setCategorias] = useState<Categoria[]>([]);
  const [codigo, setCodigo] = useState('');
  const [nome, setNome] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [carregou, setCarregou] = useState(false);

  const [editando, setEditando] = useState<Categoria | null>(null);
  const [editCodigo, setEditCodigo] = useState('');
  const [editNome, setEditNome] = useState('');
  const [erroEdicao, setErroEdicao] = useState<string | null>(null);
  const [salvando, setSalvando] = useState(false);

  const [exclusaoPendente, setExclusaoPendente] = useState<Categoria | null>(null);
  const [excluindo, setExcluindo] = useState(false);
  const [erroExclusao, setErroExclusao] = useState<string | null>(null);

  const carregar = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/categorias'), { headers: authHeaders() });
      if (!res.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { categorias: Categoria[] };
      setCategorias(body.categorias ?? []);
      setErroCarregar(null);
      setCarregou(true);
    } catch {
      setErroCarregar(MENSAGEM_ERRO_CARREGAR);
    }
  }, []);

  useEffect(() => {
    void (async () => {
      await carregar();
    })();
  }, [carregar]);

  async function cadastrar(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enviando || codigo.trim() === '' || nome.trim() === '') {
      return;
    }
    setErro(null);
    setErroExclusao(null);
    setEnviando(true);
    try {
      const res = await fetch(apiUrl('/api/categorias'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ codigo, nome }),
      });
      if (res.status === 400 || res.status === 409) {
        setErro(await mensagemDoServidor(res, MENSAGEM_ERRO_CADASTRO));
        return;
      }
      if (!res.ok) {
        setErro(MENSAGEM_ERRO_CADASTRO);
        return;
      }
      toast.success('Categoria criada.');
      setCodigo('');
      setNome('');
      await carregar();
    } catch {
      setErro(MENSAGEM_ERRO_CADASTRO);
    } finally {
      setEnviando(false);
    }
  }

  function iniciarEdicao(c: Categoria) {
    setEditando(c);
    setEditCodigo(c.codigo);
    setEditNome(c.nome);
    setErroEdicao(null);
  }

  function cancelarEdicao() {
    setEditando(null);
    setErroEdicao(null);
  }

  async function salvarEdicao(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!editando || salvando || editCodigo.trim() === '' || editNome.trim() === '') {
      return;
    }
    setErroEdicao(null);
    setErro(null);
    setErroExclusao(null);
    setSalvando(true);
    try {
      const res = await fetch(apiUrl(`/api/categorias/${editando.id}`), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ codigo: editCodigo, nome: editNome }),
      });
      if (res.status === 400 || res.status === 409) {
        setErroEdicao(await mensagemDoServidor(res, MENSAGEM_ERRO_EDITAR));
        return;
      }
      if (res.status === 404) {
        // Linha obsoleta (removida em outra sessão): fecha a edição e recarrega.
        setEditando(null);
        setErro('Categoria não encontrada. A lista foi atualizada.');
        await carregar();
        return;
      }
      if (!res.ok) {
        setErroEdicao(MENSAGEM_ERRO_EDITAR);
        return;
      }
      toast.success('Categoria atualizada.');
      setEditando(null);
      await carregar();
    } catch {
      setErroEdicao(MENSAGEM_ERRO_EDITAR);
    } finally {
      setSalvando(false);
    }
  }

  async function excluir(id: string) {
    if (excluindo) {
      return;
    }
    setErroExclusao(null);
    setErro(null);
    setExcluindo(true);
    try {
      const res = await fetch(apiUrl(`/api/categorias/${id}`), {
        method: 'DELETE',
        headers: authHeaders(),
      });
      if (res.status === 204 || res.ok) {
        toast.success('Categoria excluída.');
      } else if (res.status === 404) {
        // Linha obsoleta (removida em outra sessão): a lista recarregada corrige.
        setErroExclusao('Categoria não encontrada. A lista foi atualizada.');
      } else if (res.status === 409) {
        // Em uso por Produtos: a mensagem do servidor traz a contagem.
        setErroExclusao(await mensagemDoServidor(res, MENSAGEM_ERRO_EXCLUIR));
      } else {
        setErroExclusao(MENSAGEM_ERRO_EXCLUIR);
      }
    } catch {
      setErroExclusao(MENSAGEM_ERRO_EXCLUIR);
    } finally {
      setExcluindo(false);
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
        <h2 className="text-heading-md">Categorias</h2>
        <CardDescription>
          Cadastre, renomeie e remova as categorias de produto da sua empresa.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form onSubmit={cadastrar} className="flex flex-col gap-2" noValidate>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
            <div className="flex flex-col gap-2 sm:w-32">
              <Label htmlFor="categoria-codigo">Código da categoria</Label>
              <Input
                id="categoria-codigo"
                value={codigo}
                maxLength={CODIGO_MAX}
                onChange={(event) => setCodigo(event.target.value)}
              />
            </div>
            <div className="flex flex-1 flex-col gap-2">
              <Label htmlFor="categoria-nome">Nome da categoria</Label>
              <Input
                id="categoria-nome"
                value={nome}
                maxLength={NOME_MAX}
                onChange={(event) => setNome(event.target.value)}
              />
            </div>
            <Button
              type="submit"
              disabled={enviando || codigo.trim() === '' || nome.trim() === ''}
            >
              {enviando ? 'Adicionando...' : 'Adicionar categoria'}
            </Button>
          </div>
          {erro && (
            <p role="alert" className="text-body text-destructive">
              {erro}
            </p>
          )}
        </form>

        {erroExclusao && (
          <p role="alert" className="text-body text-destructive">
            {erroExclusao}
          </p>
        )}
        {erroCarregar && (
          <p role="alert" className="text-body text-destructive">
            {erroCarregar}
          </p>
        )}
        {!erroCarregar && carregou && categorias.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhuma categoria cadastrada ainda.</p>
        )}
        {!erroCarregar && categorias.length > 0 && (
          <ul className="flex flex-col gap-2">
            {categorias.map((c) =>
              editando?.id === c.id ? (
                <li key={c.id} className="border-b border-border pb-2 last:border-b-0 last:pb-0">
                  <form
                    onSubmit={salvarEdicao}
                    className="flex flex-col gap-2 sm:flex-row sm:items-end"
                    noValidate
                  >
                    <div className="flex flex-col gap-2 sm:w-32">
                      <Label htmlFor={`categoria-codigo-${c.id}`}>Código</Label>
                      <Input
                        id={`categoria-codigo-${c.id}`}
                        value={editCodigo}
                        maxLength={CODIGO_MAX}
                        onChange={(event) => setEditCodigo(event.target.value)}
                      />
                    </div>
                    <div className="flex flex-1 flex-col gap-2">
                      <Label htmlFor={`categoria-nome-${c.id}`}>Nome</Label>
                      <Input
                        id={`categoria-nome-${c.id}`}
                        value={editNome}
                        maxLength={NOME_MAX}
                        onChange={(event) => setEditNome(event.target.value)}
                      />
                    </div>
                    <div className="flex gap-2">
                      <Button
                        type="submit"
                        size="sm"
                        disabled={salvando || editCodigo.trim() === '' || editNome.trim() === ''}
                      >
                        {salvando ? 'Salvando...' : 'Salvar'}
                      </Button>
                      <Button type="button" variant="outline" size="sm" onClick={cancelarEdicao}>
                        Cancelar
                      </Button>
                    </div>
                  </form>
                  {erroEdicao && (
                    <p role="alert" className="text-body mt-2 text-destructive">
                      {erroEdicao}
                    </p>
                  )}
                </li>
              ) : (
                <li
                  key={c.id}
                  className="text-body flex items-center justify-between gap-2 border-b border-border pb-2 last:border-b-0 last:pb-0"
                >
                  <div className="flex min-w-0 items-baseline gap-2">
                    <span className="text-muted-foreground shrink-0 font-medium">{c.codigo}</span>
                    <span className="min-w-0 break-words">{c.nome}</span>
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      aria-label={`Editar categoria ${c.nome}`}
                      onClick={() => iniciarEdicao(c)}
                    >
                      Editar
                    </Button>
                    <Button
                      type="button"
                      variant="destructive"
                      size="sm"
                      aria-label={`Excluir categoria ${c.nome}`}
                      onClick={() => setExclusaoPendente(c)}
                      disabled={excluindo}
                    >
                      Excluir
                    </Button>
                  </div>
                </li>
              ),
            )}
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
        title={`Excluir a categoria "${exclusaoPendente?.nome ?? ''}"?`}
        description="A categoria é removida da lista. Esta ação não pode ser desfeita."
        confirmLabel="Excluir"
      />
    </Card>
  );
}

export default CategoriasSection;
