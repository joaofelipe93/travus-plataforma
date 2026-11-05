"use client";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import type { RespostaConfiguracaoBoletos } from "@/lib/tipos";

/**
 * O aviso mais importante do serviço de boletos: se alguma mensagem vai sair para o telefone de
 * um cliente, e por quê. Aparece no painel, na lista de rodadas e ao lado do interruptor em
 * Configurações — sempre com o mesmo texto, para ninguém achar que "ensaio" significa coisas
 * diferentes em telas diferentes.
 */
export function AvisoEnsaio({ dados }: { dados: RespostaConfiguracaoBoletos }) {
  const { envio_habilitado_servidor: servidor, configuracao } = dados;
  if (servidor && configuracao.envio_habilitado) {
    return (
      <Alert>
        <AlertTitle>O envio está ligado</AlertTitle>
        <AlertDescription>
          As rodadas mandam o boleto no WhatsApp dos clientes, uma mensagem a cada{" "}
          {configuracao.pausa_envio_segundos}s, no máximo {configuracao.max_mensagens_rodada} por rodada.
          {configuracao.envio_habilitado_por_nome && ` Ligado por ${configuracao.envio_habilitado_por_nome}.`}
        </AlertDescription>
      </Alert>
    );
  }
  return (
    <Alert>
      <AlertTitle>Modo ensaio: nenhuma mensagem é enviada</AlertTitle>
      <AlertDescription>
        {!servidor
          ? "A chave BOLETOS_ENVIO_HABILITADO do servidor está desligada."
          : "O envio está desligado na configuração."}{" "}
        As rodadas continuam buscando os boletos e <strong>montam</strong> as mensagens: abra uma rodada
        para ler exatamente o que seria enviado a cada cliente.
      </AlertDescription>
    </Alert>
  );
}
