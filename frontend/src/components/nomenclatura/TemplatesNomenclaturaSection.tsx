import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Seção "Templates de Nomenclatura" de `/configuracoes` (Story 10.6,
 * spec-10-6, AD-33). Só é montada para `adm`+ (gate em `ConfiguracoesPage`; o
 * servidor também recusa com 403 abaixo de `adm`).
 *
 * Lista `GET /api/nomenclatura-templates`, cadastra (`POST`), edita inline
 * (`PUT /api/nomenclatura-templates/{id}`) e exclui (`DELETE`, via
 * `ConfirmDialog` — nunca `window.confirm`). `maxLength` 255 espelha o banco.
 * Erros inline em `<p role="alert">`; `400`/`409` mostram a mensagem do
 * PRÓPRIO servidor (estrutura inválida, subtipo duplicado, contagem de
 * Produtos que bloqueia a exclusão). Editar um Template nunca altera Produtos
 * já cadastrados. O Template-marcador `[NOME LIVRE]` ÚNICO da Empresa (o
 * fallback "Genérico", AD-34) nunca oferece Excluir: o botão é renderizado
 * desabilitado com a explicação "fallback obrigatório".
 */

interface TemplateNomenclatura {
  id: string;
  subtipo: string;
  template: string;
}

const TEXTO_MAX = 255;
const MARCADOR_FALLBACK = '[NOME LIVRE]';

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar os templates de nomenclatura. Recarregue a página.';
const MENSAGEM_ERRO_CADASTRO =
  'Não foi possível cadastrar o template agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_EDITAR =
  'Não foi possível salvar o template agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_EXCLUIR =
  'Não foi possível excluir o template agora. Tente novamente em instantes.';

async function mensagemDoServidor(res: Response, padrao: string): Promise<string> {
  const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
  return body.error?.message ?? padrao;
}

export function TemplatesNomenclaturaSection() {
  const [templates, setTemplates] = useState<TemplateNomenclatura[]>([]);
  const [subtipo, setSubtipo] = useState('');
  const [texto, setTexto] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [carregou, setCarregou] = useState(false);

  const [editando, setEditando] = useState<TemplateNomenclatura | null>(null);
  const [editSubtipo, setEditSubtipo] = useState('');
  const [editTexto, setEditTexto] = useState('');
  const [erroEdicao, setErroEdicao] = useState<string | null>(null);
  const [salvando, setSalvando] = useState(false);

  const [exclusaoPendente, setExclusaoPendente] = useState<TemplateNomenclatura | null>(null);
  const [excluindo, setExcluindo] = useState(false);
  const [erroExclusao, setErroExclusao] = useState<string | null>(null);

  // Fallback obrigatório: o ÚNICO template-marcador da Empresa (identificado
  // pelo texto, não pelo subtipo — o adm pode renomear o subtipo).
  const marcadores = templates.filter((t) => t.template === MARCADOR_FALLBACK).length;
  const ehFallbackUnico = (t: TemplateNomenclatura) =>
    t.template === MARCADOR_FALLBACK && marcadores <= 1;

  const carregar = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/nomenclatura-templates'), { headers: authHeaders() });
      if (!res.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { templates: TemplateNomenclatura[] };
      setTemplates(body.templates ?? []);
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
    if (enviando || subtipo.trim() === '' || texto.trim() === '') {
      return;
    }
    setErro(null);
    setErroExclusao(null);
    setEnviando(true);
    try {
      const res = await fetch(apiUrl('/api/nomenclatura-templates'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ subtipo, template: texto }),
      });
      if (res.status === 400 || res.status === 409) {
        setErro(await mensagemDoServidor(res, MENSAGEM_ERRO_CADASTRO));
        return;
      }
      if (!res.ok) {
        setErro(MENSAGEM_ERRO_CADASTRO);
        return;
      }
      toast.success('Template criado.');
      setSubtipo('');
      setTexto('');
      await carregar();
    } catch {
      setErro(MENSAGEM_ERRO_CADASTRO);
    } finally {
      setEnviando(false);
    }
  }

  function iniciarEdicao(t: TemplateNomenclatura) {
    setEditando(t);
    setEditSubtipo(t.subtipo);
    setEditTexto(t.template);
    setErroEdicao(null);
  }

  function cancelarEdicao() {
    setEditando(null);
    setErroEdicao(null);
  }

  async function salvarEdicao(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!editando || salvando || editSubtipo.trim() === '' || editTexto.trim() === '') {
      return;
    }
    setErroEdicao(null);
    setErro(null);
    setErroExclusao(null);
    setSalvando(true);
    try {
      const res = await fetch(apiUrl(`/api/nomenclatura-templates/${editando.id}`), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ subtipo: editSubtipo, template: editTexto }),
      });
      if (res.status === 400 || res.status === 409) {
        setErroEdicao(await mensagemDoServidor(res, MENSAGEM_ERRO_EDITAR));
        return;
      }
      if (res.status === 404) {
        // Linha obsoleta (removida em outra sessão): fecha a edição e recarrega.
        setEditando(null);
        setErro('Template não encontrado. A lista foi atualizada.');
        await carregar();
        return;
      }
      if (!res.ok) {
        setErroEdicao(MENSAGEM_ERRO_EDITAR);
        return;
      }
      toast.success('Template atualizado.');
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
      const res = await fetch(apiUrl(`/api/nomenclatura-templates/${id}`), {
        method: 'DELETE',
        headers: authHeaders(),
      });
      if (res.status === 204 || res.ok) {
        toast.success('Template excluído.');
      } else if (res.status === 404) {
        // Linha obsoleta (removida em outra sessão): a lista recarregada corrige.
        setErroExclusao('Template não encontrado. A lista foi atualizada.');
      } else if (res.status === 409) {
        // Em uso por Produtos (com a contagem) ou fallback obrigatório.
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
        <h2 className="text-heading-md">Templates de Nomenclatura</h2>
        <CardDescription>
          Cadastre, edite e remova os padrões de nome dos produtos da sua empresa. Use campos entre
          colchetes, como [TIPO] ou [BITOLA], ou [NOME LIVRE] para aceitar qualquer nome. Editar um
          template não altera os produtos já cadastrados: a nova estrutura vale a partir do próximo
          cadastro ou renomeação.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form onSubmit={cadastrar} className="flex flex-col gap-2" noValidate>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
            <div className="flex flex-col gap-2 sm:w-64">
              <Label htmlFor="template-subtipo">Subtipo do template</Label>
              <Input
                id="template-subtipo"
                value={subtipo}
                maxLength={TEXTO_MAX}
                onChange={(event) => setSubtipo(event.target.value)}
              />
            </div>
            <div className="flex flex-1 flex-col gap-2">
              <Label htmlFor="template-texto">Estrutura do template</Label>
              <Input
                id="template-texto"
                value={texto}
                maxLength={TEXTO_MAX}
                placeholder="CABO [TIPO] [BITOLA]"
                onChange={(event) => setTexto(event.target.value)}
              />
            </div>
            <Button
              type="submit"
              disabled={enviando || subtipo.trim() === '' || texto.trim() === ''}
            >
              {enviando ? 'Adicionando...' : 'Adicionar template'}
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
        {!erroCarregar && carregou && templates.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhum template cadastrado ainda.</p>
        )}
        {!erroCarregar && templates.length > 0 && (
          <ul className="flex flex-col gap-2">
            {templates.map((t) =>
              editando?.id === t.id ? (
                <li key={t.id} className="border-b border-border pb-2 last:border-b-0 last:pb-0">
                  <form
                    onSubmit={salvarEdicao}
                    className="flex flex-col gap-2 sm:flex-row sm:items-end"
                    noValidate
                  >
                    <div className="flex flex-col gap-2 sm:w-64">
                      <Label htmlFor={`template-subtipo-${t.id}`}>Subtipo</Label>
                      <Input
                        id={`template-subtipo-${t.id}`}
                        value={editSubtipo}
                        maxLength={TEXTO_MAX}
                        onChange={(event) => setEditSubtipo(event.target.value)}
                      />
                    </div>
                    <div className="flex flex-1 flex-col gap-2">
                      <Label htmlFor={`template-texto-${t.id}`}>Template</Label>
                      <Input
                        id={`template-texto-${t.id}`}
                        value={editTexto}
                        maxLength={TEXTO_MAX}
                        onChange={(event) => setEditTexto(event.target.value)}
                      />
                    </div>
                    <div className="flex gap-2">
                      <Button
                        type="submit"
                        size="sm"
                        disabled={salvando || editSubtipo.trim() === '' || editTexto.trim() === ''}
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
                  key={t.id}
                  className="text-body flex items-center justify-between gap-2 border-b border-border pb-2 last:border-b-0 last:pb-0"
                >
                  <div className="flex min-w-0 flex-col gap-1">
                    <span className="min-w-0 break-words font-medium">{t.subtipo}</span>
                    <span className="text-muted-foreground min-w-0 break-words">{t.template}</span>
                    {ehFallbackUnico(t) && (
                      <span
                        id={`template-fallback-${t.id}`}
                        className="text-muted-foreground text-sm"
                      >
                        Fallback obrigatório: não pode ser excluído enquanto for o único template
                        [NOME LIVRE] da empresa.
                      </span>
                    )}
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      aria-label={`Editar template ${t.subtipo}`}
                      onClick={() => iniciarEdicao(t)}
                    >
                      Editar
                    </Button>
                    <Button
                      type="button"
                      variant="destructive"
                      size="sm"
                      aria-label={`Excluir template ${t.subtipo}`}
                      aria-describedby={ehFallbackUnico(t) ? `template-fallback-${t.id}` : undefined}
                      onClick={() => setExclusaoPendente(t)}
                      disabled={excluindo || ehFallbackUnico(t)}
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
        title={`Excluir o template "${exclusaoPendente?.subtipo ?? ''}"?`}
        description="O template é removido da lista. Esta ação não pode ser desfeita."
        confirmLabel="Excluir"
      />
    </Card>
  );
}

export default TemplatesNomenclaturaSection;
