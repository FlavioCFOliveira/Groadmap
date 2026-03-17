# sprint

## Descrição

Gestão de sprints dentro de um roadmap. Os sprints agrupam tarefas em iterações time-boxed com gestão de ciclo de vida (PENDING → OPEN → CLOSED).

## Sinopse

```
rmp sprint [subcommand] [argumentos] [flags]
```

## Subcomandos

### list

Lista os sprints no roadmap selecionado.

**Uso:** `rmp sprint list [OPTIONS]` ou `rmp sprint ls [OPTIONS]`

**Flags:**
| Flag Curta | Flag Longa | Tipo | Padrão | Descrição |
|------------|------------|------|--------|-----------|
| `-r` | `--roadmap` | string | - | Nome do roadmap (obrigatório se não houver padrão) |
| `-s` | `--status` | string | - | Filtrar por status: PENDING, OPEN, CLOSED |

**Output:** JSON array de objetos Sprint

**Exemplos:**
```bash
rmp sprint list -r project1
rmp sprint ls -r project1 -s OPEN
```

---

### create

Cria um novo sprint no roadmap especificado.

**Uso:** `rmp sprint create [OPTIONS]` ou `rmp sprint new [OPTIONS]`

**Flags:**
| Flag Curta | Flag Longa | Tipo | Padrão | Descrição |
|------------|------------|------|--------|-----------|
| `-r` | `--roadmap` | string | - | Nome do roadmap (obrigatório) |
| `-d` | `--description` | string | - | Descrição do sprint (obrigatório) |

**Output:** JSON object com o ID do sprint criado

**Exemplos:**
```bash
rmp sprint create -r project1 -d "Sprint 1 - Initial Setup"
rmp sprint new -r project1 -d "Sprint 2 - Features"
```

**Output exemplo:**
```json
{"id": 1}
```

---

### get

Obtém informação detalhada sobre um sprint específico.

**Uso:** `rmp sprint get [OPTIONS] <id>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `id` | Sim | ID do sprint |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Output:** JSON object Sprint

**Exemplo:**
```bash
rmp sprint get -r project1 1
```

---

### tasks

Lista as tarefas atribuídas a um sprint específico.

**Uso:** `rmp sprint tasks [OPTIONS] <sprint-id>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `sprint-id` | Sim | ID do sprint |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Padrão | Descrição |
|------------|------------|------|--------|-----------|
| `-r` | `--roadmap` | string | - | Nome do roadmap (obrigatório) |
| `-s` | `--status` | string | - | Filtrar por status da tarefa |

**Output:** JSON array de objetos Task

**Exemplos:**
```bash
rmp sprint tasks -r project1 1
rmp sprint tasks -r project1 1 -s DOING
```

---

### stats

Mostra estatísticas de um sprint incluindo contagens de tarefas por status.

**Uso:** `rmp sprint stats [OPTIONS] <id>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `id` | Sim | ID do sprint |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Output:** JSON object com estatísticas

**Exemplo:**
```bash
rmp sprint stats -r project1 1
```

**Output exemplo:**
```json
{
  "sprint_id": 1,
  "total_tasks": 10,
  "completed_tasks": 3,
  "progress_percentage": 30.0,
  "status_distribution": {
    "SPRINT": 4,
    "DOING": 2,
    "TESTING": 1,
    "COMPLETED": 3
  }
}
```

---

### start

Inicia um sprint, alterando o seu status de PENDING para OPEN.

**Uso:** `rmp sprint start [OPTIONS] <id>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `id` | Sim | ID do sprint a iniciar |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Exemplo:**
```bash
rmp sprint start -r project1 1
```

---

### close

Fecha um sprint, alterando o seu status de OPEN para CLOSED.

**Uso:** `rmp sprint close [OPTIONS] <id>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `id` | Sim | ID do sprint a fechar |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Exemplo:**
```bash
rmp sprint close -r project1 1
```

---

### reopen

Reabre um sprint fechado, alterando o seu status de CLOSED para OPEN.

**Uso:** `rmp sprint reopen [OPTIONS] <id>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `id` | Sim | ID do sprint a reabrir |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Exemplo:**
```bash
rmp sprint reopen -r project1 1
```

---

### add-tasks

Adiciona tarefas a um sprint. As tarefas devem estar em status BACKLOG.

**Uso:** `rmp sprint add-tasks [OPTIONS] <sprint-id> <task-ids>` ou `rmp sprint add [OPTIONS] <sprint-id> <task-ids>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `sprint-id` | Sim | ID do sprint para adicionar tarefas |
| `task-ids` | Sim | IDs das tarefas separados por vírgula (sem espaços) |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Exemplos:**
```bash
rmp sprint add-tasks -r project1 1 10,11,12
rmp sprint add -r project1 2 5,6,7,8
```

---

### remove-tasks

Remove tarefas de um sprint. As tarefas voltam ao status BACKLOG.

**Uso:** `rmp sprint remove-tasks [OPTIONS] <sprint-id> <task-ids>` ou `rmp sprint rm-tasks [OPTIONS] <sprint-id> <task-ids>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `sprint-id` | Sim | ID do sprint para remover tarefas |
| `task-ids` | Sim | IDs das tarefas separados por vírgula |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Exemplos:**
```bash
rmp sprint remove-tasks -r project1 1 10,11,12
rmp sprint rm-tasks -r project1 1 5,6
```

---

### move-tasks

Move tarefas entre sprints.

**Uso:** `rmp sprint move-tasks [OPTIONS] <from-sprint> <to-sprint> <task-ids>` ou `rmp sprint mv-tasks [OPTIONS] <from-sprint> <to-sprint> <task-ids>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `from-sprint` | Sim | ID do sprint de origem |
| `to-sprint` | Sim | ID do sprint de destino |
| `task-ids` | Sim | IDs das tarefas separados por vírgula |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Exemplos:**
```bash
rmp sprint move-tasks -r project1 1 2 10,11,12
rmp sprint mv-tasks -r project1 2 3 5,6,7
```

---

### update

Atualiza a descrição de um sprint.

**Uso:** `rmp sprint update [OPTIONS] <id>` ou `rmp sprint upd [OPTIONS] <id>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `id` | Sim | ID do sprint |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |
| `-d` | `--description` | string | Nova descrição (obrigatório) |

**Exemplos:**
```bash
rmp sprint update -r project1 1 -d "Sprint 1 - Setup and Config"
rmp sprint upd -r project1 1 -d "Updated description"
```

---

### remove

Remove um sprint permanentemente. As tarefas no sprint não são eliminadas.

**Uso:** `rmp sprint remove [OPTIONS] <id>` ou `rmp sprint rm [OPTIONS] <id>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `id` | Sim | ID do sprint a remover |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Exemplos:**
```bash
rmp sprint remove -r project1 1
rmp sprint rm -r project1 2
```

## Aliases

| Comando | Alias |
|---------|-------|
| `sprint` | `s` |
| `list` | `ls` |
| `create` | `new` |
| `remove` | `rm` |
| `update` | `upd` |
| `add-tasks` | `add` |
| `remove-tasks` | `rm-tasks` |
| `move-tasks` | `mv-tasks` |

## Ciclo de Vida do Sprint

```
PENDING → OPEN → CLOSED
   ↑              ↓
   └──────────────┘ (reopen)
```

## Notas

- Sprints são criados com status `PENDING` por padrão
- As transições de estado são validadas (não é possível fechar um sprint já fechado)
- Ao remover um sprint, as tarefas associadas voltam ao status `BACKLOG`
- Ao adicionar tarefas a um sprint, o status das tarefas muda para `SPRINT`
