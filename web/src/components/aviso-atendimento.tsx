"use client";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import type { ResumoAtendimento } from "@/lib/tipos";

/**
 * O estado do serviço numa frase: tem bot respondendo, ou não?
 *
 * Era um aviso de três casos, quando havia a chave do servidor e o interruptor de envio por bot.
 * Em 29/10/2026 o usuário pediu **um botão só** ("não quero tantas travas"), e sobrou o que
 * importa: sem a chave da Anthropic nenhum bot responde; com ela, quem decide é o botão de cada
 * bot. Aparece no painel, em Configurações e em Conexões, sempre com o mesmo texto.
 */
export function AvisoAtendimento({ resumo }: { resumo: ResumoAtendimento }) {
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
  if (resumo.bots.ativos === 0) {
    return (
      <Alert>
        <AlertTitle>Nenhum bot ligado</AlertTitle>
        <AlertDescription>
          {resumo.bots.total === 0
            ? "Nenhum bot cadastrado ainda."
            : "Os bots cadastrados estão desligados: ninguém recebe resposta."}{" "}
          Um bot só conversa depois de ligado e com o número pareado.
        </AlertDescription>
      </Alert>
    );
  }
  return (
    <Alert>
      <AlertTitle>
        {resumo.bots.ativos === 1 ? "Um bot ligado" : `${resumo.bots.ativos} bots ligados`}
      </AlertTitle>
      <AlertDescription>
        Quem escrever para o número de um bot ligado recebe resposta. O texto é escrito pelo modelo
        na hora — não há revisão humana antes do envio.
      </AlertDescription>
    </Alert>
  );
}
