/**
 * Tela cheia dedicada ao trial vencido (Story 18.2, Epic 18). `RotaProtegida`
 * (App.tsx) troca o app inteiro por esta tela assim que o interceptor global
 * de `fetch` (lib/httpTrial.ts) detecta `TRIAL_EXPIRADO` em qualquer
 * resposta — nunca o toast genérico. Texto fixo, sem fluxo real de
 * assinatura: isso fica para a Story 18.3/Epic futuro de cobrança.
 */
export function TrialExpiradoPage() {
  return (
    <output className="flex min-h-svh flex-col items-center justify-center gap-2 px-4 text-center">
      <h1 className="text-xl font-semibold">Seu período de teste acabou</h1>
      <p className="text-muted-foreground">Veja como assinar para continuar usando o stockflow.</p>
    </output>
  );
}
