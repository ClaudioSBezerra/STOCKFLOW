import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { MfaEmpresaSection } from '@/components/seguranca/MfaEmpresaSection';

/**
 * Página "Segurança da empresa" (Story 17.2). Gate `adm`+ espelhado de `nav-items.ts`;
 * sem o papel, mostra a recusa e não monta a seção (nenhum fetch).
 */
export function SegurancaEmpresaPage() {
  const { usuario } = useAuth();
  const permitido = rankPapel(usuario?.papel ?? '') >= rankPapel('adm');

  return (
    <div className="flex flex-col gap-6 p-6">
      {permitido ? (
        <>
          <h1 className="text-heading-lg">Segurança da empresa</h1>
          <MfaEmpresaSection />
        </>
      ) : (
        <p className="text-body text-muted-foreground">Você não tem acesso a Segurança da empresa.</p>
      )}
    </div>
  );
}

export default SegurancaEmpresaPage;
