package graphstore

import (
	"path/filepath"

	"github.com/FlavioCFOliveira/GoGraph/store/wal"
)

// InspectLog reports the write-ahead log of the store in graphDir as the
// engine's own read-only inspection reads it: the log's control file, and
// whether it has one at all.
//
// A log with no control file is one the engine has never opened for writing in
// the segmented format: a store still in the single-file format, or a graph
// directory nothing has opened yet (SPEC/GRAPH.md § Graph Store Format
// Migration, rule 2). The inspection is the engine's and not a reading of the
// log's files by Groadmap, and it changes nothing on disk. It fails when the
// directory holds segments without a control file, or a control file the engine
// reports as damaged; the open would refuse the same directory.
func InspectLog(graphDir string) (wal.Control, bool, error) {
	log, err := wal.OpenLog(filepath.Join(graphDir, walFileName))
	if err != nil {
		return wal.Control{}, false, err
	}
	ctl, ok := log.Control()
	if err := log.Close(); err != nil {
		return wal.Control{}, false, err
	}
	return ctl, ok, nil
}
