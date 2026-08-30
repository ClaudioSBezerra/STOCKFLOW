/**
 * Espelho mínimo da política de força de senha do backend
 * (`services.ValidarForcaSenha`, spec-1-6). A duplicação entre linguagens é
 * deliberada e documentada — mesmo caso do espelho `rankPapel` da Story 1.5:
 * a AUTORIDADE é sempre o backend (`POST /api/auth/redefinir-senha` revalida
 * e é quem de fato decide). Aqui a checagem só evita uma ida à rede para uma
 * senha obviamente fraca, com feedback inline imediato.
 *
 * Regra: mínimo 8 caracteres, ao menos uma letra e ao menos um dígito, no
 * máximo 72 bytes (limite rígido do bcrypt). "Caracteres" são code points
 * (`Array.from`), não unidades UTF-16 — um acento conta como 1, igual ao
 * `utf8.RuneCountInString` do backend. Letra = `\p{L}`, dígito = `\p{Nd}`,
 * espelhando `unicode.IsLetter` / `unicode.IsDigit`.
 */
export function senhaAtendePolitica(senha: string): boolean {
  const runes = Array.from(senha);
  if (runes.length < 8) {
    return false;
  }
  if (new TextEncoder().encode(senha).length > 72) {
    return false;
  }
  const temLetra = /\p{L}/u.test(senha);
  const temDigito = /\p{Nd}/u.test(senha);
  return temLetra && temDigito;
}
