import { useState, type FormEvent } from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { useAuth } from '@/lib/auth';
import { rotuloPapel } from '@/lib/promocao';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Seção "Cadastrar usuário" (`/admin/usuarios`) — feedback Ferreira Costa
 * (direção, 2026-10-21): "a questão de incluir usuários somente pelo convite
 * não funcionou na prática... o ADM pode incluir novos usuários também...
 * podendo incluir nos níveis que o adm quiser e poder usar ou não 2FA".
 *
 * Segundo caminho além do Convite (`ConvitesSection`, que continua existindo
 * como está): aqui o `adm` digita nome/e-mail/senha, escolhe livremente o
 * papel (inclusive `adm`) e decide se ESSA conta específica é obrigada a
 * configurar MFA — independente do papel e do flag de MFA da Empresa
 * inteira (Configurações → Segurança). A conta nasce pronta para logar, sem
 * link nem e-mail de verificação: quem cadastra já está vouching por ela.
 *
 * Só monta para `adm` (gate espelhado de `RequireRole("adm")` no backend —
 * diferente de `ConvitesSection`/`GestaoUsuariosSection`, que liberam
 * `gestor`+). `onCriada` é chamado após um 201 para o chamador (UsuariosPage)
 * poder refazer a listagem de `GestaoUsuariosSection`.
 */

const PAPEIS: string[] = ['usuario', 'almoxarife', 'gestor', 'adm'];

const MENSAGEM_ERRO_PADRAO = 'Não foi possível criar a conta.';
const MENSAGEM_EMAIL_JA_TEM_CONTA = 'Este e-mail já tem conta.';

interface ErroEnvelope {
  error?: { code?: string; message?: string };
}

function mensagemDeErro(codigo: string | undefined, mensagemServidor: string | undefined): string {
  if (codigo === 'CONFLICT') {
    return MENSAGEM_EMAIL_JA_TEM_CONTA;
  }
  if (codigo === 'VALIDATION_ERROR' && mensagemServidor) {
    return mensagemServidor;
  }
  return MENSAGEM_ERRO_PADRAO;
}

export function CriarUsuarioSection({ onCriada }: { onCriada?: () => void }) {
  const { usuario } = useAuth();
  const ehAdm = usuario?.papel === 'adm';

  const [nome, setNome] = useState('');
  const [email, setEmail] = useState('');
  const [senha, setSenha] = useState('');
  const [papel, setPapel] = useState('usuario');
  const [mfaObrigatorioIndividual, setMfaObrigatorioIndividual] = useState(false);
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [sucesso, setSucesso] = useState<string | null>(null);

  if (!ehAdm) {
    return null;
  }

  function limparFormulario() {
    setNome('');
    setEmail('');
    setSenha('');
    setPapel('usuario');
    setMfaObrigatorioIndividual(false);
  }

  async function enviar(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enviando) {
      return;
    }
    setErro(null);
    setSucesso(null);
    setEnviando(true);
    try {
      const res = await fetch(apiUrl('/api/usuarios'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({
          nome,
          email,
          senha,
          papel,
          mfaObrigatorioIndividual,
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as ErroEnvelope;
        setErro(mensagemDeErro(body.error?.code, body.error?.message));
        return;
      }
      const body = (await res.json()) as { usuario?: { nome: string } };
      setSucesso(`Conta de ${body.usuario?.nome ?? nome} criada.`);
      limparFormulario();
      onCriada?.();
    } catch {
      setErro(MENSAGEM_ERRO_PADRAO);
    } finally {
      setEnviando(false);
    }
  }

  const desabilitado = enviando || nome.trim() === '' || email.trim() === '' || senha.trim() === '';

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Cadastrar usuário</h2>
        <CardDescription>
          Crie uma conta direto, sem convite — você escolhe o papel e a senha inicial. Avise a
          pessoa por fora (WhatsApp, por exemplo).
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={enviar} className="flex flex-col gap-4" noValidate>
          <div className="flex flex-col gap-2">
            <Label htmlFor="criar-usuario-nome">Nome</Label>
            <Input
              id="criar-usuario-nome"
              value={nome}
              onChange={(event) => setNome(event.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="criar-usuario-email">E-mail</Label>
            <Input
              id="criar-usuario-email"
              type="email"
              autoComplete="off"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="criar-usuario-senha">Senha inicial</Label>
            <Input
              id="criar-usuario-senha"
              type="text"
              autoComplete="off"
              value={senha}
              onChange={(event) => setSenha(event.target.value)}
            />
          </div>
          <div className="flex flex-col gap-2">
            <Label htmlFor="criar-usuario-papel">Papel</Label>
            <select
              id="criar-usuario-papel"
              value={papel}
              onChange={(event) => setPapel(event.target.value)}
              className="text-body min-h-touch-target-min rounded-md border border-input bg-transparent px-3"
            >
              {PAPEIS.map((p) => (
                <option key={p} value={p}>
                  {rotuloPapel(p)}
                </option>
              ))}
            </select>
          </div>
          <label className="flex items-center gap-2 text-body">
            <input
              type="checkbox"
              checked={mfaObrigatorioIndividual}
              onChange={(event) => setMfaObrigatorioIndividual(event.target.checked)}
            />
            Exigir autenticação em duas etapas (2FA) para esta conta
          </label>
          <Button type="submit" disabled={desabilitado} className="self-start">
            {enviando ? 'Criando...' : 'Criar conta'}
          </Button>
        </form>

        {erro && (
          <p role="alert" className="text-body text-destructive mt-4">
            {erro}
          </p>
        )}
        {sucesso && <output className="text-body text-muted-foreground mt-4 block">{sucesso}</output>}
      </CardContent>
    </Card>
  );
}

export default CriarUsuarioSection;
