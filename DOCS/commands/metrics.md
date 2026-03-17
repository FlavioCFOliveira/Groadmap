# metrics

## Descrição

Métricas e monitorização de operações. As métricas são recolhidas automaticamente durante as operações para análise de performance.

## Sinopse

```
rmp metrics [subcommand]
```

## Subcomandos

### show

Mostra as métricas atuais.

**Uso:** `rmp metrics show` ou `rmp metrics list` ou `rmp metrics` (sem subcomando)

**Output:** Tabela formatada com métricas de operações e contadores

**Exemplo:**
```bash
rmp metrics
rmp metrics show
```

**Output exemplo:**
```
=== Operation Metrics ===
Operation                          Count        Avg          Min          Max
--------------------------------------------------------------------------------
db_query                                150     2.34ms       1.20ms       5.67ms
task_create                              42    15.20ms       8.50ms      32.10ms
sprint_list                              28     1.50ms       0.80ms       3.20ms

=== Counter Metrics ===
Counter                              Value
--------------------------------------------------
total_operations                        250
errors                                    5
```

---

### reset

Limpa todas as métricas.

**Uso:** `rmp metrics reset`

**Exemplo:**
```bash
rmp metrics reset
```

**Output:**
```
Metrics reset successfully.
```

---

### enable

Ativa a recolha de métricas.

**Uso:** `rmp metrics enable`

**Exemplo:**
```bash
rmp metrics enable
```

**Output:**
```
Metrics collection enabled.
```

---

### disable

Desativa a recolha de métricas.

**Uso:** `rmp metrics disable`

**Exemplo:**
```bash
rmp metrics disable
```

**Output:**
```
Metrics collection disabled.
```

---

### status

Mostra o estado da recolha de métricas.

**Uso:** `rmp metrics status`

**Exemplo:**
```bash
rmp metrics status
```

**Output exemplo:**
```
Metrics collection: ENABLED
Operations tracked: 5
Counters tracked: 3
```

## Aliases

| Comando | Alias |
|---------|-------|
| `show` | `list` |

## Notas

- As métricas são recolhidas automaticamente durante as operações quando ativadas
- Cada operação rastreia: contagem, tempo médio, mínimo e máximo
- Os contadores são incrementados manualmente em pontos específicos do código
- As métricas são mantidas em memória e perdem-se quando a aplicação termina
- A recolha de métricas tem impacto mínimo na performance
