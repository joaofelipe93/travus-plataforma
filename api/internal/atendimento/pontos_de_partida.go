package atendimento

// Pontos de partida de um bot novo (decisão do usuário em 29/10/2026): em vez de começar de uma
// tela em branco, quem cria escolhe um ponto de partida e **edita o que quiser**. Nada aqui é
// imposto — é texto copiado para o bot no momento da criação, e depois ele é só do bot.
//
// A Travus trabalha com **vários segmentos**. Por isso:
//
//   - nenhum ponto de partida é o "padrão" implícito: a tela mostra todos, incluindo o vazio;
//   - o catálogo é pequeno de propósito. Ponto de partida que ninguém usa é texto velho que
//     alguém vai aproveitar sem ler;
//   - os exemplos genéricos não citam segmento nenhum, e os específicos dizem o segmento no
//     nome, para ninguém sair com um atendimento de um ramo que não é o dele.
//
// Acrescentar um: uma entrada aqui, e a tela e a API já a oferecem.

// PontoDePartida é um bot pré-escrito: instruções, saudação e as perguntas, na ordem.
type PontoDePartida struct {
	ID        string     `json:"id"`
	Nome      string     `json:"nome"`
	Descricao string     `json:"descricao"`
	Instrucoes string    `json:"instrucoes"`
	Saudacao  string     `json:"saudacao"`
	Perguntas []Pergunta `json:"perguntas"`
}

func pergunta(texto, campo string, obrigatoria bool, orientacao string) Pergunta {
	q := Pergunta{Texto: texto, Campo: campo, Obrigatoria: obrigatoria}
	if orientacao != "" {
		q.Orientacao = &orientacao
	}
	return q
}

// PontosDePartida, na ordem em que a tela mostra. O primeiro é o vazio: quem sabe o que quer não
// precisa apagar texto de ninguém.
var PontosDePartida = []PontoDePartida{
	{
		ID:        "em-branco",
		Nome:      "Começar em branco",
		Descricao: "Sem nada escrito: você define as instruções e as perguntas.",
	},
	{
		ID:        "plano-saude-empresarial",
		Nome:      "Plano de saúde empresarial",
		Descricao: "Qualifica uma empresa interessada em plano de saúde e passa o contato para um consultor.",
		Instrucoes: `Você é o primeiro atendimento da Travus para empresas interessadas em plano de saúde.

O que você faz: entender a necessidade da empresa e reunir as informações que um consultor precisa
para preparar uma proposta. Quem faz cotação, fala de preço e fecha contrato é o consultor — você
não dá valores, não cita operadora como recomendação e não promete carência, cobertura nem prazo.

Se a pessoa perguntar preço, explique que o valor depende do número de vidas, das idades e da
operadora escolhida, e que o consultor traz as opções. Se ela quiser falar com alguém agora,
registre e encerre dizendo que um consultor vai responder.`,
		Saudacao: "Olá! Aqui é o atendimento da Travus. Posso fazer algumas perguntas rápidas para um consultor preparar as opções de plano de saúde para a sua empresa?",
		Perguntas: []Pergunta{
			pergunta("Qual é o nome da empresa?", "nome_da_empresa", true, ""),
			pergunta("Com quem estou falando, e qual o seu cargo na empresa?", "contato_e_cargo", true, ""),
			pergunta("Quantas pessoas entrariam no plano?", "quantas_vidas", true,
				"Aceite número aproximado. Se a pessoa não souber, pergunte quantos funcionários a empresa tem hoje."),
			pergunta("A empresa já tem plano de saúde hoje? Se sim, com qual operadora?", "plano_atual", true,
				"Se já tem, pergunte o que a levou a procurar outra opção — isso ajuda o consultor."),
			pergunta("Quais as faixas de idade das pessoas que entrariam?", "faixas_de_idade", false,
				"Serve uma ideia geral (ex.: a maioria entre 30 e 45). Não insista se a pessoa não souber."),
			pergunta("Qual o CNPJ da empresa?", "cnpj", false,
				"Se a pessoa não quiser informar agora, siga em frente: o consultor pede depois."),
			pergunta("Qual o melhor telefone e horário para o consultor falar com você?", "melhor_contato", true, ""),
		},
	},
	{
		ID:        "consorcio",
		Nome:      "Consórcio",
		Descricao: "Entende o que a pessoa procura em consórcio e passa o contato para um consultor.",
		Instrucoes: `Você é o primeiro atendimento da Travus para quem tem interesse em consórcio.

O que você faz: entender o que a pessoa procura e reunir o que um consultor precisa para
apresentar as opções. Você **não** fala de valor de parcela, prazo, taxa, chance de contemplação
nem lance — isso é do consultor, e cada grupo tem condições próprias.

Se a pessoa perguntar sobre contemplação ou lance, diga que depende do grupo e que o consultor
explica. Se ela já é cliente e quer falar de uma cota que já tem, registre o assunto e encerre
dizendo que a equipe vai responder.`,
		Saudacao: "Olá! Aqui é o atendimento da Travus. Posso fazer algumas perguntas rápidas para um consultor trazer as opções certas para você?",
		Perguntas: []Pergunta{
			pergunta("Com quem estou falando?", "nome", true, ""),
			pergunta("O consórcio é para imóvel, veículo ou serviço?", "objetivo", true, ""),
			pergunta("Qual valor você tem em mente para o bem?", "valor_pretendido", true,
				"Não comente se o valor é alto ou baixo, e não fale de parcela: só registre."),
			pergunta("Você já tem consórcio hoje, aqui ou em outra administradora?", "ja_tem_consorcio", false, ""),
			pergunta("Qual o melhor telefone e horário para o consultor falar com você?", "melhor_contato", true, ""),
		},
	},
	{
		ID:        "qualificacao-generica",
		Nome:      "Qualificação genérica",
		Descricao: "Para qualquer segmento: descobre o que a pessoa quer e pega o contato.",
		Instrucoes: `Você é o primeiro atendimento da Travus.

O que você faz: entender o que a pessoa procura e reunir o contato para que a equipe responda.
Você não dá valores, não promete prazo e não fala de condição comercial — quem faz isso é a
equipe. Fora disso, seja útil: responda o que estiver nestas instruções e seja direto quando não
souber algo.`,
		Saudacao: "Olá! Aqui é o atendimento da Travus. Para eu direcionar você à pessoa certa, posso fazer duas ou três perguntas rápidas?",
		Perguntas: []Pergunta{
			pergunta("Com quem estou falando?", "nome", true, ""),
			pergunta("Como posso ajudar? O que você está procurando?", "assunto", true, ""),
			pergunta("Qual o melhor telefone e horário para a equipe falar com você?", "melhor_contato", true, ""),
		},
	},
}

// PontoDePartidaPorID devolve o ponto de partida e se ele existe.
func PontoDePartidaPorID(id string) (PontoDePartida, bool) {
	for _, p := range PontosDePartida {
		if p.ID == id {
			return p, true
		}
	}
	return PontoDePartida{}, false
}
