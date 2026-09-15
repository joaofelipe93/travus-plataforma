"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { Toaster } from "@/components/ui/sonner";
import { ErroApi } from "@/lib/api";

export function Providers({ children }: { children: React.ReactNode }) {
  const [cliente] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            refetchOnWindowFocus: false,
            // Erros 4xx (sessão, permissão, validação) não adiantam repetir.
            retry: (tentativas, erro) => !(erro instanceof ErroApi && erro.status >= 400 && erro.status < 500) && tentativas < 2,
          },
        },
      }),
  );
  return (
    <QueryClientProvider client={cliente}>
      {children}
      {/* Embaixo: no alto ficaria por cima do menu do usuário. */}
      <Toaster richColors position="bottom-right" />
    </QueryClientProvider>
  );
}
