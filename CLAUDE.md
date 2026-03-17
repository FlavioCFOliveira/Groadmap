# CLAUDE.md

## Project Identity

**Groadmap** é uma ferramenta CLI em Go para gestão de roadmaps técnicos, utilizando SQLite como backend.

---

## Agent Responsibilities (Strict Ownership)

### spec-orchestrator
**Autoridade exclusiva** para especificação técnica.
- Cria e mantém todos os documentos em `SPEC/`
- Produz especificações APENAS a partir do input do utilizador
- NUNCA deriva especificações do código existente
- SEMPRE pergunta ao utilizador para decisões (o utilizador é a única fonte de verdade)
- Atua como gatekeeper: nenhuma implementação sem especificação clara

### go-elite-developer
**Autoridade exclusiva** para desenvolvimento de código Go.
- Implementa features conforme especificação técnica
- Garante código idiomático, eficiente e seguro
- Realiza validação: `go build`, `go test`, `go vet`, `go fmt`
- NUNCA implementa sem especificação aprovada
- SEMPRE segue padrões do projeto em `SPEC/`

### go-gitflow
**Autoridade exclusiva** para operações Git.
- Cria branches, commits, merges, PRs
- Valida estado do repositório antes de operações
- Garante mensagens de commit técnicas e descritivas
- Requer confirmação explícita do utilizador para operações destrutivas
- SEMPRE verifica `go fmt`, `go vet`, `go test` antes de commits

### exhaustive-qa-engineer
**Autoridade exclusiva** para testes e validação.
- Executa testes exaustivos, fuzzing, análise de edge cases
- Valida segurança e robustez
- Atua em: features críticas, validação pré-release, alterações de schema

### red-team-hacker
**Autoridade exclusiva** para segurança ofensiva.
- Realiza auditorias de segurança, penetration testing
- Identifica vulnerabilidades e cadeias de exploit
- Atua em: features de segurança, validação de input, operações críticas

### go-performance-advisor
**Autoridade exclusiva** para análise de performance.
- Análise estática e dinâmica de código Go
- Identifica bottlenecks, problemas de memória, concorrência
- Fornece recomendações de otimização

---

## Execution Rules

### Rule 1: Specification First
```
Utilizador Request → spec-orchestrator → SPEC/ → go-elite-developer → Implementação
```
- NENHUMA implementação começa sem especificação em `SPEC/`
- Dúvidas de requisitos → SEMPRE perguntar ao utilizador

### Rule 2: Skill Delegation
- Tarefas de código → `go-elite-developer`
- Tarefas Git → `go-gitflow`
- Tarefas de especificação → `spec-orchestrator`
- Tarefas de testes exaustivos → `exhaustive-qa-engineer`
- Tarefas de segurança → `red-team-hacker`
- Tarefas de performance → `go-performance-advisor`

### Rule 3: Validation Gates
Antes de qualquer commit/merge:
1. `go fmt ./...` (formato)
2. `go vet ./...` (análise estática)
3. `go test ./...` (testes)
4. `go build -o ./bin/ ./cmd/rmp` (build)

### Rule 4: Output Standards
- **Sucesso**: JSON para stdout
- **Erros/Help**: Plain text para stderr
- **Datas**: ISO 8601 UTC

### Rule 5: Commit Standards (Strict)

#### Proibido
- **NENHUMA referência ao Claude** nas mensagens de commit
- **NENHUM `Co-Author`** ou similar nos commits
- **NENHUMA menção** a assistentes AI, ferramentas externas, ou origem do código

#### Obrigatório
- **Descrição detalhada** do que foi alterado
- **Motivo da alteração** (porquê, não apenas o quê)
- **Contexto técnico** relevante (structs, funções, packages afetados)
- **Impacto** das mudanças (breaking changes, dependências, etc.)

#### Formato
```
type(scope): subject

- Detailed explanation of what changed
- Technical reasoning for the change
- Impact on existing code
- References to SPEC/ if applicable
```

---

## Project Structure

```
/Users/flaviocfo/dev/github.com/FlavioCFOliveira/Groadmap/
├── cmd/rmp/main.go          # Entry point CLI
├── internal/
│   ├── commands/            # Subcomandos (roadmap, task, sprint, audit)
│   ├── db/                  # SQLite, schema, queries parametrizadas
│   ├── models/              # Structs e enums
│   └── utils/               # JSON, datas ISO 8601, paths
├── bin/                     # Output de build
├── SPEC/                    # Especificações técnicas (spec-orchestrator only)
└── CLAUDE.md               # Este ficheiro
```

---

## Development Commands

```bash
# Build (output para ./bin/)
go build -o ./bin/ ./cmd/rmp

# Test
go test ./...

# Run (desenvolvimento)
go run ./cmd/rmp [args]        # ex: go run ./cmd/rmp roadmap list

# Format e Vet
go fmt ./...
go vet ./...
```

---

## Technical Constraints

### Security
- Input validation obrigatória para todos os argumentos CLI
- SQL queries parametrizadas (prepared statements)
- Permissões de filesystem: `0700` para diretório de dados (`~/.roadmaps/`)

### Data Standards
- Todas as datas: ISO 8601 UTC
- Output sucesso: JSON
- Output erro: Plain text para stderr

### SQLite
- Ficheiros `.db` individuais em `~/.roadmaps/`
- Schema versionado
- Migrations quando necessário

---

## SPEC Directory Reference

| Ficheiro | Conteúdo | Owner |
|----------|----------|-------|
| `SPEC/ARCHITECTURE.md` | Design do sistema, estrutura | spec-orchestrator |
| `SPEC/COMMANDS.md` | Hierarquia CLI, aliases | spec-orchestrator |
| `SPEC/DATABASE.md` | Schema SQLite, relações | spec-orchestrator |
| `SPEC/DATA_FORMATS.md` | Schema JSON outputs | spec-orchestrator |
| `SPEC/HELP_EXAMPLES.md` | Mensagens de ajuda/erro | spec-orchestrator |
| `SPEC/IMPLEMENTATION_PLAN.md` | Plano de implementação | spec-orchestrator |
| `SPEC/MODELS.md` | Definição de modelos | spec-orchestrator |
| `SPEC/STATE_MACHINE.md` | Máquinas de estado | spec-orchestrator |

---

## Decision Matrix

| Situação | Ação |
|----------|------|
| Utilizador pede nova feature | Invocar `spec-orchestrator` primeiro |
| Especificação existe, implementar | Invocar `go-elite-developer` |
| Operações Git (commit, branch, PR) | Invocar `go-gitflow` |
| Testes exaustivos necessários | Invocar `exhaustive-qa-engineer` |
| Auditoria de segurança | Invocar `red-team-hacker` |
| Análise de performance | Invocar `go-performance-advisor` |
| Dúvida de requisitos | PERGUNTAR ao utilizador |
| Código vs Especificação divergem | Seguir especificação, perguntar ao utilizador |

---

## Anti-Patterns (Proibido)

- NUNCA implementar sem especificação
- NUNCA derivar especificação do código existente
- NUNCA tomar decisões de produto (sempre perguntar ao utilizador)
- NUNCA ignorar falhas em `go vet` ou `go test`
- NUNCA fazer operações Git destrutivas sem confirmação
- NUNCA comprometer segurança (input validation, SQL injection)
- NUNCA referenciar Claude/AI em commits (mensagens devem ser técnicas e neutras)
- NUNCA adicionar Co-Author em commits (o utilizador é o único autor)

---

## Communication Protocol

1. **Entender**: Analisar pedido do utilizador
2. **Roteamento**: Identificar skill/agente correto
3. **Delegação**: Invocar skill com contexto completo
4. **Validação**: Verificar gates obrigatórios
5. **Entrega**: Apresentar resultado ao utilizador

Quando múltiplos agents são necessários, orquestrar sequencialmente conforme dependências.
