import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Seção "Convites" (`/configuracoes`, Story 9.3, spec-9-3). Montada só para
 * `gestor`/`adm` — o mesmo gate de `GestaoUsuariosSection`; o 403 real é do
 * middleware do backend, este gate só evita montar uma seção inútil.
 *
 * O autocadastro deixou de ser aberto: uma conta nova só nasce a partir de um
 * convite NOMINAL (para um e-mail específico) e de USO ÚNICO. Aqui o `gestor`:
 *
 *  - emite um convite -> `POST /api/convites {"email"}`, e recebe de volta o
 *    LINK para compartilhar com a pessoa (por WhatsApp, e-mail próprio, o que
 *    for). O produto NÃO envia esse e-mail: quem compartilha é o emissor.
 *  - vê a lista de `GET /api/convites` com a `situacao` de cada um
 *    (pendente/usado/revogado/expirado) — derivada no servidor.
 *  - cancela um convite pendente -> `POST /api/convites/{id}/revogacao`, sob
 *    `ConfirmDialog` (a ação reduz acesso, como "Desativar" na seção de
 *    Gestão de Usuários).
 *
 * O link fica visível em um campo somente-leitura, que é o caminho PRIMÁRIO de
 * cópia (selecionar e copiar sempre funciona). O botão "Copiar" é conveniência
 * sobre `navigator.clipboard`, que nem sempre existe — daí o `try/catch`.
 *
 * Falha de carga e falha de ação viram mensagem inline `role="alert"` (sem
 * toast, molde de `ConfiguracoesPage`); toda ação — sucesso OU falha — refaz o
 * `GET /api/convites`.
 */

interface Convite {
  id: string;
  email: string;
  situacao: 'pendente' | 'usado' | 'revogado' | 'expirado';
  expiraEm: string;
  criadoEm: string;
  criadoPorNome: string;
  link?: string;
}

interface ErroEnvelope {
  error?: { code?: string; message?: string };
}

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar a lista de convites. Recarregue a página.';
const MENSAGEM_ERRO_EMITIR = 'Não foi possível emitir o convite.';
const MENSAGEM_ERRO_REVOGAR = 'Não foi possível cancelar o convite.';
const MENSAGEM_EMAIL_INVALIDO = 'Informe um e-mail válido para convidar.';
const MENSAGEM_EMAIL_JA_TEM_CONTA = 'Este e-mail já tem conta nesta empresa.';

const rotuloSituacao: Record<Convite['situacao'], string> = {
  pendente: 'Pendente',
  usado: 'Utilizado',
  revogado: 'Cancelado',
  expirado: 'Expirado',
};

function mensagemDeErroEmissao(codigo: string | undefined): string {
  if (codigo === 'VALIDATION_ERROR') {
    return MENSAGEM_EMAIL_INVALIDO;
  }
  if (codigo === 'CONFLICT') {
    return MENSAGEM_EMAIL_JA_TEM_CONTA;
  }
  return MENSAGEM_ERRO_EMITIR;
}

function formatarData(iso: string): string {
  const data = new Date(iso);
  return Number.isNaN(data.getTime()) ? '' : data.toLocaleDateString('pt-BR');
}

export function ConvitesSection() {
  const { usuario } = useAuth();
  const podeConvidar = rankPapel(usuario?.papel ?? '') >= rankPapel('gestor');

  const [convites, setConvites] = useState<Convite[]>([]);
  const [email, setEmail] = useState('');
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [erroAcao, setErroAcao] = useState<string | null>(null);
  const [emitindo, setEmitindo] = useState(false);
  const [acaoEmCurso, setAcaoEmCurso] = useState(false);
  const [linkNovo, setLinkNovo] = useState<string | null>(null);
  const [copiado, setCopiado] = useState(false);
  const [conviteARevogar, setConviteARevogar] = useState<Convite | null>(null);

  const carregar = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/convites'), { headers: authHeaders() });
      if (!res.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { convites: Convite[] };
      setConvites(body.convites ?? []);
      setErroCarregar(null);
    } catch {
      setErroCarregar(MENSAGEM_ERRO_CARREGAR);
    }
  }, []);

  useEffect(() => {
    void (async () => {
      if (podeConvidar) {
        await carregar();
      }
    })();
  }, [podeConvidar, carregar]);

  async function emitir(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    // Mesmo guard de duplo-submit de CadastroPage: o `disabled` do botão só
    // reflete `emitindo` no próximo repaint, e um convite é de uso único —
    // dois POSTs criariam dois convites para a mesma pessoa.
    if (emitindo) {
      return;
    }
    setErroAcao(null);
    setCopiado(false);

    if (!email.trim()) {
      setErroAcao(MENSAGEM_EMAIL_INVALIDO);
      return;
    }

    setEmitindo(true);
    try {
      const res = await fetch(apiUrl('/api/convites'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ email }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as ErroEnvelope;
        setErroAcao(mensagemDeErroEmissao(body.error?.code));
        return;
      }
      const body = (await res.json()) as { convite?: Convite };
      setLinkNovo(body.convite?.link ?? null);
      setEmail('');
    } catch {
      setErroAcao(MENSAGEM_ERRO_EMITIR);
    } finally {
      setEmitindo(false);
      // Sucesso OU falha: a lista é refeita para refletir o estado real.
      await carregar();
    }
  }

  async function revogar(id: string) {
    if (acaoEmCurso) {
      return;
    }
    setErroAcao(null);
    setAcaoEmCurso(true);
    try {
      const res = await fetch(apiUrl(`/api/convites/${id}/revogacao`), {
        method: 'POST',
        headers: authHeaders(),
      });
      if (!res.ok) {
        setErroAcao(MENSAGEM_ERRO_REVOGAR);
      }
    } catch {
      setErroAcao(MENSAGEM_ERRO_REVOGAR);
    } finally {
      setAcaoEmCurso(false);
      await carregar();
    }
  }

  async function copiarLink(link: string) {
    // `navigator.clipboard` não existe em todo contexto (jsdom, HTTP sem TLS,
    // permissão negada). O campo somente-leitura ao lado continua sendo o
    // caminho garantido, então uma falha aqui é silenciosa de propósito.
    try {
      await navigator.clipboard.writeText(link);
      setCopiado(true);
    } catch {
      setCopiado(false);
    }
  }

  function confirmarRevogacao() {
    if (!conviteARevogar) {
      return;
    }
    const { id } = conviteARevogar;
    setConviteARevogar(null);
    void revogar(id);
  }

  if (!podeConvidar) {
    return null;
  }

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Convites</h2>
        <CardDescription>
          Convide alguém pelo e-mail e compartilhe o link gerado. Sem convite, ninguém cria conta
          nesta empresa.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form onSubmit={emitir} className="flex flex-col gap-2" noValidate>
          <Label htmlFor="email-convite">E-mail da pessoa convidada</Label>
          <div className="flex flex-col gap-2 sm:flex-row">
            <Input
              id="email-convite"
              type="email"
              autoComplete="off"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
            <Button type="submit" disabled={emitindo}>
              {emitindo ? 'Enviando...' : 'Convidar'}
            </Button>
          </div>
        </form>

        {erroAcao && (
          <p role="alert" className="text-body text-destructive">
            {erroAcao}
          </p>
        )}

        {linkNovo && (
          <div className="flex flex-col gap-2 rounded-md border border-border p-3">
            <Label htmlFor="link-convite">Link do convite</Label>
            <p className="text-label text-muted-foreground">
              Compartilhe este link com a pessoa convidada. Ele vale uma única vez.
            </p>
            <div className="flex flex-col gap-2 sm:flex-row">
              <Input id="link-convite" readOnly value={linkNovo} />
              <Button type="button" variant="outline" onClick={() => void copiarLink(linkNovo)}>
                Copiar
              </Button>
            </div>
            {copiado && <output className="text-label text-muted-foreground">Link copiado.</output>}
          </div>
        )}

        {erroCarregar && (
          <p role="alert" className="text-body text-destructive">
            {erroCarregar}
          </p>
        )}
        {!erroCarregar && convites.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhum convite emitido.</p>
        )}
        {!erroCarregar && convites.length > 0 && (
          <ul className="flex flex-col gap-3">
            {convites.map((c) => (
              <li
                key={c.id}
                className="flex flex-col gap-2 border-b border-border pb-3 last:border-b-0 last:pb-0"
              >
                <div className="flex flex-col">
                  <span className="text-body">{c.email}</span>
                  <span className="text-label text-muted-foreground">
                    {rotuloSituacao[c.situacao]}
                    {c.situacao === 'pendente' && ` — expira em ${formatarData(c.expiraEm)}`}
                  </span>
                  {c.criadoPorNome && (
                    <span className="text-label text-muted-foreground">
                      Convidado por {c.criadoPorNome}
                    </span>
                  )}
                </div>
                {c.situacao === 'pendente' && (
                  <div className="flex flex-col gap-2 sm:flex-row">
                    {c.link && <Input readOnly aria-label={`Link do convite de ${c.email}`} value={c.link} />}
                    <Button
                      type="button"
                      size="sm"
                      variant="destructive"
                      aria-label={`Cancelar convite de ${c.email}`}
                      onClick={() => setConviteARevogar(c)}
                      disabled={acaoEmCurso}
                    >
                      Cancelar convite
                    </Button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </CardContent>

      <ConfirmDialog
        open={conviteARevogar !== null}
        onOpenChange={(aberto) => {
          if (!aberto) {
            setConviteARevogar(null);
          }
        }}
        onConfirm={confirmarRevogacao}
        title={`Cancelar o convite de ${conviteARevogar?.email ?? ''}?`}
        description="O link deixa de funcionar imediatamente. Para convidar essa pessoa de novo será preciso emitir um convite novo."
        confirmLabel="Cancelar convite"
        cancelLabel="Voltar"
        confirmVariant="destructive"
      />
    </Card>
  );
}

export default ConvitesSection;
