# audit

## Descrição

Visualização do registo de auditoria e histórico de entidades. Todas as alterações a tarefas e sprints são automaticamente registadas para rastreabilidade.

## Sinopse

```
rmp audit [subcommand] [argumentos] [flags]
```

## Subcomandos

### list

Lista as entradas do registo de auditoria com filtros opcionais.

**Uso:** `rmp audit list [OPTIONS]` ou `rmp audit ls [OPTIONS]`

**Flags:**
| Flag Curta | Flag Longa | Tipo | Padrão | Descrição |
|------------|------------|------|--------|-----------|
| `-r` | `--roadmap` | string | - | Nome do roadmap (obrigatório) |
| `-o` | `--operation` | string | - | Filtrar por tipo de operação |
| `-e` | `--entity-type` | string | - | Filtrar por tipo de entidade: TASK, SPRINT |
| N/A | `--entity-id` | int | - | Filtrar por ID específico da entidade |
| N/A | `--since` | string | - | Incluir entradas a partir desta data (ISO 8601) |
| N/A | `--until` | string | - | Incluir entradas até esta data (ISO 8601) |
| `-l` | `--limit` | int | 100 | Limitar número de resultados |

**Tipos de Operação:**
- `TASK_CREATE`, `TASK_UPDATE`, `TASK_STATUS_CHANGE`, `TASK_PRIORITY_CHANGE`, `TASK_SEVERITY_CHANGE`, `TASK_DELETE`
- `SPRINT_CREATE`, `SPRINT_UPDATE`, `SPRINT_START`, `SPRINT_CLOSE`, `SPRINT_REOPEN`, `SPRINT_DELETE`
- `SPRINT_ADD_TASK`, `SPRINT_REMOVE_TASK`, `SPRINT_MOVE_TASK`

**Output:** JSON array de entradas de auditoria

**Exemplos:**
```bash
rmp audit list -r project1
rmp audit ls -r project1 -o TASK_STATUS_CHANGE
rmp audit ls -r project1 -e TASK --since 2026-03-01T00:00:00.000Z
```

---

### history

Mostra o histórico completo para uma entidade específica (tarefa ou sprint).

**Uso:** `rmp audit history [OPTIONS] <type> <id>` ou `rmp audit hist [OPTIONS] <type> <id>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `type` | Sim | Tipo de entidade: TASK, SPRINT |
| `id` | Sim | ID da entidade |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Output:** JSON array de entradas de auditoria para a entidade

**Exemplos:**
```bash
rmp audit history -r project1 -e TASK 42
rmp audit hist -r project1 -e SPRINT 1
```

---

### stats

Mostra estatísticas de auditoria incluindo contagens de operações e tendências.

**Uso:** `rmp audit stats [OPTIONS]`

**Flags:**
| Flag Curta | Flag Longa | Tipo | Padrão | Descrição |
|------------|------------|------|--------|-----------|
| `-r` | `--roadmap` | string | - | Nome do roadmap (obrigatório) |
| N/A | `--since` | string | - | Incluir entradas a partir desta data (ISO 8601) |
| N/A | `--until` | string | - | Incluir entradas até esta data (ISO 8601) |

**Output:** JSON object com estatísticas

**Exemplos:**
```bash
rmp audit stats -r project1
rmp audit stats -r project1 --since 2026-03-01T00:00:00.000Z
```

## Aliases

| Comando | Alias |
|---------|-------|
| `audit` | `aud` |
| `list` | `ls` |
| `history` | `hist` |

## Notas

- Todas as operações de criação, atualização e eliminação são automaticamente registadas
- O registo de auditoria é armazenado na tabela `audit` da base de dados SQLite
- Cada entrada de auditoria inclui: operação, tipo de entidade, ID da entidade, timestamp
- O histórico permite rastrear todas as alterações feitas a uma tarefa ou sprint específica
