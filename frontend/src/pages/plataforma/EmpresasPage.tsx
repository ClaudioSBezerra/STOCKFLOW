import { useCallback, useEffect, useState, type ComponentProps, type FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { cn } from '@/lib/utils';
import {
  ErroPlataforma,
  SLUG_MAX_EMPRESA,
  criarEmpresa,
  desativarEmpresa,
  formatarCNPJ,
  limparTokenPlataforma,
  listarEmpresas,
  logoutPlataforma,
  normalizarSlug,
  reativarEmpresa,
  type EmpresaResumo,
  type EnderecoEmpresa,
} from '@/lib/plataforma';

interface FormEmpresa {
  nomeFantasia: string;
  razaoSocial: string;
  cnpj: string;
  slug: string;
  logradouro: string;
  numero: string;
  complemento: string;
  bairro: string;
  cidade: string;
  cep: string;
  uf: string;
  admNome: string;
  admEmail: string;
}

const FORM_VAZIO: FormEmpresa = {
  nomeFantasia: '',
  razaoSocial: '',
  cnpj: '',
  slug: '',
  logradouro: '',
  numero: '',
  complemento: '',
  bairro: '',
  cidade: '',
  cep: '',
  uf: '',
  admNome: '',
  admEmail: '',
};

const MENSAGEM_SUCESSO = 'Empresa criada. O administrador receberá um e-mail para definir a senha.';
const MENSAGEM_FALHA_CRIAR = 'Não foi possível criar a empresa. Tente novamente em instantes.';
const MENSAGEM_FALHA_LISTA = 'Não foi possível carregar as empresas. Tente novamente em instantes.';
const MENSAGEM_FALHA_STATUS = 'Não foi possível alterar o status da empresa. Tente novamente em instantes.';

/** 401 depois da tentativa de renovação: a sessão do Dono acabou. */
function sessaoEncerrada(e: unknown): boolean {
  return e instanceof ErroPlataforma && e.status === 401;
}

function capitalizar(texto: string): string {
  return texto ? texto.charAt(0).toUpperCase() + texto.slice(1) : texto;
}

function formatarData(iso: string): string {
  const data = new Date(iso);
  return Number.isNaN(data.getTime()) ? '' : data.toLocaleDateString('pt-BR');
}

function enderecoResumido(e: EnderecoEmpresa): string {
  const rua = [e.logradouro, e.numero].filter(Boolean).join(', ');
  const complemento = e.complemento ? ` — ${e.complemento}` : '';
  const cep = e.cep.length === 8 ? `${e.cep.slice(0, 5)}-${e.cep.slice(5)}` : e.cep;
  return `${rua}${complemento} · ${e.bairro}, ${e.cidade}/${e.uf} · CEP ${cep}`;
}

interface CampoProps extends Omit<ComponentProps<'input'>, 'id' | 'value' | 'onChange'> {
  id: string;
  label: string;
  value: string;
  onChange: (valor: string) => void;
  dica?: string;
}

function Campo({ id, label, value, onChange, dica, className, ...rest }: CampoProps) {
  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-describedby={dica ? `${id}-dica` : undefined}
        {...rest}
      />
      {dica ? (
        <p id={`${id}-dica`} className="text-label text-muted-foreground">
          {dica}
        </p>
      ) : null}
    </div>
  );
}

function BadgeStatus({ status }: { status: string }) {
  const ativa = status === 'ativa';
  return (
    <span
      className={cn(
        'rounded-full px-2 py-0.5 text-label',
        ativa ? 'bg-success/10 text-on-tint-success' : 'bg-destructive/10 text-on-tint-destructive',
      )}
    >
      {ativa ? 'Ativa' : 'Inativa'}
    </span>
  );
}

/**
 * Área "Empresas" do Dono da Plataforma — Story 9.2 (spec-9-2). Cria a
 * Empresa (com o Ambiente de Treinamento e o primeiro `adm`), lista SÓ
 * metadado cadastral e desativa/reativa o par Empresa + Treinamento. Nenhum
 * dado operacional de Empresa aparece aqui, e o Dono nunca vê nem define a
 * senha do `adm`.
 */
export function EmpresasPage() {
  const navigate = useNavigate();
  const [empresas, setEmpresas] = useState<EmpresaResumo[] | null>(null);
  const [erroLista, setErroLista] = useState<string | null>(null);
  const [form, setForm] = useState<FormEmpresa>(FORM_VAZIO);
  const [slugEditado, setSlugEditado] = useState(false);
  const [enviando, setEnviando] = useState(false);
  const [erroForm, setErroForm] = useState<string | null>(null);
  const [paraDesativar, setParaDesativar] = useState<EmpresaResumo | null>(null);
  const [alterandoId, setAlterandoId] = useState<string | null>(null);

  const irParaLogin = useCallback(() => {
    limparTokenPlataforma();
    navigate('/plataforma/login', { replace: true });
  }, [navigate]);

  const aplicarFalhaLista = useCallback(
    (e: unknown) => {
      if (sessaoEncerrada(e)) {
        irParaLogin();
        return;
      }
      setErroLista(MENSAGEM_FALHA_LISTA);
      setEmpresas([]);
    },
    [irParaLogin],
  );

  // Recarga depois de criar, desativar ou reativar: a lista anterior fica na
  // tela até a nova chegar.
  const recarregar = useCallback(async () => {
    try {
      const lista = await listarEmpresas();
      setEmpresas(lista);
      setErroLista(null);
    } catch (e) {
      aplicarFalhaLista(e);
    }
  }, [aplicarFalhaLista]);

  // Carga inicial. Os setState ficam nos callbacks da promessa (nunca
  // síncronos no efeito), e `ativo` descarta a resposta de uma montagem já
  // desfeita (StrictMode).
  useEffect(() => {
    let ativo = true;
    listarEmpresas().then(
      (lista) => {
        if (ativo) {
          setEmpresas(lista);
          setErroLista(null);
        }
      },
      (e: unknown) => {
        if (ativo) {
          aplicarFalhaLista(e);
        }
      },
    );
    return () => {
      ativo = false;
    };
  }, [aplicarFalhaLista]);

  function atualizar(campo: keyof FormEmpresa, valor: string) {
    setForm((atual) => {
      const novo = { ...atual, [campo]: valor };
      // O endereço de acesso acompanha o Nome Fantasia até ser editado à mão.
      if (campo === 'nomeFantasia' && !slugEditado) {
        novo.slug = normalizarSlug(valor);
      }
      return novo;
    });
  }

  function atualizarSlug(valor: string) {
    setSlugEditado(valor !== '');
    setForm((atual) => ({ ...atual, slug: valor }));
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enviando) {
      return;
    }
    setErroForm(null);
    setEnviando(true);
    try {
      await criarEmpresa({
        nomeFantasia: form.nomeFantasia,
        razaoSocial: form.razaoSocial,
        cnpj: form.cnpj,
        slug: form.slug,
        endereco: {
          logradouro: form.logradouro,
          numero: form.numero,
          complemento: form.complemento,
          bairro: form.bairro,
          cidade: form.cidade,
          cep: form.cep,
          uf: form.uf,
        },
        admNome: form.admNome,
        admEmail: form.admEmail,
      });
      toast.success(MENSAGEM_SUCESSO);
      setForm(FORM_VAZIO);
      setSlugEditado(false);
      await recarregar();
    } catch (e) {
      if (sessaoEncerrada(e)) {
        irParaLogin();
        return;
      }
      if (e instanceof ErroPlataforma && (e.status === 400 || e.status === 409) && e.mensagem) {
        setErroForm(capitalizar(e.mensagem));
      } else {
        setErroForm(MENSAGEM_FALHA_CRIAR);
      }
    } finally {
      setEnviando(false);
    }
  }

  async function alterarStatus(empresa: EmpresaResumo, acao: 'desativar' | 'reativar') {
    setAlterandoId(empresa.id);
    try {
      if (acao === 'desativar') {
        await desativarEmpresa(empresa.id);
        toast.success(`${empresa.nomeFantasia} desativada.`);
      } else {
        await reativarEmpresa(empresa.id);
        toast.success(`${empresa.nomeFantasia} reativada.`);
      }
      await recarregar();
    } catch (e) {
      if (sessaoEncerrada(e)) {
        irParaLogin();
        return;
      }
      toast.error(MENSAGEM_FALHA_STATUS);
    } finally {
      setAlterandoId(null);
    }
  }

  async function sair() {
    await logoutPlataforma();
    navigate('/plataforma/login', { replace: true });
  }

  const slugExibido = form.slug || 'endereco';

  return (
    <div className="min-h-svh bg-background text-foreground">
      <header className="flex h-14 items-center justify-between gap-4 border-b border-border bg-card px-4">
        <h1 className="text-heading-md">Plataforma — Empresas</h1>
        <Button variant="outline" className="min-h-touch-target-min" onClick={() => void sair()}>
          Sair
        </Button>
      </header>

      <main className="mx-auto flex max-w-5xl flex-col gap-6 p-4 md:p-6">
        <Card>
          <CardHeader>
            <CardTitle>Nova Empresa</CardTitle>
            <CardDescription>
              Cria a empresa, o Ambiente de Treinamento dela (com dados de exemplo) e o primeiro
              administrador. O administrador recebe por e-mail o link para definir a própria senha.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmit} noValidate className="grid gap-4 md:grid-cols-2">
              <Campo
                id="nomeFantasia"
                label="Nome Fantasia"
                value={form.nomeFantasia}
                onChange={(v) => atualizar('nomeFantasia', v)}
                maxLength={241}
                required
              />
              <Campo
                id="razaoSocial"
                label="Razão Social"
                value={form.razaoSocial}
                onChange={(v) => atualizar('razaoSocial', v)}
                maxLength={255}
                required
              />
              <Campo
                id="cnpj"
                label="CNPJ"
                value={form.cnpj}
                onChange={(v) => atualizar('cnpj', v)}
                inputMode="numeric"
                maxLength={18}
                required
              />
              <Campo
                id="slug"
                label="Endereço de acesso"
                value={form.slug}
                onChange={atualizarSlug}
                maxLength={SLUG_MAX_EMPRESA}
                autoCapitalize="none"
                spellCheck={false}
                dica={`Empresa: /e/${slugExibido} · Treinamento: /e/${slugExibido}-treinamento`}
              />
              <Campo
                id="logradouro"
                label="Logradouro"
                value={form.logradouro}
                onChange={(v) => atualizar('logradouro', v)}
                required
              />
              <Campo
                id="numero"
                label="Número"
                value={form.numero}
                onChange={(v) => atualizar('numero', v)}
                maxLength={20}
                required
              />
              <Campo
                id="complemento"
                label="Complemento (opcional)"
                value={form.complemento}
                onChange={(v) => atualizar('complemento', v)}
              />
              <Campo
                id="bairro"
                label="Bairro"
                value={form.bairro}
                onChange={(v) => atualizar('bairro', v)}
                required
              />
              <Campo
                id="cidade"
                label="Cidade"
                value={form.cidade}
                onChange={(v) => atualizar('cidade', v)}
                required
              />
              <div className="grid grid-cols-2 gap-4">
                <Campo
                  id="cep"
                  label="CEP"
                  value={form.cep}
                  onChange={(v) => atualizar('cep', v)}
                  inputMode="numeric"
                  maxLength={9}
                  required
                />
                <Campo
                  id="uf"
                  label="UF"
                  value={form.uf}
                  onChange={(v) => atualizar('uf', v.toUpperCase())}
                  maxLength={2}
                  required
                />
              </div>
              <Campo
                id="admNome"
                label="Nome do primeiro administrador"
                value={form.admNome}
                onChange={(v) => atualizar('admNome', v)}
                required
              />
              <Campo
                id="admEmail"
                label="E-mail do primeiro administrador"
                type="email"
                autoComplete="off"
                value={form.admEmail}
                onChange={(v) => atualizar('admEmail', v)}
                required
              />

              {erroForm && (
                <p role="alert" className="text-body text-destructive md:col-span-2">
                  {erroForm}
                </p>
              )}

              <div className="md:col-span-2">
                <Button type="submit" className="min-h-touch-target-min" disabled={enviando}>
                  {enviando ? 'Criando...' : 'Criar empresa'}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>

        <section aria-labelledby="titulo-empresas" className="flex flex-col gap-3">
          <h2 id="titulo-empresas" className="text-body font-semibold">
            Empresas
          </h2>
          {empresas === null ? (
            <output className="text-body text-muted-foreground">Carregando...</output>
          ) : erroLista ? (
            <p role="alert" className="text-body text-destructive">
              {erroLista}
            </p>
          ) : empresas.length === 0 ? (
            <p className="text-body text-muted-foreground">Nenhuma empresa cadastrada ainda.</p>
          ) : (
            <ul className="flex flex-col gap-3">
              {empresas.map((empresa) => (
                <li key={empresa.id}>
                  <Card>
                    <CardContent className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                      <div className="flex min-w-0 flex-col gap-1 text-body">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-semibold">{empresa.nomeFantasia}</span>
                          <BadgeStatus status={empresa.status} />
                        </div>
                        <span>
                          {empresa.razaoSocial} · CNPJ {formatarCNPJ(empresa.cnpj)}
                        </span>
                        <span className="text-muted-foreground">{enderecoResumido(empresa.endereco)}</span>
                        <span>
                          Administrador:{' '}
                          {empresa.adm ? `${empresa.adm.nome} (${empresa.adm.email})` : '—'}
                        </span>
                        <span className="break-all">
                          Acesso: <code className="font-mono text-code">/e/{empresa.slug}</code>
                          {empresa.treinamento ? (
                            <>
                              {' '}
                              · Treinamento:{' '}
                              <code className="font-mono text-code">/e/{empresa.treinamento.slug}</code>
                            </>
                          ) : null}
                        </span>
                        <span className="text-muted-foreground">
                          Criada em {formatarData(empresa.criadoEm)}
                        </span>
                      </div>
                      <div className="shrink-0">
                        {empresa.status === 'ativa' ? (
                          <Button
                            variant="destructive"
                            className="min-h-touch-target-min"
                            aria-label={`Desativar ${empresa.nomeFantasia}`}
                            disabled={alterandoId === empresa.id}
                            onClick={() => setParaDesativar(empresa)}
                          >
                            Desativar
                          </Button>
                        ) : (
                          <Button
                            variant="outline"
                            className="min-h-touch-target-min"
                            aria-label={`Reativar ${empresa.nomeFantasia}`}
                            disabled={alterandoId === empresa.id}
                            onClick={() => void alterarStatus(empresa, 'reativar')}
                          >
                            Reativar
                          </Button>
                        )}
                      </div>
                    </CardContent>
                  </Card>
                </li>
              ))}
            </ul>
          )}
        </section>
      </main>

      <ConfirmDialog
        open={paraDesativar !== null}
        onOpenChange={(aberto) => {
          if (!aberto) {
            setParaDesativar(null);
          }
        }}
        onConfirm={() => {
          const alvo = paraDesativar;
          if (alvo) {
            void alterarStatus(alvo, 'desativar');
          }
        }}
        title={paraDesativar ? `Desativar ${paraDesativar.nomeFantasia}?` : 'Desativar empresa?'}
        description="Ninguém desta empresa nem do Ambiente de Treinamento dela conseguirá entrar enquanto ela estiver desativada. Nenhum dado é apagado — você pode reativá-la depois."
        confirmLabel="Desativar"
        confirmVariant="destructive"
      />
    </div>
  );
}

export default EmpresasPage;
