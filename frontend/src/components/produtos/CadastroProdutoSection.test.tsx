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
// miniatura da foto usa exatamente essa dupla). O stub devolve um valor
// DISTINTO por chamada (contador crescente) — não uma constante fixa —
// porque a galeria (Story 3.6) pode ter 2+ fotos ao mesmo tempo: um mock
// constante deixaria passar despercebido um bug em que o lightbox sempre
// mostra a primeira foto, não importa em qual miniatura o usuário clicou.
beforeEach(() => {
  let proximoId = 0;
  URL.createObjectURL = vi.fn(() => `blob:mock-url-${proximoId++}`);
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
    // GET /api/produtos/{id}/fotos (Story 3.6, listagem) — sem `/{arquivo}`
    // final, distinto do padrão abaixo que serve o arquivo em si.
    if (/^\/api\/produtos\/[^/]+\/fotos$/.test(url) && (!init?.method || init.method === 'GET')) {
      const handler = extra?.getFotos as FetchImpl | undefined;
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

  it('upload de foto com sucesso: envia multipart autenticado, rebusca a galeria e mostra a miniatura', async () => {
    const user = userEvent.setup();
    const fetchMock = await cadastrarComSucesso(user, {
      postFoto: () =>
        Promise.resolve({
          ok: true,
          status: 201,
          json: async () => ({ foto: { nome: 'p-1-111.jpg', url: '/api/produtos/p-1/fotos/p-1-111.jpg' } }),
        }),
      getFotos: () =>
        jsonOk({ fotos: [{ nome: 'p-1-111.jpg', url: '/api/produtos/p-1/fotos/p-1-111.jpg' }] }),
      getFoto: () => fotoBlobOk(),
    });

    await user.upload(
      screen.getByLabelText('Foto do produto'),
      new File(['conteudo'], 'foto.jpg', { type: 'image/jpeg' }),
    );
    await user.click(screen.getByRole('button', { name: 'Enviar foto' }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Foto enviada.'));

    const miniatura = await screen.findByRole('button', { name: 'Ampliar foto 1 de 1' });
    // A miniatura usa `alt=""` (decorativa — a acessibilidade já vem do
    // `aria-label` do próprio botão), então o navegador expõe role
    // "presentation", não "img" — consulta pelo elemento `<img>` diretamente.
    expect(miniatura.querySelector('img')).toHaveAttribute('src', 'blob:mock-url-0');

    const chamadaPost = fetchMock.mock.calls.find(
      (args: unknown[]) => args[0] === '/api/produtos/p-1/fotos' && (args[1] as RequestInit)?.method === 'POST',
    ) as [string, RequestInit] | undefined;
    if (!chamadaPost) {
      throw new Error('nenhuma chamada POST /api/produtos/p-1/fotos encontrada');
    }
    expect((chamadaPost[1].headers as Record<string, string>).Authorization).toBe('Bearer token-de-teste');
    expect(chamadaPost[1].body).toBeInstanceOf(FormData);
    expect((chamadaPost[1].body as FormData).get('foto')).toBeInstanceOf(File);

    const chamadaListagem = fetchMock.mock.calls.find(
      (args: unknown[]) =>
        args[0] === '/api/produtos/p-1/fotos' &&
        (!(args[1] as RequestInit)?.method || (args[1] as RequestInit)?.method === 'GET'),
    ) as [string, RequestInit] | undefined;
    if (!chamadaListagem) {
      throw new Error('nenhuma chamada GET /api/produtos/p-1/fotos (listagem) encontrada');
    }
    expect((chamadaListagem[1]?.headers as Record<string, string> | undefined)?.Authorization).toBe(
      'Bearer token-de-teste',
    );

    const chamadaGet = fetchMock.mock.calls.find(
      (args: unknown[]) => args[0] === '/api/produtos/p-1/fotos/p-1-111.jpg',
    ) as [string, RequestInit] | undefined;
    if (!chamadaGet) {
      throw new Error('nenhuma chamada GET da foto salva encontrada');
    }
    expect((chamadaGet[1].headers as Record<string, string>).Authorization).toBe('Bearer token-de-teste');
  });

  it('duas fotos enviadas: a galeria mostra as 2 miniaturas, ordem de envio preservada', async () => {
    const user = userEvent.setup();
    let fotosSalvas: { nome: string; url: string }[] = [];
    await cadastrarComSucesso(user, {
      postFoto: () => {
        const nome = `p-1-${100 + fotosSalvas.length}.jpg`;
        fotosSalvas = [...fotosSalvas, { nome, url: `/api/produtos/p-1/fotos/${nome}` }];
        return Promise.resolve({ ok: true, status: 201, json: async () => ({}) });
      },
      getFotos: () => jsonOk({ fotos: fotosSalvas }),
      getFoto: () => fotoBlobOk(),
    });

    await user.upload(
      screen.getByLabelText('Foto do produto'),
      new File(['conteudo-1'], 'foto1.jpg', { type: 'image/jpeg' }),
    );
    await user.click(screen.getByRole('button', { name: 'Enviar foto' }));
    await screen.findByRole('button', { name: 'Ampliar foto 1 de 1' });

    await user.upload(
      screen.getByLabelText('Foto do produto'),
      new File(['conteudo-2'], 'foto2.jpg', { type: 'image/jpeg' }),
    );
    await user.click(screen.getByRole('button', { name: 'Enviar foto' }));

    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Ampliar foto 1 de 2' })).toBeInTheDocument(),
    );
    expect(screen.getByRole('button', { name: 'Ampliar foto 2 de 2' })).toBeInTheDocument();
  });

  it('clique numa miniatura da galeria abre o lightbox em tela cheia com a foto certa', async () => {
    const user = userEvent.setup();
    await cadastrarComSucesso(user, {
      postFoto: () => Promise.resolve({ ok: true, status: 201, json: async () => ({}) }),
      getFotos: () =>
        jsonOk({ fotos: [{ nome: 'p-1-1.jpg', url: '/api/produtos/p-1/fotos/p-1-1.jpg' }] }),
      getFoto: () => fotoBlobOk(),
    });

    await user.upload(
      screen.getByLabelText('Foto do produto'),
      new File(['conteudo'], 'foto.jpg', { type: 'image/jpeg' }),
    );
    await user.click(screen.getByRole('button', { name: 'Enviar foto' }));

    const miniatura = await screen.findByRole('button', { name: 'Ampliar foto 1 de 1' });
    // Antes do clique, nenhum lightbox está montado como aberto.
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();

    await user.click(miniatura);

    expect(await screen.findByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Foto ampliada de Tubo PVC 100mm')).toBeInTheDocument();
  });

  it('lightbox mostra a foto correspondente à miniatura clicada, não sempre a primeira', async () => {
    const user = userEvent.setup();
    let fotosSalvas: { nome: string; url: string }[] = [];
    await cadastrarComSucesso(user, {
      postFoto: () => {
        const nome = `p-1-${100 + fotosSalvas.length}.jpg`;
        fotosSalvas = [...fotosSalvas, { nome, url: `/api/produtos/p-1/fotos/${nome}` }];
        return Promise.resolve({ ok: true, status: 201, json: async () => ({}) });
      },
      getFotos: () => jsonOk({ fotos: fotosSalvas }),
      getFoto: () => fotoBlobOk(),
    });

    await user.upload(
      screen.getByLabelText('Foto do produto'),
      new File(['conteudo-1'], 'foto1.jpg', { type: 'image/jpeg' }),
    );
    await user.click(screen.getByRole('button', { name: 'Enviar foto' }));
    await screen.findByRole('button', { name: 'Ampliar foto 1 de 1' });

    await user.upload(
      screen.getByLabelText('Foto do produto'),
      new File(['conteudo-2'], 'foto2.jpg', { type: 'image/jpeg' }),
    );
    await user.click(screen.getByRole('button', { name: 'Enviar foto' }));
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Ampliar foto 2 de 2' })).toBeInTheDocument(),
    );

    // O mock de URL.createObjectURL devolve um valor DISTINTO por chamada —
    // as duas miniaturas têm Object URLs diferentes, condição necessária
    // para este teste provar algo (senão não haveria como distinguir "abriu
    // a foto certa" de "sempre abre a primeira").
    const miniatura1 = screen.getByRole('button', { name: 'Ampliar foto 1 de 2' });
    const miniatura2 = screen.getByRole('button', { name: 'Ampliar foto 2 de 2' });
    const srcMiniatura1 = miniatura1.querySelector('img')?.getAttribute('src');
    const srcMiniatura2 = miniatura2.querySelector('img')?.getAttribute('src');
    expect(srcMiniatura1).toBeTruthy();
    expect(srcMiniatura2).toBeTruthy();
    expect(srcMiniatura1).not.toBe(srcMiniatura2);

    await user.click(miniatura2);

    const dialog = await screen.findByRole('dialog');
    const imagemAmpliada = dialog.querySelector('img');
    expect(imagemAmpliada).toHaveAttribute('src', srcMiniatura2);
    expect(imagemAmpliada?.getAttribute('src')).not.toBe(srcMiniatura1);
  });

  // Fecha o lightbox das 3 formas descritas na AC (clique fora, Esc, botão
  // "Fechar") — nas 3, o `Dialog` some e a galeria por trás permanece
  // exatamente como estava (nenhuma navegação, nenhum reload).
  describe('fechar o lightbox', () => {
    async function abrirLightbox(user: ReturnType<typeof userEvent.setup>) {
      await cadastrarComSucesso(user, {
        postFoto: () => Promise.resolve({ ok: true, status: 201, json: async () => ({}) }),
        getFotos: () =>
          jsonOk({ fotos: [{ nome: 'p-1-1.jpg', url: '/api/produtos/p-1/fotos/p-1-1.jpg' }] }),
        getFoto: () => fotoBlobOk(),
      });
      await user.upload(
        screen.getByLabelText('Foto do produto'),
        new File(['conteudo'], 'foto.jpg', { type: 'image/jpeg' }),
      );
      await user.click(screen.getByRole('button', { name: 'Enviar foto' }));
      const miniatura = await screen.findByRole('button', { name: 'Ampliar foto 1 de 1' });
      await user.click(miniatura);
      await screen.findByRole('dialog');
    }

    it('Esc fecha o lightbox sem alterar o restante da tela', async () => {
      const user = userEvent.setup();
      await abrirLightbox(user);

      await user.keyboard('{Escape}');

      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
      expect(screen.getByRole('button', { name: 'Ampliar foto 1 de 1' })).toBeInTheDocument();
    });

    it('clique no botão "Fechar" fecha o lightbox sem alterar o restante da tela', async () => {
      const user = userEvent.setup();
      await abrirLightbox(user);

      await user.click(screen.getByRole('button', { name: 'Fechar' }));

      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
      expect(screen.getByRole('button', { name: 'Ampliar foto 1 de 1' })).toBeInTheDocument();
    });

    it('clique fora (overlay) fecha o lightbox sem alterar o restante da tela', async () => {
      const user = userEvent.setup();
      await abrirLightbox(user);

      const overlay = document.querySelector('[data-slot="dialog-overlay"]');
      if (!overlay) {
        throw new Error('overlay do dialog não encontrado');
      }
      await user.click(overlay as HTMLElement);

      await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
      expect(screen.getByRole('button', { name: 'Ampliar foto 1 de 1' })).toBeInTheDocument();
    });
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
    expect(screen.queryByRole('button', { name: /Ampliar foto/ })).not.toBeInTheDocument();
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

  // O upload em si (`POST`) teve sucesso — só a rebusca da galeria
  // (`GET /api/produtos/{id}/fotos`) falhou depois. O usuário NÃO pode
  // receber a mesma mensagem de "não foi possível enviar a foto": o arquivo
  // já está salvo no servidor, e sugerir reenvio duplicaria o arquivo (nunca
  // sobrescrito, Story 3.5).
  it('upload com sucesso mas rebusca da galeria falha: confirma o envio (toast) e avisa só sobre a galeria, nunca com a mensagem de erro de upload', async () => {
    const user = userEvent.setup();
    await cadastrarComSucesso(user, {
      postFoto: () => Promise.resolve({ ok: true, status: 201, json: async () => ({}) }),
      getFotos: () => Promise.resolve({ ok: false, status: 500, json: async () => ({}) }),
    });

    await user.upload(
      screen.getByLabelText('Foto do produto'),
      new File(['conteudo'], 'foto.jpg', { type: 'image/jpeg' }),
    );
    await user.click(screen.getByRole('button', { name: 'Enviar foto' }));

    // O upload teve sucesso — o toast confirma isso incondicionalmente,
    // mesmo com a rebusca da galeria falhando logo em seguida.
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Foto enviada.'));

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent(
      'Foto enviada, mas não foi possível atualizar a galeria agora. Recarregue a página para vê-la.',
    );
    expect(alerta).not.toHaveTextContent(
      'Não foi possível enviar a foto agora. Tente novamente em instantes.',
    );
    // A galeria não atualizou (a rebusca falhou), mas isso não aparece como
    // falha do upload.
    expect(screen.queryByRole('button', { name: /Ampliar foto/ })).not.toBeInTheDocument();
  });

  // A listagem (`GET /api/produtos/{id}/fotos`) teve sucesso, mas a busca do
  // BLOB de uma foto específica (`GET /api/produtos/{id}/fotos/{nome}`)
  // falhou — mesmo tratamento do caso acima (a listagem em si falhando):
  // `carregarFotos` só chama `setFotos` se TODAS as buscas tiverem sucesso,
  // então a galeria não deve renderizar um subconjunto incompleto de
  // miniaturas nem esconder o aviso.
  it('upload com sucesso mas a busca do blob de uma foto da galeria falha: avisa só sobre a galeria, sem renderizar miniaturas parciais', async () => {
    const user = userEvent.setup();
    await cadastrarComSucesso(user, {
      postFoto: () => Promise.resolve({ ok: true, status: 201, json: async () => ({}) }),
      getFotos: () =>
        jsonOk({
          fotos: [
            { nome: 'p-1-1.jpg', url: '/api/produtos/p-1/fotos/p-1-1.jpg' },
            { nome: 'p-1-2.jpg', url: '/api/produtos/p-1/fotos/p-1-2.jpg' },
          ],
        }),
      getFoto: (url: string) =>
        url.endsWith('/p-1-2.jpg')
          ? Promise.resolve({ ok: false, status: 500, json: async () => ({}) })
          : fotoBlobOk(),
    });

    await user.upload(
      screen.getByLabelText('Foto do produto'),
      new File(['conteudo'], 'foto.jpg', { type: 'image/jpeg' }),
    );
    await user.click(screen.getByRole('button', { name: 'Enviar foto' }));

    // O upload teve sucesso — o toast confirma isso incondicionalmente,
    // mesmo com a busca de uma das fotos da galeria falhando logo em seguida.
    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith('Foto enviada.'));

    const alerta = await screen.findByRole('alert');
    expect(alerta).toHaveTextContent(
      'Foto enviada, mas não foi possível atualizar a galeria agora. Recarregue a página para vê-la.',
    );
    // Nenhuma miniatura é renderizada — nem a foto cuja busca teve sucesso —
    // porque `carregarFotos` só publica a galeria quando TODAS as buscas
    // (listagem + cada foto) tiverem sucesso; um subconjunto parcial seria
    // pior que nenhum, pois esconderia a existência da 2ª foto sem avisar.
    expect(screen.queryByRole('button', { name: /Ampliar foto/ })).not.toBeInTheDocument();
  });

  it('produto recém-cadastrado sem nenhuma foto: bloco "Adicionar foto" aparece sem miniatura, sem erro', async () => {
    const user = userEvent.setup();
    await cadastrarComSucesso(user);

    expect(screen.getByText('Adicionar foto — Tubo PVC 100mm')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Ampliar foto/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
