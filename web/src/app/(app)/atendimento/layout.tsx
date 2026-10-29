import { EspacoServico } from "@/components/espaco-servico";

export default function LayoutAtendimento({ children }: LayoutProps<"/atendimento">) {
  return <EspacoServico id="atendimento">{children}</EspacoServico>;
}
