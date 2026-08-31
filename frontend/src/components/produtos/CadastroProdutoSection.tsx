import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { getAccessToken } from '@/lib/session';

/**
 * Seção "Cadastrar Produto" da `CatalogoPage` (Story 3.1, spec-3-1; Story 3.2,
 * spec-3-2, acrescenta a Nomenclatura Guiada), visível só a `almoxarife`+
 * (gate na própria página). Um `Card` com formulário: `nome`/`codigo`/
 * `observacoes` (`Input`), `categoria`/`estoque`/`template de nomenclatura`
 * (`Select`, os dois primeiros obrigatórios, o terceiro opcional — carregados
 * de `GET /api/categorias`/`GET /api/estoques` (`Promise.all`, obrigatórios)
 * e, isoladamente, `GET /api/nomenclatura-templates` — uma falha só nesse
 * endpoint opcional não bloqueia os dois primeiros) e as 5
 * dimensões pareadas (`Input` numérico + `Select` de unidade `mm/cm/m`),
 * todas opcionais — AD-9. `quantidade_inicial` é `Input` numérico obrigatório.
 *
 * Quando um template é selecionado, um texto de apoio abaixo do campo Nome
 * mostra o formato exato esperado (ex. "Formato: CABO [TIPO] [TENSÃO] ...")
 * — só orientação visual, nenhuma validação de formato roda no cliente; o
 * servidor (`services.CriarProduto`) é a única fonte de verdade (Story 3.2,
 * AC1/AC2).
 *
 * Submit -> `POST /api/produtos`. Sucesso: `toast.success('Produto
 * cadastrado.')` (molde de `LocaisEstoqueSection`) e o formulário é limpo.
 * `400` -> `<p role="alert">` com a mensagem devolvida pelo servidor (nomeia
 * o campo específico quando aplicável, ex. dimensão incompleta, ou o nome não
 * corresponder ao template selecionado); qualquer outro erro (rede, 500) ->
 * `<p role="alert">` genérico. Botão desabilitado durante o envio ou com
 * `nome`/`categoria_id`/`estoque_id`/`quantidade_inicial` em branco.
 *
 * Categoria é sempre selecionada da lista fixa de `GET /api/categorias`
 * (AC4) — nunca um campo de texto livre.
 *
 * Upload de foto (Story 3.5, spec-3-5): o `201` de `POST /api/produtos`
 * guarda `{id, nome}` do Produto recém-criado em estado local (não afeta a
 * limpeza do formulário acima) e passa a exibir um bloco "Adicionar foto" —
 * único ponto da UI hoje com um `produto_id` em mãos, já que o Catálogo
 * (Epic 4) ainda não existe. `<input type="file" accept="image/jpeg,
 * image/png,image/webp" capture>` seleciona o arquivo; o botão "Enviar foto"
 * dispara `POST /api/produtos/{id}/fotos` (multipart, campo `foto`, mesmo
 * `authHeaders()`). Sucesso -> `toast.success` e busca
 * `GET /api/produtos/{id}/fotos/{nome}` via `fetch` com `Authorization`,
 * montando um Object URL (`URL.createObjectURL`) para a miniatura — nunca um
 * `<img src="/api/...">` direto, que não carrega o header de auth. Erro
 * (`400`/`404`/rede) -> `<p role="alert">` com a mensagem do servidor (ou
 * genérica); botão "Enviar foto" desabilitado durante o envio.
 */

interface Categoria {
  id: string;
  codigo: string;
  nome: string;
}

interface Estoque {
  id: string;
  nome: string;
}

interface NomenclaturaTemplate {
  id: string;
  subtipo: string;
  template: string;
}

interface DimensaoEstado {
  valor: string;
  unidade: string;
}

const DIMENSAO_VAZIA: DimensaoEstado = { valor: '', unidade: '' };
const UNIDADES = ['mm', 'cm', 'm'] as const;

// SEM_TEMPLATE é o valor sentinela da opção "nome livre" do `<Select>` de
// template — Radix `Select.Item` proíbe `value=""` (usado internamente para
// representar "nada selecionado"), então a opção vazia descrita na spec-3-2
// precisa de um valor não-vazio próprio, traduzido de volta para `''`
// (estado `templateId`) no `onValueChange`.
const SEM_TEMPLATE = '__sem-template__';

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar categorias/estoques. Recarregue a página.';
const MENSAGEM_ERRO_CADASTRO =
  'Não foi possível cadastrar o produto agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_FOTO =
  'Não foi possível enviar a foto agora. Tente novamente em instantes.';

interface ProdutoCriado {
  id: string;
  nome: string;
}

/**
 * Converte o estado local de uma dimensão para o par aceito por
 * `POST /api/produtos`: os dois campos em branco -> `undefined` (chave
 * omitida do JSON, dimensão não informada); só um preenchido -> objeto
 * parcial, deixando o servidor rejeitar citando o campo (AD-9) — o cliente
 * não duplica essa validação.
 */
function montarDimensao(d: DimensaoEstado): { valor?: number; unidade?: string } | undefined {
  if (d.valor.trim() === '' && d.unidade === '') {
    return undefined;
  }
  const payload: { valor?: number; unidade?: string } = {};
  if (d.valor.trim() !== '') {
    payload.valor = Number(d.valor);
  }
  if (d.unidade !== '') {
    payload.unidade = d.unidade;
  }
  return payload;
}

function DimensaoField({
  label,
  idPrefix,
  dimensao,
  onChange,
}: {
  label: string;
  idPrefix: string;
  dimensao: DimensaoEstado;
  onChange: (d: DimensaoEstado) => void;
}) {
  return (
    <div className="flex flex-col gap-2">
      <Label htmlFor={`${idPrefix}-valor`}>{label}</Label>
      <div className="flex gap-2">
        <Input
          id={`${idPrefix}-valor`}
          type="number"
          inputMode="decimal"
          value={dimensao.valor}
          onChange={(event) => onChange({ ...dimensao, valor: event.target.value })}
          className="flex-1"
        />
        <Select
          value={dimensao.unidade}
          onValueChange={(unidade) => onChange({ ...dimensao, unidade })}
        >
          <SelectTrigger id={`${idPrefix}-unidade`} aria-label={`Unidade de ${label}`} className="w-20">
            <SelectValue placeholder="Un." />
          </SelectTrigger>
          <SelectContent>
            {UNIDADES.map((unidade) => (
              <SelectItem key={unidade} value={unidade}>
                {unidade}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </div>
  );
}

export function CadastroProdutoSection() {
  const [nome, setNome] = useState('');
  const [codigo, setCodigo] = useState('');
  const [observacoes, setObservacoes] = useState('');
  const [categoriaId, setCategoriaId] = useState('');
  const [estoqueId, setEstoqueId] = useState('');
  const [templateId, setTemplateId] = useState('');
  const [quantidadeInicial, setQuantidadeInicial] = useState('');
  const [comprimento, setComprimento] = useState<DimensaoEstado>(DIMENSAO_VAZIA);
  const [largura, setLargura] = useState<DimensaoEstado>(DIMENSAO_VAZIA);
  const [diametro, setDiametro] = useState<DimensaoEstado>(DIMENSAO_VAZIA);
  const [altura, setAltura] = useState<DimensaoEstado>(DIMENSAO_VAZIA);
  const [espessura, setEspessura] = useState<DimensaoEstado>(DIMENSAO_VAZIA);

  const [categorias, setCategorias] = useState<Categoria[]>([]);
  const [estoques, setEstoques] = useState<Estoque[]>([]);
  const [templates, setTemplates] = useState<NomenclaturaTemplate[]>([]);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);

  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);

  // Upload de foto (Story 3.5) — só existe depois de um cadastro bem-sucedido
  // nesta mesma sessão de tela (`produtoCriado`). `fotoInputKey` força
  // desmontar/remontar o `<input type="file">` após um envio bem-sucedido
  // (`components/ui/input.tsx` não encaminha `ref`, mesmo padrão de
  // `ImportacaoProdutosSection`).
  const [produtoCriado, setProdutoCriado] = useState<ProdutoCriado | null>(null);
  const [fotoInputKey, setFotoInputKey] = useState(0);
  const [arquivoFoto, setArquivoFoto] = useState<File | null>(null);
  const [enviandoFoto, setEnviandoFoto] = useState(false);
  const [erroFoto, setErroFoto] = useState<string | null>(null);
  const [fotoThumbUrl, setFotoThumbUrl] = useState<string | null>(null);

  // O Object URL da miniatura é revogado sempre que troca ou quando o
  // componente desmonta — evita vazar memória entre uploads/telas.
  useEffect(() => {
    return () => {
      if (fotoThumbUrl) {
        URL.revokeObjectURL(fotoThumbUrl);
      }
    };
  }, [fotoThumbUrl]);

  const carregarListas = useCallback(async () => {
    try {
      const [resCategorias, resEstoques] = await Promise.all([
        fetch('/api/categorias', { headers: authHeaders() }),
        fetch('/api/estoques', { headers: authHeaders() }),
      ]);
      if (!resCategorias.ok || !resEstoques.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const bodyCategorias = (await resCategorias.json()) as { categorias: Categoria[] };
      const bodyEstoques = (await resEstoques.json()) as { estoques: Estoque[] };
      setCategorias(bodyCategorias.categorias ?? []);
      setEstoques(bodyEstoques.estoques ?? []);
      setErroCarregar(null);
    } catch {
      setErroCarregar(MENSAGEM_ERRO_CARREGAR);
      return;
    }

    // Template de nomenclatura é opcional (Story 3.2, AC2) — buscado FORA do
    // Promise.all acima, com seu próprio try/catch: nem uma resposta não-ok
    // nem a própria fetch rejeitando (falha de rede isolada nesse endpoint)
    // pode bloquear o cadastro inteiro, que não depende dela. Degrada
    // silenciosamente para "nenhum template disponível" (o `<Select>` mostra
    // só a opção "Nome livre"), sem acionar `erroCarregar`.
    try {
      const resTemplates = await fetch('/api/nomenclatura-templates', { headers: authHeaders() });
      if (resTemplates.ok) {
        const bodyTemplates = (await resTemplates.json()) as { templates: NomenclaturaTemplate[] };
        setTemplates(bodyTemplates.templates ?? []);
      }
    } catch {
      // Falha de rede isolada em templates — mantém "Nome livre" (templates == []).
    }
  }, []);

  useEffect(() => {
    void (async () => {
      await carregarListas();
    })();
  }, [carregarListas]);

  function limparFormulario() {
    setNome('');
    setCodigo('');
    setObservacoes('');
    setCategoriaId('');
    setEstoqueId('');
    setTemplateId('');
    setQuantidadeInicial('');
    setComprimento(DIMENSAO_VAZIA);
    setLargura(DIMENSAO_VAZIA);
    setDiametro(DIMENSAO_VAZIA);
    setAltura(DIMENSAO_VAZIA);
    setEspessura(DIMENSAO_VAZIA);
  }

  const templateSelecionado = templates.find((t) => t.id === templateId);

  const desabilitado =
    enviando ||
    nome.trim() === '' ||
    categoriaId === '' ||
    estoqueId === '' ||
    quantidadeInicial.trim() === '';

  async function enviar(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    // Defesa em profundidade contra duplo-submit (molde de LocaisEstoqueSection):
    // o `disabled` do botão só reflete `enviando` após o próximo repaint.
    if (desabilitado) {
      return;
    }
    setErro(null);
    setEnviando(true);
    try {
      const res = await fetch('/api/produtos', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({
          nome,
          codigo: codigo.trim() === '' ? undefined : codigo.trim(),
          observacoes: observacoes.trim() === '' ? undefined : observacoes.trim(),
          categoria_id: categoriaId,
          estoque_id: estoqueId,
          template_id: templateId === '' ? undefined : templateId,
          quantidade_inicial: Number(quantidadeInicial),
          comprimento: montarDimensao(comprimento),
          largura: montarDimensao(largura),
          diametro: montarDimensao(diametro),
          altura: montarDimensao(altura),
          espessura: montarDimensao(espessura),
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
        setErro(body.error?.message ?? MENSAGEM_ERRO_CADASTRO);
        return;
      }
      const body = (await res.json()) as { produto: ProdutoCriado };
      toast.success('Produto cadastrado.');
      limparFormulario();

      // Bloco "Adicionar foto" (Story 3.5) passa a apontar para o Produto
      // recém-criado — qualquer upload/miniatura de um cadastro anterior é
      // descartado junto.
      setProdutoCriado(body.produto);
      setArquivoFoto(null);
      setErroFoto(null);
      setFotoInputKey((k) => k + 1);
      setFotoThumbUrl((anterior) => {
        if (anterior) {
          URL.revokeObjectURL(anterior);
        }
        return null;
      });
    } catch {
      setErro(MENSAGEM_ERRO_CADASTRO);
    } finally {
      setEnviando(false);
    }
  }

  // Envia `arquivoFoto` para POST /api/produtos/{id}/fotos (Story 3.5).
  // Sucesso: busca o arquivo salvo via GET (mesma sessão Bearer, `fetch` +
  // `authHeaders()`) e monta um Object URL para a miniatura — nunca um
  // `<img src="/api/...">` direto, que não carregaria o header `Authorization`.
  async function enviarFoto() {
    if (!produtoCriado || !arquivoFoto || enviandoFoto) {
      return;
    }
    setErroFoto(null);
    setEnviandoFoto(true);
    try {
      const formData = new FormData();
      formData.append('foto', arquivoFoto);
      const res = await fetch(`/api/produtos/${produtoCriado.id}/fotos`, {
        method: 'POST',
        headers: authHeaders(),
        body: formData,
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
        setErroFoto(body.error?.message ?? MENSAGEM_ERRO_FOTO);
        return;
      }
      const body = (await res.json()) as { foto: { nome: string; url: string } };

      const resFoto = await fetch(body.foto.url, { headers: authHeaders() });
      if (!resFoto.ok) {
        setErroFoto(MENSAGEM_ERRO_FOTO);
        return;
      }
      const blob = await resFoto.blob();
      setFotoThumbUrl((anterior) => {
        if (anterior) {
          URL.revokeObjectURL(anterior);
        }
        return URL.createObjectURL(blob);
      });

      toast.success('Foto enviada.');
      setArquivoFoto(null);
      setFotoInputKey((k) => k + 1);
    } catch {
      setErroFoto(MENSAGEM_ERRO_FOTO);
    } finally {
      setEnviandoFoto(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-lg">Cadastrar Produto</h2>
        <CardDescription>Cadastro manual de Produto com dimensões estruturadas.</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={enviar} className="flex flex-col gap-4" noValidate>
          {erroCarregar && (
            <p role="alert" className="text-body text-destructive">
              {erroCarregar}
            </p>
          )}

          <div className="flex flex-col gap-2">
            <Label htmlFor="produto-nome">Nome</Label>
            <Input
              id="produto-nome"
              value={nome}
              onChange={(event) => setNome(event.target.value)}
              aria-describedby={templateSelecionado ? 'produto-nome-formato' : undefined}
            />
            {templateSelecionado && (
              <p id="produto-nome-formato" className="text-label text-muted-foreground">
                Formato: {templateSelecionado.template}
              </p>
            )}
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="produto-template">Template de nomenclatura (opcional)</Label>
            <Select
              value={templateId === '' ? SEM_TEMPLATE : templateId}
              onValueChange={(valor) => setTemplateId(valor === SEM_TEMPLATE ? '' : valor)}
            >
              <SelectTrigger id="produto-template">
                <SelectValue placeholder="Nome livre (sem template)" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={SEM_TEMPLATE}>Nome livre (sem template)</SelectItem>
                {templates.map((template) => (
                  <SelectItem key={template.id} value={template.id}>
                    {template.subtipo}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="produto-codigo">Código</Label>
            <Input
              id="produto-codigo"
              value={codigo}
              onChange={(event) => setCodigo(event.target.value)}
            />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="produto-categoria">Categoria</Label>
            <Select value={categoriaId} onValueChange={setCategoriaId}>
              <SelectTrigger id="produto-categoria">
                <SelectValue placeholder="Selecione uma categoria" />
              </SelectTrigger>
              <SelectContent>
                {categorias.map((categoria) => (
                  <SelectItem key={categoria.id} value={categoria.id}>
                    {categoria.codigo} — {categoria.nome}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="produto-estoque">Estoque</Label>
            <Select value={estoqueId} onValueChange={setEstoqueId}>
              <SelectTrigger id="produto-estoque">
                <SelectValue placeholder="Selecione um estoque" />
              </SelectTrigger>
              <SelectContent>
                {estoques.map((estoque) => (
                  <SelectItem key={estoque.id} value={estoque.id}>
                    {estoque.nome}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="produto-quantidade-inicial">Quantidade inicial</Label>
            <Input
              id="produto-quantidade-inicial"
              type="number"
              inputMode="decimal"
              value={quantidadeInicial}
              onChange={(event) => setQuantidadeInicial(event.target.value)}
            />
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <DimensaoField
              label="Comprimento"
              idPrefix="produto-comprimento"
              dimensao={comprimento}
              onChange={setComprimento}
            />
            <DimensaoField
              label="Largura"
              idPrefix="produto-largura"
              dimensao={largura}
              onChange={setLargura}
            />
            <DimensaoField
              label="Diâmetro"
              idPrefix="produto-diametro"
              dimensao={diametro}
              onChange={setDiametro}
            />
            <DimensaoField
              label="Altura"
              idPrefix="produto-altura"
              dimensao={altura}
              onChange={setAltura}
            />
            <DimensaoField
              label="Espessura"
              idPrefix="produto-espessura"
              dimensao={espessura}
              onChange={setEspessura}
            />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="produto-observacoes">Observações</Label>
            <Input
              id="produto-observacoes"
              value={observacoes}
              onChange={(event) => setObservacoes(event.target.value)}
            />
          </div>

          {erro && (
            <p role="alert" className="text-body text-destructive">
              {erro}
            </p>
          )}

          <Button type="submit" disabled={desabilitado} className="self-start">
            {enviando ? 'Cadastrando...' : 'Cadastrar produto'}
          </Button>
        </form>

        {produtoCriado && (
          <div className="mt-4 flex flex-col gap-2 rounded-md border border-border p-4">
            <p className="text-body font-medium">Adicionar foto — {produtoCriado.nome}</p>
            <Input
              key={fotoInputKey}
              type="file"
              accept="image/jpeg,image/png,image/webp"
              capture
              aria-label="Foto do produto"
              onChange={(event) => setArquivoFoto(event.target.files?.[0] ?? null)}
            />

            {erroFoto && (
              <p role="alert" className="text-body text-destructive">
                {erroFoto}
              </p>
            )}

            <Button
              type="button"
              variant="outline"
              disabled={enviandoFoto || !arquivoFoto}
              onClick={() => void enviarFoto()}
              className="self-start"
            >
              {enviandoFoto ? 'Enviando...' : 'Enviar foto'}
            </Button>

            {fotoThumbUrl && (
              <img
                src={fotoThumbUrl}
                alt={`Foto de ${produtoCriado.nome}`}
                className="h-24 w-24 rounded-md border border-border object-cover"
              />
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export default CadastroProdutoSection;
