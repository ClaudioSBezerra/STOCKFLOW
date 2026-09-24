import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Cliente HTTP puro da exigência de MFA da Empresa e da auditoria de
 * segurança — Story 14.3 (FR-53, AD-35). Molde de `lib/privacidade.ts`: só
 * tipos + `fetch`, nenhum estado React. As três rotas vivem atrás de
 * `RequireRole(adm)` no servidor, que é sempre a autoridade.
 *
 *   GET /api/seguranca/mfa-empresa -> `obterExigenciaMFA()`
 *   PUT /api/seguranca/mfa-empresa -> `alterarExigenciaMFA(valor)`
 *   GET /api/seguranca/auditoria   -> `listarAuditoriaSeguranca()`
 *
 * Resposta não-ok lança `Error` com `body.error.message` do servidor (ou o
 * fallback de cada função).
 */

export interface ExigenciaMFA {
  mfaObrigatorio: boolean;
  /** Contas `gestor`/`adm` ativas, com senha e sem MFA — as que o gate bloquearia. */
  contasSemMfa: number;
}

export interface ResultadoAlteracaoMFA {
  mfaObrigatorio: boolean;
  /** `false` quando o valor já era esse (idempotente, sem auditoria). */
  alterado: boolean;
}

export interface EventoAuditoriaSeguranca {
  id: string;
  acao: 'exigencia_alterada' | 'mfa_resetado' | 'mfa_desligado' | string;
  atorId: string;
  atorNome: string | null;
  alvoId: string | null;
  alvoNome: string | null;
  detalhe: Record<string, unknown>;
  criadoEm: string;
}

export const MENSAGEM_ERRO_OBTER_EXIGENCIA =
  'Não foi possível carregar a exigência de dupla autenticação. Recarregue a página.';
export const MENSAGEM_ERRO_ALTERAR_EXIGENCIA =
  'Não foi possível alterar a exigência de dupla autenticação. Tente novamente em instantes.';
export const MENSAGEM_ERRO_LISTAR_AUDITORIA =
  'Não foi possível carregar o histórico de segurança. Recarregue a página.';

async function mensagemDeErro(res: Response, fallback: string): Promise<string> {
  const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
  return body.error?.message ?? fallback;
}

export async function obterExigenciaMFA(): Promise<ExigenciaMFA> {
  const res = await fetch(apiUrl('/api/seguranca/mfa-empresa'), { headers: authHeaders() });
  if (!res.ok) {
    throw new Error(await mensagemDeErro(res, MENSAGEM_ERRO_OBTER_EXIGENCIA));
  }
  return (await res.json()) as ExigenciaMFA;
}

export async function alterarExigenciaMFA(valor: boolean): Promise<ResultadoAlteracaoMFA> {
  const res = await fetch(apiUrl('/api/seguranca/mfa-empresa'), {
    method: 'PUT',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ mfaObrigatorio: valor }),
  });
  if (!res.ok) {
    throw new Error(await mensagemDeErro(res, MENSAGEM_ERRO_ALTERAR_EXIGENCIA));
  }
  return (await res.json()) as ResultadoAlteracaoMFA;
}

export async function listarAuditoriaSeguranca(): Promise<EventoAuditoriaSeguranca[]> {
  const res = await fetch(apiUrl('/api/seguranca/auditoria'), { headers: authHeaders() });
  if (!res.ok) {
    throw new Error(await mensagemDeErro(res, MENSAGEM_ERRO_LISTAR_AUDITORIA));
  }
  const body = (await res.json()) as { eventos?: EventoAuditoriaSeguranca[] };
  return body.eventos ?? [];
}
