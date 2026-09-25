import { describe, expect, it } from 'vitest';
import { nomeDaMarca } from './marca';

describe('nomeDaMarca', () => {
  it('usa "Suprimentos" quando o host começa com "suprimentos."', () => {
    expect(nomeDaMarca('suprimentos.fcxlabs.com')).toBe('Suprimentos');
    expect(nomeDaMarca('Suprimentos.fcxlabs.com')).toBe('Suprimentos');
  });

  it('usa "stockflow" nos demais hosts', () => {
    expect(nomeDaMarca('stockflow.fbtechia.com')).toBe('stockflow');
    expect(nomeDaMarca('localhost')).toBe('stockflow');
    expect(nomeDaMarca('')).toBe('stockflow');
    expect(nomeDaMarca('app.suprimentos.com')).toBe('stockflow');
    expect(nomeDaMarca('suprimentos')).toBe('stockflow');
  });
});
