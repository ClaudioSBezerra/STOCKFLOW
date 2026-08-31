import { describe, expect, it, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { CadastroProdutoSection } from './CadastroProdutoSection';

const toastSuccess = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess } }));

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

// jsdom não implementa URL.createObjectURL/revokeObjectURL (Story 3.5: a
// miniatura da foto usa exatamente essa dupla) — stub simples, chamadas só
// verificadas por identidade/contagem, nunca pelo conteúdo do blob.
beforeEach(() => {
  URL.createObjectURL = vi.fn(() => 'blob:mock-thumb-url');
  URL.revokeObjectURL = vi.fn();
});

type FetchImpl = (
  url: string,
  init?: RequestInit,
) => Promise<{ ok: boolean; status?: number; json: () => Promise<unknown>; blob?: () => Promise<Blob> }>;

function stubFetch(impl: FetchImpl) {
  const fn = vi.fn(impl);
  vi.stubGlobal('fetch', fn);
  return fn;
}

function jsonOk(body: unknown) {
  return Promise.resolve({ ok: true, status: 200, json: async () => body });
}

function fotoBlobOk() {
  return Promise.resolve({
    ok: true,
    status: 200,
    json: async () => ({}),
    blob: async () => new Blob(['bytes-jpeg-fake'], { type: 'image/jpeg' }),
  });
}

const CATEGORIAS = [
  { id: 'cat-1', codigo: '04.001', nome: 'Materiais Civis' },
  { id: 'cat-2', codigo: '04.002', nome: 'Materiais Elétricos' },
];
const ESTOQUES = [{ id: 'est-1', nome: 'Canteiro A' }];
const TEMPLATES = [
  { id: 'tpl-1', subtipo: 'Tubo — PEAD/PPR', template: 'TUBO PEAD [PN] DN[XX]' },
  {
    id: 'tpl-2',
    subtipo: 'Cabos — Elétrico',
    template: 'CABO [TIPO] [TENSÃO] Ø[SEÇÃO]MM² [COR] [COMPLEMENTO]',
  },
];

function stubListasPadrao(extra?: Partial<Record<string, unknown>>) {
  return stubFetch((url, init) => {
    if (url === '/api/categorias') return jsonOk({ categorias: CATEGORIAS });
    if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
    if (url === '/api/nomenclatura-templates') return jsonOk({ templates: TEMPLATES });
    if (url === '/api/produtos' && init?.method === 'POST') {
      const handler = extra?.postProdutos as FetchImpl | undefined;
      if (handler) return handler(url, init);
    }
    if (/^\/api\/produtos\/[^/]+\/fotos$/.test(url) && init?.method === 'POST') {
      const handler = extra?.postFoto as FetchImpl | undefined;
      if (handler) return handler(url, init);
    }
    if (/^\/api\/produtos\/[^/]+\/fotos\/[^/]+$/.test(url) && (!init?.method || init.method === 'GET')) {
      const handler = extra?.getFoto as FetchImpl | undefined;
      if (handler) return handler(url, init);
    }
    throw new Error(`URL inesperada: ${url} (${init?.method ?? 'GET'})`);
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

// corpoDoPost extrai e decodifica o corpo JSON da chamada POST /api/produtos
// dentre todas as chamadas registradas em `fetchMock` — falha o teste (em vez
// de um TypeError obscuro) se a chamada não existir.
function corpoDoPost(fetchMock: ReturnType<typeof vi.fn>): Record<string, unknown> {
  const chamada = fetchMock.mock.calls.find(
    (args: unknown[]) => args[0] === '/api/produtos' && (args[1] as RequestInit)?.method === 'POST',
  ) as [string, RequestInit] | undefined;
  if (!chamada) {
    throw new Error('nenhuma chamada POST /api/produtos encontrada');
  }
  return JSON.parse(chamada[1].body as string) as Record<string, unknown>;
}

async function preencherCamposObrigatorios(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('Nome'), 'Tubo PVC 100mm');
  await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
  await user.click(await screen.findByRole('option', { name: '04.001 — Materiais Civis' }));
  await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
  await user.click(await screen.findByRole('option', { name: 'Canteiro A' }));
  await user.type(screen.getByLabelText('Quantidade inicial'), '10');
}

describe('CadastroProdutoSection', () => {
  it('carrega categorias e estoques no mount e popula os selects', async () => {
    stubListasPadrao();
    const user = userEvent.setup();
    render(<CadastroProdutoSection />);

    await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
    expect(await screen.findByRole('option', { name: '04.001 — Materiais Civis' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: '04.002 — Materiais Elétricos' })).toBeInTheDocument();
  });

  it('carrega templates de nomenclatura no mount e popula o select, com a opção "nome livre"', async () => {
    stubListasPadrao();
    const user = userEvent.setup();
    render(<CadastroProdutoSection />);

    await user.click(screen.getByRole('combobox', { name: 'Template de nomenclatura (opcional)' }));
    expect(await screen.findByRole('option', { name: 'Nome livre (sem template)' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Tubo — PEAD/PPR' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Cabos — Elétrico' })).toBeInTheDocument();
  });

  it('falha só em /api/nomenclatura-templates: não bloqueia o formulário (categoria/estoque carregaram)', async () => {
    stubFetch((url) => {
      if (url === '/api/categorias') return jsonOk({ categorias: CATEGORIAS });
      if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
      if (url === '/api/nomenclatura-templates') {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
      }
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<CadastroProdutoSection />);

    // Categoria/estoque (obrigatórios) carregaram normalmente — sem alerta de erro.
    await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
    await user.click(await screen.findByRole('option', { name: '04.001 — Materiais Civis' }));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    // Template (opcional) fica só com "Nome livre" — a lista não veio, mas o
    // cadastro continua utilizável sem template.
    await user.click(screen.getByRole('combobox', { name: 'Template de nomenclatura (opcional)' }));
    expect(await screen.findByRole('option', { name: 'Nome livre (sem template)' })).toBeInTheDocument();
  });

  it('rede falha só em /api/nomenclatura-templates (fetch rejeita): não bloqueia o formulário (categoria/estoque carregaram)', async () => {
    stubFetch((url) => {
      if (url === '/api/categorias') return jsonOk({ categorias: CATEGORIAS });
      if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
      if (url === '/api/nomenclatura-templates') return Promise.reject(new Error('falha de rede'));
      throw new Error(`URL inesperada: ${url}`);
    });

    const user = userEvent.setup();
    render(<CadastroProdutoSection />);

    // Categoria/estoque (obrigatórios) carregaram normalmente — sem alerta de erro.
    await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
    await user.click(await screen.findByRole('option', { name: '04.001 — Materiais Civis' }));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();

    // Template (opcional) fica só com "Nome livre" — a fetch rejeitou, mas o
    // cadastro continua utilizável sem template.
    await user.click(screen.getByRole('combobox', { name: 'Template de nomenclatura (opcional)' }));
    expect(await screen.findByRole('option', { name: 'Nome livre (sem template)' })).toBeInTheDocument();
  });

  it('erro ao carregar categorias/estoques: mostra mensagem em role="alert"', async () => {
    stubFetch((url) => {
      if (url === '/api/categorias') {
        return Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
      }
      if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
      if (url === '/api/nomenclatura-templates') return jsonOk({ templates: TEMPLATES });
      throw new Error(`URL inesperada: ${url}`);
    });

    render(<CadastroProdutoSection />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar categorias/estoques. Recarregue a página.',
    );
  });

  // Timeout explícito (padrão do vitest é 5000ms): este caso encadeia mais
  // interações de usuário (dois `Select` via portal + múltiplos campos) que
  // os demais testes desta suíte, e pode passar perto do limite padrão sob
  // execução paralela de toda a suíte de arquivos.
  it('cadastro válido completo: POST com dimensões pareadas, toast.success e formulário limpo', async () => {
    const fetchMock = stubListasPadrao({
      postProdutos: () =>
        Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ produto: { id: 'p-1', nome: 'Tubo PVC 100mm' } }),
        }),
    });

    const user = userEvent.setup();
    render(<CadastroProdutoSection />);
    await preencherCamposObrigatorios(user);

    await user.type(screen.getByLabelText('Comprimento'), '6');
    await user.click(screen.getByRole('combobox', { name: 'Unidade de Comprimento' }));
    await user.click(await screen.findByRole('option', { name: 'm' }));

    await user.click(screen.getByRole('button', { name: 'Cadastrar produto' }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Produto cadastrado.'));

    const corpo = corpoDoPost(fetchMock);
    expect(corpo.nome).toBe('Tubo PVC 100mm');
    expect(corpo.categoria_id).toBe('cat-1');
    expect(corpo.estoque_id).toBe('est-1');
    expect(corpo.quantidade_inicial).toBe(10);
    expect(corpo.comprimento).toEqual({ valor: 6, unidade: 'm' });
    expect(corpo.largura).toBeUndefined();
    expect(corpo.template_id).toBeUndefined();

    expect(screen.getByLabelText('Nome')).toHaveValue('');
    expect(screen.getByLabelText('Quantidade inicial')).toHaveValue(null);
  }, 15000);

  // Mesmo motivo do timeout explícito das duas acima.
  it('cadastro com template selecionado: mostra o formato e envia template_id no payload', async () => {
    const fetchMock = stubListasPadrao({
      postProdutos: () =>
        Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ produto: { id: 'p-3', nome: 'TUBO PEAD PN80 DN50' } }),
        }),
    });

    const user = userEvent.setup();
    render(<CadastroProdutoSection />);
    await user.type(screen.getByLabelText('Nome'), 'TUBO PEAD PN80 DN50');
    await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
    await user.click(await screen.findByRole('option', { name: '04.001 — Materiais Civis' }));
    await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
    await user.click(await screen.findByRole('option', { name: 'Canteiro A' }));
    await user.type(screen.getByLabelText('Quantidade inicial'), '10');

    await user.click(screen.getByRole('combobox', { name: 'Template de nomenclatura (opcional)' }));
    await user.click(await screen.findByRole('option', { name: 'Tubo — PEAD/PPR' }));

    expect(screen.getByText('Formato: TUBO PEAD [PN] DN[XX]')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Cadastrar produto' }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Produto cadastrado.'));
    const corpo = corpoDoPost(fetchMock);
    expect(corpo.template_id).toBe('tpl-1');

    // O reset pós-sucesso também limpa o template selecionado: o texto de
    // formato some e o combobox volta a mostrar "Nome livre (sem template)".
    expect(screen.queryByText('Formato: TUBO PEAD [PN] DN[XX]')).not.toBeInTheDocument();
    expect(
      screen.getByRole('combobox', { name: 'Template de nomenclatura (opcional)' }),
    ).toHaveTextContent('Nome livre (sem template)');
  }, 15000);

  it('voltar para "Nome livre (sem template)" limpa o template selecionado: some o formato e o payload não leva template_id', async () => {
    const fetchMock = stubListasPadrao({
      postProdutos: () =>
        Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ produto: { id: 'p-4', nome: 'Produto qualquer' } }),
        }),
    });

    const user = userEvent.setup();
    render(<CadastroProdutoSection />);
    await preencherCamposObrigatorios(user);

    await user.click(screen.getByRole('combobox', { name: 'Template de nomenclatura (opcional)' }));
    await user.click(await screen.findByRole('option', { name: 'Tubo — PEAD/PPR' }));
    expect(screen.getByText('Formato: TUBO PEAD [PN] DN[XX]')).toBeInTheDocument();

    await user.click(screen.getByRole('combobox', { name: 'Template de nomenclatura (opcional)' }));
    await user.click(await screen.findByRole('option', { name: 'Nome livre (sem template)' }));
    expect(screen.queryByText('Formato: TUBO PEAD [PN] DN[XX]')).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Cadastrar produto' }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Produto cadastrado.'));
    const corpo = corpoDoPost(fetchMock);
    expect(corpo.template_id).toBeUndefined();
  }, 15000);

  // Mesmo motivo do timeout explícito acima.
  it('cadastro válido sem dimensões: nenhuma chave de dimensão no payload', async () => {
    const fetchMock = stubListasPadrao({
      postProdutos: () =>
        Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ produto: { id: 'p-2', nome: 'Produto Simples' } }),
        }),
    });

    const user = userEvent.setup();
    render(<CadastroProdutoSection />);
    await user.type(screen.getByLabelText('Nome'), 'Produto Simples');
    await user.click(screen.getByRole('combobox', { name: 'Categoria' }));
    await user.click(await screen.findByRole('option', { name: '04.001 — Materiais Civis' }));
    await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
    await user.click(await screen.findByRole('option', { name: 'Canteiro A' }));
    await user.type(screen.getByLabelText('Quantidade inicial'), '0');

    await user.click(screen.getByRole('button', { name: 'Cadastrar produto' }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Produto cadastrado.'));
    const corpo = corpoDoPost(fetchMock);
    expect(corpo.comprimento).toBeUndefined();
    expect(corpo.largura).toBeUndefined();
    expect(corpo.diametro).toBeUndefined();
    expect(corpo.altura).toBeUndefined();
    expect(corpo.espessura).toBeUndefined();
  }, 15000);

  it('400 de campo específico: mostra a mensagem do servidor em role="alert"', async () => {
    stubListasPadrao({
      postProdutos: () =>
        Promise.resolve({
          ok: false,
          status: 400,
          json: async () => ({
            error: { code: 'VALIDATION_ERROR', message: 'largura: valor e unidade devem ser informados juntos' },
          }),
        }),
    });

    const user = userEvent.setup();
    render(<CadastroProdutoSection />);
    await preencherCamposObrigatorios(user);
    await user.type(screen.getByLabelText('Largura'), '10');

    await user.click(screen.getByRole('button', { name: 'Cadastrar produto' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'largura: valor e unidade devem ser informados juntos',
    );
    expect(toastSuccess).not.toHaveBeenCalled();
    // O formulário não é limpo num erro.
    expect(screen.getByLabelText('Nome')).toHaveValue('Tubo PVC 100mm');
  });

  it('erro genérico (rede/500): role="alert" com a mensagem padrão', async () => {
    stubListasPadrao({
      postProdutos: () => Promise.reject(new Error('falha de rede')),
    });

    const user = userEvent.setup();
    render(<CadastroProdutoSection />);
    await preencherCamposObrigatorios(user);

    await user.click(screen.getByRole('button', { name: 'Cadastrar produto' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível cadastrar o produto agora. Tente novamente em instantes.',
    );
  });

  it('botão desabilitado com nome/categoria/estoque/quantidade em branco', async () => {
    stubListasPadrao();
    const user = userEvent.setup();
    render(<CadastroProdutoSection />);

    const botao = screen.getByRole('button', { name: 'Cadastrar produto' });
    expect(botao).toBeDisabled();

    await preencherCamposObrigatorios(user);
    expect(botao).toBeEnabled();
  });
});

// Bloco "Adicionar foto" (Story 3.5, spec-3-5): só existe depois de um
// cadastro bem-sucedido nesta mesma tela (produtoCriado = {id, nome} do
// `201` de POST /api/produtos).
describe('CadastroProdutoSection — Adicionar foto', () => {
  async function cadastrarComSucesso(
    user: ReturnType<typeof userEvent.setup>,
    extra?: Partial<Record<string, unknown>>,
  ) {
    const fetchMock = stubListasPadrao({
      postProdutos: () =>
        Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ produto: { id: 'p-1', nome: 'Tubo PVC 100mm' } }),
        }),
      ...extra,
    });
    render(<CadastroProdutoSection />);
    await preencherCamposObrigatorios(user);
    await user.click(screen.getByRole('button', { name: 'Cadastrar produto' }));
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Produto cadastrado.'));
    return fetchMock;
  }

  it('cadastro bem-sucedido exibe o bloco "Adicionar foto" com o botão desabilitado sem arquivo', async () => {
    const user = userEvent.setup();
    await cadastrarComSucesso(user);

    expect(screen.getByText('Adicionar foto — Tubo PVC 100mm')).toBeInTheDocument();
    const botaoFoto = screen.getByRole('button', { name: 'Enviar foto' });
    expect(botaoFoto).toBeDisabled();

    await user.upload(
      screen.getByLabelText('Foto do produto'),
      new File(['conteudo'], 'foto.jpg', { type: 'image/jpeg' }),
    );
    expect(botaoFoto).toBeEnabled();
  });

  it('upload de foto com sucesso: envia multipart autenticado, busca a foto salva e mostra a miniatura', async () => {
    const user = userEvent.setup();
    const fetchMock = await cadastrarComSucesso(user, {
      postFoto: () =>
        Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ foto: { nome: 'p-1-111.jpg', url: '/api/produtos/p-1/fotos/p-1-111.jpg' } }),
        }),
      getFoto: () => fotoBlobOk(),
    });

    await user.upload(
      screen.getByLabelText('Foto do produto'),
      new File(['conteudo'], 'foto.jpg', { type: 'image/jpeg' }),
    );
    await user.click(screen.getByRole('button', { name: 'Enviar foto' }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Foto enviada.'));

    const miniatura = await screen.findByRole('img', { name: 'Foto de Tubo PVC 100mm' });
    expect(miniatura).toHaveAttribute('src', 'blob:mock-thumb-url');

    const chamadaPost = fetchMock.mock.calls.find(
      (args: unknown[]) => args[0] === '/api/produtos/p-1/fotos' && (args[1] as RequestInit)?.method === 'POST',
    ) as [string, RequestInit] | undefined;
    if (!chamadaPost) {
      throw new Error('nenhuma chamada POST /api/produtos/p-1/fotos encontrada');
    }
    expect((chamadaPost[1].headers as Record<string, string>).Authorization).toBe('Bearer token-de-teste');
    expect(chamadaPost[1].body).toBeInstanceOf(FormData);
    expect((chamadaPost[1].body as FormData).get('foto')).toBeInstanceOf(File);

    const chamadaGet = fetchMock.mock.calls.find(
      (args: unknown[]) => args[0] === '/api/produtos/p-1/fotos/p-1-111.jpg',
    ) as [string, RequestInit] | undefined;
    if (!chamadaGet) {
      throw new Error('nenhuma chamada GET da foto salva encontrada');
    }
    expect((chamadaGet[1].headers as Record<string, string>).Authorization).toBe('Bearer token-de-teste');
  });

  it('erro do servidor no upload de foto: mostra a mensagem em role="alert", sem toast', async () => {
    const user = userEvent.setup();
    await cadastrarComSucesso(user, {
      postFoto: () =>
        Promise.resolve({
          ok: false,
          status: 400,
          json: async () => ({
            error: { code: 'VALIDATION_ERROR', message: 'arquivo excede o tamanho máximo permitido' },
          }),
        }),
    });

    await user.upload(
      screen.getByLabelText('Foto do produto'),
      new File(['conteudo'], 'foto.jpg', { type: 'image/jpeg' }),
    );
    await user.click(screen.getByRole('button', { name: 'Enviar foto' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'arquivo excede o tamanho máximo permitido',
    );
    expect(toastSuccess).not.toHaveBeenCalledWith('Foto enviada.');
    expect(screen.queryByRole('img')).not.toBeInTheDocument();
  });

  it('erro genérico (rede) no upload de foto: role="alert" com mensagem padrão', async () => {
    const user = userEvent.setup();
    await cadastrarComSucesso(user, {
      postFoto: () => Promise.reject(new Error('falha de rede')),
    });

    await user.upload(
      screen.getByLabelText('Foto do produto'),
      new File(['conteudo'], 'foto.jpg', { type: 'image/jpeg' }),
    );
    await user.click(screen.getByRole('button', { name: 'Enviar foto' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível enviar a foto agora. Tente novamente em instantes.',
    );
  });
});
