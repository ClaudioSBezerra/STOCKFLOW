import { describe, expect, it } from 'vitest';
import { proximoPapel, rotuloPapel } from './promocao';

describe('proximoPapel (espelho de services.proximoPapelPromocao)', () => {
  const casos: Array<[string, string | null]> = [
    ['usuario', 'almoxarife'],
    ['almoxarife', 'gestor'],
    ['gestor', null],
    ['adm', null],
    ['', null],
    ['desconhecido', null],
  ];

  it.each(casos)('proximoPapel(%j) -> %j', (papel, esperado) => {
    expect(proximoPapel(papel)).toBe(esperado);
  });
});

describe('rotuloPapel', () => {
  it('mapeia os quatro papéis da hierarquia', () => {
    expect(rotuloPapel('usuario')).toBe('Usuário');
    expect(rotuloPapel('almoxarife')).toBe('Almoxarife');
    expect(rotuloPapel('gestor')).toBe('Gestor');
    expect(rotuloPapel('adm')).toBe('Adm');
  });

  it('devolve o valor original para papel desconhecido', () => {
    expect(rotuloPapel('supervisor')).toBe('supervisor');
  });
});
