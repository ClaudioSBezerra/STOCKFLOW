import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

/**
 * Página servida quando a URL não traz Empresa nenhuma (`/`, `/login`, ...) —
 * Story 9.2 (spec-9-2). Substitui o login quebrado que a app da Empresa
 * montaria sem slug: só explica que cada empresa tem o próprio endereço de
 * acesso. De propósito, NÃO lista Empresas (nenhuma é descobrível por aqui)
 * nem aponta para a área da Plataforma.
 */
export function SemEmpresaPage() {
  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>Acesse pelo endereço da sua empresa</CardTitle>
          <CardDescription>Cada empresa entra no stockflow pelo próprio endereço de acesso.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3 text-body">
          <p>Use o endereço que o administrador da sua empresa enviou, no formato:</p>
          <p>
            <code className="break-all font-mono text-code">
              {window.location.origin}/e/nome-da-empresa
            </code>
          </p>
          <p className="text-muted-foreground">
            Não tem esse endereço? Peça ao administrador da sua empresa.
          </p>
        </CardContent>
      </Card>
    </div>
  );
}

export default SemEmpresaPage;
