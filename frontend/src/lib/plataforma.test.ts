import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  ErroPlataforma,
  criarEmpresa,
  desativarEmpresa,
  formatarCNPJ,
  getTokenPlataforma,
  limparTokenPlataforma,
  listarEmpresas,
  loginPlataforma,
  logoutPlataforma,
  normalizarSlug,
  reativarEmpresa,
  renovarSessaoPlataforma,
  setTokenPlataforma,
} from './plataforma';
import { getAccessToken } from './session';

function resposta(status: number, body?: unknown) {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body ?? {},
  });
}

const fetchMock = vi.fn();

beforeEach(() => {
  fetchMock.mockReset();
  vi.stubGlobal('fetch', fetchMock);
  limparTokenPlataforma();
});

afterEach(() => {
  vi.unstubAllGlobals();
  limparTokenPlataforma();
  window.history.pushState({}, '', '/');
});

describe('loginPlataforma', () => {
  it('envia e-mail, senha e código para /api/plataforma/auth/login e guarda o token só na guarda própria', async () => {
    fetchMock.mockReturnValueOnce(
      resposta(200, { token: 'tk-dono', dono: { id: '1', nome: 'Dona', email: 'dona@x.com' } }),
    );

    const dono = await loginPlataforma('dona@x.com', 'senha-1', '123456');

    expect(dono.nome).toBe('Dona');
    expect(getTokenPlataforma()).toBe('tk-dono');
    expect(getAccessToken()).toBeNull();
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/plataforma/auth/login');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({
      email: 'dona@x.com',
      senha: 'senha-1',
      codigo: '123456',
    });
  });

  it('rejeita com ErroPlataforma carregando o código do envelope', async () => {
    fetchMock.mockReturnValueOnce(
      resposta(401, { error: { code: 'INVALID_CREDENTIALS', message: 'qualquer' } }),
    );

    const erro = await loginPlataforma('a', 'b', 'c').catch((e: unknown) => e);

    expect(erro).toBeInstanceOf(ErroPlataforma);
    expect(erro).toMatchObject({ status: 401, codigo: 'INVALID_CREDENTIALS' });
    expect(getTokenPlataforma()).toBeNull();
  });
});

describe('chamadas autenticadas', () => {
  it('nunca levam o prefixo /e/{slug}, mesmo sob o caminho de uma Empresa', async () => {
    window.history.pushState({}, '', '/e/acme/pedidos');
    setTokenPlataforma('tk');
    fetchMock.mockReturnValueOnce(resposta(200, { empresas: [] }));

    await listarEmpresas();

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/plataforma/empresas');
    expect(init.headers.Authorization).toBe('Bearer tk');
  });

  it('em 401 renovam a sessão uma vez e repetem a chamada com o token novo', async () => {
    setTokenPlataforma('vencido');
    fetchMock
      .mockReturnValueOnce(resposta(401, { error: { code: 'TOKEN_EXPIRED' } }))
      .mockReturnValueOnce(resposta(200, { token: 'novo' }))
      .mockReturnValueOnce(resposta(200, { empresas: [{ id: 'e1' }] }));

    const empresas = await listarEmpresas();

    expect(empresas).toEqual([{ id: 'e1' }]);
    expect(fetchMock.mock.calls[1][0]).toBe('/api/plataforma/auth/refresh');
    expect(fetchMock.mock.calls[2][1].headers.Authorization).toBe('Bearer novo');
  });

  it('sem sessão renovável, o 401 chega a quem chamou', async () => {
    setTokenPlataforma('vencido');
    fetchMock
      .mockReturnValueOnce(resposta(401, { error: { code: 'TOKEN_EXPIRED' } }))
      .mockReturnValueOnce(resposta(401, { error: { code: 'TOKEN_EXPIRED' } }));

    await expect(listarEmpresas()).rejects.toMatchObject({ status: 401, codigo: 'TOKEN_EXPIRED' });
    expect(getTokenPlataforma()).toBeNull();
  });

  it('criarEmpresa envia o JSON e devolve {empresa, treinamento}', async () => {
    setTokenPlataforma('tk');
    fetchMock.mockReturnValueOnce(
      resposta(201, { empresa: { id: 'r' }, treinamento: { id: 't' } }),
    );

    const criada = await criarEmpresa({
      nomeFantasia: 'Acme',
      razaoSocial: 'Acme LTDA',
      cnpj: '11222333000181',
      slug: 'acme',
      endereco: {
        logradouro: 'Rua A',
        numero: '1',
        complemento: '',
        bairro: 'Centro',
        cidade: 'Recife',
        cep: '50000000',
        uf: 'PE',
      },
      admNome: 'Ana',
      admEmail: 'ana@acme.com',
    });

    expect(criada.treinamento.id).toBe('t');
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/plataforma/empresas');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toMatchObject({ slug: 'acme', admEmail: 'ana@acme.com' });
  });

  it('409 do servidor vira ErroPlataforma com a mensagem do campo', async () => {
    setTokenPlataforma('tk');
    fetchMock.mockReturnValueOnce(
      resposta(409, { error: { code: 'CONFLICT', message: 'Já existe uma empresa com este CNPJ.' } }),
    );

    await expect(criarEmpresa({} as never)).rejects.toMatchObject({
      status: 409,
      codigo: 'CONFLICT',
      mensagem: 'Já existe uma empresa com este CNPJ.',
    });
  });

  it('desativar/reativar chamam as rotas de ação da Empresa', async () => {
    setTokenPlataforma('tk');
    fetchMock.mockReturnValue(resposta(200, {}));

    await desativarEmpresa('id-1');
    await reativarEmpresa('id-1');

    expect(fetchMock.mock.calls[0][0]).toBe('/api/plataforma/empresas/id-1/desativacao');
    expect(fetchMock.mock.calls[1][0]).toBe('/api/plataforma/empresas/id-1/reativacao');
    expect(fetchMock.mock.calls[0][1].method).toBe('POST');
  });
});

describe('renovarSessaoPlataforma', () => {
  it('pedidos simultâneos compartilham uma única chamada de refresh', async () => {
    fetchMock.mockReturnValue(resposta(200, { token: 'novo' }));

    const [a, b] = await Promise.all([renovarSessaoPlataforma(), renovarSessaoPlataforma()]);

    expect(a).toBe(true);
    expect(b).toBe(true);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(getTokenPlataforma()).toBe('novo');
  });

  it('cookie inválido -> false e token limpo', async () => {
    setTokenPlataforma('antigo');
    fetchMock.mockReturnValueOnce(resposta(401, { error: { code: 'TOKEN_EXPIRED' } }));

    expect(await renovarSessaoPlataforma()).toBe(false);
    expect(getTokenPlataforma()).toBeNull();
  });
});

describe('logoutPlataforma', () => {
  it('limpa o token e revoga o refresh no servidor', async () => {
    setTokenPlataforma('tk');
    fetchMock.mockReturnValueOnce(resposta(204));

    await logoutPlataforma();

    expect(getTokenPlataforma()).toBeNull();
    expect(fetchMock.mock.calls[0][0]).toBe('/api/plataforma/auth/logout');
  });
});

describe('normalizarSlug / formatarCNPJ', () => {
  it.each([
    ['Ferreira Costa', 'ferreira-costa'],
    ['  Construtora Ávila & Filhos  ', 'construtora-avila-filhos'],
    ['Ação--Çuíça', 'acao-cuica'],
    ['---', ''],
  ])('normalizarSlug(%j) -> %j', (entrada, esperado) => {
    expect(normalizarSlug(entrada)).toBe(esperado);
  });

  it('corta no teto de 51 caracteres sem deixar hífen na ponta', () => {
    const slug = normalizarSlug(`${'a'.repeat(50)} b`);
    expect(slug).toBe('a'.repeat(50));
    expect(normalizarSlug('x'.repeat(80))).toHaveLength(51);
  });

  it('formata o CNPJ com máscara', () => {
    expect(formatarCNPJ('12345678000195')).toBe('12.345.678/0001-95');
    expect(formatarCNPJ('123')).toBe('123');
  });
});
