// Formatos das respostas da API (api/internal/httpapi).

export type Perfil = "admin" | "operador" | "leitura";

export type Usuario = {
  id: number;
  email: string;
  nome: string;
  perfil: Perfil;
};

export type Sessao = {
  usuario: Usuario;
  csrf_token: string;
};

export type Cota = {
  id: number;
  cliente_id: number;
  cliente_nome: string;
  administradora: string;
  grupo: string;
  cota: string;
  versao: string;
  tipo_consorcio: string | null;
  modalidade_padrao: string;
  ativa: boolean;
  atualizado_em: string;
};

export type ClienteResumo = {
  id: number;
  nome: string;
  telefone: string | null;
  email: string | null;
  total_cotas: number;
  cotas_ativas: number;
};

export type Cliente = {
  id: number;
  nome: string;
  telefone: string | null;
  email: string | null;
  origem: string;
  criado_em: string;
  atualizado_em: string;
};

export type Totais = {
  linhas: number;
  clientes: number;
  clientes_novos: number;
  clientes_alterados: number;
  cotas_novas: number;
  cotas_alteradas: number;
  cotas_sem_mudanca: number;
  fora_da_planilha: number;
  erros: number;
};

export type Mudanca = { campo: string; antes: string; depois: string };

export type CotaPrevia = {
  linha?: number;
  administradora: string;
  grupo: string;
  cota: string;
  versao: string;
  cliente: string;
  tipo_consorcio?: string;
  ativa?: boolean;
  mudancas?: Mudanca[];
};

export type ClientePrevia = {
  nome: string;
  telefone?: string;
  email?: string;
  cotas: number;
  mudancas?: Mudanca[];
};

export type Previa = {
  totais: Totais;
  erros: { linha: number; mensagem: string }[];
  clientes_novos: ClientePrevia[];
  clientes_alterados: ClientePrevia[];
  cotas_novas: CotaPrevia[];
  cotas_alteradas: CotaPrevia[];
  fora_da_planilha: CotaPrevia[];
};

export type StatusImportacao = "previa" | "aplicada" | "descartada";

export type Importacao = {
  id: number;
  status: StatusImportacao;
  arquivo_nome: string;
  criada_por_nome: string;
  criada_em: string;
  finalizada_em: string | null;
  previa: Previa;
};

export type ImportacaoResumo = {
  id: number;
  status: StatusImportacao;
  arquivo_nome: string;
  criada_por_nome: string;
  totais: Totais | null;
  criada_em: string;
  finalizada_em: string | null;
};
