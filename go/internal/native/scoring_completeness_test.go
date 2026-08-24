package native

import (
	"fmt"
	"testing"
)

func TestFortyFiveThousandZeroScoreJavaFilesRemainInReport(t *testing.T) {
	const count = 45_000
	inputs := newScoreInputs()
	for index := 0; index < count; index++ {
		path := fmt.Sprintf("src/main/java/example/Class%05d.java", index)
		inputs.coverage[path] = map[string]string{}
		inputs.languages[path] = "java"
	}
	document, err := scoreInputsReport(catalogDocument{}, []string{"java"}, inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Files) != count || document.ReturnedFiles != count {
		t.Fatalf("zero-score Java rows = %d/%d, want %d", len(document.Files), document.ReturnedFiles, count)
	}
	for _, file := range document.Files {
		if file.Score != 0 || !file.ValidZero {
			t.Fatalf("zero-score file was changed: %#v", file)
		}
	}
}
