import { slugDaURL } from '@/lib/api';

/**
 * Qual app o `main.tsx` monta para o caminho atual — Story 9.2 (spec-9-2),
 * handoff (b) da Story 9.1. A escolha acontece ANTES de montar qualquer
 * provider, e as três apps são disjuntas:
 *
 * - `plataforma`: `/plataforma` e tudo abaixo — a área do Dono da
 *   Plataforma, com sessão própria (nunca `AuthProvider`/`CarrinhoProvider`);
 * - `empresa`: `/e/{slug}/...` com um slug canônico — a app de sempre, sob a
 *   Empresa do slug;
 * - `sem-empresa`: qualquer outro caminho — o login pela conta (Story 15.2):
 *   e-mail e senha descobrem a Empresa e a pessoa é levada para
 *   `/e/{slug}/` (a app da Empresa nunca sobe sem slug). Antes de montá-la,
 *   `main.tsx` chama `abrirEmpresaPadrao` (Story 15.4): num servidor de um
 *   cliente só (`EMPRESA_PADRAO`) a pessoa vai direto para `/e/{slug}/`.
 */
export type AppDeEntrada = 'plataforma' | 'empresa' | 'sem-empresa';

export function escolherApp(pathname: string): AppDeEntrada {
  if (pathname === '/plataforma' || pathname.startsWith('/plataforma/')) {
    return 'plataforma';
  }
  if (slugDaURL(pathname) !== '') {
    return 'empresa';
  }
  return 'sem-empresa';
}

// --- Empresa padrão de um servidor de um cliente só (Story 15.4, AD-36) ---

/** Prazo de `GET /api/entrada`: passou disso, a raiz mostra o login pela conta. */
export const TIMEOUT_EMPRESA_PADRAO_MS = 3000;

/**
 * GET /api/entrada — o slug da Empresa padrão do servidor, ou `null`. Nunca
 * `apiUrl`: a rota vive fora do prefixo de Empresa. Rede, timeout, não-`2xx`,
 * JSON inválido, `null` ou slug fora da forma canônica -> `null` (o slug é
 * validado de novo aqui para um corpo inesperado nunca virar redirecionamento
 * para caminho arbitrário).
 */
export async function buscarEmpresaPadrao(): Promise<string | null> {
  const controle = new AbortController();
  const timer = setTimeout(() => controle.abort(), TIMEOUT_EMPRESA_PADRAO_MS);
  try {
    const res = await fetch('/api/entrada', {
      credentials: 'same-origin',
      cache: 'no-store',
      signal: controle.signal,
    });
    if (!res.ok) return null;
    const body = (await res.json()) as { empresaPadrao?: unknown } | null;
    const slug = body?.empresaPadrao;
    if (typeof slug !== 'string' || slug === '' || slugDaURL(`/e/${slug}/`) !== slug) return null;
    return slug;
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
}

/**
 * Com Empresa padrão, troca a página por `/e/{slug}/` (`location.replace`:
 * a raiz não fica no histórico) e devolve `true` — quem chamou não monta
 * nada. Sem ela, `false`: a raiz mostra o login pela conta, nunca um erro.
 *
 * Fora da raiz, o resto do caminho, a query e o hash vão junto: `/login`
 * vira `/e/{slug}/login`, e o retorno do SSO registrado na raiz
 * (`/auth/callback?code=...&state=...`, como no exemplo de
 * `IAM_REDIRECT_URI` do `.env.template`) chega à tela de callback da Empresa
 * sem perder o `code`.
 */
export async function abrirEmpresaPadrao(): Promise<boolean> {
  const slug = await buscarEmpresaPadrao();
  if (!slug) return false;
  const { pathname, search, hash } = window.location;
  window.location.replace(`/e/${slug}${pathname.startsWith('/') ? pathname : '/'}${search}${hash}`);
  return true;
}

/**
 * Decisão completa do `main.tsx` para o caminho atual: a app de
 * `escolherApp` ou, na `sem-empresa` de um servidor com Empresa padrão,
 * `'redirecionado'` (a página já foi trocada; não montar nada). Só a
 * `sem-empresa` consulta `GET /api/entrada`.
 */
export async function decidirEntrada(pathname: string): Promise<AppDeEntrada | 'redirecionado'> {
  const app = escolherApp(pathname);
  if (app === 'sem-empresa' && (await abrirEmpresaPadrao())) {
    return 'redirecionado';
  }
  return app;
}

// --- Login pela conta na raiz do domínio (Story 15.2, AD-36) ---
//
// A app `sem-empresa` não monta `AuthProvider`: só conversa com as rotas da
// raiz (`/api/auth/entrar*`, sempre SEM prefixo de Empresa — nunca `apiUrl`)
// e redireciona. A sessão é da Empresa: o cookie de refresh vem com
// `Path=/e/{slug}/api/auth` e o AuthProvider de `/e/{slug}/` a restaura.

/** Envelope de erro fixo (AD-14). */
interface ErroEnvelope {
  error?: { code?: string; message?: string };
}

/** Um botão da pergunta "Ambiente real ou Treinamento?". */
export interface OpcaoEscolha {
  slug: string;
  nomeFantasia: string;
  treinamento: boolean;
}

export type ResultadoEntrada =
  | { tipo: 'sessao'; slug: string }
  | { tipo: 'mfa'; slug: string; mfaToken: string }
  | { tipo: 'escolha'; escolha: OpcaoEscolha[]; escolhaToken: string }
  /** `codigo` ausente = erro de rede ou resposta sem envelope. */
  | { tipo: 'erro'; codigo?: string };

interface RespostaEntrar {
  slug?: string;
  mfaRequerido?: boolean;
  mfaToken?: string;
  escolha?: OpcaoEscolha[];
  escolhaToken?: string;
}

async function postarEntrada(caminho: string, corpo: unknown): Promise<ResultadoEntrada> {
  let res: Response;
  try {
    res = await fetch(caminho, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify(corpo),
    });
  } catch {
    return { tipo: 'erro' };
  }

  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as ErroEnvelope;
    return { tipo: 'erro', codigo: body.error?.code };
  }

  const body = (await res.json().catch(() => ({}))) as RespostaEntrar;
  if (body.escolha && body.escolhaToken) {
    return { tipo: 'escolha', escolha: body.escolha, escolhaToken: body.escolhaToken };
  }
  if (body.slug && body.mfaRequerido && body.mfaToken) {
    return { tipo: 'mfa', slug: body.slug, mfaToken: body.mfaToken };
  }
  if (body.slug) {
    return { tipo: 'sessao', slug: body.slug };
  }
  return { tipo: 'erro' };
}

/** POST /api/auth/entrar — e-mail e senha, sem Empresa na URL. */
export function entrarPelaConta(email: string, senha: string): Promise<ResultadoEntrada> {
  return postarEntrada('/api/auth/entrar', { email, senha });
}

/** POST /api/auth/entrar/escolha — conclui a pergunta real/Treinamento. */
export function concluirEscolha(escolhaToken: string, slug: string): Promise<ResultadoEntrada> {
  return postarEntrada('/api/auth/entrar/escolha', { escolhaToken, slug });
}

/**
 * POST /api/auth/esqueci-senha na raiz (Story 15.3) — um e-mail de
 * redefinição por conta do e-mail, cada um com o link da sua Empresa. O
 * backend responde sempre `202` com a mesma mensagem genérica: qualquer `2xx`
 * é sucesso (`true`); rede ou não-`2xx` é `false`. Nunca `apiUrl`: a rota
 * vive fora do prefixo de Empresa.
 */
export async function pedirRedefinicaoPelaConta(email: string): Promise<boolean> {
  try {
    const res = await fetch('/api/auth/esqueci-senha', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email }),
    });
    return res.ok;
  } catch {
    return false;
  }
}

/**
 * Repasse do MFA da raiz para `/e/{slug}/login`: as duas são apps
 * diferentes, então o `mfaToken` atravessa o redirect em `sessionStorage`
 * (mesma origem, some ao fechar a aba). Sozinho ele não dá acesso — exige o
 * código TOTP e vence em 5 min.
 */
export const CHAVE_MFA_PENDENTE = 'entrada_mfa_pendente';

export function gravarMfaPendente(slug: string, mfaToken: string): void {
  try {
    window.sessionStorage.setItem(CHAVE_MFA_PENDENTE, JSON.stringify({ slug, mfaToken }));
  } catch {
    // Sem storage a tela da Empresa abre na etapa de senha — nada quebra.
  }
}

/** Devolve o `mfaToken` repassado para `slug`, ou `''` (sem repasse, ou de outra Empresa). */
export function lerMfaPendente(slug: string): string {
  if (!slug) return '';
  try {
    const bruto = window.sessionStorage.getItem(CHAVE_MFA_PENDENTE);
    if (!bruto) return '';
    const valor = JSON.parse(bruto) as { slug?: unknown; mfaToken?: unknown };
    if (valor.slug === slug && typeof valor.mfaToken === 'string') {
      return valor.mfaToken;
    }
  } catch {
    // valor corrompido ou storage indisponível: sem repasse.
  }
  return '';
}

export function removerMfaPendente(): void {
  try {
    window.sessionStorage.removeItem(CHAVE_MFA_PENDENTE);
  } catch {
    // ignora
  }
}

/**
 * Texto exibido para o código de erro do login (AD-14) — compartilhado por
 * `LoginPage` (dentro da Empresa) e pelo login pela conta na raiz. Só o
 * código decide o texto; a mensagem do backend nunca é exibida direto.
 */
export function mensagemDeErroLogin(codigo: string | undefined): string {
  // INVALID_CREDENTIALS é deliberadamente a MESMA mensagem para todo cenário
  // de credencial inválida (senha errada, e-mail inexistente, e-mail não
  // verificado, conta desativada, conta só-SSO) — o backend nunca revela qual
  // condição falhou nem se o e-mail existe (regra explícita do contexto do
  // épico), e esta tela não pode reintroduzir essa distinção no texto.
  if (codigo === 'INVALID_CREDENTIALS') {
    return 'E-mail ou senha inválidos.';
  }
  if (codigo === 'VALIDATION_ERROR') {
    return 'Preencha e-mail e senha para continuar.';
  }
  // ACCOUNT_LOCKED (Story 1.10): conta bloqueada após 5 tentativas falhas. A
  // mensagem NUNCA revela o tempo restante e NÃO promete que redefinir a senha
  // destrava a conta — só a expiração do prazo faz isso (RedefinirSenha não
  // toca nas colunas de bloqueio).
  if (codigo === 'ACCOUNT_LOCKED') {
    return 'Muitas tentativas de login sem sucesso. Por segurança, novas tentativas ficam bloqueadas temporariamente. Tente novamente mais tarde.';
  }
  // MFA_CODIGO_INVALIDO/MFA_TOKEN_INVALIDO (Story 1.11): segunda etapa do
  // login, POST /api/auth/mfa/verificar.
  if (codigo === 'MFA_CODIGO_INVALIDO') {
    return 'Código de autenticação inválido.';
  }
  if (codigo === 'MFA_TOKEN_INVALIDO') {
    return 'Código de login expirado. Faça login novamente.';
  }
  // NOT_FOUND (Story 9.2): o slug da URL não resolve — Empresa inexistente
  // ou desativada pelo Dono da Plataforma (RequireEmpresa responde 404 antes
  // de olhar qualquer credencial). Uma mensagem honesta, não "tente em
  // instantes": repetir não vai adiantar.
  if (codigo === 'NOT_FOUND') {
    return 'Este endereço de acesso não está disponível. Confira o endereço com o administrador da sua empresa.';
  }
  // ESCOLHA_INVALIDA (Story 15.2): o `escolhaToken` da pergunta
  // real/Treinamento venceu ou já foi usado.
  if (codigo === 'ESCOLHA_INVALIDA') {
    return 'A escolha expirou. Faça login novamente.';
  }
  return 'Não foi possível entrar. Tente novamente em instantes.';
}
