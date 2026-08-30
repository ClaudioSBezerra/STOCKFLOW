/**
 * Guarda do access token de sessão (Story 1.4, spec-1-4, AD-6). Só em
 * memória — uma variável de módulo — NUNCA `localStorage`/`sessionStorage`:
 * o access token de 30min não sobrevive a um reload da página por design;
 * quem precisa de persistência entre abas/reloads é o refresh token, que já
 * vive em cookie HttpOnly (fora do alcance de JavaScript).
 *
 * O bootstrap automático de sessão (silent refresh via cookie ao montar o
 * app) vive em `lib/auth.tsx` (`AuthProvider`, Story 1.5): ele grava aqui o
 * access token devolvido por `POST /api/auth/refresh` antes de chamar
 * `GET /api/auth/me`. Este módulo continua sendo só a guarda em memória.
 */
let accessToken: string | null = null;

export function getAccessToken(): string | null {
  return accessToken;
}

export function setAccessToken(token: string): void {
  accessToken = token;
}

export function clearAccessToken(): void {
  accessToken = null;
}
