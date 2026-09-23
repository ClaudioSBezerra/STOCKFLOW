import { useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog';
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
import {
  UNIDADES_MEDIDA,
  dimensaoParaEstado,
  montarDimensao,
  type DimensaoEstado,
} from '@/lib/produtos';
import { DimensaoField } from './DimensaoField';

/**
 * Diálogo "Editar produto" (spec-13-1): formulário pré-preenchido a partir do
 * detalhe do Produto, `PUT /api/produtos/{id}`. Código é somente-leitura;
 * saldo, reservas e fotos não são editáveis aqui. Nenhuma validação de
 * formato roda no cliente — o servidor é a fonte de verdade e o erro dele
 * aparece em `role="alert"`.
 */

export interface ProdutoEditavel {
  id: string;
  nome: string;
  codigo: string | null;
  categoria: { id: string };
  dimensoes: {
    comprimento: { valor: number; unidade: string } | null;
    largura: { valor: number; unidade: string } | null;
    diametro: { valor: number; unidade: string } | null;
    altura: { valor: number; unidade: string } | null;
    espessura: { valor: number; unidade: string } | null;
  };
  unidadeMedida?: string | null;
  embalagem?: string | null;
  codigoFornecedor?: string | null;
  ean13?: string | null;
  templateId?: string | null;
  observacoes?: string | null;
}

interface Categoria {
  id: string;
  codigo: string;
  nome: string;
}

interface Template {
  id: string;
  subtipo: string;
  template: string;
}

const MENSAGEM_ERRO_CARREGAR = 'Não foi possível carregar categorias/templates. Feche e tente novamente.';
const MENSAGEM_ERRO_EDICAO = 'Não foi possível salvar o produto agora. Tente novamente em instantes.';

interface Props {
  produto: ProdutoEditavel;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSalvo: () => void | Promise<void>;
}

export function EditarProdutoDialog({ produto, open, onOpenChange, onSalvo }: Props) {
  const [nome, setNome] = useState('');
  const [observacoes, setObservacoes] = useState('');
  const [categoriaId, setCategoriaId] = useState('');
  const [templateId, setTemplateId] = useState('');
  const [comprimento, setComprimento] = useState<DimensaoEstado>(dimensaoParaEstado(null));
  const [largura, setLargura] = useState<DimensaoEstado>(dimensaoParaEstado(null));
  const [diametro, setDiametro] = useState<DimensaoEstado>(dimensaoParaEstado(null));
  const [altura, setAltura] = useState<DimensaoEstado>(dimensaoParaEstado(null));
  const [espessura, setEspessura] = useState<DimensaoEstado>(dimensaoParaEstado(null));
  const [codigoFornecedor, setCodigoFornecedor] = useState('');
  const [ean13, setEan13] = useState('');
  const [unidadeMedida, setUnidadeMedida] = useState('');
  const [embalagem, setEmbalagem] = useState('');

  const [categorias, setCategorias] = useState<Categoria[]>([]);
  const [templates, setTemplates] = useState<Template[]>([]);
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);

  // Pré-preenche a cada abertura e (re)carrega as listas.
  useEffect(() => {
    if (!open) return;
    setNome(produto.nome);
    setObservacoes(produto.observacoes ?? '');
    setCategoriaId(produto.categoria.id);
    setTemplateId(produto.templateId ?? '');
    setComprimento(dimensaoParaEstado(produto.dimensoes.comprimento));
    setLargura(dimensaoParaEstado(produto.dimensoes.largura));
    setDiametro(dimensaoParaEstado(produto.dimensoes.diametro));
    setAltura(dimensaoParaEstado(produto.dimensoes.altura));
    setEspessura(dimensaoParaEstado(produto.dimensoes.espessura));
    setCodigoFornecedor(produto.codigoFornecedor ?? '');
    setEan13(produto.ean13 ?? '');
    setUnidadeMedida(produto.unidadeMedida ?? '');
    setEmbalagem(produto.embalagem ?? '');
    setErro(null);

    let cancelado = false;
    void (async () => {
      try {
        const [resCategorias, resTemplates] = await Promise.all([
          fetch(apiUrl('/api/categorias'), { headers: authHeaders() }),
          fetch(apiUrl('/api/nomenclatura-templates'), { headers: authHeaders() }),
        ]);
        if (!resCategorias.ok || !resTemplates.ok) {
          if (!cancelado) setErro(MENSAGEM_ERRO_CARREGAR);
          return;
        }
        const bc = (await resCategorias.json()) as { categorias: Categoria[] };
        const bt = (await resTemplates.json()) as { templates: Template[] };
        if (!cancelado) {
          setCategorias(bc.categorias ?? []);
          setTemplates(bt.templates ?? []);
        }
      } catch {
        if (!cancelado) setErro(MENSAGEM_ERRO_CARREGAR);
      }
    })();
    return () => {
      cancelado = true;
    };
    // Só reinicializa ao abrir; o detalhe pode recarregar (SSE) com o diálogo aberto.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const templateSelecionado = templates.find((t) => t.id === templateId);
  const desabilitado = enviando || [...nome.trim()].length < 10 || categoriaId === '';

  async function enviar(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (desabilitado) return;
    setErro(null);
    setEnviando(true);
    try {
      const res = await fetch(apiUrl(`/api/produtos/${produto.id}`), {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({
          nome,
          observacoes: observacoes.trim() === '' ? undefined : observacoes.trim(),
          categoria_id: categoriaId,
          template_id: templateId === '' ? undefined : templateId,
          comprimento: montarDimensao(comprimento),
          largura: montarDimensao(largura),
          diametro: montarDimensao(diametro),
          altura: montarDimensao(altura),
          espessura: montarDimensao(espessura),
          codigo_fornecedor: codigoFornecedor.trim() === '' ? undefined : codigoFornecedor.trim(),
          ean13: ean13.trim() === '' ? undefined : ean13.trim(),
          unidade_medida: unidadeMedida === '' ? undefined : unidadeMedida,
          embalagem: embalagem.trim() === '' ? undefined : embalagem.trim(),
        }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
        setErro(body.error?.message ?? MENSAGEM_ERRO_EDICAO);
        return;
      }
      toast.success('Produto atualizado.');
      onOpenChange(false);
      await onSalvo();
    } catch {
      setErro(MENSAGEM_ERRO_EDICAO);
    } finally {
      setEnviando(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(aberto) => {
        if (!aberto && enviando) return;
        onOpenChange(aberto);
      }}
    >
      <DialogContent className="max-h-[90vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Editar produto</DialogTitle>
        </DialogHeader>
        <form onSubmit={enviar} className="flex flex-col gap-4" noValidate>
          <div className="flex flex-col gap-2">
            <Label htmlFor="editar-codigo">Código</Label>
            <Input id="editar-codigo" value={produto.codigo ?? ''} disabled readOnly />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="editar-nome">Nome</Label>
            <Input
              id="editar-nome"
              value={nome}
              onChange={(event) => setNome(event.target.value)}
              aria-describedby={templateSelecionado ? 'editar-nome-formato' : undefined}
            />
            {templateSelecionado && (
              <p id="editar-nome-formato" className="text-label text-muted-foreground">
                Formato: {templateSelecionado.template}
              </p>
            )}
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="editar-template">Template de nomenclatura</Label>
            <Select value={templateId} onValueChange={setTemplateId}>
              <SelectTrigger id="editar-template">
                <SelectValue placeholder="Sem template" />
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
            <Label htmlFor="editar-categoria">Categoria</Label>
            <Select value={categoriaId} onValueChange={setCategoriaId}>
              <SelectTrigger id="editar-categoria">
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
            <Label htmlFor="editar-codigo-fornecedor">Código do Fornecedor</Label>
            <Input
              id="editar-codigo-fornecedor"
              value={codigoFornecedor}
              onChange={(event) => setCodigoFornecedor(event.target.value)}
            />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="editar-ean13">EAN-13</Label>
            <Input id="editar-ean13" value={ean13} onChange={(event) => setEan13(event.target.value)} />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="editar-unidade-medida">Unidade de Medida</Label>
            <Select value={unidadeMedida} onValueChange={setUnidadeMedida}>
              <SelectTrigger id="editar-unidade-medida">
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
            <Label htmlFor="editar-embalagem">Embalagem</Label>
            <Input
              id="editar-embalagem"
              value={embalagem}
              onChange={(event) => setEmbalagem(event.target.value)}
            />
          </div>

          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <DimensaoField label="Comprimento" idPrefix="editar-comprimento" dimensao={comprimento} onChange={setComprimento} />
            <DimensaoField label="Largura" idPrefix="editar-largura" dimensao={largura} onChange={setLargura} />
            <DimensaoField label="Diâmetro" idPrefix="editar-diametro" dimensao={diametro} onChange={setDiametro} />
            <DimensaoField label="Altura" idPrefix="editar-altura" dimensao={altura} onChange={setAltura} />
            <DimensaoField label="Espessura" idPrefix="editar-espessura" dimensao={espessura} onChange={setEspessura} />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="editar-observacoes">Observações</Label>
            <Input
              id="editar-observacoes"
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
            {enviando ? 'Salvando...' : 'Salvar'}
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  );
}
