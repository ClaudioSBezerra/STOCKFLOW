import { describe, expect, it } from 'vitest';
import { senhaAtendePolitica } from './senha';

describe('senhaAtendePolitica (espelho de services.ValidarForcaSenha)', () => {
  const casos: Array<[string, string, boolean]> = [
    ['7 caracteres é curto demais', 'abc1234', false],
    ['8 caracteres com letra e dígito passa', 'abcd1234', true],
    ['sem dígito reprova', 'abcdefgh', false],
    ['sem letra reprova', '12345678', false],
    ['73 bytes reprova (limite do bcrypt)', 'a'.repeat(72) + '1', false],
    ['72 bytes no limite passa', 'a'.repeat(71) + '1', true],
    ['acento conta como 1 rune: 8 runes / 13 bytes passa', 'áéíóú123', true],
    ['string vazia reprova', '', false],
  ];

  it.each(casos)('%s', (_nome, senha, esperado) => {
    expect(senhaAtendePolitica(senha)).toBe(esperado);
  });
});
