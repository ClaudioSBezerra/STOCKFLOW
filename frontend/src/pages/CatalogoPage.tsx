import { useCallback, useEffect, useRef, useState } from 'react';
import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { BuscaCatalogo } from '@/components/catalogo/BuscaCatalogo';
import { CatalogoListagem } from '@/components/catalogo/CatalogoListagem';
import { ScannerProdutoFab } from '@/components/catalogo/ScannerProdutoFab';
import { CadastroProdutoSection } from '@/components/produtos/CadastroProdutoSection';
import { ImportacaoProdutosSection } from '@/components/produtos/ImportacaoProdutosSection';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

// DEBOUNCE_MS_TERMO_FILTRO é o mesmo debounce (300ms) de BuscaCatalogo — a
// listagem filtrada e as sugestões inline reagem ao MESMO termo digitado,
// cada uma no seu próprio debounce independente (Always, spec-4-2).
const DEBOUNCE_MS_TERMO_FILTRO = 300;

/**
 * Página "Catálogo" (`/`, item de nav "Catálogo", Story 3.1, spec-3-1) —
 * substitui a `PlaceholderPage` da rota índice. Renderizada dentro do
 * `AppShell`/`RotaProtegida`.
 *
 * `BuscaCatalogo` (Story 4.1, spec-4-1) fica sempre no topo, para qualquer
 * papel — não depende do gate `podeCadastrar` abaixo. Logo abaixo,
 * `CatalogoListagem` (Story 4.3, spec-4-3; filtros da Story 4.2, spec-4-2)
 * mostra o catálogo paginado em grade (cards) ou tabela agrupada (alternador
 * em viewport ≥768px), também para qualquer papel.
 *
 * Ponte de busca->filtro (Story 4.2, spec-4-2): `BuscaCatalogo` reporta o
 * valor BRUTO digitado a cada tecla via `onTermoChange`; esta página debounça
 * 300ms (mesmo valor de `BuscaCatalogo`, `useRef` com cleanup no unmount) e
 * repassa o termo TRIMADO como prop `termo` para `CatalogoListagem`. As duas
 * requisições (sugestões de `BuscaCatalogo`, listagem filtrada de
 * `CatalogoListagem`) coexistem, cada uma no seu próprio debounce — nenhuma
 * mudança no comportamento próprio de `BuscaCatalogo` (suas sugestões
 * inline continuam exatamente como na Story 4.1).
 *
 * Quando `rankPapel(papel) >= rankPapel('almoxarife')`, mostra também
 * `Tabs` ("Cadastro"/"Importação", Story 3.3, spec-3-3) envolvendo
 * `CadastroProdutoSection`/`ImportacaoProdutosSection` — resolve o que antes
 * era uma simplificação deliberada (empilhamento simples, sem abas), agora
 * que a Story 3.3 entrega o segundo fluxo que faz as abas valerem a pena.
 *
 * `ScannerProdutoFab` (Story 4.5, spec-4-5, FR-35) é montado ao final do
 * container: o `fab-scanner` (botão flutuante) só existe onde este
 * componente é montado — hoje, só o Catálogo — satisfazendo "nunca em telas
 * administrativas" sem lógica de rota. `aoFalharLeitura` devolve o foco ao
 * campo de `BuscaCatalogo` (via `buscaInputRef`) sempre que a leitura de QR
 * Code / código de barras falha (HTTPS ausente, permissão, hardware, código
 * não reconhecido) — o scanner nunca é a única forma de achar um Produto.
 *
 * Gate de papel espelhado do `nav-items.ts`/`EstoquesPage`: o servidor
 * continua sendo a autoridade real — `POST /api/produtos`/`POST
 * /api/importacoes` respondem 403 para papéis abaixo de `almoxarife` mesmo em
 * chamada direta à API; este espelho é só de experiência.
 */
export function CatalogoPage() {
  const { usuario } = useAuth();
  const podeCadastrar = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  const [termoFiltro, setTermoFiltro] = useState('');
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const buscaInputRef = useRef<HTMLInputElement>(null);

  const devolverFocoABusca = useCallback(() => {
    buscaInputRef.current?.focus();
  }, []);

  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, []);

  function aoTermoDigitado(valor: string) {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
    }
    debounceRef.current = setTimeout(() => {
      setTermoFiltro(valor.trim());
    }, DEBOUNCE_MS_TERMO_FILTRO);
  }

  return (
    <div className="flex flex-col gap-6 p-6">
      <BuscaCatalogo onTermoChange={aoTermoDigitado} inputRef={buscaInputRef} />
      <CatalogoListagem termo={termoFiltro} />
      {podeCadastrar && (
        <Tabs defaultValue="cadastro">
          <TabsList>
            <TabsTrigger value="cadastro">Cadastro</TabsTrigger>
            <TabsTrigger value="importacao">Importação</TabsTrigger>
          </TabsList>
          <TabsContent value="cadastro">
            <CadastroProdutoSection />
          </TabsContent>
          <TabsContent value="importacao">
            <ImportacaoProdutosSection />
          </TabsContent>
        </Tabs>
      )}
      <ScannerProdutoFab aoFalharLeitura={devolverFocoABusca} />
    </div>
  );
}

export default CatalogoPage;
