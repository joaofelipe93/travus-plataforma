import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Gera .next/standalone com um server.js mínimo, usado na imagem Docker.
  output: "standalone",

  // O espaço Canopus virou Lances e Boletos, o cadastro de clientes saiu para /clientes e a aba
  // WhatsApp de cada serviço virou Conexões (todo serviço tem Painel, Configurações e Conexões).
  // Endereço antigo que alguém tenha guardado continua chegando ao lugar certo. Temporário
  // (307): some quando ninguém mais usar.
  //
  // O endereço do serviço sem aba abre o Painel, que é a primeira aba dele.
  async redirects() {
    return [
      { source: "/lances", destination: "/lances/painel", permanent: false },
      { source: "/boletos", destination: "/boletos/painel", permanent: false },
      { source: "/reservas", destination: "/reservas/painel", permanent: false },
      { source: "/boletos/whatsapp", destination: "/boletos/conexoes", permanent: false },
      { source: "/reservas/whatsapp", destination: "/reservas/conexoes", permanent: false },
      { source: "/canopus", destination: "/lances/painel", permanent: false },
      { source: "/canopus/clientes", destination: "/clientes", permanent: false },
      { source: "/canopus/clientes/:id", destination: "/clientes/:id", permanent: false },
      { source: "/canopus/boletos", destination: "/boletos/rodadas", permanent: false },
      { source: "/canopus/boletos/whatsapp", destination: "/boletos/conexoes", permanent: false },
      { source: "/canopus/boletos/:id", destination: "/boletos/rodadas/:id", permanent: false },
      { source: "/canopus/:path*", destination: "/lances/:path*", permanent: false },
    ];
  },
};

export default nextConfig;
