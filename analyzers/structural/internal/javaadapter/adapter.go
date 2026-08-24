// Package javaadapter bridges the JDK compiler-tree parser into normalized structural facts.
package javaadapter

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"unicode/utf16"

	"slopslap.dev/structural/internal/facts"
)

const (
	helperName            = "slopslap-structural-java.jar"
	requestMagic          = uint32(0x53534a46)
	responseMagic         = uint32(0x53534a4f)
	parallelMinPaths      = 512
	parallelBatchPathGoal = 512
	parallelBatchLimit    = 2
)

// Adapter invokes the bundled Java parser without annotation processing or project execution.
type Adapter struct {
	JavaExecutable string
	HelperJar      string
}

func defaultHelperJar() string {
	host, err := os.Executable()
	if err != nil {
		return filepath.Join("adapters", "java", "build", helperName)
	}
	directory := filepath.Dir(host)
	for _, candidate := range []string{
		filepath.Join(directory, helperName),
		filepath.Join(directory, "adapters", "java", "build", helperName),
	} {
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate
		}
	}
	return filepath.Join(directory, helperName)
}

func defaultJavaExecutable() string {
	host, err := os.Executable()
	if err != nil {
		return filepath.Join("java-runtime", "bin", "java")
	}
	return filepath.Join(filepath.Dir(host), "java-runtime", "bin", "java")
}

func (Adapter) Language() string       { return "java" }
func (Adapter) FactSchemaVersion() int { return facts.SchemaVersion }
func (Adapter) ParserModes() []string  { return []string{"jdk-compiler-tree-no-processing"} }

// Analyze requests facts for the exact canonical inventory from the Java helper.
func (adapter Adapter) Analyze(workspace string, paths []string, options map[string]any) (*facts.Program, error) {
	java := adapter.JavaExecutable
	if configured, ok := options["java_path"].(string); java == "" && ok {
		java = configured
	}
	if java == "" {
		java = defaultJavaExecutable()
	}
	jar := adapter.HelperJar
	if jar == "" {
		jar = defaultHelperJar()
	}
	includeTests, _ := options["include_tests"].(bool)
	program, err := analyzePaths(paths, func(batch []string) (*facts.Program, error) {
		return analyzeBatch(java, jar, workspace, batch, includeTests)
	})
	if err != nil {
		return nil, err
	}
	if err := program.LinkTypeMethods(); err != nil {
		return nil, fmt.Errorf("link Java method facts: %w", err)
	}
	return program, nil
}

func analyzeBatch(java, jar, workspace string, paths []string, includeTests bool) (*facts.Program, error) {
	var input bytes.Buffer
	if err := writeRequest(&input, workspace, paths, includeTests); err != nil {
		return nil, fmt.Errorf("encode Java fact request: %w", err)
	}
	command := javaCommand(java, jar)
	command.Stdin = &input
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open Java fact adapter output: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("Java fact adapter failed: %w: %s", err, stderr.String())
	}
	program, decodeErr := readResponse(stdout)
	if decodeErr != nil {
		_, _ = io.Copy(io.Discard, stdout)
	}
	if err := command.Wait(); err != nil {
		return nil, fmt.Errorf("Java fact adapter failed: %w: %s", err, stderr.String())
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("decode Java facts: %w", decodeErr)
	}
	return program, nil
}

func javaCommand(java, jar string) *exec.Cmd {
	return exec.Command(java, "-XX:TieredStopAtLevel=1", "-XX:+UseSerialGC", "-jar", jar)
}

type batchResult struct {
	program *facts.Program
	err     error
}

func analyzePaths(paths []string, analyze func([]string) (*facts.Program, error)) (*facts.Program, error) {
	batchCount := javaBatchCount(len(paths))
	if batchCount == 1 {
		return analyze(paths)
	}

	results := make([]batchResult, batchCount)
	var workers sync.WaitGroup
	workers.Add(batchCount)
	start := 0
	for index := 0; index < batchCount; index++ {
		remaining := len(paths) - start
		batchSize := (remaining + batchCount - index - 1) / (batchCount - index)
		batch := paths[start : start+batchSize]
		start += batchSize
		go func(index int, batch []string) {
			defer workers.Done()
			results[index].program, results[index].err = analyze(batch)
		}(index, batch)
	}
	workers.Wait()

	programs := make([]*facts.Program, batchCount)
	for index, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		programs[index] = result.program
	}
	return mergePrograms(programs), nil
}

func javaBatchCount(pathCount int) int {
	if pathCount < parallelMinPaths {
		return 1
	}
	count := (pathCount + parallelBatchPathGoal - 1) / parallelBatchPathGoal
	if count > parallelBatchLimit {
		return parallelBatchLimit
	}
	return count
}

func mergePrograms(programs []*facts.Program) *facts.Program {
	merged := &facts.Program{Unavailable: make(map[string]map[string]string)}
	for _, program := range programs {
		merged.Functions = append(merged.Functions, program.Functions...)
		merged.Types = append(merged.Types, program.Types...)
		merged.PublicOperations = append(merged.PublicOperations, program.PublicOperations...)
		merged.Representation = append(merged.Representation, program.Representation...)
		merged.Files = append(merged.Files, program.Files...)
		for path, components := range program.Unavailable {
			if merged.Unavailable[path] == nil {
				merged.Unavailable[path] = make(map[string]string, len(components))
			}
			for component, reason := range components {
				merged.Unavailable[path][component] = reason
			}
		}
	}
	javaOrder := make(map[string][]uint16, len(merged.Files))
	ordered := func(value string) []uint16 {
		if encoded, ok := javaOrder[value]; ok {
			return encoded
		}
		encoded := utf16.Encode([]rune(value))
		javaOrder[value] = encoded
		return encoded
	}
	lessString := func(left, right string) bool {
		leftUnits, rightUnits := ordered(left), ordered(right)
		for index := 0; index < len(leftUnits) && index < len(rightUnits); index++ {
			if leftUnits[index] != rightUnits[index] {
				return leftUnits[index] < rightUnits[index]
			}
		}
		return len(leftUnits) < len(rightUnits)
	}
	lessLocation := func(left, right facts.Location) bool {
		if left.Path != right.Path {
			return lessString(left.Path, right.Path)
		}
		if left.Line != right.Line {
			return left.Line < right.Line
		}
		return left.Column < right.Column
	}
	sort.SliceStable(merged.Functions, func(left, right int) bool {
		return lessLocation(merged.Functions[left].Location, merged.Functions[right].Location)
	})
	sort.SliceStable(merged.Types, func(left, right int) bool {
		return lessLocation(merged.Types[left].Location, merged.Types[right].Location)
	})
	sort.SliceStable(merged.PublicOperations, func(left, right int) bool {
		return lessLocation(merged.PublicOperations[left].Location, merged.PublicOperations[right].Location)
	})
	sort.SliceStable(merged.Files, func(left, right int) bool {
		return lessString(merged.Files[left], merged.Files[right])
	})
	return merged
}

func writeRequest(writer io.Writer, workspace string, paths []string, includeTests bool) error {
	data := bufio.NewWriter(writer)
	if err := binary.Write(data, binary.BigEndian, requestMagic); err != nil {
		return err
	}
	if err := binary.Write(data, binary.BigEndian, uint32(facts.SchemaVersion)); err != nil {
		return err
	}
	if includeTests {
		if err := data.WriteByte(1); err != nil {
			return err
		}
	} else if err := data.WriteByte(0); err != nil {
		return err
	}
	if err := writeString(data, workspace); err != nil {
		return err
	}
	if err := writeCount(data, len(paths)); err != nil {
		return err
	}
	for _, path := range paths {
		if err := writeString(data, path); err != nil {
			return err
		}
	}
	return data.Flush()
}

func readResponse(reader io.Reader) (*facts.Program, error) {
	data := bufio.NewReader(reader)
	magic, err := readUint32(data)
	if err != nil || magic != responseMagic {
		return nil, fmt.Errorf("invalid Java fact response")
	}
	version, err := readUint32(data)
	if err != nil || version != facts.SchemaVersion {
		return nil, fmt.Errorf("Java fact schema %d is unsupported", version)
	}
	success, err := data.ReadByte()
	if err != nil {
		return nil, err
	}
	if success == 0 {
		message, messageErr := readString(data)
		if messageErr != nil {
			return nil, messageErr
		}
		return nil, fmt.Errorf("Java syntax analysis failed: %s", message)
	}
	if success != 1 {
		return nil, fmt.Errorf("invalid Java fact response status")
	}
	program, err := readProgram(data)
	if err != nil {
		return nil, err
	}
	program.Unavailable = make(map[string]map[string]string)
	return program, nil
}

func readProgram(data *bufio.Reader) (*facts.Program, error) {
	program := &facts.Program{}
	var err error
	if program.Functions, err = readFunctions(data); err != nil {
		return nil, err
	}
	if program.Types, err = readTypes(data); err != nil {
		return nil, err
	}
	if program.PublicOperations, err = readOperations(data); err != nil {
		return nil, err
	}
	if program.Representation, err = readExposures(data); err != nil {
		return nil, err
	}
	if program.Files, err = readStrings(data); err != nil {
		return nil, err
	}
	return program, nil
}

func readFunctions(data *bufio.Reader) ([]*facts.Function, error) {
	count, err := readCount(data)
	if err != nil {
		return nil, err
	}
	result := make([]*facts.Function, count)
	for index := range result {
		if result[index], err = readFunction(data); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func readFunction(data *bufio.Reader) (*facts.Function, error) {
	result := &facts.Function{}
	var err error
	if result.Name, err = readString(data); err != nil {
		return nil, err
	}
	if result.Receiver, err = readString(data); err != nil {
		return nil, err
	}
	if result.ReceiverVar, err = readString(data); err != nil {
		return nil, err
	}
	if result.Location, err = readLocation(data); err != nil {
		return nil, err
	}
	if result.Body, err = readStatements(data); err != nil {
		return nil, err
	}
	return result, nil
}

func writeCount(data io.Writer, count int) error {
	if count < 0 || uint64(count) > math.MaxInt32 {
		return fmt.Errorf("invalid Java fact collection size")
	}
	return binary.Write(data, binary.BigEndian, uint32(count))
}

func readCount(data io.Reader) (int, error) {
	value, err := readUint32(data)
	if err != nil {
		return 0, err
	}
	if value > math.MaxInt32 {
		return 0, fmt.Errorf("invalid Java fact collection size")
	}
	return int(value), nil
}

func writeString(data io.Writer, value string) error {
	encoded := []byte(value)
	if uint64(len(encoded)) > math.MaxInt32 {
		return fmt.Errorf("invalid Java fact string size")
	}
	if err := binary.Write(data, binary.BigEndian, uint32(len(encoded))); err != nil {
		return err
	}
	_, err := data.Write(encoded)
	return err
}

func readString(data io.Reader) (string, error) {
	length, err := readUint32(data)
	if err != nil {
		return "", err
	}
	if length > math.MaxInt32 {
		return "", fmt.Errorf("invalid Java fact string size")
	}
	encoded := make([]byte, int(length))
	if _, err := io.ReadFull(data, encoded); err != nil {
		return "", err
	}
	return string(encoded), nil
}

func readUint32(data io.Reader) (uint32, error) {
	var value uint32
	err := binary.Read(data, binary.BigEndian, &value)
	return value, err
}
