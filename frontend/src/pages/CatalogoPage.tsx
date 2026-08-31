import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { BuscaCatalogo } from '@/components/catalogo/BuscaCatalogo';
import { CadastroProdutoSection } from '@/components/produtos/CadastroProdutoSection';
import { ImportacaoProdutosSection } from '@/components/produtos/ImportacaoProdutosSection';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';

/**
 * Página "Catálogo" (`/`, item de nav "Catálogo", Story 3.1, spec-3-1) —
 * substitui a `PlaceholderPage` da rota índice. Renderizada dentro do
 * `AppShell`/`RotaProtegida`.
 *
 * `BuscaCatalogo` (Story 4.1, spec-4-1) fica sempre no topo, para qualquer
 * papel — não depende do gate `podeCadastrar` abaixo. Visualização em
 * grade/tabela (Story 4.3) ainda não existe: o texto residual abaixo da
 * busca cita só o que falta.
 *
 * Quando `rankPapel(papel) >= rankPapel('almoxarife')`, mostra também
 * `Tabs` ("Cadastro"/"Importação", Story 3.3, spec-3-3) envolvendo
 * `CadastroProdutoSection`/`ImportacaoProdutosSection` — resolve o que antes
 * era uma simplificação deliberada (empilhamento simples, sem abas), agora
 * que a Story 3.3 entrega o segundo fluxo que faz as abas valerem a pena.
 *
 * Gate de papel espelhado do `nav-items.ts`/`EstoquesPage`: o servidor
 * continua sendo a autoridade real — `POST /api/produtos`/`POST
 * /api/importacoes` respondem 403 para papéis abaixo de `almoxarife` mesmo em
 * chamada direta à API; este espelho é só de experiência.
 */
export function CatalogoPage() {
  const { usuario } = useAuth();
  const podeCadastrar = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  return (
    <div className="flex flex-col gap-6 p-6">
      <BuscaCatalogo />
      <p className="text-body text-muted-foreground">
        Visualização em grade e tabela chega em breve.
      </p>
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
    </div>
  );
}

export default CatalogoPage;
