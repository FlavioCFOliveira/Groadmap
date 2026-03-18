package db

import (
	"fmt"
)

// BatchProcessor handles chunking large ID lists into manageable batches.
// It processes items in chunks to avoid exceeding SQLite parameter limits
// and to enable query plan caching for common batch sizes.
type BatchProcessor struct {
	batchSize int
}

// NewBatchProcessor creates a batch processor with the specified chunk size.
// The batchSize should be chosen based on:
//   - SQLite parameter limits (SQLITE_LIMIT_VARIABLE_NUMBER, typically 999)
//   - Query cache efficiency (sizes 1-100, 250, 500, 1000 are cached)
//   - Memory usage for each batch
//
// Recommended batchSize: 100 (balances cache efficiency with batch overhead)
func NewBatchProcessor(batchSize int) *BatchProcessor {
	if batchSize <= 0 {
		batchSize = 100
	}
	return &BatchProcessor{batchSize: batchSize}
}

// ProcessChunks splits a slice of IDs into chunks and executes fn for each chunk.
// It processes chunks sequentially and stops on first error.
//
// Example usage:
//
// bp := NewBatchProcessor(100)
//
//	err := bp.ProcessChunks(ids, func(chunk []int) error {
//	    // Process this chunk
//	    return db.UpdateTasks(chunk)
//	})
func (bp *BatchProcessor) ProcessChunks(ids []int, fn func(chunk []int) error) error {
	for i := 0; i < len(ids); i += bp.batchSize {
		end := i + bp.batchSize
		if end > len(ids) {
			end = len(ids)
		}

		if err := fn(ids[i:end]); err != nil {
			return fmt.Errorf("processing batch %d-%d: %w", i, end, err)
		}
	}
	return nil
}

// ProcessChunksWithResult splits IDs into chunks and collects results.
// Similar to ProcessChunks but accumulates results from each chunk.
//
// Type parameter T represents the result type for each chunk operation.
func ProcessChunksWithResult[T any](ids []int, batchSize int, fn func(chunk []int) ([]T, error)) ([]T, error) {
	var allResults []T

	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}

		results, err := fn(ids[i:end])
		if err != nil {
			return nil, fmt.Errorf("processing batch %d-%d: %w", i, end, err)
		}

		allResults = append(allResults, results...)
	}

	return allResults, nil
}

// BatchSize returns the configured batch size.
func (bp *BatchProcessor) BatchSize() int {
	return bp.batchSize
}

// CalculateBatches returns the number of batches needed for the given ID count.
func (bp *BatchProcessor) CalculateBatches(totalIDs int) int {
	if totalIDs <= 0 {
		return 0
	}
	batches := totalIDs / bp.batchSize
	if totalIDs%bp.batchSize > 0 {
		batches++
	}
	return batches
}
