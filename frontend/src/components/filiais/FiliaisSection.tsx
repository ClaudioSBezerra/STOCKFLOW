import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { apiUrl, authHeaders } from '@/lib/api';
import { formatarCEP, formatarCNPJ, maiusculasAscii } from '@/lib/formatacao';

/**
 * Seção "Filiais" de `/cadastros/filiais` (Story 12.1, FR-51 +
 * spec-filial-dados-fiscais). Só é montada para `gestor`+ (gate em
 * `FiliaisPage`; o servidor também recusa com 403 abaixo de `gestor`).
 *
 * Lista `GET /api/filiais`, cadastra (`POST /api/filiais`) e edita
 * (`PUT /api/filiais/{id}`) com o MESMO formulário: nome (obrigatório) +
 * CNPJ, Inscrição Estadual e endereço, todos opcionais. O endereço é um
 * bloco (se algum campo for preenchido, todos menos complemento passam a ser
 * obrigatórios) — a regra é do servidor; `400`/`409` mostram a mensagem do
 * PRÓPRIO servidor em `<p role="alert">`. A lista mostra CNPJ formatado e
 * Cidade/UF e é recarregada após cada gravação. Sem excluir Filial.
 *
 * Edição: "Editar" fica desabilitado enquanto um salvamento está em
 * andamento; com alterações não salvas no formulário, pede confirmação
 * (`ConfirmDialog`) antes de descartá-las; ao entrar em edição, rola até o
 * formulário e foca o primeiro campo. O PUT manda o formulário inteiro (o
 * servidor aceita parcial, mas a tela sempre conhece todos os campos).
 */

interface EnderecoFilial {
  logradouro: string;
  numero: string;
  complemento: string;
  bairro: string;
  cidade: string;
  cep: string;
  uf: string;
}

interface Filial {
  id: string;
  nome: string;
  cnpj?: string | null;
  inscricaoEstadual?: string | null;
  endereco?: EnderecoFilial | null;
}

interface FormularioFilial {
  nome: string;
  cnpj: string;
  inscricaoEstadual: string;
  endereco: EnderecoFilial;
}

const NOME_MAX = 255;

const UFS = [
  'AC', 'AL', 'AP', 'AM', 'BA', 'CE', 'DF', 'ES', 'GO', 'MA', 'MT', 'MS', 'MG', 'PA',
  'PB', 'PR', 'PE', 'PI', 'RJ', 'RN', 'RS', 'RO', 'RR', 'SC', 'SP', 'SE', 'TO',
];

const ENDERECO_VAZIO: EnderecoFilial = {
  logradouro: '',
  numero: '',
  complemento: '',
  bairro: '',
  cidade: '',
  cep: '',
  uf: '',
};

const FORMULARIO_VAZIO: FormularioFilial = {
  nome: '',
  cnpj: '',
  inscricaoEstadual: '',
  endereco: ENDERECO_VAZIO,
};

const MENSAGEM_ERRO_CARREGAR = 'Não foi possível carregar as filiais. Recarregue a página.';
const MENSAGEM_ERRO_CADASTRO =
  'Não foi possível cadastrar a filial agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_EDICAO =
  'Não foi possível salvar a filial agora. Tente novamente em instantes.';

function formularioDaFilial(f: Filial): FormularioFilial {
  const e = f.endereco;
  return {
    nome: f.nome,
    cnpj: f.cnpj ? formatarCNPJ(f.cnpj) : '',
    inscricaoEstadual: f.inscricaoEstadual ?? '',
    endereco: e
      ? {
          logradouro: e.logradouro ?? '',
          numero: e.numero ?? '',
          complemento: e.complemento ?? '',
          bairro: e.bairro ?? '',
          cidade: e.cidade ?? '',
          cep: e.cep ? formatarCEP(e.cep) : '',
          uf: e.uf ?? '',
        }
      : ENDERECO_VAZIO,
  };
}

async function mensagemDoServidor(res: Response, padrao: string): Promise<string> {
  const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
  return body.error?.message || padrao;
}

const selectClasse =
  'text-body min-h-touch-target-min rounded-md border border-border bg-background px-3';

export function FiliaisSection() {
  const [filiais, setFiliais] = useState<Filial[]>([]);
  const [form, setForm] = useState<FormularioFilial>(FORMULARIO_VAZIO);
  // null = cadastrando uma nova; id = editando essa Filial.
  const [editandoId, setEditandoId] = useState<string | null>(null);
  // Estado do formulário ao abrir (vazio ou a Filial em edição): diferente
  // dele = há alterações não salvas.
  const [base, setBase] = useState<FormularioFilial>(FORMULARIO_VAZIO);
  // Filial cujo "Editar" espera confirmação para descartar alterações.
  const [edicaoPendente, setEdicaoPendente] = useState<Filial | null>(null);
  // Incrementado a cada entrada em edição: dispara rolagem + foco.
  const [pedidoFoco, setPedidoFoco] = useState(0);
  const formRef = useRef<HTMLFormElement>(null);
  const nomeRef = useRef<HTMLInputElement>(null);
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [carregou, setCarregou] = useState(false);

  const carregar = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/filiais'), { headers: authHeaders() });
      if (!res.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { filiais: Filial[] };
      setFiliais(body.filiais ?? []);
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

  function campo<K extends keyof Omit<FormularioFilial, 'endereco'>>(chave: K, valor: string) {
    setForm((f) => ({ ...f, [chave]: valor }));
  }

  function campoEndereco(chave: keyof EnderecoFilial, valor: string) {
    setForm((f) => ({ ...f, endereco: { ...f.endereco, [chave]: valor } }));
  }

  useEffect(() => {
    if (pedidoFoco === 0) return;
    formRef.current?.scrollIntoView?.({ behavior: 'smooth', block: 'start' });
    nomeRef.current?.focus();
  }, [pedidoFoco]);

  const temAlteracoes = JSON.stringify(form) !== JSON.stringify(base);

  function abrirEdicao(f: Filial) {
    const inicial = formularioDaFilial(f);
    setErro(null);
    setEditandoId(f.id);
    setForm(inicial);
    setBase(inicial);
    setPedidoFoco((n) => n + 1);
  }

  function editar(f: Filial) {
    if (enviando) return;
    if (temAlteracoes) {
      setEdicaoPendente(f);
      return;
    }
    abrirEdicao(f);
  }

  function cancelarEdicao() {
    setErro(null);
    setEditandoId(null);
    setForm(FORMULARIO_VAZIO);
    setBase(FORMULARIO_VAZIO);
  }

  async function salvar(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enviando || form.nome.trim() === '') {
      return;
    }
    const editando = editandoId !== null;
    const padrao = editando ? MENSAGEM_ERRO_EDICAO : MENSAGEM_ERRO_CADASTRO;
    setErro(null);
    setEnviando(true);
    try {
      const res = await fetch(
        apiUrl(editando ? `/api/filiais/${encodeURIComponent(editandoId)}` : '/api/filiais'),
        {
          method: editando ? 'PUT' : 'POST',
          headers: { 'Content-Type': 'application/json', ...authHeaders() },
          body: JSON.stringify(form),
        },
      );
      if (res.status === 400 || res.status === 404 || res.status === 409) {
        setErro(await mensagemDoServidor(res, padrao));
        return;
      }
      if (!res.ok) {
        setErro(padrao);
        return;
      }
      toast.success(editando ? 'Filial atualizada.' : 'Filial criada.');
      setEditandoId(null);
      setForm(FORMULARIO_VAZIO);
      setBase(FORMULARIO_VAZIO);
      await carregar();
    } catch {
      setErro(padrao);
    } finally {
      setEnviando(false);
    }
  }

  const editando = editandoId !== null;
  const textoBotao = editando
    ? enviando
      ? 'Salvando...'
      : 'Salvar alterações'
    : enviando
      ? 'Adicionando...'
      : 'Adicionar filial';

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Filiais</h2>
        <CardDescription>
          Cadastre as filiais da sua empresa. Cada estoque é vinculado a uma filial e usa o
          endereço dela. CNPJ, Inscrição Estadual e endereço são opcionais e aparecem na
          Pré-Nota.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form
          ref={formRef}
          onSubmit={salvar}
          className="flex flex-col gap-3"
          noValidate
          aria-label={editando ? 'Editar filial' : 'Nova filial'}
        >
          {editando && <h3 className="text-body font-semibold">Editar filial</h3>}
          <div className="flex flex-col gap-2">
            <Label htmlFor="filial-nome">Nome da filial</Label>
            <Input
              ref={nomeRef}
              id="filial-nome"
              value={form.nome}
              maxLength={NOME_MAX}
              onChange={(event) => campo('nome', event.target.value)}
            />
          </div>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-2">
              <Label htmlFor="filial-cnpj">CNPJ (opcional)</Label>
              <Input
                id="filial-cnpj"
                value={form.cnpj}
                maxLength={18}
                inputMode="text"
                autoCapitalize="characters"
                autoCorrect="off"
                spellCheck={false}
                placeholder="00.000.000/0000-00"
                aria-describedby="filial-cnpj-dica"
                onChange={(event) => campo('cnpj', maiusculasAscii(event.target.value))}
              />
              <p id="filial-cnpj-dica" className="text-label text-muted-foreground">
                Aceita CNPJ numérico ou alfanumérico (letras A-Z).
              </p>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="filial-ie">Inscrição Estadual (opcional)</Label>
              <Input
                id="filial-ie"
                value={form.inscricaoEstadual}
                maxLength={20}
                placeholder="ex.: ISENTO"
                onChange={(event) => campo('inscricaoEstadual', event.target.value)}
              />
            </div>
          </div>
          <fieldset className="flex flex-col gap-3 rounded-md border border-border p-3">
            <legend className="text-body px-1 text-muted-foreground">
              Endereço (opcional — se preencher, só o complemento pode ficar vazio)
            </legend>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_8rem]">
              <div className="flex flex-col gap-2">
                <Label htmlFor="filial-logradouro">Logradouro</Label>
                <Input
                  id="filial-logradouro"
                  value={form.endereco.logradouro}
                  maxLength={255}
                  onChange={(event) => campoEndereco('logradouro', event.target.value)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="filial-numero">Número</Label>
                <Input
                  id="filial-numero"
                  value={form.endereco.numero}
                  maxLength={20}
                  onChange={(event) => campoEndereco('numero', event.target.value)}
                />
              </div>
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-2">
                <Label htmlFor="filial-complemento">Complemento</Label>
                <Input
                  id="filial-complemento"
                  value={form.endereco.complemento}
                  maxLength={255}
                  onChange={(event) => campoEndereco('complemento', event.target.value)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="filial-bairro">Bairro</Label>
                <Input
                  id="filial-bairro"
                  value={form.endereco.bairro}
                  maxLength={255}
                  onChange={(event) => campoEndereco('bairro', event.target.value)}
                />
              </div>
            </div>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_8rem_6rem]">
              <div className="flex flex-col gap-2">
                <Label htmlFor="filial-cidade">Cidade</Label>
                <Input
                  id="filial-cidade"
                  value={form.endereco.cidade}
                  maxLength={255}
                  onChange={(event) => campoEndereco('cidade', event.target.value)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="filial-cep">CEP</Label>
                <Input
                  id="filial-cep"
                  value={form.endereco.cep}
                  maxLength={9}
                  inputMode="numeric"
                  placeholder="00000-000"
                  onChange={(event) => campoEndereco('cep', event.target.value)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="filial-uf">UF</Label>
                <select
                  id="filial-uf"
                  value={form.endereco.uf}
                  onChange={(event) => campoEndereco('uf', event.target.value)}
                  className={selectClasse}
                >
                  <option value="">—</option>
                  {UFS.map((uf) => (
                    <option key={uf} value={uf}>
                      {uf}
                    </option>
                  ))}
                </select>
              </div>
            </div>
          </fieldset>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" disabled={enviando || form.nome.trim() === ''}>
              {textoBotao}
            </Button>
            {editando && (
              <Button type="button" variant="outline" onClick={cancelarEdicao} disabled={enviando}>
                Cancelar
              </Button>
            )}
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
        {!erroCarregar && carregou && filiais.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhuma filial cadastrada ainda.</p>
        )}
        {!erroCarregar && filiais.length > 0 && (
          <div className="overflow-x-auto">
            <table className="text-body w-full border-collapse">
              <thead>
                <tr className="border-b border-border text-left text-muted-foreground">
                  <th className="py-2 pr-3 font-medium">Nome</th>
                  <th className="py-2 pr-3 font-medium">CNPJ</th>
                  <th className="py-2 pr-3 font-medium">Cidade/UF</th>
                  <th className="py-2 font-medium">
                    <span className="sr-only">Ações</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {filiais.map((f) => (
                  <tr key={f.id} className="border-b border-border last:border-b-0">
                    <td className="min-w-0 break-words py-2 pr-3">{f.nome}</td>
                    <td className="whitespace-nowrap py-2 pr-3">
                      {f.cnpj ? formatarCNPJ(f.cnpj) : '—'}
                    </td>
                    <td className="py-2 pr-3">
                      {f.endereco?.cidade ? `${f.endereco.cidade}/${f.endereco.uf}` : '—'}
                    </td>
                    <td className="py-2 text-right">
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        aria-label={`Editar ${f.nome}`}
                        disabled={enviando}
                        onClick={() => editar(f)}
                      >
                        Editar
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
      <ConfirmDialog
        open={edicaoPendente !== null}
        onOpenChange={(aberto) => {
          if (!aberto) {
            setEdicaoPendente(null);
          }
        }}
        onConfirm={() => {
          if (edicaoPendente) {
            abrirEdicao(edicaoPendente);
          }
          setEdicaoPendente(null);
        }}
        title="Descartar alterações não salvas?"
        description={`O formulário tem alterações que ainda não foram salvas. Abrir "${edicaoPendente?.nome ?? ''}" para edição descarta essas alterações.`}
        confirmLabel="Descartar e editar"
        cancelLabel="Continuar editando"
      />
    </Card>
  );
}

export default FiliaisSection;
