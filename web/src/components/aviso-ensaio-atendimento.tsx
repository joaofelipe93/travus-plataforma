"use client";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import type { ResumoAtendimento } from "@/lib/tipos";

/**
 * O aviso mais importante do Atendimento: algum bot está mesmo falando com gente, ou tudo é
 * ensaio? Aparece no painel, em Configurações e em Conexões, sempre com o mesmo texto — "ensaio"
 * não pode significar coisas diferentes em telas diferentes (é a mesma ideia do AvisoEnsaio dos
 * boletos, mas o estado do Atendimento tem três condições, não duas).
 *
 * A ordem dos casos é a ordem em que as coisas travam: sem chave da Anthropic nenhum bot pensa;
 * com a chave e sem a do servidor, ele pensa e nada sai; com as duas, quem decide é cada bot.
 */
export function AvisoEnsaioAtendimento({ resumo }: { resumo: ResumoAtendimento }) {
  if (!resumo.modelo_disponivel) {
    return (
      <Alert variant="destructive">
        <AlertTitle>Nenhum bot responde: falta a chave da Anthropic</AlertTitle>
        <AlertDescription>
          Sem <code>ANTHROPIC_API_KEY</code> no servidor, o bot não tem com o que pensar. Dá para
          cadastrar bots e montar o fluxo agora; eles só começam a responder quando a chave existir.
        </AlertDescription>
      </Alert>
    );
  }
  if (!resumo.envio_habilitado_servico) {
    return (
      <Alert>
        <AlertTitle>Modo ensaio: nenhuma mensagem é enviada</AlertTitle>
        <AlertDescription>
          A chave <code>ATENDIMENTO_ENVIO_HABILITADO</code> do servidor está desligada. Os bots
          compõem a resposta e ela fica <strong>guardada na conversa</strong> com o motivo: abra uma
          conversa para ler exatamente o que sairia.
        </AlertDescription>
      </Alert>
    );
  }
  if (resumo.bots.com_envio === 0) {
    return (
      <Alert>
        <AlertTitle>Ensaio: nenhum bot com envio ligado</AlertTitle>
        <AlertDescription>
          O servidor libera o envio, mas nenhum bot está autorizado a mandar mensagem. Cada bot tem
          o próprio interruptor, e só um administrador o liga.
        </AlertDescription>
      </Alert>
    );
  }
  return (
    <Alert>
      <AlertTitle>
        {resumo.bots.com_envio === 1 ? "Um bot está respondendo" : `${resumo.bots.com_envio} bots estão respondendo`}
      </AlertTitle>
      <AlertDescription>
        As respostas saem no WhatsApp de quem escrever para esses bots. O texto é escrito pelo
        modelo na hora — não há revisão humana antes do envio.
      </AlertDescription>
    </Alert>
  );
}
