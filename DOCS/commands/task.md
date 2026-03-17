# task

## Descrição

Gestão de tarefas dentro de um roadmap. As tarefas seguem o trabalho com status, prioridade, severidade e descrições detalhadas.

## Sinopse

```
rmp task [subcommand] [argumentos] [flags]
```

## Subcomandos

### list

Lista as tarefas no roadmap selecionado.

**Uso:** `rmp task list [OPTIONS]` ou `rmp task ls [OPTIONS]`

**Flags:**
| Flag Curta | Flag Longa | Tipo | Padrão | Descrição |
|------------|------------|------|--------|-----------|
| `-r` | `--roadmap` | string | - | Nome do roadmap (obrigatório se não houver padrão) |
| `-s` | `--status` | string | - | Filtrar por status: BACKLOG, SPRINT, DOING, TESTING, COMPLETED |
| `-p` | `--priority` | int | - | Filtrar por prioridade mínima (0-9) |
| N/A | `--severity` | int | - | Filtrar por severidade mínima (0-9) |
| `-l` | `--limit` | int | - | Limitar número de resultados |

**Output:** JSON array de objetos Task

**Exemplos:**
```bash
rmp task list -r project1
rmp task ls -r project1 -s DOING
rmp task ls -r project1 -p 5 -l 20
```

---

### create

Cria uma nova tarefa no roadmap especificado.

**Uso:** `rmp task create [OPTIONS]` ou `rmp task new [OPTIONS]`

**Flags Obrigatórias:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap |
| `-d` | `--description` | string | Descrição da tarefa |
| `-a` | `--action` | string | Ação técnica a realizar |
| `-e` | `--expected-result` | string | Resultado esperado |

**Flags Opcionais:**
| Flag Curta | Flag Longa | Tipo | Padrão | Descrição |
|------------|------------|------|--------|-----------|
| `-p` | `--priority` | int | 0 | Prioridade 0-9 |
| N/A | `--severity` | int | 0 | Severidade 0-9 |
| `-sp` | `--specialists` | string | - | Lista de especialistas separados por vírgula |

**Output:** JSON object com o ID da tarefa criada

**Exemplos:**
```bash
rmp task create -r project1 -d "Fix login bug" -a "Debug auth" -e "Login works"
rmp task new -r project1 -d "Update docs" -a "Write README" -e "Docs complete" -p 5
```

**Output exemplo:**
```json
{"id": 42}
```

---

### get

Obtém informação detalhada sobre uma ou mais tarefas.

**Uso:** `rmp task get [OPTIONS] <ids>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `ids` | Sim | IDs das tarefas separados por vírgula (sem espaços) |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Output:** JSON array de objetos Task

**Exemplos:**
```bash
rmp task get -r project1 42
rmp task get -r project1 1,2,3,10
```

---

### set-status (stat)

Altera o status de uma ou mais tarefas.

**Uso:** `rmp task set-status [OPTIONS] <ids> <state>` ou `rmp task stat [OPTIONS] <ids> <state>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `ids` | Sim | IDs das tarefas separados por vírgula |
| `state` | Sim | Novo status: BACKLOG, SPRINT, DOING, TESTING, COMPLETED |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Fluxo de Status:**
```
BACKLOG ↔ SPRINT ↔ DOING ↔ TESTING → COMPLETED
```

**Exemplos:**
```bash
rmp task set-status -r project1 42 DOING
rmp task stat -r project1 1,2,3 COMPLETED
```

---

### set-priority (prio)

Altera a prioridade de uma ou mais tarefas.

**Uso:** `rmp task set-priority [OPTIONS] <ids> <priority>` ou `rmp task prio [OPTIONS] <ids> <priority>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `ids` | Sim | IDs das tarefas separados por vírgula |
| `priority` | Sim | Valor de prioridade 0-9 |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Escala de Prioridade:**
- 0 = baixa urgência
- 9 = urgência máxima (perspetiva do Product Owner)

**Exemplos:**
```bash
rmp task set-priority -r project1 42 9
rmp task prio -r project1 1,2,3 5
```

---

### set-severity (sev)

Altera a severidade de uma ou mais tarefas.

**Uso:** `rmp task set-severity [OPTIONS] <ids> <severity>` ou `rmp task sev [OPTIONS] <ids> <severity>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `ids` | Sim | IDs das tarefas separados por vírgula |
| `severity` | Sim | Valor de severidade 0-9 |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Escala de Severidade:**
- 0 = impacto mínimo
- 9 = impacto crítico (perspetiva da Equipa de Dev)

**Exemplos:**
```bash
rmp task set-severity -r project1 42 5
rmp task sev -r project1 1,2,3 9
```

---

### edit

Edita as propriedades de uma tarefa existente. Apenas os campos especificados são atualizados.

**Uso:** `rmp task edit [OPTIONS] <id>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `id` | Sim | ID da tarefa a editar |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |
| `-d` | `--description` | string | Nova descrição |
| `-a` | `--action` | string | Nova ação |
| `-e` | `--expected-result` | string | Novo resultado esperado |
| `-p` | `--priority` | int | Nova prioridade (0-9) |
| N/A | `--severity` | int | Nova severidade (0-9) |
| `-sp` | `--specialists` | string | Novos especialistas |

**Exemplos:**
```bash
rmp task edit -r project1 42 -d "New description" -p 7
rmp task edit -r project1 1 --specialists "go-developer"
```

---

### remove

Remove uma ou mais tarefas permanentemente. Esta ação não pode ser desfeita.

**Uso:** `rmp task remove [OPTIONS] <ids>` ou `rmp task rm [OPTIONS] <ids>`

**Argumentos:**
| Argumento | Obrigatório | Descrição |
|-----------|-------------|-----------|
| `ids` | Sim | IDs das tarefas separados por vírgula |

**Flags:**
| Flag Curta | Flag Longa | Tipo | Descrição |
|------------|------------|------|-----------|
| `-r` | `--roadmap` | string | Nome do roadmap (obrigatório) |

**Exemplos:**
```bash
rmp task remove -r project1 42
rmp task rm -r project1 1,2,3
```

## Aliases

| Comando | Alias |
|---------|-------|
| `task` | `t` |
| `list` | `ls` |
| `create` | `new` |
| `remove` | `rm` |
| `set-status` | `stat` |
| `set-priority` | `prio` |
| `set-severity` | `sev` |

## Notas

- As tarefas são criadas com status `BACKLOG` por padrão
- A transição de status é validada (não é possível ir de `COMPLETED` para `BACKLOG` diretamente)
- Quando uma tarefa é marcada como `COMPLETED`, o campo `completed_at` é preenchido automaticamente
- A flag `-r`/`--roadmap` pode ser omitida se um roadmap padrão tiver sido definido com `rmp roadmap use`
