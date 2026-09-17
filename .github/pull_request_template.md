## O que muda

<!-- Uma ou duas frases, do ponto de vista de quem usa. O título do PR vira o commit na main:
     tipo(escopo): descrição (feat, fix, perf, revert, docs, ci, build, refactor, test, chore, style). -->

## Como foi testado

<!-- Testes que passaram localmente e o que foi conferido na tela, se for o caso. -->

## Checklist

- [ ] `make test` e `make verificar` passaram
- [ ] Nada registra lance real, clica em "Confirmar" nem cria execução no Newcon
- [ ] Nada manda mensagem ao grupo real do WhatsApp nem pareia o número da produção
- [ ] Sem segredos nem dados de clientes/hóspedes (o repositório é público; testes só com dados fictícios)
- [ ] Migração nova (se houver): número livre na `main`, sem editar migração existente, Up sem apagar ou renomear
- [ ] `CLAUDE.md` / `docs/` atualizados se mudou regra, comando, rota ou tela
