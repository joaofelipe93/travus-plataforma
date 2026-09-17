package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/joaofelipe93/travus-plataforma/api/internal/credenciais"
	"github.com/joaofelipe93/travus-plataforma/api/internal/db"
)

// Meu perfil: cada pessoa cuida dos próprios dados de contato (para o suporte saber a quem
// recorrer) e das próprias credenciais em serviços de terceiros. E-mail e perfil não se
// editam aqui: o e-mail é o login e o perfil é decisão de admin (make usuario).

const (
	tamanhoMaximoNome        = 120
	tamanhoMaximoCargo       = 80
	tamanhoMaximoObservacoes = 500
	tamanhoMaximoTelefone    = 25
	digitosMinimosTelefone   = 10
	digitosMaximosTelefone   = 15
)

type perfilJSON struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	Nome         string    `json:"nome"`
	Perfil       string    `json:"perfil"`
	Telefone     *string   `json:"telefone"`
	Cargo        *string   `json:"cargo"`
	Observacoes  *string   `json:"observacoes"`
	CriadoEm     time.Time `json:"criado_em"`
	AtualizadoEm time.Time `json:"atualizado_em"`
}

// credencialJSON é a definição do catálogo mais o que esta pessoa já cadastrou. O valor
// nunca sai daqui: só a dica (últimos caracteres) e as datas.
type credencialJSON struct {
	credenciais.Definicao
	Cadastrada   bool       `json:"cadastrada"`
	Dica         string     `json:"dica,omitempty"`
	AtualizadaEm *time.Time `json:"atualizada_em"`
}

type respostaPerfil struct {
	Usuario     perfilJSON       `json:"usuario"`
	Credenciais []credencialJSON `json:"credenciais"`
	// Sem cofre (CHAVE_CRIPTOGRAFIA), a tela avisa em vez de oferecer o formulário.
	CofreConfigurado bool `json:"cofre_configurado"`
}

func perfilDaLinha(id int64, email, nome string, perfil db.PerfilUsuario, telefone, cargo, observacoes *string, criadoEm, atualizadoEm time.Time) perfilJSON {
	return perfilJSON{
		ID: id, Email: email, Nome: nome, Perfil: string(perfil),
		Telefone: telefone, Cargo: cargo, Observacoes: observacoes,
		CriadoEm: criadoEm, AtualizadoEm: atualizadoEm,
	}
}

func (s *Servidor) meuPerfil(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	ctx := r.Context()
	p, err := s.q.BuscarPerfil(ctx, u.ID)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	lista, err := s.credenciaisDoUsuario(r, u.ID)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	responderJSON(w, http.StatusOK, respostaPerfil{
		Usuario:          perfilDaLinha(p.ID, p.Email, p.Nome, p.Perfil, p.Telefone, p.Cargo, p.Observacoes, p.CriadoEm, p.AtualizadoEm),
		Credenciais:      lista,
		CofreConfigurado: s.cfg.Cofre != nil,
	})
}

func (s *Servidor) credenciaisDoUsuario(r *http.Request, usuarioID int64) ([]credencialJSON, error) {
	gravadas, err := s.q.ListarCredenciaisDoUsuario(r.Context(), usuarioID)
	if err != nil {
		return nil, err
	}
	porID := make(map[string]db.ListarCredenciaisDoUsuarioRow, len(gravadas))
	for _, g := range gravadas {
		porID[g.Credencial] = g
	}
	catalogo := credenciais.Catalogo()
	lista := make([]credencialJSON, 0, len(catalogo))
	for _, d := range catalogo {
		item := credencialJSON{Definicao: d}
		if g, ok := porID[d.ID]; ok {
			atualizada := g.AtualizadoEm
			item.Cadastrada, item.Dica, item.AtualizadaEm = true, g.Dica, &atualizada
		}
		lista = append(lista, item)
	}
	return lista, nil
}

// pedidoPerfil usa ponteiros: campo ausente fica como está, campo vazio limpa o que havia.
type pedidoPerfil struct {
	Nome        *string `json:"nome"`
	Telefone    *string `json:"telefone"`
	Cargo       *string `json:"cargo"`
	Observacoes *string `json:"observacoes"`
}

func (s *Servidor) atualizarMeuPerfil(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	var p pedidoPerfil
	if err := lerJSON(w, r, &p, 8<<10); err != nil {
		responderErro(w, http.StatusBadRequest, "envie nome, telefone, cargo ou observações")
		return
	}
	ctx := r.Context()
	atual, err := s.q.BuscarPerfil(ctx, u.ID)
	if err != nil {
		erroInterno(w, r, err)
		return
	}

	params := db.AtualizarPerfilParams{
		ID: u.ID, Nome: atual.Nome, Telefone: atual.Telefone, Cargo: atual.Cargo, Observacoes: atual.Observacoes,
	}
	var mudados []string
	if p.Nome != nil {
		nome := espacosUnicos(*p.Nome)
		if utf8.RuneCountInString(nome) < 2 || utf8.RuneCountInString(nome) > tamanhoMaximoNome {
			responderErro(w, http.StatusBadRequest, "o nome precisa ter de 2 a 120 caracteres")
			return
		}
		if nome != params.Nome {
			params.Nome, mudados = nome, append(mudados, "nome")
		}
	}
	if p.Telefone != nil {
		telefone := espacosUnicos(*p.Telefone)
		if telefone != "" && !telefoneValido(telefone) {
			responderErro(w, http.StatusBadRequest, "telefone inválido: use DDD e número, por exemplo (11) 90000-0000")
			return
		}
		if telefone != valorOuVazio(params.Telefone) {
			params.Telefone, mudados = textoOuNil(telefone), append(mudados, "telefone")
		}
	}
	if p.Cargo != nil {
		cargo := espacosUnicos(*p.Cargo)
		if utf8.RuneCountInString(cargo) > tamanhoMaximoCargo {
			responderErro(w, http.StatusBadRequest, "o cargo precisa ter no máximo 80 caracteres")
			return
		}
		if cargo != valorOuVazio(params.Cargo) {
			params.Cargo, mudados = textoOuNil(cargo), append(mudados, "cargo")
		}
	}
	if p.Observacoes != nil {
		obs := strings.TrimSpace(*p.Observacoes)
		if utf8.RuneCountInString(obs) > tamanhoMaximoObservacoes {
			responderErro(w, http.StatusBadRequest, "as observações precisam ter no máximo 500 caracteres")
			return
		}
		if obs != valorOuVazio(params.Observacoes) {
			params.Observacoes, mudados = textoOuNil(obs), append(mudados, "observacoes")
		}
	}

	novo, err := s.q.AtualizarPerfil(ctx, params)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if len(mudados) > 0 {
		// Sem os valores na auditoria: são dados pessoais de quem usa a plataforma.
		s.auditar(ctx, r, &u.ID, "perfil_atualizado", "usuario", idTexto(u.ID), map[string]any{"campos": mudados})
	}
	responderJSON(w, http.StatusOK, perfilDaLinha(novo.ID, novo.Email, novo.Nome, novo.Perfil, novo.Telefone, novo.Cargo, novo.Observacoes, novo.CriadoEm, novo.AtualizadoEm))
}

type pedidoCredencial struct {
	Valores map[string]string `json:"valores"`
}

// gravarMinhaCredencial cadastra ou troca a credencial da própria pessoa. Os valores são
// cifrados e nunca voltam numa resposta.
func (s *Servidor) gravarMinhaCredencial(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	def, ok := s.definicaoDoCaminho(w, r)
	if !ok {
		return
	}
	var p pedidoCredencial
	if err := lerJSON(w, r, &p, 16<<10); err != nil {
		responderErro(w, http.StatusBadRequest, "envie os campos em \"valores\"")
		return
	}
	valores, err := def.Validar(p.Valores)
	if err != nil {
		responderErro(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	linha, err := credenciais.Gravar(ctx, s.q, s.cfg.Cofre, u.ID, def, valores)
	if errors.Is(err, credenciais.ErrSemCofre) {
		responderErro(w, http.StatusServiceUnavailable, "a API está sem CHAVE_CRIPTOGRAFIA: sem ela não dá para guardar credenciais com segurança")
		return
	}
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	s.auditar(ctx, r, &u.ID, "credencial_gravada", "credencial", def.ID, map[string]any{"credencial": def.ID})
	atualizada := linha.AtualizadoEm
	responderJSON(w, http.StatusOK, credencialJSON{
		Definicao: def, Cadastrada: true, Dica: linha.Dica, AtualizadaEm: &atualizada,
	})
}

func (s *Servidor) apagarMinhaCredencial(w http.ResponseWriter, r *http.Request, u *UsuarioSessao) {
	def, ok := s.definicaoDoCaminho(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	apagou, err := credenciais.Apagar(ctx, s.q, u.ID, def.ID)
	if err != nil {
		erroInterno(w, r, err)
		return
	}
	if apagou {
		s.auditar(ctx, r, &u.ID, "credencial_removida", "credencial", def.ID, map[string]any{"credencial": def.ID})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Servidor) definicaoDoCaminho(w http.ResponseWriter, r *http.Request) (credenciais.Definicao, bool) {
	def, err := credenciais.Buscar(strings.TrimSpace(r.PathValue("credencial")))
	if err != nil {
		responderErro(w, http.StatusNotFound, "esta credencial não existe no catálogo da plataforma")
		return credenciais.Definicao{}, false
	}
	return def, true
}

type usuarioCredencialJSON struct {
	ID           int64      `json:"id"`
	Nome         string     `json:"nome"`
	Email        string     `json:"email"`
	Perfil       string     `json:"perfil"`
	Cadastrada   bool       `json:"cadastrada"`
	AtualizadaEm *time.Time `json:"atualizada_em"`
}

type credencialAdminJSON struct {
	credenciais.Definicao
	Usuarios []usuarioCredencialJSON `json:"usuarios"`
}

// panoramaCredenciais (só admin): quem já cadastrou cada credencial e quem falta, para o
// suporte saber por que um serviço não roda para alguém. Nunca mostra valor nem dica.
func (s *Servidor) panoramaCredenciais(w http.ResponseWriter, r *http.Request, _ *UsuarioSessao) {
	ctx := r.Context()
	catalogo := credenciais.Catalogo()
	lista := make([]credencialAdminJSON, 0, len(catalogo))
	// Uma consulta por credencial: o catálogo é curto e a tela é de uso ocasional.
	for _, d := range catalogo {
		linhas, err := s.q.ListarUsuariosComCredencial(ctx, d.ID)
		if err != nil {
			erroInterno(w, r, err)
			return
		}
		usuarios := make([]usuarioCredencialJSON, 0, len(linhas))
		for _, l := range linhas {
			usuarios = append(usuarios, usuarioCredencialJSON{
				ID: l.ID, Nome: l.Nome, Email: l.Email, Perfil: string(l.Perfil),
				Cadastrada: l.Cadastrada, AtualizadaEm: l.AtualizadoEm,
			})
		}
		lista = append(lista, credencialAdminJSON{Definicao: d, Usuarios: usuarios})
	}
	responderJSON(w, http.StatusOK, map[string]any{"credenciais": lista})
}

// espacosUnicos tira os espaços das pontas e junta os do meio ("  Ana   Maria " → "Ana Maria").
func espacosUnicos(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// telefoneValido aceita o que a pessoa digitou (parênteses, traços, +55) desde que a
// quantidade de dígitos faça sentido para um celular ou fixo.
func telefoneValido(s string) bool {
	if utf8.RuneCountInString(s) > tamanhoMaximoTelefone {
		return false
	}
	digitos := 0
	for _, r := range s {
		switch {
		case unicode.IsDigit(r):
			digitos++
		case strings.ContainsRune("+()- .", r):
		default:
			return false
		}
	}
	return digitos >= digitosMinimosTelefone && digitos <= digitosMaximosTelefone
}
