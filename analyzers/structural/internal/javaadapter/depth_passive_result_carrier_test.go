package javaadapter

import (
	"slopslap.dev/structural/internal/facts"
	"slopslap.dev/structural/internal/metrics"
	"testing"
)

func hasJavaDepthEvidence(boundary *facts.BoundaryAssessment, kind, status string) bool {
	for _, evidence := range boundary.Evidence {
		if evidence.Kind == kind && evidence.Status == status {
			return true
		}
	}
	return false
}

func TestJavaDepthPassiveResultCarrierUsesStructure(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "RenamedResult.java", `
enum ResultStatus { EMPTY, READY }
public final class RenamedResult {
    private int value;
    private ResultStatus status = ResultStatus.EMPTY;
    public int currentValue() { return value; }
    public ResultStatus currentStatus() { return status; }
    public void replace(int value, ResultStatus status) {
        this.value = value;
        this.status = status;
    }
    public void reset() {
        value = 0;
        status = ResultStatus.EMPTY;
    }
}`)
	program, err := adapter.Analyze(root, []string{"RenamedResult.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	boundary := javaDepthBoundaryBySymbol(t, program, "RenamedResult")
	if !hasJavaDepthEvidence(boundary, "passive-result-carrier-v1", "proven") {
		t.Fatalf("renamed passive result carrier was not proven: %#v", boundary)
	}

	writeSource(t, root, "RenamedResult.java", `
enum ResultStatus { EMPTY, READY }
public final class RenamedResult {
    private int value;
    private ResultStatus status = ResultStatus.EMPTY;
    public int currentValue() { return value; }
    public ResultStatus currentStatus() { return status; }
    public void replace(int value, ResultStatus status) {
        if (value < 0) throw new IllegalArgumentException();
        this.value = value;
        this.status = status;
    }
    public void reset() { value = 0; status = ResultStatus.EMPTY; }
}`)
	program, err = adapter.Analyze(root, []string{"RenamedResult.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	boundary = javaDepthBoundaryBySymbol(t, program, "RenamedResult")
	if hasJavaDepthEvidence(boundary, "passive-result-carrier-v1", "proven") {
		t.Fatalf("validated result carrier was exempted: %#v", boundary)
	}

	for _, source := range []string{
		`enum ResultStatus { EMPTY, READY }
public final class RenamedResult {
    private int value;
    private ResultStatus status = ResultStatus.EMPTY;
    public int currentValue() { return value + 1; }
    public ResultStatus currentStatus() { return status; }
    public void replace(int value, ResultStatus status) { this.value = value; this.status = status; }
    public void reset() { value = 0; status = ResultStatus.EMPTY; }
}`,
		`enum ResultStatus { EMPTY, READY }
public final class RenamedResult {
    private int value;
    private ResultStatus status = ResultStatus.EMPTY;
    public int currentValue() { return value; }
    public ResultStatus currentStatus() { return status; }
    public void replace(int value, ResultStatus status) { this.value = value; this.status = status; }
    public void reset() { value = value + 1; status = ResultStatus.EMPTY; }
}`,
	} {
		writeSource(t, root, "RenamedResult.java", source)
		program, err = adapter.Analyze(root, []string{"RenamedResult.java"}, map[string]any{"depth_profile": "responsibility-v4"})
		if err != nil {
			t.Fatal(err)
		}
		boundary = javaDepthBoundaryBySymbol(t, program, "RenamedResult")
		if hasJavaDepthEvidence(boundary, "passive-result-carrier-v1", "proven") {
			t.Fatalf("impure result carrier was exempted: %#v", boundary)
		}
	}
}

func TestJavaDepthDirectoryOperationResultShapeIsMeasuredZero(t *testing.T) {
	root, adapter := javaTestAdapter(t)
	writeSource(t, root, "DirectoryOperationResultCopy.java", `
enum CopyStatus { NOT_APPLIED, VISIBLE }
final class FileToken {}
public final class DirectoryOperationResultCopy {
    private FileToken file;
    private CopyStatus durability = CopyStatus.NOT_APPLIED;
    public FileToken resultFile() { return file; }
    public CopyStatus resultDurability() { return durability; }
    public void setResult(FileToken file, CopyStatus durability) {
        this.file = file;
        this.durability = durability;
    }
    public void resetResult() {
        file = null;
        durability = CopyStatus.NOT_APPLIED;
    }
}`)
	program, err := adapter.Analyze(root, []string{"DirectoryOperationResultCopy.java"}, map[string]any{"depth_profile": "responsibility-v4"})
	if err != nil {
		t.Fatal(err)
	}
	score := scoreForJavaBoundary(metrics.MeasureDepth(program), "DirectoryOperationResultCopy")
	if score == nil || score.State != facts.KnowledgeMeasured || score.Shallow == nil || *score.Shallow != 0 {
		t.Fatalf("directory operation result copy was not measured zero: %#v", score)
	}
	boundary := javaDepthBoundaryBySymbol(t, program, "DirectoryOperationResultCopy")
	if !hasJavaDepthEvidence(boundary, "passive-result-carrier-v1", "proven") {
		t.Fatalf("directory operation result proof missing: %#v", boundary)
	}
}
