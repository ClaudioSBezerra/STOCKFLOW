import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { XIcon } from 'lucide-react';
import { Link } from 'react-router-dom';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Dialog, DialogClose, DialogContent, DialogTitle } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { apiUrl, authHeaders } from '@/lib/api';
import { DIMENSAO_VAZIA, UNIDADES_MEDIDA, montarDimensao, type DimensaoEstado } from '@/lib/produtos';
import { DimensaoField } from './DimensaoField';

/**
 * Seção "Cadastrar Produto" da `CatalogoPage` (Story 3.1, spec-3-1; Story 3.2,
 * spec-3-2, acrescenta a Nomenclatura Guiada; Story 10.1, spec-10-1, torna o
 * Template sempre obrigatório; Story 10.2, spec-10-2, tira `codigo` da
 * entrada — o servidor gera o código sequencial; Story 10.3, spec-10-3,
 * acrescenta Código do Fornecedor/EAN-13/Embalagem, texto livre opcional, e
 * Unidade de Medida, `Select` com as 12 opções fixas do enum, obrigatório),
 * visível só a `almoxarife`+ (gate na própria página). Um `Card` com
 * formulário: `nome`/`observacoes`/`código do fornecedor`/`ean-13`/
 * `embalagem` (`Input`), `categoria`/`template de nomenclatura`/
 * `unidade de medida` (`Select`, os três obrigatórios — categoria/template
 * carregados de `GET /api/categorias`/`GET /api/nomenclatura-templates` num
 * único `Promise.all`; falha em QUALQUER um dos dois aciona `erroCarregar` e
 * bloqueia o cadastro, já que sem a lista de templates o Almoxarife não tem
 * como selecionar um) e as 5 dimensões pareadas (`Input` numérico + `Select`
 * de unidade `mm/cm/m`), todas opcionais — AD-9.
 *
 * Story 11.6 (FR8, AD-29): o cadastro NÃO tem Estoque destino nem quantidade
 * inicial — o Produto nasce sem saldo e a entrada de saldo é feita
 * exclusivamente pelo Lançamento de Saldo (Story 11.1). O formulário não faz
 * `GET /api/estoques` nem envia `estoque_id`/`quantidade_inicial`.
 * `codigo` (Story 10.2) é um `Input` `disabled`, só leitura — nenhum estado
 * próprio, `value={produtoCriado?.codigo ?? ''}`: vazio antes do cadastro,
 * preenchido com o código gerado pelo servidor depois de um `201`. Nenhuma
 * validação de formato de EAN-13 roda no cliente — o servidor
 * (`services.CriarProduto`) é a única fonte de verdade (Story 10.3, mesmo
 * princípio já usado para o formato de nome/template).
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
 * `nome`/`categoria_id`/`template_id`/`unidade_medida` em branco.
 *
 * Categoria é sempre selecionada da lista fixa de `GET /api/categorias`
 * (AC4) — nunca um campo de texto livre.
 *
 * Upload de foto (Story 3.5, spec-3-5): o `201` de `POST /api/produtos`
 * guarda `{id, nome, codigo}` do Produto recém-criado em estado local (não
 * afeta a limpeza do formulário acima) e passa a exibir um bloco "Adicionar
 * foto" — único ponto da UI hoje com um `produto_id` em mãos, já que o Catálogo
 * (Epic 4) ainda não existe. `<input type="file" accept="image/jpeg,
 * image/png,image/webp" capture>` seleciona o arquivo; o botão "Enviar foto"
 * dispara `POST /api/produtos/{id}/fotos` (multipart, campo `foto`, mesmo
 * `authHeaders()`). Erro (`400`/`404`/rede) -> `<p role="alert">` com a
 * mensagem do servidor (ou genérica); botão "Enviar foto" desabilitado
 * durante o envio.
 *
 * Galeria e lightbox (Story 3.6, spec-3-6): cada upload bem-sucedido rebusca
 * `GET /api/produtos/{id}/fotos` (nunca guarda só a última foto) e, para
 * cada `nome` ainda sem Object URL em cache local (`objectUrlCacheRef`),
 * busca `GET /api/produtos/{id}/fotos/{nome}` (mesmo `authHeaders()`) e
 * monta `URL.createObjectURL` — miniaturas já resolvidas não são
 * rebuscadas; nunca um `<img src="/api/...">` direto, que não carregaria o
 * header de auth. O `toast.success('Foto enviada.')`/reset do input
 * confirmam o upload assim que o `201` chega — ANTES dessa rebusca —
 * porque o arquivo já está salvo no servidor nesse ponto; se só a rebusca
 * falhar depois, `erroFoto` mostra uma mensagem DISTINTA (nunca a de erro
 * de upload) para não sugerir um reenvio que duplicaria o arquivo. A
 * galeria é uma grade de `<button>` (miniatura + `aria-label="Ampliar foto
 * N de M"`); clicar numa abre um `Dialog` (`components/ui/dialog.tsx`) em
 * tela cheia (`sm:!max-w-none` força a largura total mesmo >= 640px, onde o
 * `sm:max-w-lg` padrão do `Dialog` venceria por especificidade) com a foto
 * ampliada — fechar (clique fora, `Esc`, ou o botão "Fechar" com cor clara
 * explícita, próprio deste call site por causa do fundo escuro) só muda
 * estado local, nunca navega nem recarrega a página.
 */

interface Categoria {
  id: string;
  codigo: string;
  nome: string;
}

interface NomenclaturaTemplate {
  id: string;
  subtipo: string;
  template: string;
}

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar categorias/templates. Recarregue a página.';
const MENSAGEM_ERRO_CADASTRO =
  'Não foi possível cadastrar o produto agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_FOTO =
  'Não foi possível enviar a foto agora. Tente novamente em instantes.';
// MENSAGEM_ERRO_GALERIA (Story 3.6) é DISTINTA de MENSAGEM_ERRO_FOTO de
// propósito: só aparece quando o upload em si já teve sucesso (`201`) e
// SÓ a rebusca da galeria (GET /api/produtos/{id}/fotos ou uma das buscas
// de blob por foto) falhou depois — nunca implica que o arquivo não foi
// salvo, para não induzir o usuário a reenviar (duplicando o arquivo no
// servidor, que nunca sobrescreve).
const MENSAGEM_ERRO_GALERIA =
  'Foto enviada, mas não foi possível atualizar a galeria agora. Recarregue a página para vê-la.';

interface ProdutoCriado {
  id: string;
  nome: string;
  codigo: string;
}

// FotoGaleria é uma entrada resolvida da galeria (Story 3.6): `nome`/`url`
// vêm de GET /api/produtos/{id}/fotos; `objectUrl` é o Object URL montado a
// partir de GET /api/produtos/{id}/fotos/{nome} (cache local — nunca
// rebuscado enquanto o `nome` não mudar).
interface FotoGaleria {
  nome: string;
  url: string;
  objectUrl: string;
}

export function CadastroProdutoSection() {
  const [nome, setNome] = useState('');
  const [observacoes, setObservacoes] = useState('');
  const [categoriaId, setCategoriaId] = useState('');
  const [templateId, setTemplateId] = useState('');
  const [comprimento, setComprimento] = useState<DimensaoEstado>(DIMENSAO_VAZIA);
  const [largura, setLargura] = useState<DimensaoEstado>(DIMENSAO_VAZIA);
  const [diametro, setDiametro] = useState<DimensaoEstado>(DIMENSAO_VAZIA);
  const [altura, setAltura] = useState<DimensaoEstado>(DIMENSAO_VAZIA);
  const [espessura, setEspessura] = useState<DimensaoEstado>(DIMENSAO_VAZIA);

  // Código do Fornecedor, EAN-13, Unidade de Medida e Embalagem (Story 10.3,
  // spec-10-3, FR45/FR46): os dois primeiros e Embalagem são texto livre
  // opcional; Unidade de Medida é obrigatória (entra em `desabilitado`) —
  // nenhuma validação de formato de EAN-13 no cliente, o servidor é a única
  // fonte de verdade.
  const [codigoFornecedor, setCodigoFornecedor] = useState('');
  const [ean13, setEan13] = useState('');
  const [unidadeMedida, setUnidadeMedida] = useState('');
  const [embalagem, setEmbalagem] = useState('');

  const [categorias, setCategorias] = useState<Categoria[]>([]);
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
  // O campo Código só mostra o código do Produto recém-criado até o usuário
  // começar o próximo cadastro (senão exibiria o código do Produto anterior
  // ao lado de um formulário novo).
  const [codigoVisivel, setCodigoVisivel] = useState(false);
  const [fotoInputKey, setFotoInputKey] = useState(0);
  const [arquivoFoto, setArquivoFoto] = useState<File | null>(null);
  const [enviandoFoto, setEnviandoFoto] = useState(false);
  const [erroFoto, setErroFoto] = useState<string | null>(null);

  // Galeria (Story 3.6): `fotos` é a lista completa (já com Object URL
  // resolvido) devolvida pela última rebusca de GET /api/produtos/{id}/fotos.
  // `objectUrlCacheRef` guarda o Object URL por `nome` — mutável (não dispara
  // render sozinho) para que `carregarFotos` possa decidir SEM rebuscar uma
  // miniatura já resolvida, mesmo entre uploads sucessivos. `lightboxIndex`
  // é o índice em `fotos` cuja foto está ampliada no momento; `null` ==
  // lightbox fechado.
  const [fotos, setFotos] = useState<FotoGaleria[]>([]);
  const [lightboxIndex, setLightboxIndex] = useState<number | null>(null);
  const objectUrlCacheRef = useRef<Map<string, string>>(new Map());

  // Revoga todos os Object URLs em cache quando o componente desmonta —
  // evita vazar memória entre telas (as trocas de Produto/cadastro já
  // revogam individualmente em `enviar`, abaixo).
  useEffect(() => {
    const cache = objectUrlCacheRef.current;
    return () => {
      for (const url of cache.values()) {
        URL.revokeObjectURL(url);
      }
    };
  }, []);

  const carregarListas = useCallback(async () => {
    // Story 10.1: Template de nomenclatura passou a ser sempre obrigatório
    // no cadastro (AC2) — sem a lista, o Almoxarife não tem como selecionar
    // um, então uma falha aqui recebe o MESMO tratamento hoje dado a
    // categorias (mesmo `Promise.all`, mesmo `erroCarregar`), em vez
    // de degradar silenciosamente como na Story 3.2.
    try {
      const [resCategorias, resTemplates] = await Promise.all([
        fetch(apiUrl('/api/categorias'), { headers: authHeaders() }),
        fetch(apiUrl('/api/nomenclatura-templates'), { headers: authHeaders() }),
      ]);
      if (!resCategorias.ok || !resTemplates.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const bodyCategorias = (await resCategorias.json()) as { categorias: Categoria[] };
      const bodyTemplates = (await resTemplates.json()) as { templates: NomenclaturaTemplate[] };
      setCategorias(bodyCategorias.categorias ?? []);
      setTemplates(bodyTemplates.templates ?? []);
      setErroCarregar(null);
    } catch {
      setErroCarregar(MENSAGEM_ERRO_CARREGAR);
    }
  }, []);

  useEffect(() => {
    void (async () => {
      await carregarListas();
    })();
  }, [carregarListas]);

  function limparFormulario() {
    setNome('');
    setObservacoes('');
    setCategoriaId('');
    setTemplateId('');
    setComprimento(DIMENSAO_VAZIA);
    setLargura(DIMENSAO_VAZIA);
    setDiametro(DIMENSAO_VAZIA);
    setAltura(DIMENSAO_VAZIA);
    setEspessura(DIMENSAO_VAZIA);
    setCodigoFornecedor('');
    setEan13('');
    setUnidadeMedida('');
    setEmbalagem('');
  }

  const templateSelecionado = templates.find((t) => t.id === templateId);

  const desabilitado =
    enviando ||
    [...nome.trim()].length < 10 ||
    categoriaId === '' ||
    templateId === '' ||
    unidadeMedida === '';

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
      const res = await fetch(apiUrl('/api/produtos'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({
          nome,
          observacoes: observacoes.trim() === '' ? undefined : observacoes.trim(),
          categoria_id: categoriaId,
          template_id: templateId,
          comprimento: montarDimensao(comprimento),
          largura: montarDimensao(largura),
          diametro: montarDimensao(diametro),
          altura: montarDimensao(altura),
          espessura: montarDimensao(espessura),
          codigo_fornecedor: codigoFornecedor.trim() === '' ? undefined : codigoFornecedor.trim(),
          ean13: ean13.trim() === '' ? undefined : ean13.trim(),
          unidade_medida: unidadeMedida,
          embalagem: embalagem.trim() === '' ? undefined : embalagem.trim(),
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
      // recém-criado — qualquer upload/galeria de um cadastro anterior é
      // descartado junto (Story 3.6: revoga todo o cache de Object URLs,
      // não só o último).
      setProdutoCriado(body.produto);
      setCodigoVisivel(true);
      setArquivoFoto(null);
      setErroFoto(null);
      setFotoInputKey((k) => k + 1);
      setLightboxIndex(null);
      for (const url of objectUrlCacheRef.current.values()) {
        URL.revokeObjectURL(url);
      }
      objectUrlCacheRef.current.clear();
      setFotos([]);
    } catch {
      setErro(MENSAGEM_ERRO_CADASTRO);
    } finally {
      setEnviando(false);
    }
  }

  // Rebusca GET /api/produtos/{id}/fotos (Story 3.6) e resolve o Object URL
  // de cada foto ainda ausente do cache local (`objectUrlCacheRef`) — uma
  // miniatura já resolvida NUNCA é rebuscada, mesmo entre uploads
  // sucessivos. Atualiza `fotos` (a galeria completa) só se TODAS as buscas
  // (listagem + cada foto ainda não cacheada) tiverem sucesso; devolve
  // `false` sem alterar `fotos` caso qualquer uma falhe (rede/`!ok`) — o
  // chamador decide o que fazer com o erro.
  async function carregarFotos(produtoId: string): Promise<boolean> {
    try {
      const res = await fetch(apiUrl(`/api/produtos/${produtoId}/fotos`), { headers: authHeaders() });
      if (!res.ok) {
        return false;
      }
      const body = (await res.json()) as { fotos: { nome: string; url: string }[] };

      const itens: FotoGaleria[] = [];
      for (const foto of body.fotos) {
        let objectUrl = objectUrlCacheRef.current.get(foto.nome);
        if (!objectUrl) {
          const resFoto = await fetch(apiUrl(foto.url), { headers: authHeaders() });
          if (!resFoto.ok) {
            return false;
          }
          const blob = await resFoto.blob();
          objectUrl = URL.createObjectURL(blob);
          objectUrlCacheRef.current.set(foto.nome, objectUrl);
        }
        itens.push({ nome: foto.nome, url: foto.url, objectUrl });
      }

      setFotos(itens);
      return true;
    } catch {
      return false;
    }
  }

  // Envia `arquivoFoto` para POST /api/produtos/{id}/fotos (Story 3.5).
  // Sucesso (`201`): o arquivo JÁ está salvo no servidor a partir daqui —
  // `toast.success` e reset do input confirmam isso incondicionalmente.
  // Rebusca a galeria inteira via carregarFotos (Story 3.6) DEPOIS: se ela
  // falhar (rede, listagem, ou uma busca de blob), isso é um problema
  // separado — SÓ a galeria não atualizou, o upload não falhou — então usa
  // MENSAGEM_ERRO_GALERIA (nunca MENSAGEM_ERRO_FOTO) para não sugerir um
  // reenvio que duplicaria o arquivo no servidor.
  async function enviarFoto() {
    if (!produtoCriado || !arquivoFoto || enviandoFoto) {
      return;
    }
    setErroFoto(null);
    setEnviandoFoto(true);
    try {
      const formData = new FormData();
      formData.append('foto', arquivoFoto);
      const res = await fetch(apiUrl(`/api/produtos/${produtoCriado.id}/fotos`), {
        method: 'POST',
        headers: authHeaders(),
        body: formData,
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
        setErroFoto(body.error?.message ?? MENSAGEM_ERRO_FOTO);
        return;
      }

      toast.success('Foto enviada.');
      setArquivoFoto(null);
      setFotoInputKey((k) => k + 1);

      const sucesso = await carregarFotos(produtoCriado.id);
      if (!sucesso) {
        setErroFoto(MENSAGEM_ERRO_GALERIA);
      }
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
              onChange={(event) => {
                setNome(event.target.value);
                setCodigoVisivel(false);
              }}
              aria-describedby={templateSelecionado ? 'produto-nome-formato' : undefined}
            />
            {templateSelecionado && (
              <p id="produto-nome-formato" className="text-label text-muted-foreground">
                Formato: {templateSelecionado.template}
              </p>
            )}
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="produto-template">Template de nomenclatura</Label>
            <Select value={templateId} onValueChange={setTemplateId}>
              <SelectTrigger id="produto-template">
                <SelectValue placeholder="Selecione um template" />
              </SelectTrigger>
              <SelectContent>
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
              value={codigoVisivel ? (produtoCriado?.codigo ?? '') : ''}
              placeholder="Gerado automaticamente ao cadastrar"
              disabled
            />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="produto-codigo-fornecedor">Código do Fornecedor</Label>
            <Input
              id="produto-codigo-fornecedor"
              value={codigoFornecedor}
              onChange={(event) => setCodigoFornecedor(event.target.value)}
            />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="produto-ean13">EAN-13</Label>
            <Input
              id="produto-ean13"
              value={ean13}
              onChange={(event) => setEan13(event.target.value)}
            />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="produto-unidade-medida">Unidade de Medida</Label>
            <Select value={unidadeMedida} onValueChange={setUnidadeMedida}>
              <SelectTrigger id="produto-unidade-medida">
                <SelectValue placeholder="Selecione uma unidade de medida" />
              </SelectTrigger>
              <SelectContent>
                {UNIDADES_MEDIDA.map((unidade) => (
                  <SelectItem key={unidade} value={unidade}>
                    {unidade}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="produto-embalagem">Embalagem</Label>
            <Input
              id="produto-embalagem"
              value={embalagem}
              onChange={(event) => setEmbalagem(event.target.value)}
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
            <p role="status" className="text-body text-muted-foreground">
              Produto cadastrado sem saldo em estoque. Para dar entrada, use{' '}
              <Link to="/estoques" className="underline">
                Estoques
              </Link>{' '}
              → aba &quot;Lançar saldo&quot;.
            </p>
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

            {fotos.length > 0 && (
              <div className="grid grid-cols-3 gap-2 sm:grid-cols-4">
                {fotos.map((foto, index) => (
                  <button
                    key={foto.nome}
                    type="button"
                    onClick={() => setLightboxIndex(index)}
                    aria-label={`Ampliar foto ${index + 1} de ${fotos.length}`}
                    className="overflow-hidden rounded-md border border-border"
                  >
                    <img
                      src={foto.objectUrl}
                      alt=""
                      className="h-24 w-24 object-cover"
                    />
                  </button>
                ))}
              </div>
            )}
          </div>
        )}

        {/* Lightbox (Story 3.6): abre em tela cheia sobre a foto clicada na
            galeria. Fechar (clique fora, Esc, ou o botão "Fechar" abaixo) só
            muda `lightboxIndex` para `null` — nenhuma navegação, nenhum
            reload, a página permanece exatamente onde estava.

            `max-w-none` sozinho NÃO basta: o `DialogContent` padrão
            (components/ui/dialog.tsx) traz `sm:max-w-lg`, que — por viver
            dentro de uma media query — vence o `max-w-none` sem variante em
            qualquer viewport >= 640px (empate de especificidade decidido
            pela ordem de declaração no CSS gerado, não pela ordem das
            classes aqui). `sm:!max-w-none` força esse call site a ganhar via
            `!important`, sem tocar o padrão de `dialog.tsx` usado por outros
            diálogos claros.

            `showCloseButton={false}` desliga o botão "Fechar" padrão (cor
            herdada de `text-foreground`, quase invisível sobre
            `bg-black/95`) — o botão abaixo o substitui só aqui, com cor
            clara explícita, sem alterar o padrão usado pelos diálogos claros
            do resto do app. */}
        <Dialog
          open={lightboxIndex !== null}
          onOpenChange={(open) => {
            if (!open) {
              setLightboxIndex(null);
            }
          }}
        >
          <DialogContent
            showCloseButton={false}
            className="flex w-screen h-screen max-w-none sm:!max-w-none translate-x-0 translate-y-0 top-0 left-0 items-center justify-center border-none bg-black/95 p-0"
          >
            <DialogTitle className="sr-only">
              Foto ampliada de {produtoCriado?.nome}
            </DialogTitle>
            <DialogClose className="absolute top-4 right-4 rounded-xs text-white opacity-90 ring-offset-background transition-opacity hover:opacity-100 focus:opacity-100 focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:outline-hidden">
              <XIcon className="size-6" />
              <span className="sr-only">Fechar</span>
            </DialogClose>
            {lightboxIndex !== null && fotos[lightboxIndex] && (
              <img
                src={fotos[lightboxIndex].objectUrl}
                alt=""
                className="max-h-full max-w-full object-contain"
              />
            )}
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  );
}

export default CadastroProdutoSection;
