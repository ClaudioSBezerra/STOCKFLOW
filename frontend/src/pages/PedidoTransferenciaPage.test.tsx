import { describe, expect, it, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { PedidoTransferenciaPage } from './PedidoTransferenciaPage';

const toastSuccess = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: toastSuccess, error: toastError } }));

vi.mock('@/lib/session', () => ({
  getAccessToken: () => 'token-de-teste',
}));

// submeterPedidoTransferencia é mockado diretamente — a lógica de
// requisição em si já tem cobertura dedicada em lib/pedidos (e no backend);
// esta página é só a view/formulário sobre ela.
const submeterPedidoTransferenciaMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/pedidos', () => ({
  submeterPedidoTransferencia: submeterPedidoTransferenciaMock,
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
// (porEstoque[].{estoqueId,disponivel}), mesmo formato de PedidoBaixaPage.test.tsx.
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
  await waitFor(() =>
    expect(screen.getByRole('combobox', { name: 'Estoque origem' })).toBeEnabled(),
  );
}

async function preencherAteProduto(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('Produto'), 'cimento');
  await user.click(await screen.findByRole('button', { name: /Cimento CP-II 50kg/ }));
}

async function preencherFormularioCompleto(user: ReturnType<typeof userEvent.setup>) {
  await aguardarEstoquesCarregados();
  await preencherAteProduto(user);

  await user.click(screen.getByRole('combobox', { name: 'Estoque origem' }));
  await user.click(await screen.findByRole('option', { name: /Almoxarifado Central/ }));

  await user.click(screen.getByRole('combobox', { name: 'Estoque destino' }));
  await user.click(await screen.findByRole('option', { name: /Canteiro A/ }));

  await user.type(screen.getByLabelText(/Quantidade/), '5');
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});

describe('PedidoTransferenciaPage', () => {
  it('renderiza o formulário (sem campo Solicitante) e carrega a lista de Estoques (com o nome da Filial em cada opção)', async () => {
    stubFetchPadrao();
    render(<PedidoTransferenciaPage />);

    expect(screen.getByRole('heading', { name: 'Pedido de Transferência entre Filiais' })).toBeInTheDocument();
    expect(screen.queryByLabelText('Solicitante')).not.toBeInTheDocument();
    await aguardarEstoquesCarregados();

    await userEvent.setup().click(screen.getByRole('combobox', { name: 'Estoque origem' }));
    expect(await screen.findByRole('option', { name: 'Almoxarifado Central — Matriz' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'Canteiro A — Filial Sul' })).toBeInTheDocument();
  });

  it('busca Produto por nome e permite escolher um resultado', async () => {
    stubFetchPadrao();
    const user = userEvent.setup();
    render(<PedidoTransferenciaPage />);
    await aguardarEstoquesCarregados();

    await preencherAteProduto(user);

    expect(screen.getByText('Cimento CP-II 50kg')).toBeInTheDocument();
    expect(screen.getByText(/000001/)).toBeInTheDocument();
    // O campo de busca é substituído pelo produto escolhido + "Trocar produto".
    expect(screen.queryByLabelText('Produto')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Trocar produto' })).toBeInTheDocument();
  });

  it('mostra o saldo disponível da ORIGEM assim que Produto e Estoque origem estão selecionados', async () => {
    stubFetchPadrao();
    const user = userEvent.setup();
    render(<PedidoTransferenciaPage />);
    await aguardarEstoquesCarregados();
    await preencherAteProduto(user);

    await user.click(screen.getByRole('combobox', { name: 'Estoque origem' }));
    await user.click(await screen.findByRole('option', { name: /Almoxarifado Central/ }));

    expect(await screen.findByText('Disponível: 8')).toBeInTheDocument();
  });

  it('Estoque origem sem nenhuma linha de saldo para o Produto mostra disponível 0', async () => {
    stubFetchPadrao();
    const user = userEvent.setup();
    render(<PedidoTransferenciaPage />);
    await aguardarEstoquesCarregados();
    await preencherAteProduto(user);

    await user.click(screen.getByRole('combobox', { name: 'Estoque origem' }));
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
    render(<PedidoTransferenciaPage />);
    await aguardarEstoquesCarregados();
    await preencherAteProduto(user);

    await user.click(screen.getByRole('combobox', { name: 'Estoque origem' }));
    await user.click(await screen.findByRole('option', { name: /Almoxarifado Central/ }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar o saldo disponível agora.',
    );
  });

  it('Estoque destino nunca lista a opção já escolhida como origem; trocar a origem reseta um destino que empata com ela', async () => {
    stubFetchPadrao();
    const user = userEvent.setup();
    render(<PedidoTransferenciaPage />);
    await aguardarEstoquesCarregados();

    await user.click(screen.getByRole('combobox', { name: 'Estoque origem' }));
    await user.click(await screen.findByRole('option', { name: /Almoxarifado Central/ }));

    await user.click(screen.getByRole('combobox', { name: 'Estoque destino' }));
    expect(screen.queryByRole('option', { name: /Almoxarifado Central/ })).not.toBeInTheDocument();
    await user.click(await screen.findByRole('option', { name: /Canteiro A/ }));
    expect(screen.getByRole('combobox', { name: 'Estoque destino' })).toHaveTextContent('Canteiro A — Filial Sul');

    // Troca a origem para o MESMO estoque que já era o destino — o destino
    // escolhido deixa de ser válido e é resetado (Spec Change Log, spec-20-1).
    await user.click(screen.getByRole('combobox', { name: 'Estoque origem' }));
    await user.click(await screen.findByRole('option', { name: /Canteiro A/ }));
    expect(screen.getByRole('combobox', { name: 'Estoque destino' })).toHaveTextContent('Selecione o destino');
  });

  it('submete com sucesso: chama submeterPedidoTransferencia SEM solicitante, com obraCentroCusto vazio quando não preenchido; mostra toast e limpa o formulário', async () => {
    stubFetchPadrao();
    submeterPedidoTransferenciaMock.mockResolvedValue({
      id: 'ped-1',
      status: 'pendente',
      tipo: 'transferencia',
    });
    const user = userEvent.setup();
    render(<PedidoTransferenciaPage />);

    await preencherFormularioCompleto(user);
    await user.click(screen.getByRole('button', { name: 'Enviar pedido de transferência' }));

    await waitFor(() => expect(submeterPedidoTransferenciaMock).toHaveBeenCalledTimes(1));
    expect(submeterPedidoTransferenciaMock).toHaveBeenCalledWith({
      obraCentroCusto: '',
      observacao: '',
      produtoId: 'p-1',
      estoqueOrigemId: 'e-1',
      estoqueDestinoId: 'e-2',
      quantidade: 5,
    });
    await waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith(
        'Pedido de Transferência enviado. O saldo da origem ficou reservado até a decisão do gestor.',
      ),
    );
    // Formulário limpo após o sucesso — o produto some, novo campo de busca volta.
    expect(await screen.findByLabelText('Produto')).toHaveValue('');
  });

  it('erro do servidor aparece inline em role="alert", sem toast de sucesso', async () => {
    stubFetchPadrao();
    submeterPedidoTransferenciaMock.mockRejectedValue(
      new Error('disponibilidade insuficiente para: Cimento CP-II 50kg'),
    );
    const user = userEvent.setup();
    render(<PedidoTransferenciaPage />);

    await preencherFormularioCompleto(user);
    await user.click(screen.getByRole('button', { name: 'Enviar pedido de transferência' }));

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
    render(<PedidoTransferenciaPage />);

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Não foi possível carregar a lista de estoques.',
    );
  });

  it('botão de envio fica desabilitado até Produto, Estoque origem, Estoque destino e quantidade > 0 estarem preenchidos — Obra/centro de custo NUNCA bloqueia', async () => {
    stubFetchPadrao();
    const user = userEvent.setup();
    render(<PedidoTransferenciaPage />);
    await aguardarEstoquesCarregados();

    const botao = screen.getByRole('button', { name: 'Enviar pedido de transferência' });
    expect(botao).toBeDisabled();

    await preencherAteProduto(user);
    expect(botao).toBeDisabled();

    await user.click(screen.getByRole('combobox', { name: 'Estoque origem' }));
    await user.click(await screen.findByRole('option', { name: /Almoxarifado Central/ }));
    expect(botao).toBeDisabled();

    await user.click(screen.getByRole('combobox', { name: 'Estoque destino' }));
    await user.click(await screen.findByRole('option', { name: /Canteiro A/ }));
    expect(botao).toBeDisabled();

    await user.type(screen.getByLabelText(/Quantidade/), '5');
    expect(botao).toBeEnabled();
  });

  it('envia com Obra/centro de custo preenchida quando o usuário escolhe preencher', async () => {
    stubFetchPadrao();
    submeterPedidoTransferenciaMock.mockResolvedValue({ id: 'ped-1', status: 'pendente', tipo: 'transferencia' });
    const user = userEvent.setup();
    render(<PedidoTransferenciaPage />);

    await preencherFormularioCompleto(user);
    await user.type(screen.getByLabelText('Obra / centro de custo (opcional)'), 'Obra Norte');
    await user.click(screen.getByRole('button', { name: 'Enviar pedido de transferência' }));

    await waitFor(() => expect(submeterPedidoTransferenciaMock).toHaveBeenCalledTimes(1));
    expect(submeterPedidoTransferenciaMock).toHaveBeenCalledWith(
      expect.objectContaining({ obraCentroCusto: 'Obra Norte' }),
    );
  });
});
