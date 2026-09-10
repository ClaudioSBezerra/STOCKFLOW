/**
 * Cliente da área do Dono da Plataforma — Story 9.2 (spec-9-2, AD-21).
 *
 * Isolado da sessão de `usuarios` por construção:
 * - o access token do Dono vive numa variável de módulo PRÓPRIA (nunca em
 *   `lib/session`, nunca em `localStorage`/`sessionStorage`);
 * - o refresh token vive no cookie HttpOnly `refresh_token_plataforma`
 *   (Path `/api/plataforma/auth`), fora do alcance deste código;
 * - toda URL é `/api/plataforma/...` escrita por extenso, SEM `apiUrl`: a
 *   área do Dono não pertence a Empresa nenhuma e nunca leva o prefixo
 *   `/e/{slug}`.
 */

let tokenPlataforma: string | null = null;

export function getTokenPlataforma(): string | null {
  return tokenPlataforma;
}

export function setTokenPlataforma(token: string): void {
  tokenPlataforma = token;
}

export function limparTokenPlataforma(): void {
  tokenPlataforma = null;
}

/** Erro de uma chamada à Plataforma: status HTTP + envelope AD-14. */
export class ErroPlataforma extends Error {
  readonly status: number;
  readonly codigo: string;
  readonly mensagem: string;

  constructor(status: number, codigo: string, mensagem: string) {
    super(mensagem || codigo);
    this.name = 'ErroPlataforma';
    this.status = status;
    this.codigo = codigo;
    this.mensagem = mensagem;
  }
}

export interface Dono {
  id: string;
  nome: string;
  email: string;
}

export interface EnderecoEmpresa {
  logradouro: string;
  numero: string;
  complemento: string;
  bairro: string;
  cidade: string;
  cep: string;
  uf: string;
}

/** Uma linha da área "Empresas": só metadado, com o Treinamento aninhado. */
export interface EmpresaResumo {
  id: string;
  nomeFantasia: string;
  razaoSocial: string;
  cnpj: string;
  endereco: EnderecoEmpresa;
  slug: string;
  status: string;
  criadoEm: string;
  adm: { nome: string; email: string } | null;
  treinamento: { id: string; slug: string; status: string } | null;
}

/** Payload de criação. `slug` vazio -> o servidor deriva do nome fantasia. */
export interface NovaEmpresa {
  nomeFantasia: string;
  razaoSocial: string;
  cnpj: string;
  slug: string;
  endereco: EnderecoEmpresa;
  admNome: string;
  admEmail: string;
}

export interface EmpresaCriada {
  id: string;
  nomeFantasia: string;
  slug: string;
  status: string;
  empresaOrigemId: string | null;
}

async function erroDaResposta(res: Response): Promise<ErroPlataforma> {
  const body = (await res.json().catch(() => ({}))) as {
    error?: { code?: string; message?: string };
  };
  return new ErroPlataforma(res.status, body.error?.code ?? 'ERRO_HTTP', body.error?.message ?? '');
}

async function lerJSON<T>(res: Response): Promise<T> {
  if (!res.ok) {
    throw await erroDaResposta(res);
  }
  return (await res.json()) as T;
}

/** Login em uma etapa: e-mail + senha + código TOTP (a MFA é obrigatória). */
export async function loginPlataforma(email: string, senha: string, codigo: string): Promise<Dono> {
  const res = await fetch('/api/plataforma/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, senha, codigo }),
  });
  const body = await lerJSON<{ token: string; dono: Dono }>(res);
  setTokenPlataforma(body.token);
  return body.dono;
}

async function executarRenovacao(): Promise<boolean> {
  try {
    const res = await fetch('/api/plataforma/auth/refresh', { method: 'POST' });
    if (!res.ok) {
      limparTokenPlataforma();
      return false;
    }
    const { token } = (await res.json()) as { token?: string };
    if (!token) {
      limparTokenPlataforma();
      return false;
    }
    setTokenPlataforma(token);
    return true;
  } catch {
    limparTokenPlataforma();
    return false;
  }
}

let renovacaoEmCurso: Promise<boolean> | null = null;

/**
 * Troca o cookie de refresh por um access token novo. Pedidos simultâneos
 * compartilham UMA chamada: o refresh rotaciona e revoga o cookie, então um
 * segundo POST em paralelo (o StrictMode monta duas vezes) derrubaria a
 * sessão recém-renovada.
 */
export function renovarSessaoPlataforma(): Promise<boolean> {
  renovacaoEmCurso ??= executarRenovacao().finally(() => {
    renovacaoEmCurso = null;
  });
  return renovacaoEmCurso;
}

/** Encerra a sessão: limpa o token na hora e revoga o refresh (best-effort). */
export async function logoutPlataforma(): Promise<void> {
  limparTokenPlataforma();
  try {
    await fetch('/api/plataforma/auth/logout', { method: 'POST' });
  } catch {
    // O cliente já considera a sessão encerrada; o cookie expira sozinho.
  }
}

/**
 * Chamada autenticada. Um 401 (access token de 30min vencido) tenta UMA
 * renovação pelo cookie e repete a chamada; sem sessão renovável, o 401
 * volta para quem chamou.
 */
async function requisitar(caminho: string, init: RequestInit = {}): Promise<Response> {
  const enviar = () => {
    const headers: Record<string, string> = { ...(init.headers as Record<string, string> | undefined) };
    const token = getTokenPlataforma();
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }
    return fetch(caminho, { ...init, headers });
  };
  const res = await enviar();
  if (res.status === 401 && (await renovarSessaoPlataforma())) {
    return enviar();
  }
  return res;
}

export async function buscarDono(): Promise<Dono> {
  return lerJSON<Dono>(await requisitar('/api/plataforma/auth/me'));
}

export async function listarEmpresas(): Promise<EmpresaResumo[]> {
  const body = await lerJSON<{ empresas: EmpresaResumo[] | null }>(
    await requisitar('/api/plataforma/empresas'),
  );
  return body.empresas ?? [];
}

export async function criarEmpresa(
  dados: NovaEmpresa,
): Promise<{ empresa: EmpresaCriada; treinamento: EmpresaCriada }> {
  return lerJSON(
    await requisitar('/api/plataforma/empresas', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(dados),
    }),
  );
}

async function acaoDeStatus(id: string, acao: 'desativacao' | 'reativacao'): Promise<void> {
  const res = await requisitar(`/api/plataforma/empresas/${encodeURIComponent(id)}/${acao}`, {
    method: 'POST',
  });
  if (!res.ok) {
    throw await erroDaResposta(res);
  }
}

/** Desativa a Empresa real E o Treinamento dela. Nada é apagado. */
export function desativarEmpresa(id: string): Promise<void> {
  return acaoDeStatus(id, 'desativacao');
}

/** Reativa a Empresa real e o Treinamento dela. */
export function reativarEmpresa(id: string): Promise<void> {
  return acaoDeStatus(id, 'reativacao');
}

/**
 * Teto do slug da Empresa real: 63 (coluna) menos o sufixo `-treinamento`
 * (12) — espelho de `services.slugRealMaxRunes`.
 */
export const SLUG_MAX_EMPRESA = 51;

/**
 * Espelho de `services.NormalizarSlug` para PRÉ-PREENCHER o endereço de
 * acesso a partir do nome fantasia: minúsculas, sem acento, qualquer
 * sequência fora de [a-z0-9] vira um hífen, sem hífen nas pontas, cortado no
 * teto. O servidor continua sendo a autoridade (revalida tudo).
 */
export function normalizarSlug(texto: string): string {
  return texto
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, SLUG_MAX_EMPRESA)
    .replace(/-+$/, '');
}

/** "12345678000195" -> "12.345.678/0001-95"; outro formato volta como veio. */
export function formatarCNPJ(cnpj: string): string {
  const d = cnpj.replace(/\D/g, '');
  if (d.length !== 14) {
    return cnpj;
  }
  return `${d.slice(0, 2)}.${d.slice(2, 5)}.${d.slice(5, 8)}/${d.slice(8, 12)}-${d.slice(12)}`;
}
