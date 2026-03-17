# Plano de Implementação - Correções Pós-Auditoria

**Data:** 2026-03-16
**Versão:** 1.0.0
**Baseado em:** Relatório de Auditoria Completa Groadmap
**Total de Tarefas:** 28

---

## Legenda de Prioridades

| Prioridade | Código | Significado | SLA |
|------------|--------|-------------|-----|
| **P0 - Critical** | `CRIT-XXX` | Bloqueante para produção | 1-2 dias |
| **P1 - High** | `HIGH-XXX` | Alto risco de segurança/estabilidade | 1 semana |
| **P2 - Medium** | `MED-XXX` | Melhorias importantes | 2 semanas |
| **P3 - Low** | `LOW-XXX` | Melhorias desejáveis | 1 mês |
| **P4 - Optional** | `OPT-XXX` | Nice to have | Backlog |

---

## P0 - CRÍTICO (Bloqueante para Produção)

---

### CRIT-001: Implementar Retry Logic para Operações SQLite

**Problema Identificado:**
Race conditions em cenários de concorrência resultam em erros `SQLITE_BUSY` (database is locked). Testes de stress demonstraram 14% de falhas em operações simultâneas, causando perda de dados e inconsistência de estado.

**Descrição Técnica da Necessidade:**
Implementar mecanismo de retry com backoff exponencial para operações de base de dados que falham devido a locks. O retry deve:
- Detectar erros do tipo `SQLITE_BUSY` (código 5)
- Aplicar backoff exponencial: 100ms, 200ms, 400ms, 800ms, 1000ms (máximo)
- Limitar a 5 tentativas antes de falhar definitivamente
- Ser aplicado nas funções: `Open()`, `OpenExisting()`, e operações de escrita críticas
- Garantir thread-safety no mecanismo de retry

**Requisitos de Validação:**
- [ ] Teste de stress com 100 operações concorrentes deve ter 0% de falhas
- [ ] Teste de stress com 500 operações concorrentes deve ter <1% de falhas
- [ ] Logs devem indicar quando retries ocorrem (para debugging)
- [ ] Timeout total não deve exceder 30 segundos
- [ ] Testes unitários cobrindo: sucesso na 1ª tentativa, sucesso na 2ª tentativa, falha após max retries

**Ficheiros Afetados:**
- `internal/db/connection.go` (modificar `Open()`, `OpenExisting()`)
- `internal/db/queries.go` (adicionar wrapper de retry para writes)

**Dependências:** Nenhuma

---

### CRIT-002: Criar Suite de Testes Unitários - Camada DB

**Problema Identificado:**
O projeto tem 0% de cobertura de testes. Não existem ficheiros `*_test.go`, impossibilitando a deteção de regressões e dificultando refatorações seguras.

**Descrição Técnica da Necessidade:**
Criar testes unitários para a camada de base de dados (`internal/db/`) com:
- Setup de base de dados em memória (`:memory:`) para testes isolados
- Testes para todas as funções CRUD: `CreateTask`, `GetTask`, `GetTasks`, `ListTasks`, `UpdateTask`, `DeleteTask`
- Testes para operações de sprint: `CreateSprint`, `GetSprint`, `UpdateSprint`, `DeleteSprint`, `AddTasksToSprint`, `RemoveTasksFromSprint`
- Testes para audit: `LogAudit`, `ListAudit`, `GetAuditHistory`
- Testes para transações: `WithTransaction` (commit e rollback)
- Mock de timestamps para testes determinísticos

**Requisitos de Validação:**
- [ ] Cobertura mínima de 80% no package `internal/db`
- [ ] Todos os testes devem passar: `go test ./internal/db/... -v`
- [ ] Testes devem usar base de dados em memória (não criar ficheiros no disco)
- [ ] Testes devem ser determinísticos (mesmo resultado em múltiplas execuções)
- [ ] Testes devem limpar recursos após execução (defer close)

**Ficheiros a Criar:**
- `internal/db/connection_test.go`
- `internal/db/queries_test.go`
- `internal/db/schema_test.go`

**Dependências:** Nenhuma

---

### CRIT-003: Criar Suite de Testes Unitários - Camada Commands

**Problema Identificado:**
Falta de testes para handlers de comandos CLI, impossibilitando validar comportamento de comandos e argumentos.

**Descrição Técnica da Necessidade:**
Criar testes unitários para handlers de comandos em `internal/commands/`:
- Testes para `roadmap.go`: `list`, `create`, `remove`, `use`
- Testes para `task.go`: `list`, `create`, `get`, `edit`, `remove`, `set-status`, `set-priority`, `set-severity`
- Testes para `sprint.go`: `list`, `create`, `get`, `update`, `remove`, `start`, `close`, `reopen`, `tasks`, `stats`, `add-tasks`, `remove-tasks`, `move-tasks`
- Testes para `audit.go`: `list`, `history`, `stats`
- Mock da camada de base de dados para testes isolados
- Testes de casos de erro: argumentos inválidos, recursos não encontrados, permissões

**Requisitos de Validação:**
- [ ] Cobertura mínima de 70% no package `internal/commands`
- [ ] Todos os testes devem passar: `go test ./internal/commands/... -v`
- [ ] Testes devem validar saída JSON quando aplicável
- [ ] Testes devem validar códigos de saída (exit codes)
- [ ] Testes devem cobrir casos de erro e caminhos felizes

**Ficheiros a Criar:**
- `internal/commands/roadmap_test.go`
- `internal/commands/task_test.go`
- `internal/commands/sprint_test.go`
- `internal/commands/audit_test.go`

**Dependências:** CRIT-002 (estrutura de testes da camada DB)

---

### CRIT-004: Criar Suite de Testes Unitários - Camada Utils

**Problema Identificado:**
Funções utilitárias críticas (validação de paths, parsing de datas, JSON) não têm testes.

**Descrição Técnica da Necessidade:**
Criar testes unitários para `internal/utils/`:
- Testes para `path.go`: `ValidateRoadmapName` (casos válidos e inválidos), `GetRoadmapPath`, `RoadmapExists`, `ListRoadmaps`
- Testes para `time.go`: `ParseISO8601` (formatos válidos e inválidos), `FormatISO8601`, `NowISO8601`, `IsValidISO8601`
- Testes para `json.go`: `PrintJSON`, `PrintJSONIndent`, `ToJSON`, `FromJSON`
- Testes de path traversal: tentativas de `../`, `..\`, paths absolutos
- Testes de datas: formatos ISO 8601 válidos, inválidos, edge cases (leap years, timezones)

**Requisitos de Validação:**
- [ ] Cobertura mínima de 90% no package `internal/utils`
- [ ] Todos os testes devem passar: `go test ./internal/utils/... -v`
- [ ] Testes de path traversal devem demonstrar bloqueio efetivo
- [ ] Testes de datas devem cobrir edge cases (anos bissextos, datas futuras/passa)
- [ ] Testes JSON devem validar escape de caracteres especiais

**Ficheiros a Criar:**
- `internal/utils/path_test.go`
- `internal/utils/time_test.go`
- `internal/utils/json_test.go`

**Dependências:** Nenhuma

---

### CRIT-005: Validar Nomes de Roadmap Contra Flags

**Problema Identificado:**
A regex atual (`^[a-z0-9_-]+$`) permite nomes como `-r` e `--help`, que podem causar confusão com flags de comando e dificuldade em remover ficheiros com nomes especiais.

**Descrição Técnica da Necessidade:**
Modificar a validação de nomes de roadmap em `internal/utils/path.go`:
- Alterar regex para exigir que o nome comece com uma letra: `^[a-z][a-z0-9_-]*$`
- Adicionar validação explícita contra prefixos perigosos: nomes começados por `-` devem ser rejeitados
- Adicionar validação de comprimento máximo (255 caracteres)
- Adicionar validação contra nomes reservados do sistema: `CON`, `PRN`, `AUX`, `NUL`, `COM1-9`, `LPT1-9` (Windows)
- Garantir que nomes existentes inválidos sejam tratados (migração ou erro claro)

**Requisitos de Validação:**
- [ ] `./rmp roadmap create "-r"` deve retornar erro: "roadmap name cannot start with '-'"
- [ ] `./rmp roadmap create "--help"` deve retornar erro: "roadmap name cannot start with '-'"
- [ ] `./rmp roadmap create "-"` deve retornar erro
- [ ] `./rmp roadmap create "valid-name"` deve funcionar
- [ ] `./rmp roadmap create "a"` deve funcionar (mínimo 1 caractere)
- [ ] Nomes com mais de 255 caracteres devem ser rejeitados
- [ ] Testes unitários devem cobrir todos os casos de borda

**Ficheiros Afetados:**
- `internal/utils/path.go` (modificar `ValidRoadmapNameRegex` e `ValidateRoadmapName`)

**Dependências:** CRIT-004 (testes unitários para utils)

---

## P1 - HIGH (Alto Risco)

---

### HIGH-001: Validar Campos Obrigatórios em Updates de Tasks

**Problema Identificado:**
A função `taskEdit` permite atualizar campos obrigatórios (`description`, `action`, `expected_result`) com strings vazias, violando as regras de validação do modelo e criando dados inconsistentes.

**Descrição Técnica da Necessidade:**
Modificar `internal/commands/task.go` na função `taskEdit`:
- Antes de aplicar updates, validar que campos obrigatórios não sejam strings vazias
- Campos obrigatórios: `description`, `action`, `expected_result`
- Retornar erro específico indicando qual campo é inválido
- Não aplicar updates parciais se houver campos inválidos (tudo ou nada)
- Manter comportamento atual para campos opcionais (`specialists` pode ser vazio/null)

**Requisitos de Validação:**
- [ ] `./rmp task edit -r roadmap 1 --description ""` deve retornar erro
- [ ] `./rmp task edit -r roadmap 1 --action ""` deve retornar erro
- [ ] `./rmp task edit -r roadmap 1 --expected-result ""` deve retornar erro
- [ ] `./rmp task edit -r roadmap 1 --specialists ""` deve funcionar (campo opcional)
- [ ] Mensagens de erro devem indicar claramente o campo inválido
- [ ] Updates válidos devem continuar a funcionar normalmente
- [ ] Testes unitários devem cobrir validação de campos obrigatórios

**Ficheiros Afetados:**
- `internal/commands/task.go` (modificar função `taskEdit`)

**Dependências:** CRIT-003 (testes unitários para commands)

---

### HIGH-002: Adicionar Validação de IDs (Positivos e Limites)

**Problema Identificado:**
Não há validação de limites para IDs de tasks e sprints, permitindo valores negativos, zero, ou valores extremamente altos que podem causar comportamento inesperado ou erros de base de dados.

**Descrição Técnica da Necessidade:**
Criar função de validação de IDs e aplicar em todos os comandos:
- Criar função `validateID(id int, entity string) error` em `internal/utils/` ou `internal/models/`
- Validar que ID > 0 (positivo)
- Validar que ID <= MaxInt32 (2,147,483,647) para evitar overflow
- Aplicar validação em todos os comandos que aceitam IDs: `task get`, `task edit`, `task remove`, `sprint get`, `sprint update`, `sprint remove`, etc.
- Retornar erro específico com mensagem clara quando ID é inválido

**Requisitos de Validação:**
- [ ] `./rmp task get -r roadmap 0` deve retornar erro: "invalid task ID: 0"
- [ ] `./rmp task get -r roadmap -1` deve retornar erro: "invalid task ID: -1"
- [ ] `./rmp task get -r roadmap 99999999999999999` deve retornar erro de overflow
- [ ] `./rmp task get -r roadmap 1` deve funcionar normalmente
- [ ] Validação deve ser aplicada em todos os comandos que usam IDs
- [ ] Testes unitários devem cobrir IDs: negativos, zero, positivos, valores extremos

**Ficheiros Afetados:**
- `internal/commands/task.go` (adicionar validação em funções que usam IDs)
- `internal/commands/sprint.go` (adicionar validação em funções que usam IDs)
- `internal/db/queries.go` (adicionar validação em `GetTask`, `GetSprint`)

**Dependências:** CRIT-003, CRIT-004

---

### HIGH-003: Melhorar Tratamento de Erros com errors.Is

**Problema Identificado:**
O mapeamento de erros para exit codes em `cmd/rmp/main.go` usa `strings.Contains`, que pode ter falsos positivos (ex: "configuration file not found" seria mapeado para `ExitNotFound` em vez de `ExitFailure`).

**Descrição Técnica da Necessidade:**
Refatorar tratamento de erros para usar sentinel errors e `errors.Is`:
- Definir sentinel errors no package `internal/db`: `ErrNotFound`, `ErrAlreadyExists`, `ErrInvalidData`
- Modificar funções da camada DB para retornar sentinel errors apropriados
- Atualizar `cmd/rmp/main.go` para usar `errors.Is(err, db.ErrNotFound)` em vez de `strings.Contains`
- Garantir que erros wrapped continuam a ser detetáveis com `errors.Is`
- Manter compatibilidade com mensagens de erro existentes

**Requisitos de Validação:**
- [ ] `errors.Is(err, db.ErrNotFound)` deve retornar true para erros de "not found"
- [ ] `errors.Is(err, db.ErrAlreadyExists)` deve retornar true para erros de "already exists"
- [ ] Mensagens como "configuration file not found" NÃO devem ser mapeadas para `ExitNotFound`
- [ ] Exit codes devem ser consistentes com o tipo de erro
- [ ] Testes unitários devem validar mapeamento correto de erros

**Ficheiros Afetados:**
- `internal/db/errors.go` (criar - definir sentinel errors)
- `internal/db/queries.go` (modificar para usar sentinel errors)
- `cmd/rmp/main.go` (modificar mapeamento de erros)

**Dependências:** CRIT-002, CRIT-003

---

### HIGH-004: Adicionar Testes de Integração End-to-End

**Problema Identificado:**
Falta de testes que validem o fluxo completo de comandos CLI, desde parsing de argumentos até saída JSON.

**Descrição Técnica da Necessidade:**
Criar testes de integração em `cmd/rmp/`:
- Testes que executam o binário real (ou usam `os/exec` para chamar `go run`)
- Testes de fluxo completo: criar roadmap → criar task → editar task → listar tasks → remover task → remover roadmap
- Testes de casos de erro: comandos inválidos, argumentos em falta, recursos inexistentes
- Testes de concorrência: múltiplas operações simultâneas
- Limpeza de ambiente: remover ficheiros de teste após execução
- Uso de diretório temporário para dados de teste (não poluir `~/.roadmaps/`)

**Requisitos de Validação:**
- [ ] Testes devem executar comandos reais e validar saída
- [ ] Testes devem validar JSON output quando aplicável
- [ ] Testes devem validar exit codes
- [ ] Testes devem limpar ambiente após execução (defer cleanup)
- [ ] Testes de concorrência devem demonstrar estabilidade (sem race conditions)
- [ ] Cobertura de pelo menos 5 fluxos completos de utilização

**Ficheiros a Criar:**
- `cmd/rmp/integration_test.go`

**Dependências:** CRIT-002, CRIT-003, CRIT-004

---

## P2 - MEDIUM (Melhorias Importantes)

---

### MED-001: Adicionar Timeouts em Operações de Base de Dados

**Problema Identificado:**
Não há timeout definido para operações de base de dados, podendo causar hangs indefinidos em cenários de rede lenta ou locks prolongados.

**Descrição Técnica da Necessidade:**
Implementar timeouts para operações de base de dados:
- Adicionar `context.Context` como primeiro parâmetro em todas as funções da camada DB
- Usar versões com contexto das funções sql: `QueryRowContext`, `ExecContext`, `QueryContext`
- Definir timeout padrão de 30 segundos para operações normais
- Definir timeout de 5 segundos para operações de leitura simples
- Permitir cancellation via context (Ctrl+C deve cancelar operação DB)

**Requisitos de Validação:**
- [ ] Operações DB devem aceitar `context.Context`
- [ ] Timeout de 30s deve cancelar operações longas
- [ ] `context.Cancelled` deve ser respeitado
- [ ] Mensagens de erro devem indicar "context deadline exceeded" quando timeout ocorre
- [ ] Testes devem validar comportamento de timeout
- [ ] Não deve haver alteração de API pública (comandos CLI funcionam igual)

**Ficheiros Afetados:**
- `internal/db/queries.go` (todas as funções devem aceitar context)
- `internal/db/connection.go` (adicionar suporte a context)
- `internal/commands/*.go` (propagar context dos comandos)

**Dependências:** HIGH-003 (refatoração de erros)

---

### MED-002: Implementar Logging Estruturado

**Problema Identificado:**
O projeto não usa logging estruturado, dificultando debugging, monitorização e análise de problemas em produção.

**Descrição Técnica da Necessidade:**
Implementar logging estruturado usando `log/slog` (padrão Go 1.21+):
- Substituir `fmt.Println` e `fmt.Fprintf(os.Stderr, ...)` por chamadas de log apropriadas
- Usar níveis de log: DEBUG (desenvolvimento), INFO (operações normais), WARN (anomalias), ERROR (falhas)
- Incluir campos estruturados: timestamp, nível, componente, operação, erro (se aplicável)
- Suportar output em JSON para integração com sistemas de log aggregation
- Adicionar flag `--verbose` ou `-v` para aumentar nível de log
- Logar operações importantes: abertura de DB, criação de recursos, erros de validação

**Requisitos de Validação:**
- [ ] Logs devem ser output em formato estruturado (JSON ou key=value)
- [ ] Níveis de log devem ser respeitados (DEBUG < INFO < WARN < ERROR)
- [ ] Flag `-v` deve aumentar verbosidade
- [ ] Logs devem incluir timestamp ISO 8601
- [ ] Erros devem ser logados com stack trace (quando aplicável)
- [ ] Performance: logging não deve degradar operações (>5% overhead)

**Ficheiros Afetados:**
- `cmd/rmp/main.go` (configurar logger global)
- `internal/commands/*.go` (adicionar logging)
- `internal/db/*.go` (adicionar logging)

**Dependências:** Nenhuma

---

### MED-003: Adicionar Limites de Tamanho em Campos de Texto

**Problema Identificado:**
Não há limitação no tamanho de campos de texto (description, action, expected_result), permitindo potencial abuso através de inputs extremamente grandes que consomem memória e espaço em disco.

**Descrição Técnica da Necessidade:**
Implementar validação de tamanho máximo para campos de texto:
- Definir limites máximos: `description` (1000 chars), `action` (2000 chars), `expected_result` (2000 chars), `specialists` (500 chars)
- Validar tamanho antes de inserir na base de dados
- Retornar erro claro indicando qual campo excede o limite e qual o limite
- Considerar bytes vs runes (UTF-8) - usar `utf8.RuneCountInString()`
- Aplicar limites tanto em criação como em atualização

**Requisitos de Validação:**
- [ ] `./rmp task create -r roadmap -d "$(python3 -c 'print("A"*1001)')"` deve retornar erro
- [ ] `./rmp task create -r roadmap -d "$(python3 -c 'print("A"*1000)')"` deve funcionar
- [ ] Mensagens de erro devem indicar: "description exceeds maximum length of 1000 characters"
- [ ] Campos com caracteres multibyte UTF-8 devem ser contados corretamente (runes, não bytes)
- [ ] Testes unitários devem validar limites de tamanho

**Ficheiros Afetados:**
- `internal/commands/task.go` (adicionar validação em `taskCreate` e `taskEdit`)
- `internal/models/task.go` (adicionar constantes de limites)

**Dependências:** CRIT-003

---

### MED-004: Validar Campos de Update Contra Whitelist

**Problema Identificado:**
A função `UpdateTask` em `internal/db/queries.go` usa concatenação de strings para construir queries dinâmicas. Embora os valores sejam parametrizados, os nomes dos campos vêm do mapa `updates` sem validação contra whitelist de campos permitidos.

**Descrição Técnica da Necessidade:**
Implementar validação de campos contra whitelist:
- Definir whitelist de campos permitidos para update: `description`, `action`, `expected_result`, `specialists`, `priority`, `severity`, `status`
- Validar que todos os campos no mapa `updates` estão na whitelist
- Retornar erro se campo não permitido for tentado
- Considerar usar struct em vez de map para updates (mais type-safe)
- Manter compatibilidade com API atual (map[string]interface{})

**Requisitos de Validação:**
- [ ] Tentativa de atualizar campo não existente deve retornar erro
- [ ] Campos na whitelist devem funcionar normalmente
- [ ] Testes devem validar rejeição de campos não permitidos
- [ ] SQL injection em nomes de campos deve ser impossível

**Ficheiros Afetados:**
- `internal/db/queries.go` (modificar `UpdateTask`)
- `internal/models/task.go` (definir whitelist de campos)

**Dependências:** CRIT-002

---

### MED-005: Adicionar Rate Limiting em Operações de Audit

**Problema Identificado:**
Não há limitação no número de entradas de audit que podem ser consultadas, permitindo potencial DoS através de queries com limites muito altos que consomem memória e CPU.

**Descrição Técnica da Necessidade:**
Implementar limites máximos para operações de audit:
- Definir constante `MaxAuditLimit = 10000`
- Validar parâmetro `--limit` em `audit list` e `audit history`
- Retornar erro se limit > MaxAuditLimit
- Considerar adicionar paginação (offset/limit) para queries grandes
- Aplicar limites também em queries internas que possam retornar muitos resultados

**Requisitos de Validação:**
- [ ] `./rmp audit list -r roadmap --limit 999999999` deve retornar erro
- [ ] `./rmp audit list -r roadmap --limit 10000` deve funcionar
- [ ] `./rmp audit list -r roadmap --limit 10001` deve retornar erro
- [ ] Mensagem de erro deve indicar o limite máximo permitido
- [ ] Testes unitários devem validar limites

**Ficheiros Afetados:**
- `internal/commands/audit.go` (adicionar validação em `auditList` e `auditHistory`)
- `internal/models/audit.go` (adicionar constante MaxAuditLimit)

**Dependências:** CRIT-003

---

### MED-006: Configurar Connection Pooling SQLite

**Problema Identificado:**
Não há configuração explícita de connection pooling para SQLite, podendo resultar em uso ineficiente de recursos ou exaustão de conexões em cenários de alta carga.

**Descrição Técnica da Necessidade:**
Configurar connection pooling na abertura da base de dados:
- Definir `SetMaxOpenConns(25)` - máximo de conexões abertas
- Definir `SetMaxIdleConns(5)` - conexões idle mantidas
- Definir `SetConnMaxLifetime(5 * time.Minute)` - tempo máximo de vida de uma conexão
- Ajustar valores baseado em benchmarks (SQLite tem limitações diferentes de PostgreSQL/MySQL)
- Documentar escolhas de configuração

**Requisitos de Validação:**
- [ ] Configurações devem ser aplicadas na abertura da DB
- [ ] Testes de stress devem demonstrar melhoria em cenários concorrentes
- [ ] Não deve haver degradação de performance em uso normal
- [ ] Documentação deve explicar valores escolhidos

**Ficheiros Afetados:**
- `internal/db/connection.go` (adicionar configuração de pool)

**Dependências:** CRIT-001

---

### MED-007: Implementar Testes de Race Condition

**Problema Identificado:**
Não há testes específicos para detetar race conditions, que são um problema crítico identificado na auditoria.

**Descrição Técnica da Necessidade:**
Criar testes específicos para race conditions:
- Usar `go test -race ./...` como parte do pipeline de CI
- Criar testes que executam operações concorrentes: múltiplas criações de tasks simultâneas, leituras durante escritas, updates concorrentes
- Usar `sync.WaitGroup` para coordenar goroutines em testes
- Validar que não há data races detetados pelo race detector
- Documentar padrões seguros de concorrência usados no projeto

**Requisitos de Validação:**
- [ ] `go test -race ./...` deve passar sem erros
- [ ] Testes devem cobrir operações concorrentes comuns
- [ ] Não devem existir data races detetados
- [ ] Documentação deve explicar estratégia de concorrência

**Ficheiros a Criar:**
- `internal/db/race_test.go` (testes específicos de race conditions)

**Dependências:** CRIT-002, CRIT-003

---

## P3 - LOW (Melhorias Desejáveis)

---

### LOW-001: Revisar SetEscapeHTML(false) em JSON

**Problema Identificado:**
O encoder JSON em `internal/utils/json.go` tem `SetEscapeHTML(false)`, o que pode permitir XSS se o output for usado em contextos web.

**Descrição Técnica da Necessidade:**
Avaliar e potencialmente remover `SetEscapeHTML(false)`:
- Analisar se o output JSON é usado em contextos web (provavelmente não, é CLI)
- Se não houver necessidade de HTML escaping, documentar explicitamente a decisão
- Se houver possibilidade de uso web, remover `SetEscapeHTML(false)`
- Considerar adicionar opção de configuração para comportamento de escaping

**Requisitos de Validação:**
- [ ] Decisão documentada em comentário no código
- [ ] Se removido: caracteres HTML especiais (`<`, `>`, `&`) devem ser escapados em JSON output
- [ ] Testes devem validar comportamento de escaping

**Ficheiros Afetados:**
- `internal/utils/json.go` (revisar `SetEscapeHTML`)

**Dependências:** CRIT-004

---

### LOW-002: Adicionar Validação de Intervalo de Datas

**Problema Identificado:**
A função `ParseISO8601` aceita datas em formatos inválidos ou fora de intervalos razoáveis (ex: datas antes de 1970 ou muito futuras).

**Descrição Técnica da Necessidade:**
Implementar validação de intervalo de datas:
- Definir intervalo válido: 1970-01-01 a 9999-12-31
- Validar que datas de criação não são futuras (com tolerância de 1 minuto para drift de relógio)
- Validar que `completed_at` não é anterior a `created_at`
- Usar `time.Parse(time.RFC3339, ...)` para parsing estrito
- Retornar erro específico para datas fora do intervalo

**Requisitos de Validação:**
- [ ] Datas antes de 1970-01-01 devem ser rejeitadas
- [ ] Datas futuras (além de 1 minuto) devem ser rejeitadas para `created_at`
- [ ] `completed_at` anterior a `created_at` deve ser rejeitado
- [ ] Datas válidas devem continuar a funcionar
- [ ] Testes unitários devem cobrir edge cases de datas

**Ficheiros Afetados:**
- `internal/utils/time.go` (modificar `ParseISO8601`)
- `internal/models/task.go` (adicionar validação de datas)

**Dependências:** CRIT-004

---

### LOW-003: Verificar Permissões de Ficheiro Após Criação

**Problema Identificado:**
Após criar a base de dados, as permissões são definidas com `os.Chmod`, mas não há verificação se o umask do sistema não as alterou.

**Descrição Técnica da Necessidade:**
Adicionar verificação de permissões após criação:
- Após `os.Chmod`, usar `os.Stat` para verificar permissões efetivas
- Se permissões não forem as esperadas (0600), retornar erro
- Aplicar mesma lógica para diretório de dados (0700)
- Logar warning se permissões estiverem incorretas

**Requisitos de Validação:**
- [ ] Se umask alterar permissões, erro deve ser retornado
- [ ] Permissões 0600 devem ser verificadas para ficheiros .db
- [ ] Permissões 0700 devem ser verificadas para diretório
- [ ] Testes devem simular umask diferente e validar comportamento

**Ficheiros Afetados:**
- `internal/db/connection.go` (adicionar verificação após `os.Chmod`)
- `internal/utils/path.go` (adicionar verificação em `EnsureDataDir`)

**Dependências:** CRIT-004

---

### LOW-004: Adicionar Sanitização de Input em Descrições

**Problema Identificado:**
Não há sanitização de caracteres de controlo ou sequências de escape em campos de texto, podendo resultar em comportamento inesperado ou problemas de display.

**Descrição Técnica da Necessidade:**
Implementar sanitização básica de input:
- Remover ou escapar caracteres de controlo (0x00-0x1F, exceto \n, \t, \r permitidos)
- Normalizar Unicode (NFC) para evitar problemas de comparação
- Validar contra null bytes (0x00) que podem causar problemas em C strings
- Considerar truncamento silencioso vs erro para caracteres inválidos

**Requisitos de Validação:**
- [ ] Null bytes (\x00) devem ser rejeitados ou removidos
- [ ] Caracteres de controle (exceto \n, \t, \r) devem ser rejeitados
- [ ] Unicode deve ser normalizado para NFC
- [ ] Testes devem validar comportamento com inputs maliciosos

**Ficheiros Afetados:**
- `internal/commands/task.go` (adicionar sanitização em `taskCreate` e `taskEdit`)
- `internal/utils/sanitize.go` (criar - funções de sanitização)

**Dependências:** CRIT-003

---

### LOW-005: Melhorar Defer em Loop

**Problema Identificado:**
O `defer rows.Close()` é usado corretamente, mas em cenários de múltiplas queries em loop, pode acumular handles se não for chamado imediatamente.

**Descrição Técnica da Necessidade:**
Revisar uso de defer em loops:
- Identificar loops que fazem múltiplas queries
- Considerar fechar rows imediatamente após uso em vez de defer
- Ou extrair lógica de query para função separada (defer funciona corretamente ao sair da função)
- Documentar padrão recomendado para o projeto

**Requisitos de Validação:**
- [ ] Não deve haver acumulação de handles de rows em loops
- [ ] `defer rows.Close()` deve ser usado apenas em funções, não em loops
- [ ] Testes de stress não devem demonstrar fuga de recursos

**Ficheiros Afetados:**
- `internal/db/queries.go` (revisar uso de defer em `GetTasks`, `ListTasks`)

**Dependências:** CRIT-002

---

### LOW-006: Validar Máquina de Estados de Task

**Problema Identificado:**
A máquina de estados de Task não valida se o status atual é válido antes de verificar transições permitidas.

**Descrição Técnica da Necessidade:**
Melhorar validação de máquina de estados:
- Validar que status atual é um valor válido do enum antes de permitir transição
- Definir transições válidas explicitamente (ex: BACKLOG → SPRINT, SPRINT → DOING)
- Rejeitar transições inválidas com erro claro
- Documentar diagrama de estados em SPEC/

**Requisitos de Validação:**
- [ ] Transições inválidas devem retornar erro
- [ ] Transições válidas devem funcionar
- [ ] Documentação deve mostrar diagrama de estados
- [ ] Testes devem cobrir todas as transições possíveis

**Ficheiros Afetados:**
- `internal/models/task.go` (melhorar `IsValidStatusTransition`)
- `SPEC/STATE_MACHINE.md` (criar - documentar diagrama)

**Dependências:** CRIT-003

---

## P4 - OPTIONAL (Nice to Have)

---

### OPT-001: Adicionar Métricas de Performance

**Problema Identificado:**
Não há visibilidade sobre performance de operações (latência de queries, throughput).

**Descrição Técnica da Necessidade:**
Implementar métricas básicas de performance:
- Adicionar timing de operações críticas (criação de tasks, queries complexas)
- Expor métricas via comando `audit stats` ou novo comando `metrics`
- Usar `time.Since()` para medir duração
- Considerar integração com Prometheus/OpenTelemetry (opcional)

**Requisitos de Validação:**
- [ ] Comando deve mostrar latência média de operações
- [ ] Métricas devem ser persistentes entre execuções
- [ ] Overhead de métricas deve ser <1%

**Ficheiros Afetados:**
- `internal/metrics/metrics.go` (criar)
- `internal/commands/metrics.go` (criar)

**Dependências:** MED-002

---

### OPT-002: Implementar Backup e Restore

**Problema Identificado:**
Não há mecanismo de backup automático ou manual dos roadmaps.

**Descrição Técnica da Necessidade:**
Implementar comandos de backup e restore:
- Comando `roadmap backup <name>`: cria cópia .db com timestamp
- Comando `roadmap restore <name> <backup-file>`: restaura de backup
- Comando `roadmap list-backups <name>`: lista backups disponíveis
- Backups armazenados em `~/.roadmaps/backups/`
- Compressão opcional (gzip)

**Requisitos de Validação:**
- [ ] Backup deve criar cópia consistente (usar SQLite backup API)
- [ ] Restore deve validar integridade do backup
- [ ] Backups antigos devem ser rotacionados (manter últimos 10)
- [ ] Testes devem validar backup/restore de roadmaps completos

**Ficheiros a Criar:**
- `internal/commands/backup.go`
- `internal/db/backup.go`

**Dependências:** CRIT-003

---

### OPT-003: Adicionar Suporte a Múltiplos Utilizadores

**Problema Identificado:**
O sistema é single-user, sem conceito de utilizadores ou permissões.

**Descrição Técnica da Necessidade:**
Implementar suporte multi-user (opcional, modo avançado):
- Adicionar tabela `users` com username, password hash (bcrypt)
- Adicionar campo `owner` a roadmaps
- Implementar autenticação básica (token JWT ou session)
- Comandos: `user create`, `user login`, `user logout`
- Roadmaps privados vs públicos

**Requisitos de Validação:**
- [ ] Utilizadores podem criar contas
- [ ] Apenas owners podem modificar seus roadmaps
- [ ] Autenticação deve ser segura (bcrypt, JWT)
- [ ] Testes devem validar isolamento entre utilizadores

**Ficheiros Afetados:**
- Múltiplos - alteração arquitetural significativa

**Dependências:** Todas as tarefas P0-P2

---

### OPT-004: Implementar Export/Import JSON

**Problema Identificado:**
Não há forma de exportar/importar roadmaps em formato aberto (JSON).

**Descrição Técnica da Necessidade:**
Implementar exportação e importação JSON:
- Comando `roadmap export <name> [file.json]`: exporta roadmap completo para JSON
- Comando `roadmap import <file.json> [new-name]`: importa de JSON
- Formato JSON deve incluir tasks, sprints, audit (opcional)
- Validação de schema na importação
- Suporte a formatos: JSON, CSV (opcional)

**Requisitos de Validação:**
- [ ] Export deve produzir JSON válido e completo
- [ ] Import deve validar schema e rejeitar JSON inválido
- [ ] Dados exportados devem ser importáveis sem perda
- [ ] Testes devem validar round-trip export/import

**Ficheiros a Criar:**
- `internal/commands/export.go`
- `internal/export/json.go`

**Dependências:** CRIT-003

---

## Roadmap de Implementação

```
Semana 1-2: P0 - Critical
├── CRIT-001: Retry Logic SQLite
├── CRIT-002: Testes Unitários DB
├── CRIT-003: Testes Unitários Commands
├── CRIT-004: Testes Unitários Utils
└── CRIT-005: Validar Nomes de Roadmap

Semana 3-4: P0/P1 - Critical/High
├── CRIT-002/003/004: Continuação de testes
├── HIGH-001: Validar Campos Obrigatórios
├── HIGH-002: Validação de IDs
├── HIGH-003: Melhorar Tratamento de Erros
└── HIGH-004: Testes de Integração

Semana 5-6: P1/P2 - High/Medium
├── MED-001: Timeouts em Operações DB
├── MED-002: Logging Estruturado
├── MED-003: Limites de Tamanho
├── MED-004: Whitelist de Campos
└── MED-005: Rate Limiting Audit

Semana 7-8: P2 - Medium
├── MED-006: Connection Pooling
├── MED-007: Testes de Race Condition
├── LOW-001: Revisar SetEscapeHTML
├── LOW-002: Validação de Datas
└── LOW-003: Verificação de Permissões

Semana 9+: P3/P4 - Low/Optional
├── LOW-004: Sanitização de Input
├── LOW-005: Melhorar Defer
├── LOW-006: Máquina de Estados
├── OPT-001: Métricas
└── OPT-002/003/004: Features opcionais
```

---

## Checklist de Conclusão

### P0 - Critical
- [ ] CRIT-001: Retry Logic SQLite
- [ ] CRIT-002: Testes Unitários DB
- [ ] CRIT-003: Testes Unitários Commands
- [ ] CRIT-004: Testes Unitários Utils
- [ ] CRIT-005: Validar Nomes de Roadmap

### P1 - High
- [ ] HIGH-001: Validar Campos Obrigatórios
- [ ] HIGH-002: Validação de IDs
- [ ] HIGH-003: Melhorar Tratamento de Erros
- [ ] HIGH-004: Testes de Integração

### P2 - Medium
- [ ] MED-001: Timeouts em Operações DB
- [ ] MED-002: Logging Estruturado
- [ ] MED-003: Limites de Tamanho
- [ ] MED-004: Whitelist de Campos
- [ ] MED-005: Rate Limiting Audit
- [ ] MED-006: Connection Pooling
- [ ] MED-007: Testes de Race Condition

### P3 - Low
- [ ] LOW-001: Revisar SetEscapeHTML
- [ ] LOW-002: Validação de Datas
- [ ] LOW-003: Verificação de Permissões
- [ ] LOW-004: Sanitização de Input
- [ ] LOW-005: Melhorar Defer
- [ ] LOW-006: Máquina de Estados

### P4 - Optional
- [ ] OPT-001: Métricas
- [ ] OPT-002: Backup/Restore
- [ ] OPT-003: Multi-User
- [ ] OPT-004: Export/Import JSON

---

## Notas de Implementação

1. **Ordem de Execução:** Seguir a ordem de prioridades (P0 → P1 → P2 → P3 → P4)
2. **Dependências:** Respeitar dependências indicadas em cada tarefa
3. **Testes:** Cada tarefa deve incluir testes unitários antes de ser considerada completa
4. **Documentação:** Atualizar SPEC/ conforme necessário para refletir mudanças
5. **Code Review:** Todas as alterações devem passar por code review antes de merge

---

*Plano de Implementação gerado em 2026-03-16*
*Baseado em Auditoria Completa Groadmap*
