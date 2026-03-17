# Gocene Project Roadmap

**Project:** Gocene - Apache Lucene Port to Go
**Module:** `github.com/FlavioCFOliveira/Gocene`
**Last Updated:** 2026-03-12

---

## Overview

This roadmap outlines the complete development plan for porting Apache Lucene 10.x to idiomatic Go. The project follows a phased approach with critical foundation components first, followed by core index/search functionality, and finally advanced features.

---

## PENDING TASKS

| ID | SEVERITY | PRIORITY | TASK | SPECIALISTS | ACTIONABLE TECHNICAL DESCRIPTION |
| :--- | :--- | :--- | :--- | :--- | :--- |
| GC-078 | LOW | LOW | Implement QueryParser - QueryParser base | go-elite-developer | Create QueryParser for classic Lucene query syntax. Parse text queries into Query objects. Location: queryparser/query_parser.go |
| GC-079 | LOW | LOW | Implement QueryParser - QueryParserTokenManager | go-elite-developer | Create token manager for query parser using recursive descent or generated lexer. Location: queryparser/query_parser_token_manager.go |
| GC-080 | LOW | LOW | Implement Document - Numeric fields | go-elite-developer | Create IntField, LongField, FloatField, DoubleField with corresponding Point types for numeric indexing. Location: document/int_field.go, document/long_field.go, document/float_field.go, document/double_field.go |
| GC-081 | LOW | LOW | Implement Document - DocValues fields | go-elite-developer | Create NumericDocValuesField, BinaryDocValuesField, SortedDocValuesField, SortedSetDocValuesField types. Location: document/numeric_doc_values_field.go, document/binary_doc_values_field.go, document/sorted_doc_values_field.go, document/sorted_set_doc_values_field.go |
| GC-082 | LOW | LOW | Implement Search - PhraseQuery | go-elite-developer | Create PhraseQuery for exact phrase matching with optional slop parameter. Location: search/phrase_query.go |
| GC-084 | LOW | LOW | Implement Search - RangeQuery | go-elite-developer | Create TermRangeQuery and PointRangeQuery for range queries on terms and numeric points. Location: search/term_range_query.go, search/point_range_query.go |
| GC-085 | LOW | LOW | Implement Search - WildcardQuery | go-elite-developer | Create WildcardQuery for wildcard pattern matching (? and *). Location: search/wildcard_query.go |
| GC-086 | LOW | LOW | Implement Search - FuzzyQuery | go-elite-developer | Create FuzzyQuery for fuzzy/approximate string matching with edit distance parameter. Location: search/fuzzy_query.go |
| GC-093 | LOW | LOW | Implement Search - DisjunctionMaxQuery | go-elite-developer | Create DisjunctionMaxQuery for disjunction with maximum scoring (useful for searching across fields). Location: search/disjunction_max_query.go |
| GC-094 | LOW | LOW | Implement Search - BoostQuery | go-elite-developer | Create BoostQuery wrapping another Query with score multiplier. Location: search/boost_query.go |
| GC-095 | LOW | LOW | Implement Search - ConstantScoreQuery | go-elite-developer | Create ConstantScoreQuery wrapping another Query with constant score. Location: search/constant_score_query.go |
| GC-096 | LOW | LOW | Implement Search - ClassicSimilarity | go-elite-developer | Implement ClassicSimilarity with TF/IDF scoring as alternative to BM25. Location: search/classic_similarity.go |
| GC-097 | LOW | LOW | Implement Analysis - WhitespaceTokenizer | go-elite-developer | Create WhitespaceTokenizer splitting on whitespace characters only. Location: analysis/whitespace_tokenizer.go |
| GC-098 | LOW | LOW | Implement Analysis - LetterTokenizer | go-elite-developer | Create LetterTokenizer tokenizing sequences of letters. Location: analysis/letter_tokenizer.go |
| GC-099 | LOW | LOW | Implement Analysis - WhitespaceAnalyzer | go-elite-developer | Create WhitespaceAnalyzer using WhitespaceTokenizer without lowercasing. Location: analysis/whitespace_analyzer.go |
| GC-100 | LOW | LOW | Implement Analysis - SimpleAnalyzer | go-elite-developer | Create SimpleAnalyzer using LetterTokenizer + LowerCaseFilter. Location: analysis/simple_analyzer.go |
| GC-104 | LOW | LOW | Implement Search - MoreLikeThis | go-elite-developer | Create MoreLikeThis for finding similar documents based on term frequency analysis. Location: search/more_like_this.go |
| GC-105 | LOW | HIGH | Implementar Cache de Termos | go-elite-developer,go-performance-advisor | Criar cache LRU para termos frequentemente acessados, reduzindo I/O em consultas repetidas. Location: index/term_cache.go |

---

## Implementation Phases

### Phase 8: Simple Query Types
**Status:** COMPLETED | **Tasks:** 3 | **Completed:** 2026-03-16
**Focus:** Basic query implementations building on existing infrastructure
**Dependencies:** Phase 5 (Search Framework)

| Task ID | Task Name | Specialists | SEVERITY | PRIORITY |
|:--------|:----------|:------------|:---------|:---------|
| GC-087 | MatchAllDocsQuery | go-elite-developer | LOW | LOW |
| GC-103 | FieldExistsQuery | go-elite-developer | LOW | LOW |
| GC-083 | PrefixQuery | go-elite-developer | LOW | LOW |

**Dependencies:** Phase 5 (GC-053 through GC-067)

### Phase 9: Additional Analysis Components
**Tasks:** GC-097 through GC-100
**Focus:** Additional tokenizers and analyzers
**Dependencies:** Phase 3 (Analysis Pipeline)

### Phase 10: Complex Query Types
**Tasks:** GC-082, GC-084, GC-085, GC-086
**Focus:** Advanced query implementations (Phrase, Range, Wildcard, Fuzzy)
**Dependencies:** Phase 8 (Simple Query Types)

### Phase 11: Query Wrapper Types
**Tasks:** GC-093, GC-094, GC-095
**Focus:** Query decorators and combiners
**Dependencies:** Phase 8 (Simple Query Types)

### Phase 12: Alternative Similarity
**Tasks:** GC-096
**Focus:** TF/IDF scoring implementation
**Dependencies:** Phase 5 (Search Framework)

### Phase 13: QueryParser
**Tasks:** GC-078, GC-079
**Focus:** Query syntax parsing from text
**Dependencies:** Phases 8, 10, 11 (Query implementations must be complete)
**Status:** BLOCKED until query types are implemented

### Phase 14: Advanced Features (Blocked)
**Tasks:** GC-080, GC-081, GC-104
**Focus:** Numeric fields with Point indexing, DocValues, MoreLikeThis
**Dependencies:** Missing infrastructure (Point indexing, DocValues format, Term vectors)
**Status:** BLOCKED - requires additional infrastructure development

---

## DEVELOPMENT PHASES (Auto-Generated)

| Phase | Status | Tasks | Focus | Dependencies |
|:------|:-------|:------|:------|:-------------|
| 8 | COMPLETED | GC-087, GC-103, GC-083 | Simple Query Types | Phase 5 |
| 9 | PENDING | GC-097 to GC-100 | Additional Analysis | Phase 3 |
| 10 | PENDING | GC-082, GC-084 to GC-086 | Complex Query Types | Phase 8 |
| 11 | PENDING | GC-093 to GC-095 | Query Wrapper Types | Phase 8 |
| 12 | PENDING | GC-096 | Alternative Similarity | Phase 5 |
| 13 | PENDING | GC-078 to GC-079 | QueryParser | Phases 8, 10, 11 |
| 14 | BLOCKED | GC-080 to GC-081, GC-104 | Advanced Features | Infrastructure |

---

## Component Dependencies

```
                    QueryParser
                         |
        Analysis ----+   +---- Search ---- Similarity
            |        |          |
            +--------+----------+
                         |
                       Index
                  (Writer/Reader)
                         |
        +----------------+-------------+
        |                |             |
     Codec            Store        Document
        |
    MergePolicy
```

**Dependency Order:** Store -> Document -> Index (Core) -> Analysis -> Search -> Codec/Merge

---

## Task Status Legend

- **HIGH Severity:** Critical foundation components - must be implemented first
- **MEDIUM Severity:** Core functionality - required for basic search capability
- **LOW Severity:** Extended features - can be deferred until core is complete

---

## Audit References

- Lucene Architecture Audit: `./AUDIT/lucene_architecture_audit.md`
- Last Audit Date: 2026-03-11
- Lucene Version Analyzed: Apache Lucene 10.x

---

## Replanning Summary (2026-03-15)

### Phase Breakdown of Remaining Tasks (GC-078 to GC-104)

The Phase 8 (Query Parser + Advanced Features) has been replanned into 7 distinct phases based on dependency analysis:

**Phase 8: Simple Query Types (3 tasks)**
- GC-087: MatchAllDocsQuery - matches all documents
- GC-103: FieldExistsQuery - find documents with specific field
- GC-083: PrefixQuery - prefix matching on terms
- *Dependencies: Phase 5 (Search Framework)*

**Phase 9: Additional Analysis (4 tasks)**
- GC-097: WhitespaceTokenizer
- GC-098: LetterTokenizer
- GC-099: WhitespaceAnalyzer
- GC-100: SimpleAnalyzer
- *Dependencies: Phase 3 (Analysis Pipeline)*

**Phase 10: Complex Query Types (4 tasks)**
- GC-082: PhraseQuery - exact phrase matching with slop
- GC-084: RangeQuery - term and point range queries
- GC-085: WildcardQuery - pattern matching (? and *)
- GC-086: FuzzyQuery - approximate matching with edit distance
- *Dependencies: Phase 8 (Simple Query Types)*

**Phase 11: Query Wrapper Types (3 tasks)**
- GC-093: DisjunctionMaxQuery - disjunction with max scoring
- GC-094: BoostQuery - score multiplier wrapper
- GC-095: ConstantScoreQuery - constant score wrapper
- *Dependencies: Phase 8 (Simple Query Types)*

**Phase 12: Alternative Similarity (1 task)**
- GC-096: ClassicSimilarity - TF/IDF scoring
- *Dependencies: Phase 5 (Search Framework)*

**Phase 13: QueryParser (2 tasks) - BLOCKED**
- GC-078: QueryParser - classic Lucene query syntax parser
- GC-079: QueryParserTokenManager - token manager for parser
- *Dependencies: Phases 8, 10, 11 (all query types must exist)*
- *Status: BLOCKED until query implementations are complete*

**Phase 14: Advanced Features (3 tasks) - BLOCKED**
- GC-080: Numeric Fields - IntField, LongField, FloatField, DoubleField with Point types
- GC-081: DocValues Fields - columnar storage for sorting/faceting
- GC-104: MoreLikeThis - similar document finding
- *Dependencies: Missing infrastructure (Point indexing, DocValues format, Term vectors)*
- *Status: BLOCKED - requires significant infrastructure development*

### Critical Infrastructure Gaps Identified

1. **Point Indexing (BKD Tree)**: Required for proper numeric field range queries (GC-080)
2. **DocValues Format**: Required for DocValues field storage and retrieval (GC-081)
3. **Term Vectors**: Required for MoreLikeThis feature (GC-104)

### Recommended Implementation Order

1. Complete Phase 8 (Simple Query Types)
2. Complete Phase 9 (Additional Analysis) - can be done in parallel with Phase 8
3. Complete Phase 10 (Complex Query Types)
4. Complete Phase 11 (Query Wrapper Types)
5. Complete Phase 12 (ClassicSimilarity)
6. Implement Phase 13 (QueryParser) - after all query types are ready
7. Plan and implement infrastructure for Phase 14 (requires new tasks)

---

*End of Roadmap*
