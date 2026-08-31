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
 * Seção "Cadastrar Produto" da `CatalogoPage` (Story 3.1, spec-3-1), visível
 * só a `almoxarife`+ (gate na própria página). Um `Card` com formulário:
 * `nome`/`codigo`/`observacoes` (`Input`), `categoria`/`estoque` (`Select`,
 * carregados de `GET /api/categorias` e `GET /api/estoques` no mount) e as 5
 * dimensões pareadas (`Input` numérico + `Select` de unidade `mm/cm/m`),
 * todas opcionais — AD-9. `quantidade_inicial` é `Input` numérico obrigatório.
 *
 * Submit -> `POST /api/produtos`. Sucesso: `toast.success('Produto
 * cadastrado.')` (molde de `LocaisEstoqueSection`) e o formulário é limpo.
 * `400` -> `<p role="alert">` com a mensagem devolvida pelo servidor (nomeia
 * o campo específico quando aplicável, ex. dimensão incompleta); qualquer
 * outro erro (rede, 500) -> `<p role="alert">` genérico. Botão desabilitado
 * durante o envio ou com `nome`/`categoria_id`/`estoque_id`/
 * `quantidade_inicial` em branco.
 *
 * Categoria é sempre selecionada da lista fixa de `GET /api/categorias`
 * (AC4) — nunca um campo de texto livre.
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

interface DimensaoEstado {
  valor: string;
  unidade: string;
}

const DIMENSAO_VAZIA: DimensaoEstado = { valor: '', unidade: '' };
const UNIDADES = ['mm', 'cm', 'm'] as const;

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar categorias/estoques. Recarregue a página.';
const MENSAGEM_ERRO_CADASTRO =
  'Não foi possível cadastrar o produto agora. Tente novamente em instantes.';

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
  const [quantidadeInicial, setQuantidadeInicial] = useState('');
  const [comprimento, setComprimento] = useState<DimensaoEstado>(DIMENSAO_VAZIA);
  const [largura, setLargura] = useState<DimensaoEstado>(DIMENSAO_VAZIA);
  const [diametro, setDiametro] = useState<DimensaoEstado>(DIMENSAO_VAZIA);
  const [altura, setAltura] = useState<DimensaoEstado>(DIMENSAO_VAZIA);
  const [espessura, setEspessura] = useState<DimensaoEstado>(DIMENSAO_VAZIA);

  const [categorias, setCategorias] = useState<Categoria[]>([]);
  const [estoques, setEstoques] = useState<Estoque[]>([]);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);

  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);

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
    setQuantidadeInicial('');
    setComprimento(DIMENSAO_VAZIA);
    setLargura(DIMENSAO_VAZIA);
    setDiametro(DIMENSAO_VAZIA);
    setAltura(DIMENSAO_VAZIA);
    setEspessura(DIMENSAO_VAZIA);
  }

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
      toast.success('Produto cadastrado.');
      limparFormulario();
    } catch {
      setErro(MENSAGEM_ERRO_CADASTRO);
    } finally {
      setEnviando(false);
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
            <Input id="produto-nome" value={nome} onChange={(event) => setNome(event.target.value)} />
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
      </CardContent>
    </Card>
  );
}

export default CadastroProdutoSection;
