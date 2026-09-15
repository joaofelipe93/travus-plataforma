"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";
import { situacaoDrive } from "@/lib/execucoes";
import type { StatusDrive } from "@/lib/tipos";

type LanceComComprovante = {
  id: number;
  pdf_id: string | null;
  drive_status: StatusDrive;
  drive_link: string | null;
  drive_erro: string | null;
};

/** PDF do comprovante e situação do envio ao Google Drive de um lance. */
export function Comprovante({ lance, editavel }: { lance: LanceComComprovante; editavel: boolean }) {
  const queryClient = useQueryClient();
  const reenviar = useMutation({
    mutationFn: () => api(`/lances/${lance.id}/reenviar-drive`, { metodo: "POST" }),
    onSuccess: () => {
      toast("Envio ao Drive agendado");
      queryClient.invalidateQueries({ queryKey: ["execucao"] });
      queryClient.invalidateQueries({ queryKey: ["cliente"] });
    },
    onError: (e) => toast.error(e.message),
  });

  return (
    <div className="flex flex-col items-start gap-1">
      {lance.pdf_id ? (
        <a className="text-sm underline" href={`/api/arquivos/${lance.pdf_id}`} target="_blank" rel="noopener noreferrer">
          Abrir PDF
        </a>
      ) : (
        <span className="text-sm text-muted-foreground">Sem PDF</span>
      )}
      {lance.drive_status === "enviado" && lance.drive_link ? (
        <a href={lance.drive_link} target="_blank" rel="noopener noreferrer">
          <Badge variant={situacaoDrive.enviado.variante}>{situacaoDrive.enviado.rotulo}</Badge>
        </a>
      ) : (
        lance.pdf_id && (
          <Badge variant={situacaoDrive[lance.drive_status].variante} title={lance.drive_erro ?? undefined}>
            {situacaoDrive[lance.drive_status].rotulo}
          </Badge>
        )
      )}
      {(lance.drive_status === "erro" || lance.drive_status === "nao_enviar") && lance.pdf_id && editavel && (
        <Button variant="link" size="xs" className="px-0" onClick={() => reenviar.mutate()} disabled={reenviar.isPending}>
          {lance.drive_status === "erro" ? "Tentar de novo" : "Enviar ao Drive"}
        </Button>
      )}
    </div>
  );
}
