// Package javaadapter bridges the JDK compiler-tree parser into normalized structural facts.
package javaadapter

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"slopslap.dev/structural/internal/facts"
)

const (
	helperName    = "slopslap-structural-java.jar"
	requestMagic  = uint32(0x53534a46)
	responseMagic = uint32(0x53534a4f)
	maxItems      = 1_000_000
	maxString     = 16 * 1024 * 1024
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
	var input bytes.Buffer
	if err := writeRequest(&input, workspace, paths, includeTests); err != nil {
		return nil, fmt.Errorf("encode Java fact request: %w", err)
	}
	command := exec.Command(java, "-jar", jar)
	command.Stdin = &input
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("Java fact adapter failed: %w: %s", err, stderr.String())
	}
	program, err := readResponse(bytes.NewReader(stdout.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("decode Java facts: %w", err)
	}
	return program, nil
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
	if count < 0 || count > maxItems {
		return fmt.Errorf("invalid Java fact collection size")
	}
	return binary.Write(data, binary.BigEndian, uint32(count))
}

func readCount(data io.Reader) (int, error) {
	value, err := readUint32(data)
	if err != nil {
		return 0, err
	}
	if value > maxItems {
		return 0, fmt.Errorf("invalid Java fact collection size")
	}
	return int(value), nil
}

func writeString(data io.Writer, value string) error {
	encoded := []byte(value)
	if len(encoded) > maxString {
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
	if length > maxString {
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
