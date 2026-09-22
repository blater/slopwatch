package sourceestimate

// operationLookup is owned by one analysis, after ownership and visibility
// annotations. Contextual operations query it by their current scope, never by
// a cached caller identity. Rebuilding after annotations invalidates all buckets.
type operationLookup struct {
	scoped, exposed, workspace map[string]*operationBucket
	work                       func(string)
}

type operationBucket struct {
	candidates []*operation
	files      map[int]int
	other      *operation
	runs       []operationRun
}

type operationRun struct{ file, start, end int }

func newOperationLookup() *operationLookup {
	return &operationLookup{scoped: map[string]*operationBucket{}, exposed: map[string]*operationBucket{}, workspace: map[string]*operationBucket{}}
}

func (index *operationLookup) observe(kind string) {
	if index.work != nil {
		index.work(kind)
	}
}

func (index *operationLookup) add(buckets map[string]*operationBucket, key string, op *operation) {
	index.observe("index_candidate")
	bucket := buckets[key]
	if bucket == nil {
		bucket = &operationBucket{files: map[int]int{}}
		buckets[key] = bucket
	}
	if len(bucket.candidates) > 0 && bucket.other == nil && op.file != bucket.candidates[0].file {
		bucket.other = op
	}
	position := len(bucket.candidates)
	if len(bucket.runs) == 0 || bucket.runs[len(bucket.runs)-1].file != op.file {
		bucket.runs = append(bucket.runs, operationRun{file: op.file, start: position, end: position + 1})
	} else {
		bucket.runs[len(bucket.runs)-1].end++
	}
	bucket.candidates = append(bucket.candidates, op)
	bucket.files[op.file]++
}

func indexOperation(index *operationLookup, op *operation) {
	index.add(index.scoped, operationKey(op), op)
	if op.exposed {
		index.add(index.exposed, operationKey(op), op)
	}
	if op.exposed && (op.language == "typescript" || op.language == "rust") {
		index.add(index.workspace, workspaceOperationKey(op, op.name, op.owner), op)
	}
}

// callSelection represents a bucket with optional caller-file exclusion. It
// never stores a filtered list per file. Counts and a unique identity take O(1).
type callSelection struct {
	bucket  *operationBucket
	exclude bool
	file    int
}

func (selection callSelection) count() int {
	if selection.bucket == nil {
		return 0
	}
	count := len(selection.bucket.candidates)
	if selection.exclude {
		count -= selection.bucket.files[selection.file]
	}
	return count
}

func (selection callSelection) unique() *operation {
	if selection.count() != 1 {
		return nil
	}
	values := selection.bucket.candidates
	if !selection.exclude || values[0].file != selection.file {
		return values[0]
	}
	// Use the first candidate from a different file, recorded at insertion.
	return selection.bucket.soleOutside(selection.file)
}

func (bucket *operationBucket) soleOutside(file int) *operation {
	// The first candidate outside each of the first two distinct files is
	// sufficient: a query excludes just one file.
	if bucket.candidates[0].file != file {
		return bucket.candidates[0]
	}
	return bucket.other
}

func (selection callSelection) each(index *operationLookup, visit func(*operation)) {
	if selection.bucket == nil {
		return
	}
	for _, run := range selection.bucket.runs {
		index.observe("edge_run")
		if selection.exclude && run.file == selection.file {
			continue
		}
		for _, candidate := range selection.bucket.candidates[run.start:run.end] {
			index.observe("edge_candidate")
			visit(candidate)
		}
	}
}
