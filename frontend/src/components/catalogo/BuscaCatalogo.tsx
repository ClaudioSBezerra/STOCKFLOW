import { useEffect, useRef, useState, type ChangeEvent } from 'react';
import { Search } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { getAccessToken } from '@/lib/session';

/**
 * Campo de busca do Catálogo (Story 4.1, spec-4-1, FR-4) — sempre visível no
 * topo de `CatalogoPage`, para qualquer papel (`usuario`+): não depende do
 * gate `podeCadastrar` da página, ao contrário de `CadastroProdutoSection`/
 * `ImportacaoProdutosSection`.
 *
 * Cada digitação dispara `GET /api/produtos/busca?q=<termo>` só depois de um
 * debounce de 300ms (decisão desta spec). `termoAtualRef` guarda o termo
 * (já trimado) mais recentemente digitado; quando uma resposta chega, ela só
 * é aplicada se `termoAtualRef.current` ainda for o mesmo termo que a
 * originou — uma resposta de uma busca obsoleta (usuário já digitou outro
 * termo antes dela voltar) é descartada, nunca sobrescrevendo a lista com um
 * resultado que não corresponde mais ao termo atual (mesma classe de corrida
 * endereçada na Story 3.5 para upload de foto).
 *
 * Termo vazio (ou só espaços): nenhuma requisição é disparada, nenhuma
 * lista/mensagem aparece — só o campo em si.
 *
 * Sucesso com 1+ Produtos: lista simples abaixo do campo (nome, código em
 * `JetBrains Mono` quando presente, nome da categoria) — sem badge de
 * disponibilidade (Story 4.3/4.4) e sem link/clique (o detalhe do Produto é
 * a Story 4.4, ainda não existe — Design Notes da spec).
 *
 * Sucesso sem nenhum Produto (debounce já resolvido): mensagem exata
 * "Nenhum produto encontrado para '{busca}'." com o termo efetivamente
 * buscado (já trimado).
 *
 * Atalho de teclado `/` (desktop, UX epic-level) foca o campo quando o foco
 * não está em outro campo editável (`input`/`textarea`/`[contenteditable]`)
 * e nenhum modificador (`ctrl`/`meta`/`alt`) está pressionado.
 */

interface CategoriaBusca {
  id: string;
  codigo: string;
  nome: string;
}

interface ProdutoBusca {
  id: string;
  nome: string;
  codigo: string | null;
  categoria: CategoriaBusca;
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const DEBOUNCE_MS = 300;

const MENSAGEM_ERRO_BUSCA = 'Não foi possível buscar agora. Tente novamente em instantes.';

function elementoEhEditavel(elemento: Element | null): boolean {
  if (!elemento) return false;
  const tag = elemento.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA') return true;
  return elemento instanceof HTMLElement && elemento.isContentEditable;
}

export function BuscaCatalogo() {
  const [termo, setTermo] = useState('');
  const [resultados, setResultados] = useState<ProdutoBusca[] | null>(null);
  const [termoBuscado, setTermoBuscado] = useState<string | null>(null);
  const [erro, setErro] = useState(false);

  const inputRef = useRef<HTMLInputElement>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // termoAtualRef guarda o termo trimado mais recente digitado — a guarda de
  // corrida contra respostas obsoletas descrita acima.
  const termoAtualRef = useRef('');
  // montadoRef guarda se o componente ainda está montado — termoAtualRef só
  // descarta uma resposta de um termo DIFERENTE do atual; uma resposta
  // tardia do MESMO termo, chegando depois do componente já ter saído da
  // árvore (ex. troca de rota), passaria pelo check de termoAtualRef e
  // chamaria setState num componente desmontado sem esta guarda extra.
  const montadoRef = useRef(true);

  // Atalho `/`: foca o campo de busca (desktop). Nunca ativo dentro de outro
  // campo editável nem com modificador pressionado.
  useEffect(() => {
    function aoTeclar(evento: KeyboardEvent) {
      if (evento.key !== '/') return;
      if (evento.ctrlKey || evento.metaKey || evento.altKey) return;
      if (elementoEhEditavel(document.activeElement)) return;
      evento.preventDefault();
      inputRef.current?.focus();
    }
    document.addEventListener('keydown', aoTeclar);
    return () => document.removeEventListener('keydown', aoTeclar);
  }, []);

  // Debounce/disparo da busca acontece no PRÓPRIO handler de digitação (não
  // num `useEffect` reagindo a `termo`) — atualizar estado síncrono só em
  // reação a um evento de digitação, nunca como sincronização derivada de
  // render, evita o cascading render que um `useEffect` fazendo o mesmo
  // `setState` produziria.
  function aoDigitar(evento: ChangeEvent<HTMLInputElement>) {
    const valor = evento.target.value;
    setTermo(valor);

    const termoTrimado = valor.trim();
    termoAtualRef.current = termoTrimado;

    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }

    if (termoTrimado === '') {
      setResultados(null);
      setTermoBuscado(null);
      setErro(false);
      return;
    }

    debounceRef.current = setTimeout(() => {
      setErro(false);
      fetch(`/api/produtos/busca?q=${encodeURIComponent(termoTrimado)}`, {
        headers: authHeaders(),
      })
        .then(async (res) => {
          if (!montadoRef.current) return; // componente já desmontado — descartada
          if (termoAtualRef.current !== termoTrimado) return; // resposta obsoleta — descartada
          if (!res.ok) {
            setErro(true);
            setResultados(null);
            setTermoBuscado(termoTrimado);
            return;
          }
          const data = (await res.json()) as { produtos?: ProdutoBusca[] };
          if (!montadoRef.current) return; // desmontou durante o await de res.json()
          if (termoAtualRef.current !== termoTrimado) return; // termo mudou durante o await de res.json() — resposta obsoleta
          setResultados(Array.isArray(data.produtos) ? data.produtos : []);
          setTermoBuscado(termoTrimado);
        })
        .catch(() => {
          if (!montadoRef.current) return; // componente já desmontado — descartada
          if (termoAtualRef.current !== termoTrimado) return; // resposta obsoleta — descartada
          setErro(true);
          setResultados(null);
          setTermoBuscado(termoTrimado);
        });
    }, DEBOUNCE_MS);
  }

  // Cancela um debounce pendente se o componente desmontar antes dele
  // disparar — a única responsabilidade que sobra para um efeito aqui
  // (sincronização com o timer, um sistema externo ao React).
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, []);

  // Marca o componente como desmontado — guarda contra setState num fetch
  // que ainda está em voo quando o componente sai da árvore (ver montadoRef
  // acima).
  useEffect(() => {
    montadoRef.current = true;
    return () => {
      montadoRef.current = false;
    };
  }, []);

  const semResultado = !erro && termoBuscado !== null && resultados !== null && resultados.length === 0;
  const comResultados = !erro && resultados !== null && resultados.length > 0;

  return (
    <div className="flex flex-col gap-3">
      <div className="relative">
        <Search
          aria-hidden="true"
          className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground"
        />
        <Input
          ref={inputRef}
          type="search"
          value={termo}
          onChange={aoDigitar}
          placeholder="Buscar por nome, código ou categoria..."
          aria-label="Buscar produtos"
          className="min-h-touch-target-min pl-9"
        />
      </div>

      {erro && (
        <p role="alert" className="text-body text-destructive">
          {MENSAGEM_ERRO_BUSCA}
        </p>
      )}

      {semResultado && (
        <p className="text-body text-muted-foreground">
          Nenhum produto encontrado para '{termoBuscado}'.
        </p>
      )}

      {comResultados && (
        <ul className="flex flex-col gap-2">
          {resultados?.map((produto) => (
            <li
              key={produto.id}
              className="min-h-touch-target-min flex flex-col justify-center gap-1 rounded-md border border-border p-3"
            >
              <span className="text-body">{produto.nome}</span>
              <span className="text-label text-muted-foreground">
                {produto.codigo && <span className="font-mono">{produto.codigo}</span>}
                {produto.codigo && ' — '}
                {produto.categoria.nome}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export default BuscaCatalogo;
