import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Gera .next/standalone com um server.js mínimo, usado na imagem Docker.
  output: "standalone",

  // O espaço Canopus virou Lances e Boletos, e o cadastro de clientes saiu para /clientes.
  // Endereço antigo que alguém tenha guardado continua chegando ao lugar certo. Temporário
  // (307): some quando ninguém mais usar.
  async redirects() {
    return [
      { source: "/canopus", destination: "/lances/execucoes", permanent: false },
      { source: "/canopus/clientes", destination: "/clientes", permanent: false },
      { source: "/canopus/clientes/:id", destination: "/clientes/:id", permanent: false },
      { source: "/canopus/boletos", destination: "/boletos/rodadas", permanent: false },
      { source: "/canopus/boletos/whatsapp", destination: "/boletos/whatsapp", permanent: false },
      { source: "/canopus/boletos/:id", destination: "/boletos/rodadas/:id", permanent: false },
      { source: "/canopus/:path*", destination: "/lances/:path*", permanent: false },
    ];
  },
};

export default nextConfig;
