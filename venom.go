package venom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"plugin"
	"sort"
	"strings"

	"github.com/confluentinc/bincover"
	"github.com/fatih/color"
	"github.com/ovh/cds/sdk/interpolate"
	"github.com/pkg/errors"
	"github.com/rockbears/yaml"
	"github.com/spf13/cast"
)

var (
	// Version is set with -ldflags "-X github.com/ovh/venom/venom.Version=$(VERSION)"
	Version = "snapshot"
	IsTest  = ""
)

// OSExit is a wrapper for os.Exit
func OSExit(exitCode int) {
	if IsTest != "" {
		bincover.ExitCode = exitCode
	} else {
		os.Exit(exitCode)
	}
}

// ContextKey can be added in context to store contextual infos. Also used by logger.
type ContextKey string

// New instantiates a new venom on venom run cmd
func New() *Venom {
	v := &Venom{
		LogOutput:         os.Stdout,
		PrintFunc:         fmt.Printf,
		executorsBuiltin:  map[string]Executor{},
		executorsPlugin:   map[string]Executor{},
		executorsUser:     map[string]Executor{},
		executorFileCache: map[string][]byte{},
		variables:         map[string]interface{}{},
		secrets:           map[string]interface{}{},
		OutputFormat:      "xml",
	}
	return v
}

type Venom struct {
	LogOutput io.Writer

	PrintFunc         func(format string, a ...interface{}) (n int, err error)
	executorsBuiltin  map[string]Executor
	executorsPlugin   map[string]Executor
	executorsUser     map[string]Executor
	executorFileCache map[string][]byte

	Tests     Tests
	variables H
	secrets   H

	LibDir        string
	OutputFormat  string
	OutputDir     string
	StopOnFailure bool
	HtmlReport    bool
	Verbose       int
	OpenApiReport bool
}

var trace = color.New(color.Attribute(90)).SprintFunc()

func (v *Venom) Print(format string, a ...interface{}) {
	v.PrintFunc(format, a...) // nolint
}

func (v *Venom) Println(format string, a ...interface{}) {
	v.PrintFunc(format+"\n", a...) // nolint
}

func (v *Venom) PrintlnTrace(s string) {
	v.PrintlnIndentedTrace(s, "")
}

func (v *Venom) PrintlnIndentedTrace(s string, indent string) {
	v.Println("\t  %s%s %s", indent, trace("[trac]"), trace(s)) // nolint
}

func (v *Venom) AddVariables(variables map[string]interface{}) {
	for k, variable := range variables {
		v.variables[k] = variable
	}
}

func (v *Venom) AddSecrets(secrets map[string]interface{}) {
	for k, s := range secrets {
		v.secrets[k] = s
	}
}

// RegisterExecutorBuiltin register builtin executors
func (v *Venom) RegisterExecutorBuiltin(name string, e Executor) {
	v.executorsBuiltin[name] = e
}

// RegisterExecutorPlugin register plugin executors
func (v *Venom) RegisterExecutorPlugin(name string, e Executor) {
	v.executorsPlugin[name] = e
}

// RegisterExecutorUser register User sxecutors
func (v *Venom) RegisterExecutorUser(name string, e Executor) {
	v.executorsUser[name] = e
}

// GetExecutorRunner initializes a test by name
// no type -> exec is default
func (v *Venom) GetExecutorRunner(ctx context.Context, ts TestStep, h H) (context.Context, ExecutorRunner, error) {
	name, _ := ts.StringValue("type")
	script, _ := ts.StringValue("script")
	command, _ := ts.StringSliceValue("command")
	if name == "" && (script != "" || len(command) != 0) {
		name = "exec"
	}
	retry, err := ts.IntValue("retry")
	if err != nil {
		return nil, nil, err
	}
	retryIf, err := ts.StringSliceValue("retry_if")
	if err != nil {
		return nil, nil, err
	}
	delay, err := ts.IntValue("delay")
	if err != nil {
		return nil, nil, err
	}
	timeout, err := ts.IntValue("timeout")
	if err != nil {
		return nil, nil, err
	}

	info, _ := ts.StringSliceValue("info")
	vars, err := DumpStringPreserveCase(h)
	if err != nil {
		return ctx, nil, err
	}

	allKeys := []string{}
	for k, v := range vars {
		ctx = context.WithValue(ctx, ContextKey("var."+k), v)
		allKeys = append(allKeys, k)
	}
	ctx = context.WithValue(ctx, ContextKey("vars"), allKeys)

	if name == "" {
		return ctx, newExecutorRunner(nil, name, "builtin", retry, retryIf, delay, timeout, info), nil
	}

	if ex, ok := v.executorsBuiltin[name]; ok {
		return ctx, newExecutorRunner(ex, name, "builtin", retry, retryIf, delay, timeout, info), nil
	}

	if err := v.registerUserExecutors(ctx, name, vars); err != nil {
		Debug(ctx, "executor %q is not implemented as user executor - err:%v", name, err)
	}

	if ex, ok := v.executorsUser[name]; ok {
		return ctx, newExecutorRunner(ex, name, "user", retry, retryIf, delay, timeout, info), nil
	}

	if err := v.registerPlugin(ctx, name, vars); err != nil {
		Debug(ctx, "executor %q is not implemented as plugin - err:%v", name, err)
	}

	// then add the executor plugin to the map to not have to load it on each step
	if ex, ok := v.executorsUser[name]; ok {
		return ctx, newExecutorRunner(ex, name, "plugin", retry, retryIf, delay, timeout, info), nil
	}
	return ctx, nil, fmt.Errorf("executor %q is not implemented", name)
}

func (v *Venom) getUserExecutorFilesPath(vars map[string]string) (filePaths []string, err error) {
	var libpaths []string
	if v.LibDir != "" {
		p := strings.Split(v.LibDir, string(os.PathListSeparator))
		libpaths = append(libpaths, p...)
	}
	libpaths = append(libpaths, path.Join(vars["venom.testsuite.workdir"], "lib"))

	for _, p := range libpaths {
		p = strings.TrimSpace(p)

		err = filepath.Walk(p, func(fp string, f os.FileInfo, err error) error {
			switch ext := filepath.Ext(fp); ext {
			case ".yml", ".yaml":
				filePaths = append(filePaths, fp)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Strings(filePaths)
	if len(filePaths) == 0 {
		return nil, fmt.Errorf("no user executor yml file selected")
	}
	return filePaths, nil
}

func (v *Venom) registerUserExecutors(ctx context.Context, name string, vars map[string]string) error {
	executorsPath, err := v.getUserExecutorFilesPath(vars)
	if err != nil {
		return err
	}

	for _, f := range executorsPath {
		Info(ctx, "Reading %v", f)
		btes, ok := v.executorFileCache[f]
		if !ok {
			btes, err = os.ReadFile(f)
			if err != nil {
				return errors.Wrapf(err, "unable to read file %q", f)
			}
			v.executorFileCache[f] = btes
		}

		varsFromInput, err := getUserExecutorInputYML(ctx, btes)
		if err != nil {
			return err
		}

		// varsFromInput contains the default vars from the executor
		var varsFromInputMap map[string]string
		if len(varsFromInput) > 0 {
			varsFromInputMap, err = DumpStringPreserveCase(varsFromInput)
			if err != nil {
				return errors.Wrapf(err, "unable to parse variables")
			}
		}

		varsComputed := map[string]string{}
		for k, v := range vars {
			varsComputed[k] = v
		}
		for k, v := range varsFromInputMap {
			// we only take vars from varsFromInputMap if it's not already exist in vars from teststep vars
			if _, ok := vars[k]; !ok {
				varsComputed[k] = v
			}
		}

		content, err := interpolate.Do(string(btes), varsComputed)
		if err != nil {
			return err
		}

		ux := UserExecutor{Filename: f}
		if err := yaml.Unmarshal([]byte(content), &ux); err != nil {
			return errors.Wrapf(err, "unable to parse file %q with content %v", f, content)
		}

		for k, vr := range varsComputed {
			ux.Input.Add(k, vr)
		}

		v.RegisterExecutorUser(ux.Executor, ux)
	}
	return nil
}

func (v *Venom) registerPlugin(ctx context.Context, name string, vars map[string]string) error {
	workdir := vars["venom.testsuite.workdir"]
	// try to load from testsuite path
	p, err := plugin.Open(path.Join(workdir, "lib", name+".so"))
	if err != nil {
		// try to load from venom binary path
		p, err = plugin.Open(path.Join("lib", name+".so"))
		if err != nil {
			return fmt.Errorf("unable to load plugin %q.so", name)
		}
	}

	symbolExecutor, err := p.Lookup("Plugin")
	if err != nil {
		return err
	}

	executor := symbolExecutor.(Executor)
	v.RegisterExecutorPlugin(name, executor)

	return nil
}

func VarFromCtx(ctx context.Context, varname string) interface{} {
	i := ctx.Value(ContextKey("var." + varname))
	return i
}

func StringVarFromCtx(ctx context.Context, varname string) string {
	i := ctx.Value(ContextKey("var." + varname))
	return cast.ToString(i)
}

func StringSliceVarFromCtx(ctx context.Context, varname string) []string {
	i := ctx.Value(ContextKey("var." + varname))
	return cast.ToStringSlice(i)
}

func IntVarFromCtx(ctx context.Context, varname string) int {
	i := ctx.Value(ContextKey("var." + varname))
	return cast.ToInt(i)
}

func BoolVarFromCtx(ctx context.Context, varname string) bool {
	i := ctx.Value(ContextKey("var." + varname))
	return cast.ToBool(i)
}

func StringMapInterfaceVarFromCtx(ctx context.Context, varname string) map[string]interface{} {
	i := ctx.Value(ContextKey("var." + varname))
	return cast.ToStringMap(i)
}

func StringMapStringVarFromCtx(ctx context.Context, varname string) map[string]string {
	i := ctx.Value(ContextKey("var." + varname))
	return cast.ToStringMapString(i)
}

func AllVarsFromCtx(ctx context.Context) H {
	i := ctx.Value(ContextKey("vars"))
	allKeys := cast.ToStringSlice(i)
	res := H{}
	for _, k := range allKeys {
		res.Add(k, VarFromCtx(ctx, k))
	}
	return res
}

func JSONUnmarshal(btes []byte, i interface{}) error {
	d := json.NewDecoder(bytes.NewReader(btes))
	d.UseNumber()
	return d.Decode(i)
}

func (v *Venom) GenerateOpenApiReport() error {
	dir := v.OutputDir
	var files []FileEntry

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			ext := filepath.Ext(d.Name())
			if ext == ".json" || ext == ".xml" {
				files = append(files, FileEntry{Path: path, Entry: d})
			}
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Error walking the path %q: %v\n", dir, err)
		return nil
	}

	openAPIEndpoints := make(map[string]int)

	for _, file := range files {
		// Load OpenAPI specification if it's a JSON file
		if strings.HasSuffix(file.Entry.Name(), ".json") && !strings.Contains(file.Entry.Name(), "dump") {
			openAPI, err := LoadOpenAPISpec(filepath.Join(v.OutputDir, file.Entry.Name()))
			if err != nil {
				fmt.Println("Error:", err)
				continue
			}

			// Get all endpoints with HTTP methods
			endpoints := GetAllEndpoints(openAPI)

			// Store endpoints in the map
			for p, methods := range endpoints {
				for _, method := range methods {
					s := []string{method, p}
					endpointToStore := strings.Join(s, " ")
					openAPIEndpoints[endpointToStore] = 0
				}
			}
		}
	}

	if len(openAPIEndpoints) == 0 {
		return errors.Errorf("%s", "OpenAPI Spec file not found")
	}
	for _, file := range files {
		if strings.HasSuffix(file.Entry.Name(), ".xml") {
			testsuites, err := LoadJUnitXML(filepath.Join(v.OutputDir, file.Entry.Name()))
			if err != nil {
				fmt.Println("Error:", err)
				continue
			}

			for _, testsuite := range testsuites.TestSuites {
				httpMethod, endpoint := ExtractHttpEndpoint(testsuite.Name)
				if httpMethod != "" {
					s := []string{httpMethod, endpoint}
					endpointToCheck := strings.Join(s, " ")
					if count, ok := openAPIEndpoints[endpointToCheck]; ok {
						openAPIEndpoints[endpointToCheck] = count + 1
					}
				}
			}
		}
	}

	var filename = filepath.Join(v.OutputDir, computeOutputFilename("open_api_report.txt"))
	var data []byte

	for endpoint, count := range openAPIEndpoints {
		line := fmt.Sprintf("%s: %d\n", endpoint, count)
		data = append(data, []byte(line)...)
	}

	v.PrintFunc("Writing open api report file %s\n", filename)
	if err := os.WriteFile(filename, data, 0600); err != nil {
		return errors.Wrapf(err, "Error while creating file %s", filename)
	}
	return nil
}
