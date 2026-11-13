import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { PedidoBaixaPage } from './PedidoBaixaPage';

const toastSuccess = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess, error: toastError } }));

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

// submeterPedidoBaixa é mockado diretamente — a lógica de requisição em si
// já tem cobertura dedicada em lib/pedidos (e no backend); esta página é só
// a view/formulário sobre ela.
const submeterPedidoBaixaMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/pedidos', () => ({
  submeterPedidoBaixa: submeterPedidoBaixaMock,
}));

type FetchImpl = (
  url: string,
  init?: RequestInit,
) => Promise<{ ok: boolean; status?: number; json: () => Promise<unknown> }>;

function stubFetch(impl: FetchImpl) {
  const fn = vi.fn(impl);
  vi.stubGlobal('fetch', fn);
  return fn;
}

function jsonOk(body: unknown, status = 200) {
  return Promise.resolve({ ok: true, status, json: async () => body });
}

const ESTOQUES = [
  { id: 'e-1', nome: 'Almoxarifado Central', filial_nome: 'Matriz' },
  { id: 'e-2', nome: 'Canteiro A', filial_nome: 'Filial Sul' },
];

const PRODUTOS = [
  {
    id: 'p-1',
    nome: 'Cimento CP-II 50kg',
    codigo: '000001',
    categoria: { id: 'c-1', codigo: '01.001', nome: 'Cimentos' },
    unidadeMedida: 'un',
    embalagem: null,
  },
];

// Detalhe mínimo de GET /api/produtos/p-1 — só o que a página lê
// (porEstoque[].{estoqueId,disponivel}), mesmo formato de ProdutoDetalhePage.
// Envelope `{"produto": ...}` — o MESMO de ObterProdutoHandler
// (handlers/produtos.go:719): bug real corrigido nesta revisão, o fixture
// antes testava contra `{porEstoque: [...]}` solto, que nunca é a resposta
// de verdade — por isso o "Disponível: 0" sempre exibido não quebrava teste
// nenhum.
const DETALHE_PRODUTO_P1 = {
  produto: {
    id: 'p-1',
    porEstoque: [
      { estoqueId: 'e-1', estoqueNome: 'Almoxarifado Central', quantidade: 20, reservada: 12, disponivel: 8 },
      { estoqueId: 'e-2', estoqueNome: 'Canteiro A', quantidade: 0, reservada: 0, disponivel: 0 },
    ],
  },
};

function stubFetchPadrao() {
  return stubFetch((url) => {
    if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
    if (url.startsWith('/api/produtos/busca')) return jsonOk({ produtos: PRODUTOS });
    if (url === '/api/produtos/p-1') return jsonOk(DETALHE_PRODUTO_P1);
    throw new Error(`URL inesperada: ${url}`);
  });
}

async function aguardarEstoquesCarregados() {
  await waitFor(() => expect(screen.getByRole('combobox', { name: 'Estoque' })).toBeEnabled());
}

async function preencherAteProduto(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('Produto'), 'cimento');
  await user.click(await screen.findByRole('button', { name: /Cimento CP-II 50kg/ }));
}

async function preencherFormularioCompleto(user: ReturnType<typeof userEvent.setup>) {
  await aguardarEstoquesCarregados();
  await preencherAteProduto(user);

  await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
  await user.click(await screen.findByRole('option', { name: /Almoxarifado Central/ }));

  await user.type(screen.getByLabelText(/Quantidade/), '5');
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('PedidoBaixaPage', () => {
  it('renderiza o formulário (sem campo Solicitante) e carrega a lista de Estoques', async () => {
    stubFetchPadrao();
    render(<PedidoBaixaPage />);

    expect(screen.getByRole('heading', { name: 'Pedido de Baixa de Estoque' })).toBeInTheDocument();
    expect(screen.queryByLabelText('Solicitante')).not.toBeInTheDocument();
    await aguardarEstoquesCarregados();

    await userEvent.setup().click(screen.getByRole('combobox', { name: 'Estoque' }));
    expect(await screen.findByRole('option', { name: 'Almoxarifado Central — Matriz' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Canteiro A — Filial Sul' })).toBeInTheDocument();
  });

  it('busca Produto por nome e permite escolher um resultado', async () => {
    stubFetchPadrao();
    const user = userEvent.setup();
    render(<PedidoBaixaPage />);
    await aguardarEstoquesCarregados();

    await preencherAteProduto(user);

    expect(screen.getByText('Cimento CP-II 50kg')).toBeInTheDocument();
    expect(screen.getByText(/000001/)).toBeInTheDocument();
    // O campo de busca é substituído pelo produto escolhido + "Trocar produto".
    expect(screen.queryByLabelText('Produto')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Trocar produto' })).toBeInTheDocument();
  });

  it('mostra o saldo disponível do Estoque escolhido assim que Produto e Estoque estão selecionados', async () => {
    stubFetchPadrao();
    const user = userEvent.setup();
    render(<PedidoBaixaPage />);
    await aguardarEstoquesCarregados();
    await preencherAteProduto(user);

    await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
    await user.click(await screen.findByRole('option', { name: /Almoxarifado Central/ }));

    expect(await screen.findByText('Disponível: 8')).toBeInTheDocument();
  });

  it('Estoque sem nenhuma linha de saldo para o Produto mostra disponível 0', async () => {
    stubFetchPadrao();
    const user = userEvent.setup();
    render(<PedidoBaixaPage />);
    await aguardarEstoquesCarregados();
    await preencherAteProduto(user);

    await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
    await user.click(await screen.findByRole('option', { name: /Canteiro A/ }));

    expect(await screen.findByText('Disponível: 0')).toBeInTheDocument();
  });

  it('falha ao carregar o saldo disponível mostra alerta inline, sem travar o resto do formulário', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') return jsonOk({ estoques: ESTOQUES });
      if (url.startsWith('/api/produtos/busca')) return jsonOk({ produtos: PRODUTOS });
      if (url === '/api/produtos/p-1') return Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
      throw new Error(`URL inesperada: ${url}`);
    });
    const user = userEvent.setup();
    render(<PedidoBaixaPage />);
    await aguardarEstoquesCarregados();
    await preencherAteProduto(user);

    await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
    await user.click(await screen.findByRole('option', { name: /Almoxarifado Central/ }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar o saldo disponível agora.',
    );
  });

  it('submete com sucesso: chama submeterPedidoBaixa SEM solicitante, com obraCentroCusto vazio quando não preenchido; mostra toast e limpa o formulário', async () => {
    stubFetchPadrao();
    submeterPedidoBaixaMock.mockResolvedValue({
      id: 'ped-1',
      status: 'pendente',
      tipo: 'baixa',
    });
    const user = userEvent.setup();
    render(<PedidoBaixaPage />);

    await preencherFormularioCompleto(user);
    await user.click(screen.getByRole('button', { name: 'Enviar pedido de baixa' }));

    await waitFor(() => expect(submeterPedidoBaixaMock).toHaveBeenCalledTimes(1));
    expect(submeterPedidoBaixaMock).toHaveBeenCalledWith({
      obraCentroCusto: '',
      observacao: '',
      produtoId: 'p-1',
      estoqueId: 'e-1',
      quantidade: 5,
    });
    await waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith(
        'Pedido de Baixa enviado. O saldo ficou reservado até a decisão do gestor.',
      ),
    );
    // Formulário limpo após o sucesso — o produto some, novo campo de busca volta.
    expect(await screen.findByLabelText('Produto')).toHaveValue('');
  });

  it('erro do servidor aparece inline em role="alert", sem toast de sucesso', async () => {
    stubFetchPadrao();
    submeterPedidoBaixaMock.mockRejectedValue(
      new Error('disponibilidade insuficiente para: Cimento CP-II 50kg'),
    );
    const user = userEvent.setup();
    render(<PedidoBaixaPage />);

    await preencherFormularioCompleto(user);
    await user.click(screen.getByRole('button', { name: 'Enviar pedido de baixa' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'disponibilidade insuficiente para: Cimento CP-II 50kg',
    );
    expect(toastSuccess).not.toHaveBeenCalled();
    // Falha não limpa o formulário — o produto continua escolhido.
    expect(screen.getByText('Cimento CP-II 50kg')).toBeInTheDocument();
  });

  it('falha ao carregar Estoques mostra alerta inline', async () => {
    stubFetch((url) => {
      if (url === '/api/estoques') return Promise.resolve({ ok: false, status: 500, json: async () => ({}) });
      throw new Error(`URL inesperada: ${url}`);
    });
    render(<PedidoBaixaPage />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar a lista de estoques.',
    );
  });

  it('botão de envio fica desabilitado até Produto, Estoque e quantidade > 0 estarem preenchidos — Obra/centro de custo NUNCA bloqueia', async () => {
    stubFetchPadrao();
    const user = userEvent.setup();
    render(<PedidoBaixaPage />);
    await aguardarEstoquesCarregados();

    const botao = screen.getByRole('button', { name: 'Enviar pedido de baixa' });
    expect(botao).toBeDisabled();

    await preencherAteProduto(user);
    expect(botao).toBeDisabled();

    await user.click(screen.getByRole('combobox', { name: 'Estoque' }));
    await user.click(await screen.findByRole('option', { name: /Almoxarifado Central/ }));
    expect(botao).toBeDisabled();

    await user.type(screen.getByLabelText(/Quantidade/), '5');
    expect(botao).toBeEnabled();
  });

  it('envia com Obra/centro de custo preenchida quando o usuário escolhe preencher', async () => {
    stubFetchPadrao();
    submeterPedidoBaixaMock.mockResolvedValue({ id: 'ped-1', status: 'pendente', tipo: 'baixa' });
    const user = userEvent.setup();
    render(<PedidoBaixaPage />);

    await preencherFormularioCompleto(user);
    await user.type(screen.getByLabelText('Obra / centro de custo (opcional)'), 'Obra Norte');
    await user.click(screen.getByRole('button', { name: 'Enviar pedido de baixa' }));

    await waitFor(() => expect(submeterPedidoBaixaMock).toHaveBeenCalledTimes(1));
    expect(submeterPedidoBaixaMock).toHaveBeenCalledWith(
      expect.objectContaining({ obraCentroCusto: 'Obra Norte' }),
    );
  });
});
